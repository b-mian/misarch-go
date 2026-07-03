package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsontype"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// notFoundError renders the exact "<Type> with UUID: `<id>` not found." message
// the Rust `query_object` helper produced (human-readable type label form, per
// the spec's §10 recommendation). label is the entity name ("Wishlist" / "User").
func notFoundError(label string, id uuid.UUID) error {
	return fmt.Errorf("%s with UUID: `%s` not found.", label, id)
}

// GetWishlist fetches a wishlist by id, mirroring `query_object`: a missing
// document (or any read error) surfaces the "not found" message.
func (s *Store) GetWishlist(ctx context.Context, id uuid.UUID) (Wishlist, error) {
	var doc wishlistDoc
	err := s.wishlists.FindOne(ctx, bson.D{{Key: "_id", Value: binUUID(id)}}).Decode(&doc)
	if err != nil {
		return Wishlist{}, notFoundError("Wishlist", id)
	}
	return toWishlist(doc), nil
}

// UserExists reports whether the user shadow copy exists locally. A missing
// document (or read error) yields the "not found" message for User (matching
// validate_user -> query_object).
func (s *Store) ValidateUser(ctx context.Context, id uuid.UUID) error {
	var doc userDoc
	err := s.users.FindOne(ctx, bson.D{{Key: "_id", Value: binUUID(id)}}).Decode(&doc)
	if err != nil {
		return notFoundError("User", id)
	}
	return nil
}

// ValidateProductVariantIDs checks that every id is present in the
// product_variants shadow collection. It runs a single `$in` find, then checks
// each requested id against the returned set. The first missing id errors with
// the per-id message; a query error yields the collective message. An empty id
// slice passes trivially (no ids to check).
func (s *Store) ValidateProductVariantIDs(ctx context.Context, ids []uuid.UUID) error {
	in := make(bson.A, len(ids))
	for i, id := range ids {
		in[i] = binUUID(id)
	}
	cursor, err := s.variants.Find(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: in}}}})
	if err != nil {
		return errors.New("Product variants with the specified UUIDs are not present in the system.")
	}
	var found []productVariantDoc
	if err := cursor.All(ctx, &found); err != nil {
		return errors.New("Product variants with the specified UUIDs are not present in the system.")
	}
	present := make(map[uuid.UUID]struct{}, len(found))
	for _, pv := range found {
		present[pv.ID.UUID()] = struct{}{}
	}
	for _, id := range ids {
		if _, ok := present[id]; !ok {
			return fmt.Errorf("Product variant with the UUID: `%s` is not present in the system.", id)
		}
	}
	return nil
}

// CreateWishlist inserts a new wishlist with a freshly generated v4 UUID,
// setting created_at and last_updated_at to the same timestamp, then re-fetches
// and returns the persisted document (matching the original's insert->re-read).
// productVariantIDs must already be deduped by the caller.
func (s *Store) CreateWishlist(ctx context.Context, userID uuid.UUID, productVariantIDs []uuid.UUID, name string, now time.Time) (Wishlist, error) {
	ts := primitive.NewDateTimeFromTime(now)
	variants := make([]productVariantDoc, len(productVariantIDs))
	for i, id := range productVariantIDs {
		variants[i] = productVariantDoc{ID: binUUID(id)}
	}
	doc := wishlistDoc{
		ID:                      binUUID(uuid.New()),
		User:                    userDoc{ID: binUUID(userID)},
		Name:                    name,
		CreatedAt:               ts,
		LastUpdatedAt:           ts,
		InternalProductVariants: variants,
	}
	res, err := s.wishlists.InsertOne(ctx, doc)
	if err != nil {
		return Wishlist{}, errors.New("Adding wishlist failed in MongoDB.")
	}
	id, err := insertedUUID(res.InsertedID)
	if err != nil {
		return Wishlist{}, err
	}
	return s.GetWishlist(ctx, id)
}

// insertedUUID extracts the UUID from an InsertOne result's inserted id, which
// the driver round-trips as our binUUID. It mirrors the Rust `uuid_from_bson`
// error when the id is not a Binary.
func insertedUUID(v any) (uuid.UUID, error) {
	switch id := v.(type) {
	case binUUID:
		return id.UUID(), nil
	case primitive.Binary:
		if id.Subtype == bsontype.BinaryUUID || id.Subtype == bsontype.BinaryUUIDOld {
			if u, err := uuid.FromBytes(id.Data); err == nil {
				return u, nil
			}
		}
	}
	return uuid.Nil, fmt.Errorf("Returned id: `%v` needs to be a Binary in order to be parsed as a Uuid", v)
}

// UpdateProductVariantIDs replaces the whole internal_product_variants array and
// bumps last_updated_at. ids must already be deduped by the caller.
func (s *Store) UpdateProductVariantIDs(ctx context.Context, id uuid.UUID, ids []uuid.UUID, now time.Time) error {
	variants := make([]productVariantDoc, len(ids))
	for i, v := range ids {
		variants[i] = productVariantDoc{ID: binUUID(v)}
	}
	update := bson.D{{Key: "$set", Value: bson.D{
		{Key: "internal_product_variants", Value: variants},
		{Key: "last_updated_at", Value: primitive.NewDateTimeFromTime(now)},
	}}}
	if _, err := s.wishlists.UpdateOne(ctx, bson.D{{Key: "_id", Value: binUUID(id)}}, update); err != nil {
		return fmt.Errorf("Updating product_variant_ids of wishlist of id: `%s` failed in MongoDB.", id)
	}
	return nil
}

// UpdateName sets the wishlist name and bumps last_updated_at.
func (s *Store) UpdateName(ctx context.Context, id uuid.UUID, name string, now time.Time) error {
	update := bson.D{{Key: "$set", Value: bson.D{
		{Key: "name", Value: name},
		{Key: "last_updated_at", Value: primitive.NewDateTimeFromTime(now)},
	}}}
	if _, err := s.wishlists.UpdateOne(ctx, bson.D{{Key: "_id", Value: binUUID(id)}}, update); err != nil {
		return fmt.Errorf("Updating name of wishlist of id: `%s` failed in MongoDB.", id)
	}
	return nil
}

// DeleteWishlist hard-deletes the wishlist document (no tombstone, no event).
func (s *Store) DeleteWishlist(ctx context.Context, id uuid.UUID) error {
	if _, err := s.wishlists.DeleteOne(ctx, bson.D{{Key: "_id", Value: binUUID(id)}}); err != nil {
		return fmt.Errorf("Deleting wishlist of id: `%s` failed in MongoDB.", id)
	}
	return nil
}

// ListWishlistsByUser returns the page of wishlists owned by userID.
//
// It reproduces the mongodb-cursor-pagination semantics used by the original:
// totalCount is the count over the filter ignoring skip/limit; nodes are the
// page after sort+skip+limit; hasNextPage is (skip + len(nodes)) < totalCount.
// sortKey is the Mongo sort key ("_id", "user._id", "name", "created_at",
// "last_updated_at"); asc selects direction (1 vs -1). first/skip are optional.
func (s *Store) ListWishlistsByUser(ctx context.Context, userID uuid.UUID, first *int, skip *int, sortKey string, asc bool) (Connection[Wishlist], error) {
	filter := bson.D{{Key: "user._id", Value: binUUID(userID)}}

	total, err := s.wishlists.CountDocuments(ctx, filter)
	if err != nil {
		return Connection[Wishlist]{}, errors.New("Retrieving wishlists failed in MongoDB.")
	}

	dir := 1
	if !asc {
		dir = -1
	}
	opts := options.Find().SetSort(bson.D{{Key: sortKey, Value: dir}})
	if skip != nil {
		opts.SetSkip(int64(*skip))
	}
	if first != nil {
		opts.SetLimit(int64(*first))
	}

	cursor, err := s.wishlists.Find(ctx, filter, opts)
	if err != nil {
		return Connection[Wishlist]{}, errors.New("Retrieving wishlists failed in MongoDB.")
	}
	var docs []wishlistDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return Connection[Wishlist]{}, errors.New("Retrieving wishlists failed in MongoDB.")
	}
	nodes := make([]Wishlist, len(docs))
	for i, d := range docs {
		nodes[i] = toWishlist(d)
	}

	skipN := 0
	if skip != nil {
		skipN = *skip
	}
	hasNextPage := skipN+len(nodes) < int(total)

	return Connection[Wishlist]{Nodes: nodes, TotalCount: int(total), HasNextPage: hasNextPage}, nil
}
