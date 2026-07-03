package store

import (
	"context"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrNoDocuments is re-exported so callers can distinguish "not found" from a
// driver/transport error without importing the mongo package directly.
var ErrNoDocuments = mongo.ErrNoDocuments

// GetInvoice loads an invoice by its _id. A missing document yields
// mongo.ErrNoDocuments; any other error is the raw driver error. Mirrors the
// Rust query_object::<Invoice>(collection, id) find_one({_id: id}).
func (s *Store) GetInvoice(ctx context.Context, id uuid.UUID) (Invoice, error) {
	var inv Invoice
	err := s.invoices.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&inv)
	if err != nil {
		return Invoice{}, err
	}
	return inv, nil
}

// GetInvoiceByOrderID loads the first invoice (natural order) whose order_id
// matches. Missing → mongo.ErrNoDocuments. Mirrors the Rust
// query_invoice_by_order_id find_one({order_id: id}). Note: if several invoices
// share an order_id (possible — inserts are non-idempotent), the first in
// natural order is returned.
func (s *Store) GetInvoiceByOrderID(ctx context.Context, orderID uuid.UUID) (Invoice, error) {
	var inv Invoice
	err := s.invoices.FindOne(ctx, bson.D{{Key: "order_id", Value: orderID}}).Decode(&inv)
	if err != nil {
		return Invoice{}, err
	}
	return inv, nil
}

// InsertInvoice inserts a freshly built invoice document. No idempotency: a
// redelivered event produces a second document with a new _id (matching the
// original insert_one).
func (s *Store) InsertInvoice(ctx context.Context, inv Invoice) error {
	_, err := s.invoices.InsertOne(ctx, inv)
	return err
}

// GetUser loads a user by _id. Missing → mongo.ErrNoDocuments. Deserialization
// REQUIRES first_name/last_name to be present (no BSON defaults on the User
// struct), reproducing the Rust foreign_types::User decode behavior. Mirrors
// query_object::<User>(collection, id).
func (s *Store) GetUser(ctx context.Context, id uuid.UUID) (User, error) {
	var u User
	err := s.user.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&u)
	if err != nil {
		return User{}, err
	}
	return u, nil
}

// GetUserByAddressID reproduces the Rust query_user_address_user exactly,
// including two of the original's bugs:
//
//   - the filter matches on the `addresses` array via $elemMatch, but the
//     address-created handler pushes to `user_addresses` (a different field),
//     so this never matches in practice → mongo.ErrNoDocuments;
//   - the projection {"addresses.$": 1, "_id": 1} omits first_name/last_name,
//     which the User decoder requires — so even a matching document fails to
//     deserialize with a driver error.
//
// The returned User contains only the single projected address element in its
// Addresses slice.
func (s *Store) GetUserByAddressID(ctx context.Context, addressID uuid.UUID) (User, error) {
	projection := bson.D{
		{Key: "addresses.$", Value: 1},
		{Key: "_id", Value: 1},
	}
	filter := bson.D{{Key: "addresses", Value: bson.D{
		{Key: "$elemMatch", Value: bson.D{{Key: "_id", Value: addressID}}},
	}}}
	var u User
	err := s.user.FindOne(ctx, filter, options.FindOne().SetProjection(projection)).Decode(&u)
	if err != nil {
		return User{}, err
	}
	return u, nil
}

// GetVendorAddress loads the first vendor address (natural order, no filter).
// Missing → mongo.ErrNoDocuments. Because the vendor-address handler upserts
// only `_id` (bug), decoding the full VendorAddress shape from such a skeleton
// document fails with a driver error. Mirrors query_vendor_address
// find_one(None).
func (s *Store) GetVendorAddress(ctx context.Context) (VendorAddress, error) {
	var va VendorAddress
	err := s.vendorAddress.FindOne(ctx, bson.D{}).Decode(&va)
	if err != nil {
		return VendorAddress{}, err
	}
	return va, nil
}

// UpsertVendorAddress reproduces handler 2 (address/vendor-address/created):
// update_one({_id: id}, {$set: {_id: id}}, upsert:true). BUG: only _id is set,
// so street/city/etc. are dropped and the stored document is {_id}. Idempotent.
func (s *Store) UpsertVendorAddress(ctx context.Context, id uuid.UUID) error {
	_, err := s.vendorAddress.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: id}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "_id", Value: id}}}},
		options.Update().SetUpsert(true),
	)
	return err
}

// InsertUser reproduces handler 3 (user/user/created): insert_one of a user
// document with an empty `addresses` array. Not idempotent — a second event for
// the same id yields a duplicate-key error. (Unreachable in production because
// of the subscribe/route mismatch, but the handler exists and behaves this way
// when invoked directly.)
func (s *Store) InsertUser(ctx context.Context, u User) error {
	_, err := s.user.InsertOne(ctx, u)
	return err
}

// PushUserAddress reproduces handler 4 (address/user-address/created):
// update_one({_id: userID}, {$push: {user_addresses: <addr>}}). BUG: pushes to
// `user_addresses`, while every read path uses `addresses`. No upsert: if the
// user doc doesn't exist the update matches nothing and still succeeds. Not
// idempotent.
func (s *Store) PushUserAddress(ctx context.Context, userID uuid.UUID, addr UserAddress) error {
	_, err := s.user.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: userID}},
		bson.D{{Key: "$push", Value: bson.D{{Key: "user_addresses", Value: addr}}}},
	)
	return err
}

// PullUserAddress reproduces handler 5 (address/user-address/archived):
// update_one({_id: userID}, {$pull: {"user_addresses._id": id}}). BUG: invalid
// $pull spec on the non-array path `user_addresses._id`. If the user doc
// doesn't exist → matches nothing → success. If a `user_addresses` array does
// exist, MongoDB rejects the $pull → driver error → 500.
func (s *Store) PullUserAddress(ctx context.Context, userID, addressID uuid.UUID) error {
	_, err := s.user.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: userID}},
		bson.D{{Key: "$pull", Value: bson.D{{Key: "user_addresses._id", Value: addressID}}}},
	)
	return err
}
