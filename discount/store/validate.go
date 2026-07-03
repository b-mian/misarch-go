package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RunInTx runs fn inside a single transaction, committing if fn returns nil and
// rolling back otherwise. The order-validation handler uses this so the discount
// usage upserts AND the success-event publish share one unit: a publish failure
// (returned as an error from fn) rolls back the increments, matching the
// original @Transactional handler where the pre-commit publishEvent's failure
// aborts the transaction.
func (s *Store) RunInTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UpsertDiscountUsage adds delta to a user's usage of a discount, inserting the
// row if absent (INSERT ... ON CONFLICT (discountid, userid) DO UPDATE SET
// usages = usages + delta). The check_discount_usages trigger fires and raises
// "The user has applied the discount too often" if the new total exceeds the
// discount's cap; that message is surfaced verbatim.
func UpsertDiscountUsage(ctx context.Context, tx pgx.Tx, discountID, userID uuid.UUID, delta int64) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO discountusageentity (discountid, userid, usages)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (discountid, userid)
		 DO UPDATE SET usages = discountusageentity.usages + $3`,
		discountID, userID, delta,
	)
	if err != nil {
		if msg := triggerMessage(err); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return fmt.Errorf("upsert discount usage: %w", err)
	}
	return nil
}
