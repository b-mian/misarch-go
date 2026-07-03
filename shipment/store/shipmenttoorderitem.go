package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// CreateShipmentToOrderItem inserts one join row recording that an order item
// is part of a shipment (id assigned by the DB default uuid_generate_v4()).
// One row is written per order item on every shipment creation.
func (s *Store) CreateShipmentToOrderItem(ctx context.Context, shipmentID, orderItemID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		"INSERT INTO shipmenttoorderitementity (shipmentid, orderitemid) VALUES ($1, $2)",
		shipmentID, orderItemID,
	)
	if err != nil {
		return fmt.Errorf("create shipment_to_order_item: %w", err)
	}
	return nil
}

// FindOrderItemIDsByShipmentID returns the order-item ids linked to a shipment
// (used to fill orderItemIds in the status-updated event). Mirrors
// shipmentToOrderItemRepository.findByShipmentId(...).map { it.orderItemId }.
func (s *Store) FindOrderItemIDsByShipmentID(ctx context.Context, shipmentID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT orderitemid FROM shipmenttoorderitementity WHERE shipmentid = $1",
		shipmentID,
	)
	if err != nil {
		return nil, fmt.Errorf("find order item ids by shipment %s: %w", shipmentID, err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan order item id: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
