package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// RegisterTaxRate inserts a tax rate mirror row from a tax/tax-rate/created
// event. It is a plain INSERT (NOT idempotent): a redelivered id violates the
// PK, surfacing an error so the subscription route returns 500 and Dapr
// redelivers — exactly the original behavior.
func (s *Store) RegisterTaxRate(ctx context.Context, id uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, "INSERT INTO taxrateentity (id) VALUES ($1)", id); err != nil {
		return fmt.Errorf("register tax rate %s: %w", id, err)
	}
	return nil
}

// RegisterMedia inserts a media mirror row from a media/media/created event.
// Same plain, non-idempotent INSERT semantics as RegisterTaxRate.
func (s *Store) RegisterMedia(ctx context.Context, id uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, "INSERT INTO mediaentity (id) VALUES ($1)", id); err != nil {
		return fmt.Errorf("register media %s: %w", id, err)
	}
	return nil
}
