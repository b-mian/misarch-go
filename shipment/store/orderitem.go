package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// orderItemColumns is the projection for a full OrderItem row.
const orderItemColumns = "id, sentwithid, productvariantversionid, quantity"

// orderItemColumnsQualified is the same projection qualified with the
// orderitementity alias, used by the sentItems join query where an unqualified
// "id" would be ambiguous against the join table.
const orderItemColumnsQualified = "orderitementity.id, orderitementity.sentwithid, orderitementity.productvariantversionid, orderitementity.quantity"

// OrderItemOrderColumn is a validated ORDER BY column set for the sentItems
// (OrderItem) connection. The only order field in the SDL is ID.
type OrderItemOrderColumn []string

// OrderItemOrderByID orders by the order-item primary key (qualified because
// sentItems joins the shipment_to_order_item table).
var OrderItemOrderByID = OrderItemOrderColumn{"orderitementity.id"}

// GetOrderItem loads an order item by id. Returns (row, true, nil) when found;
// (zero, false, nil) when absent (the federation resolver turns the absent
// case into a stub OrderItem with sentWith == nil, and must NOT error).
func (s *Store) GetOrderItem(ctx context.Context, id uuid.UUID) (OrderItem, bool, error) {
	var it OrderItem
	err := s.pool.QueryRow(ctx,
		"SELECT "+orderItemColumns+" FROM orderitementity WHERE id = $1", id,
	).Scan(&it.ID, &it.SentWithID, &it.ProductVariantVersionID, &it.Quantity)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrderItem{}, false, nil
	}
	if err != nil {
		return OrderItem{}, false, fmt.Errorf("get order item %s: %w", id, err)
	}
	return it, true, nil
}

// FindOrderItemsByIDs loads the order items with the given ids (used by the
// return handler; the rows are expected to already exist from a prior order
// shipment). Order is unspecified.
func (s *Store) FindOrderItemsByIDs(ctx context.Context, ids []uuid.UUID) ([]OrderItem, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		"SELECT "+orderItemColumns+" FROM orderitementity WHERE id = ANY($1)", ids,
	)
	if err != nil {
		return nil, fmt.Errorf("find order items by ids: %w", err)
	}
	return scanOrderItems(rows)
}

// CreateOrderItem inserts an order item, doing nothing on a primary-key
// conflict (ON CONFLICT DO NOTHING) so re-used order items keep their original
// sentWithId — matching OrderItemRepository.createOrderItem.
func (s *Store) CreateOrderItem(ctx context.Context, id, sentWithID, productVariantVersionID uuid.UUID, quantity int) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO orderitementity (id, sentwithid, productvariantversionid, quantity)
		 VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
		id, sentWithID, productVariantVersionID, quantity,
	)
	if err != nil {
		return fmt.Errorf("create order item %s: %w", id, err)
	}
	return nil
}

// ListSentItems returns a page of order items linked to a shipment via the
// shipment_to_order_item join table (Shipment.sentItems). It mirrors the
// original: predicate shipmenttoorderitementity.shipmentid = shipmentID, INNER
// JOIN on orderitementity.id = shipmenttoorderitementity.orderitemid, default
// order orderitementity.id.
func (s *Store) ListSentItems(
	ctx context.Context, shipmentID uuid.UUID, first, skip *int, order OrderItemOrderColumn, ascending bool,
) (Connection[OrderItem], error) {
	p := page{
		table:     "orderitementity",
		columns:   orderItemColumnsQualified,
		countCol:  "orderitementity.id",
		join:      "INNER JOIN shipmenttoorderitementity ON shipmenttoorderitementity.orderitemid = orderitementity.id",
		where:     "shipmenttoorderitementity.shipmentid = $1",
		whereArgs: []any{shipmentID},
		orderCols: order,
		ascending: ascending,
		first:     first,
		skip:      skip,
	}

	total, err := p.totalCount(ctx, s.pool)
	if err != nil {
		return Connection[OrderItem]{}, err
	}

	q, args := p.nodesSQL()
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return Connection[OrderItem]{}, fmt.Errorf("list sent items: %w", err)
	}
	nodes, err := scanOrderItems(rows)
	if err != nil {
		return Connection[OrderItem]{}, err
	}

	next, err := p.hasNextPage(ctx, s.pool)
	if err != nil {
		return Connection[OrderItem]{}, err
	}

	return Connection[OrderItem]{Nodes: nodes, TotalCount: total, HasNextPage: next}, nil
}

// scanOrderItems collects order item rows from an open pgx.Rows.
func scanOrderItems(rows pgx.Rows) ([]OrderItem, error) {
	defer rows.Close()
	var out []OrderItem
	for rows.Next() {
		var it OrderItem
		if err := rows.Scan(&it.ID, &it.SentWithID, &it.ProductVariantVersionID, &it.Quantity); err != nil {
			return nil, fmt.Errorf("scan order item: %w", err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
