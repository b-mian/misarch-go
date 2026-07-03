package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// orderItemColumns is the projection for a full OrderItem row.
const orderItemColumns = "id, returnedwithid, sentwithid, orderid, compensatableamount, productvariantversionid"

// scanOrderItem scans one order item row (nullable columns via pointers).
func scanOrderItem(row pgx.Row) (OrderItem, error) {
	var oi OrderItem
	err := row.Scan(&oi.ID, &oi.ReturnedWithID, &oi.SentWithID, &oi.OrderID, &oi.CompensatableAmount, &oi.ProductVariantVersionID)
	return oi, err
}

// GetOrderItem loads an order item by id. The second return value is false when
// no local row exists (the federated OrderItem resolver returns a stub in that
// case rather than erroring).
func (s *Store) GetOrderItem(ctx context.Context, id uuid.UUID) (OrderItem, bool, error) {
	oi, err := scanOrderItem(s.pool.QueryRow(ctx,
		"SELECT "+orderItemColumns+" FROM orderitementity WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return OrderItem{}, false, nil
	}
	if err != nil {
		return OrderItem{}, false, fmt.Errorf("get order item %s: %w", id, err)
	}
	return oi, true, nil
}

// findOrderItemsByIDs loads order items for the given ids (IN-list). Ids with no
// row are simply absent from the result, matching findAllById.
func findOrderItemsByIDs(ctx context.Context, q querier, ids []uuid.UUID) ([]OrderItem, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx,
		"SELECT "+orderItemColumns+" FROM orderitementity WHERE id = ANY($1)", ids)
	if err != nil {
		return nil, fmt.Errorf("find order items: %w", err)
	}
	defer rows.Close()
	var out []OrderItem
	for rows.Next() {
		oi, err := scanOrderItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan order item: %w", err)
		}
		out = append(out, oi)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// UpsertOrderItemFromShipment materializes the sentWithId of an order item from
// a shipment-created event, mirroring OrderItemRepository.upsertOrderItemFromShipment.
// The orderId FK requires the OrderEntity row to already exist (else FK
// violation → the caller returns an error → HTTP 500 → Dapr retry).
func (s *Store) UpsertOrderItemFromShipment(ctx context.Context, id, sentWithID, orderID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO orderitementity (id, sentwithid, orderid)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET sentwithid = $2`,
		id, sentWithID, orderID)
	if err != nil {
		return fmt.Errorf("upsert order item from shipment %s: %w", id, err)
	}
	return nil
}

// UpsertOrderItemFromOrder materializes compensatableAmount and
// productVariantVersionId of an order item from an order-created event,
// mirroring OrderItemRepository.upsertOrderItemFromOrder.
func (s *Store) UpsertOrderItemFromOrder(ctx context.Context, id uuid.UUID, compensatableAmount int64, orderID, productVariantVersionID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO orderitementity (id, compensatableamount, orderid, productvariantversionid)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET compensatableamount = $2, productvariantversionid = $4`,
		id, compensatableAmount, orderID, productVariantVersionID)
	if err != nil {
		return fmt.Errorf("upsert order item from order %s: %w", id, err)
	}
	return nil
}

// ListReturnedItems returns a page of order items linked to a given return
// (predicate returnedwithid = returnID). Mirrors OrderItemConnection with no
// join and no authorizedUserFilter (any caller who can see the Return sees its
// items). Default order is ASC by id.
func (s *Store) ListReturnedItems(
	ctx context.Context, returnID uuid.UUID, first, skip *int, ascending bool,
) (Connection[OrderItem], error) {
	p := page{
		table:      "orderitementity",
		columns:    orderItemColumns,
		primaryKey: "id",
		conditions: []string{"returnedwithid = $1"},
		condArgs:   []any{returnID},
		orderCols:  []string{"id"},
		ascending:  ascending,
		first:      first,
		skip:       skip,
	}

	total, err := p.totalCount(ctx, s.pool)
	if err != nil {
		return Connection[OrderItem]{}, err
	}

	q, args := p.nodesSQL()
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return Connection[OrderItem]{}, fmt.Errorf("list returned items: %w", err)
	}
	defer rows.Close()
	var nodes []OrderItem
	for rows.Next() {
		oi, err := scanOrderItem(rows)
		if err != nil {
			return Connection[OrderItem]{}, fmt.Errorf("scan order item: %w", err)
		}
		nodes = append(nodes, oi)
	}
	if err := rows.Err(); err != nil {
		return Connection[OrderItem]{}, err
	}

	next, err := p.hasNextPage(ctx, s.pool)
	if err != nil {
		return Connection[OrderItem]{}, err
	}

	return Connection[OrderItem]{Nodes: nodes, TotalCount: total, HasNextPage: next}, nil
}
