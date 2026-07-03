package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// shipmentMethodColumns is the projection for a full ShipmentMethod row.
const shipmentMethodColumns = "id, name, description, externalreference, basefees, feesperitem, feesperkg, archivedat"

// ShipmentMethodOrderColumn is a validated ORDER BY column set for the
// shipmentMethods connection. The only order field in the SDL is ID.
type ShipmentMethodOrderColumn []string

// ShipmentMethodOrderByID orders by the primary key (the sole order field).
var ShipmentMethodOrderByID = ShipmentMethodOrderColumn{"id"}

// GetShipmentMethod loads a shipment method by id, erroring if it does not
// exist. The original resolved this via a data loader whose miss asserted
// non-null, surfacing a GraphQL error; reproduce with an error.
func (s *Store) GetShipmentMethod(ctx context.Context, id uuid.UUID) (ShipmentMethod, error) {
	var m ShipmentMethod
	err := s.pool.QueryRow(ctx,
		"SELECT "+shipmentMethodColumns+" FROM shipmentmethodentity WHERE id = $1", id,
	).Scan(&m.ID, &m.Name, &m.Description, &m.ExternalReference, &m.BaseFees, &m.FeesPerItem, &m.FeesPerKg, &m.ArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ShipmentMethod{}, fmt.Errorf("ShipmentMethod with id %s does not exist.", id)
	}
	if err != nil {
		return ShipmentMethod{}, fmt.Errorf("get shipment method %s: %w", id, err)
	}
	return m, nil
}

// FindShipmentMethodsByIDs loads the shipment methods with the given ids
// (order unspecified), used by calculateShipmentFees to validate + price
// groups. Missing ids are simply absent from the result; the caller checks
// completeness.
func (s *Store) FindShipmentMethodsByIDs(ctx context.Context, ids []uuid.UUID) ([]ShipmentMethod, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		"SELECT "+shipmentMethodColumns+" FROM shipmentmethodentity WHERE id = ANY($1)", ids,
	)
	if err != nil {
		return nil, fmt.Errorf("find shipment methods by ids: %w", err)
	}
	return scanShipmentMethods(rows)
}

// FindAllShipmentMethods returns every shipment method, INCLUDING archived
// ones (no archivedAt filter) — matching findLeastExpensiveShipmentMethod's
// repository.findAll().
func (s *Store) FindAllShipmentMethods(ctx context.Context) ([]ShipmentMethod, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+shipmentMethodColumns+" FROM shipmentmethodentity",
	)
	if err != nil {
		return nil, fmt.Errorf("find all shipment methods: %w", err)
	}
	return scanShipmentMethods(rows)
}

// ListShipmentMethods returns a page of shipment methods, optionally filtered
// by archived state (isArchived==true → archivedat IS NOT NULL; false → IS
// NULL; nil → no filter).
func (s *Store) ListShipmentMethods(
	ctx context.Context, first, skip *int, order ShipmentMethodOrderColumn, ascending bool, isArchived *bool,
) (Connection[ShipmentMethod], error) {
	where := ""
	if isArchived != nil {
		if *isArchived {
			where = "archivedat IS NOT NULL"
		} else {
			where = "archivedat IS NULL"
		}
	}
	p := page{
		table:     "shipmentmethodentity",
		columns:   shipmentMethodColumns,
		countCol:  "id",
		where:     where,
		orderCols: order,
		ascending: ascending,
		first:     first,
		skip:      skip,
	}

	total, err := p.totalCount(ctx, s.pool)
	if err != nil {
		return Connection[ShipmentMethod]{}, err
	}

	q, args := p.nodesSQL()
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return Connection[ShipmentMethod]{}, fmt.Errorf("list shipment methods: %w", err)
	}
	nodes, err := scanShipmentMethods(rows)
	if err != nil {
		return Connection[ShipmentMethod]{}, err
	}

	next, err := p.hasNextPage(ctx, s.pool)
	if err != nil {
		return Connection[ShipmentMethod]{}, err
	}

	return Connection[ShipmentMethod]{Nodes: nodes, TotalCount: total, HasNextPage: next}, nil
}

// CreateShipmentMethod inserts a shipment method (id assigned by the DB
// default uuid_generate_v4(), archivedat NULL) and returns the created row.
// The caller publishes the event after success.
func (s *Store) CreateShipmentMethod(
	ctx context.Context, name, description, externalReference string, baseFees, feesPerItem, feesPerKg int,
) (ShipmentMethod, error) {
	var m ShipmentMethod
	err := s.pool.QueryRow(ctx,
		`INSERT INTO shipmentmethodentity (name, description, externalreference, basefees, feesperitem, feesperkg, archivedat)
		 VALUES ($1, $2, $3, $4, $5, $6, NULL)
		 RETURNING `+shipmentMethodColumns,
		name, description, externalReference, baseFees, feesPerItem, feesPerKg,
	).Scan(&m.ID, &m.Name, &m.Description, &m.ExternalReference, &m.BaseFees, &m.FeesPerItem, &m.FeesPerKg, &m.ArchivedAt)
	if err != nil {
		return ShipmentMethod{}, fmt.Errorf("insert shipment method: %w", err)
	}
	return m, nil
}

// ArchiveShipmentMethod sets archivedat = now on the given method and returns
// the updated row. It errors if the method does not exist (matching the
// original findById().awaitSingle() empty-Mono error). Archiving an already
// archived method just overwrites archivedat (no guard), as in the original.
func (s *Store) ArchiveShipmentMethod(ctx context.Context, id uuid.UUID, archivedAt time.Time) (ShipmentMethod, error) {
	var m ShipmentMethod
	err := s.pool.QueryRow(ctx,
		`UPDATE shipmentmethodentity SET archivedat = $2 WHERE id = $1
		 RETURNING `+shipmentMethodColumns,
		id, archivedAt,
	).Scan(&m.ID, &m.Name, &m.Description, &m.ExternalReference, &m.BaseFees, &m.FeesPerItem, &m.FeesPerKg, &m.ArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ShipmentMethod{}, fmt.Errorf("ShipmentMethod with id %s does not exist.", id)
	}
	if err != nil {
		return ShipmentMethod{}, fmt.Errorf("archive shipment method %s: %w", id, err)
	}
	return m, nil
}

// scanShipmentMethods collects shipment method rows from an open pgx.Rows.
func scanShipmentMethods(rows pgx.Rows) ([]ShipmentMethod, error) {
	defer rows.Close()
	var out []ShipmentMethod
	for rows.Next() {
		var m ShipmentMethod
		if err := rows.Scan(&m.ID, &m.Name, &m.Description, &m.ExternalReference, &m.BaseFees, &m.FeesPerItem, &m.FeesPerKg, &m.ArchivedAt); err != nil {
			return nil, fmt.Errorf("scan shipment method: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
