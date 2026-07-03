package store

import (
	"context"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// The shadow collections (users, products, product_variants) are materialized
// purely from Dapr create events. The original Rust service used a plain
// insert_one, which errors on a redelivered (duplicate _id) event and thus
// loops forever (poison message). Per the spec's default recommendation, the Go
// port uses an idempotent upsert on _id so redelivery is a harmless no-op. This
// does not change the external GraphQL contract (only which id is present),
// only the retry behavior on duplicate delivery. Flagged as a deviation.

// UpsertUser inserts (or leaves) a user shadow document keyed by id.
func (s *Store) UpsertUser(ctx context.Context, id uuid.UUID) error {
	_, err := s.users.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$setOnInsert": bson.M{"_id": id}},
		options.Update().SetUpsert(true),
	)
	return err
}

// UpsertProduct inserts (or leaves) a product shadow document keyed by id.
func (s *Store) UpsertProduct(ctx context.Context, id uuid.UUID) error {
	_, err := s.products.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$setOnInsert": bson.M{"_id": id}},
		options.Update().SetUpsert(true),
	)
	return err
}

// UpsertProductVariant inserts (or updates) a product variant shadow document
// keyed by id, storing the associated product_id.
func (s *Store) UpsertProductVariant(ctx context.Context, id, productID uuid.UUID) error {
	_, err := s.productVariants.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"_id": id, "product_id": productID}},
		options.Update().SetUpsert(true),
	)
	return err
}
