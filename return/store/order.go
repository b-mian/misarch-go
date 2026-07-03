package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Order is a row of the orderentity local read model.
type Order struct {
	ID     uuid.UUID
	UserID uuid.UUID
}

// GetOrder loads an order by id, erroring if it does not exist (used by
// Query.return to fetch the order for the ownership check).
func (s *Store) GetOrder(ctx context.Context, id uuid.UUID) (Order, error) {
	return getOrder(ctx, s.pool, id)
}

// getOrder loads an order by id, erroring if it does not exist (the original
// orderRepository.findById(orderId).awaitSingle() throws on an empty result).
func getOrder(ctx context.Context, q querier, id uuid.UUID) (Order, error) {
	var o Order
	err := q.QueryRow(ctx,
		"SELECT id, userid FROM orderentity WHERE id = $1", id,
	).Scan(&o.ID, &o.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, fmt.Errorf("order with id %s not found", id)
	}
	if err != nil {
		return Order{}, fmt.Errorf("get order %s: %w", id, err)
	}
	return o, nil
}

// CreateOrder inserts an order from an order-created event, mirroring
// OrderRepository.createOrder — a raw, non-idempotent INSERT (a redelivered
// event conflicts on the PK → error → HTTP 500 → Dapr retry loop).
func (s *Store) CreateOrder(ctx context.Context, id, userID uuid.UUID) error {
	if _, err := s.pool.Exec(ctx,
		"INSERT INTO orderentity (id, userid) VALUES ($1, $2)", id, userID,
	); err != nil {
		return fmt.Errorf("create order %s: %w", id, err)
	}
	return nil
}
