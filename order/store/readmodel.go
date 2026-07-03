package store

import (
	"context"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InsertCoupon inserts a {_id} coupon document. No upsert: a duplicate delivery
// yields a duplicate-key error → the handler 500s → Dapr retries. Mirrors
// create_in_mongodb::<Coupon>.
func (s *Store) InsertCoupon(ctx context.Context, id uuid.UUID) error {
	_, err := s.coupons.InsertOne(ctx, UUIDRef{ID: id})
	return err
}

// InsertShipmentMethod inserts a {_id} shipment-method document. No upsert (dup
// → 500 → retry). Mirrors create_in_mongodb::<ShipmentMethod>.
func (s *Store) InsertShipmentMethod(ctx context.Context, id uuid.UUID) error {
	_, err := s.shipmentMethods.InsertOne(ctx, UUIDRef{ID: id})
	return err
}

// InsertUser inserts a {_id, user_address_ids: []} user document. No upsert (dup
// → 500 → retry). Mirrors create_in_mongodb::<User> (User::from(id) has an
// empty address list).
func (s *Store) InsertUser(ctx context.Context, id uuid.UUID) error {
	_, err := s.users.InsertOne(ctx, User{ID: id, UserAddressIDs: []uuid.UUID{}})
	return err
}

// GetProductVariant loads a single product variant by _id (used by the
// create-or-update event to decide insert vs update). Missing →
// mongo.ErrNoDocuments.
func (s *Store) GetProductVariant(ctx context.Context, id uuid.UUID) (ProductVariant, error) {
	var pv ProductVariant
	if err := s.productVariants.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&pv); err != nil {
		return ProductVariant{}, err
	}
	return pv, nil
}

// UpdateProductVariantVersion sets current_version on an existing product
// variant (found-first path of create_or_update_product_variant_in_mongodb).
func (s *Store) UpdateProductVariantVersion(ctx context.Context, pvID uuid.UUID, version ProductVariantVersion) error {
	_, err := s.productVariants.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: pvID}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "current_version", Value: version}}}},
	)
	return err
}

// InsertProductVariant inserts a new product variant with is_publicly_visible =
// bool true (create path of create_or_update_product_variant_in_mongodb). New
// variants default to publicly visible.
func (s *Store) InsertProductVariant(ctx context.Context, pvID uuid.UUID, version ProductVariantVersion) error {
	_, err := s.productVariants.InsertOne(ctx, ProductVariant{
		ID:                pvID,
		CurrentVersion:    version,
		IsPubliclyVisible: true,
	})
	return err
}

// SetProductVariantVisibility sets is_publicly_visible to the raw value carried
// by the update event.
//
// FIDELITY TRAP (reproduced): the original deserializes the event's
// is_publicly_visible as a Rust String and stores it AS A STRING (e.g. "true"),
// whereas the create path stores a bool. visible is therefore typed `any` and
// stored verbatim; passing the event's string value here reproduces the
// latent bug where createOrder can no longer read the variant's visibility.
func (s *Store) SetProductVariantVisibility(ctx context.Context, id uuid.UUID, visible any) error {
	_, err := s.productVariants.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: id}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "is_publicly_visible", Value: visible}}}},
	)
	return err
}

// UpsertTaxRate upserts a tax rate keyed by _id, setting _id and current_version
// (upsert = true → idempotent create-or-update). Mirrors
// create_or_update_tax_rate_in_mongodb.
func (s *Store) UpsertTaxRate(ctx context.Context, taxRateID uuid.UUID, version TaxRateVersion) error {
	_, err := s.taxRates.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: taxRateID}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "_id", Value: taxRateID},
			{Key: "current_version", Value: version},
		}}},
		options.Update().SetUpsert(true),
	)
	return err
}

// PushUserAddress appends an address id to a user's user_address_ids via $push
// (NO upsert → no-op if the user doc is missing; NOT idempotent — re-delivery
// appends a dup). Mirrors insert_user_address_in_mongodb.
func (s *Store) PushUserAddress(ctx context.Context, userID, addressID uuid.UUID) error {
	_, err := s.users.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: userID}},
		bson.D{{Key: "$push", Value: bson.D{{Key: "user_address_ids", Value: addressID}}}},
	)
	return err
}

// PullUserAddress removes all occurrences of an address id from a user's
// user_address_ids via $pull (idempotent). Mirrors remove_user_address_in_mongodb.
func (s *Store) PullUserAddress(ctx context.Context, userID, addressID uuid.UUID) error {
	_, err := s.users.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: userID}},
		bson.D{{Key: "$pull", Value: bson.D{{Key: "user_address_ids", Value: addressID}}}},
	)
	return err
}
