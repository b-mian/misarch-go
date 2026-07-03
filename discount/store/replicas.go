package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// The Register* methods populate the id-only replicas of foreign entities from
// Dapr */created events. They are NOT idempotent (a plain INSERT), matching the
// original: a redelivered event hits a PK/unique violation, the handler returns
// 500, and Dapr redelivers. No ON CONFLICT.

// RegisterUser inserts a user replica row.
func (s *Store) RegisterUser(ctx context.Context, id uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, "INSERT INTO userentity (id) VALUES ($1)", id); err != nil {
		return fmt.Errorf("register user %s: %w", id, err)
	}
	return nil
}

// RegisterCategory inserts a category replica row.
func (s *Store) RegisterCategory(ctx context.Context, id uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, "INSERT INTO categoryentity (id) VALUES ($1)", id); err != nil {
		return fmt.Errorf("register category %s: %w", id, err)
	}
	return nil
}

// RegisterProductVariant inserts a product-variant replica row (no FK on
// productid; inserted even if the product row is absent locally).
func (s *Store) RegisterProductVariant(ctx context.Context, id, productID uuid.UUID) error {
	if _, err := s.pool.Exec(ctx,
		"INSERT INTO productvariantentity (id, productid) VALUES ($1, $2)", id, productID,
	); err != nil {
		return fmt.Errorf("register product variant %s: %w", id, err)
	}
	return nil
}

// RegisterProduct inserts a product replica row and its product-to-category
// mappings (no FK on categoryid; mappings inserted even if the category is
// absent locally). Both writes share one transaction, matching the handler's DB
// scope.
func (s *Store) RegisterProduct(ctx context.Context, id uuid.UUID, categoryIDs []uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "INSERT INTO productentity (id) VALUES ($1)", id); err != nil {
		return fmt.Errorf("register product %s: %w", id, err)
	}
	for _, cid := range categoryIDs {
		if _, err := tx.Exec(ctx,
			"INSERT INTO producttocategoryentity (productid, categoryid) VALUES ($1, $2)", id, cid,
		); err != nil {
			return fmt.Errorf("register product-category %s/%s: %w", id, cid, err)
		}
	}
	return tx.Commit(ctx)
}
