package store

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// VariantExists reports whether a product variant with the given id is present
// in the `productvariantpartials` replica. Used for existence validation on
// writes (createProductItemBatch / updateProductItem).
func (s *Store) VariantExists(ctx context.Context, id string) (bool, error) {
	err := s.variantParts.FindOne(ctx, bson.M{"_id": id}).Err()
	if err == mongo.ErrNoDocuments {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("find product variant partial: %w", err)
	}
	return true, nil
}

// CreateVariantPartial inserts a product variant id into the replica. The
// `__v: 0` field is written for byte-level compatibility with the original
// Mongoose schema (which kept the version key on this collection, spec §5).
//
// Duplicate keys are treated as success (idempotent) — [BUG-COMPAT
// product-variant-created] is FIXED here: the original crashed the process on a
// redelivered event; this port logs nothing and returns nil.
func (s *Store) CreateVariantPartial(ctx context.Context, id string) error {
	_, err := s.variantParts.InsertOne(ctx, bson.M{"_id": id, "__v": 0})
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil
		}
		return fmt.Errorf("create product variant partial: %w", err)
	}
	return nil
}
