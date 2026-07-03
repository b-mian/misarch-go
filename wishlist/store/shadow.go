package store

import (
	"context"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InsertUser records a user shadow copy (id only) from a user/user/created
// event. It uses an idempotent upsert on `_id` so a redelivered event is a
// no-op rather than a duplicate-key error (see the poison-message note in the
// spec §4); the external contract is unchanged.
func (s *Store) InsertUser(ctx context.Context, id uuid.UUID) error {
	filter := bson.D{{Key: "_id", Value: binUUID(id)}}
	update := bson.D{{Key: "$setOnInsert", Value: bson.D{{Key: "_id", Value: binUUID(id)}}}}
	_, err := s.users.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// InsertProductVariant records a product-variant shadow copy (id only) from a
// catalog/product-variant/created event. Idempotent upsert on `_id`, as above.
func (s *Store) InsertProductVariant(ctx context.Context, id uuid.UUID) error {
	filter := bson.D{{Key: "_id", Value: binUUID(id)}}
	update := bson.D{{Key: "$setOnInsert", Value: bson.D{{Key: "_id", Value: binUUID(id)}}}}
	_, err := s.variants.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}
