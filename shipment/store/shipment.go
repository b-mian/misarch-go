package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// shipmentColumns is the projection for a full Shipment row.
const shipmentColumns = "id, status, shipmentmethodid, shipmentaddressid, orderid, returnid"

// ShipmentOrderColumn is a validated ORDER BY column set for the shipments
// connection. The only order field in the SDL is ID.
type ShipmentOrderColumn []string

// ShipmentOrderByID orders by the primary key (the sole order field).
var ShipmentOrderByID = ShipmentOrderColumn{"id"}

// GetShipment loads a shipment by id, erroring if it does not exist (the
// original data loader miss surfaced a GraphQL error on the non-null field).
func (s *Store) GetShipment(ctx context.Context, id uuid.UUID) (Shipment, error) {
	var sh Shipment
	err := s.pool.QueryRow(ctx,
		"SELECT "+shipmentColumns+" FROM shipmententity WHERE id = $1", id,
	).Scan(&sh.ID, &sh.Status, &sh.ShipmentMethodID, &sh.ShipmentAddressID, &sh.OrderID, &sh.ReturnID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Shipment{}, fmt.Errorf("Shipment with id %s does not exist.", id)
	}
	if err != nil {
		return Shipment{}, fmt.Errorf("get shipment %s: %w", id, err)
	}
	return sh, nil
}

// FindShipmentByReturnID loads the single shipment for a return. The original
// findByReturnId returns exactly one entity; if none exists the reactive Mono
// is empty and the non-null Return.shipment field errors. Reproduce with an
// error on no rows.
func (s *Store) FindShipmentByReturnID(ctx context.Context, returnID uuid.UUID) (Shipment, error) {
	var sh Shipment
	err := s.pool.QueryRow(ctx,
		"SELECT "+shipmentColumns+" FROM shipmententity WHERE returnid = $1", returnID,
	).Scan(&sh.ID, &sh.Status, &sh.ShipmentMethodID, &sh.ShipmentAddressID, &sh.OrderID, &sh.ReturnID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Shipment{}, fmt.Errorf("Shipment with returnId %s does not exist.", returnID)
	}
	if err != nil {
		return Shipment{}, fmt.Errorf("find shipment by return %s: %w", returnID, err)
	}
	return sh, nil
}

// ListShipmentsForOrder returns a page of shipments for one order, optionally
// filtered by status (nil status → no filter). Predicate: orderid = orderID.
func (s *Store) ListShipmentsForOrder(
	ctx context.Context, orderID uuid.UUID, first, skip *int, order ShipmentOrderColumn, ascending bool, status *string,
) (Connection[Shipment], error) {
	where := "orderid = $1"
	args := []any{orderID}
	if status != nil {
		where += " AND status = $2"
		args = append(args, *status)
	}
	p := page{
		table:     "shipmententity",
		columns:   shipmentColumns,
		countCol:  "id",
		where:     where,
		whereArgs: args,
		orderCols: order,
		ascending: ascending,
		first:     first,
		skip:      skip,
	}

	total, err := p.totalCount(ctx, s.pool)
	if err != nil {
		return Connection[Shipment]{}, err
	}

	q, qargs := p.nodesSQL()
	rows, err := s.pool.Query(ctx, q, qargs...)
	if err != nil {
		return Connection[Shipment]{}, fmt.Errorf("list shipments: %w", err)
	}
	nodes, err := scanShipments(rows)
	if err != nil {
		return Connection[Shipment]{}, err
	}

	next, err := p.hasNextPage(ctx, s.pool)
	if err != nil {
		return Connection[Shipment]{}, err
	}

	return Connection[Shipment]{Nodes: nodes, TotalCount: total, HasNextPage: next}, nil
}

// CreateShipment inserts a shipment (id assigned by the DB default
// uuid_generate_v4()) with status PENDING and returns the new id. The status
// is passed explicitly so the caller controls the enum NAME string.
func (s *Store) CreateShipment(
	ctx context.Context, status string, shipmentMethodID, shipmentAddressID uuid.UUID, orderID, returnID *uuid.UUID,
) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`INSERT INTO shipmententity (status, shipmentmethodid, shipmentaddressid, orderid, returnid)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		status, shipmentMethodID, shipmentAddressID, orderID, returnID,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert shipment: %w", err)
	}
	return id, nil
}

// UpdateShipmentStatus loads the shipment (missing → error), sets the new
// status, saves, and returns the updated row (the caller needs orderId /
// returnId for the status-updated event). No state-machine validation and no
// idempotency guard, matching the original.
func (s *Store) UpdateShipmentStatus(ctx context.Context, id uuid.UUID, status string) (Shipment, error) {
	var sh Shipment
	err := s.pool.QueryRow(ctx,
		`UPDATE shipmententity SET status = $2 WHERE id = $1
		 RETURNING `+shipmentColumns,
		id, status,
	).Scan(&sh.ID, &sh.Status, &sh.ShipmentMethodID, &sh.ShipmentAddressID, &sh.OrderID, &sh.ReturnID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Shipment{}, fmt.Errorf("Shipment with id %s does not exist.", id)
	}
	if err != nil {
		return Shipment{}, fmt.Errorf("update shipment status %s: %w", id, err)
	}
	return sh, nil
}

// scanShipments collects shipment rows from an open pgx.Rows.
func scanShipments(rows pgx.Rows) ([]Shipment, error) {
	defer rows.Close()
	var out []Shipment
	for rows.Next() {
		var sh Shipment
		if err := rows.Scan(&sh.ID, &sh.Status, &sh.ShipmentMethodID, &sh.ShipmentAddressID, &sh.OrderID, &sh.ReturnID); err != nil {
			return nil, fmt.Errorf("scan shipment: %w", err)
		}
		out = append(out, sh)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
