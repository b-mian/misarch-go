package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// findShipmentsByIDs loads shipments for the given ids (IN-list), mirroring
// shipmentRepository.findAllById. Absent ids simply don't come back.
func findShipmentsByIDs(ctx context.Context, q querier, ids []uuid.UUID) ([]Shipment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx,
		"SELECT id, deliveredat FROM shipmententity WHERE id = ANY($1)", ids)
	if err != nil {
		return nil, fmt.Errorf("find shipments: %w", err)
	}
	defer rows.Close()
	var out []Shipment
	for rows.Next() {
		var sh Shipment
		if err := rows.Scan(&sh.ID, &sh.DeliveredAt); err != nil {
			return nil, fmt.Errorf("scan shipment: %w", err)
		}
		out = append(out, sh)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateShipment inserts a shipment from a shipment-created event, mirroring
// ShipmentRepository.createShipment — a raw, non-idempotent INSERT.
// deliveredAt is non-nil only when the event's status is DELIVERED.
func (s *Store) CreateShipment(ctx context.Context, id uuid.UUID, deliveredAt *time.Time) error {
	if _, err := s.pool.Exec(ctx,
		"INSERT INTO shipmententity (id, deliveredat) VALUES ($1, $2)", id, deliveredAt,
	); err != nil {
		return fmt.Errorf("create shipment %s: %w", id, err)
	}
	return nil
}

// SetShipmentDelivered sets a shipment's deliveredAt, mirroring
// ShipmentService.updateDeliveredAt: it first loads the shipment (erroring if
// absent, matching findById().awaitSingle()) then saves the new timestamp.
func (s *Store) SetShipmentDelivered(ctx context.Context, id uuid.UUID, deliveredAt time.Time) error {
	var existingID uuid.UUID
	err := s.pool.QueryRow(ctx,
		"SELECT id FROM shipmententity WHERE id = $1", id,
	).Scan(&existingID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("shipment with id %s not found", id)
	}
	if err != nil {
		return fmt.Errorf("get shipment %s: %w", id, err)
	}
	if _, err := s.pool.Exec(ctx,
		"UPDATE shipmententity SET deliveredat = $1 WHERE id = $2", deliveredAt, id,
	); err != nil {
		return fmt.Errorf("update shipment %s: %w", id, err)
	}
	return nil
}
