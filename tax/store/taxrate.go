package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// taxRateColumns is the projection for a full TaxRate row.
const taxRateColumns = "id, name, description, currentversionid"

// TaxRateOrderColumn is a validated ORDER BY column for the taxRates
// connection. The graph layer maps the GraphQL enum to one of these.
type TaxRateOrderColumn []string

// Order-column sets for tax rates. NAME carries a secondary `id` tiebreaker,
// exactly as the original TaxRateOrderField.NAME did; ID is a single column.
var (
	TaxRateOrderByID   = TaxRateOrderColumn{"id"}
	TaxRateOrderByName = TaxRateOrderColumn{"name", "id"}
)

// GetTaxRate loads a tax rate by id, returning an error if it does not exist
// (the original data loader asserted non-null, surfacing a GraphQL error).
func (s *Store) GetTaxRate(ctx context.Context, id uuid.UUID) (TaxRate, error) {
	var t TaxRate
	err := s.pool.QueryRow(ctx,
		"SELECT "+taxRateColumns+" FROM taxrateentity WHERE id = $1", id,
	).Scan(&t.ID, &t.Name, &t.Description, &t.CurrentVersionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaxRate{}, fmt.Errorf("TaxRate with id %s does not exist.", id)
	}
	if err != nil {
		return TaxRate{}, fmt.Errorf("get tax rate %s: %w", id, err)
	}
	return t, nil
}

// ListTaxRates returns a page of tax rates ordered by the given column set and
// direction, together with the total count and next-page flag.
func (s *Store) ListTaxRates(
	ctx context.Context, first, skip *int, order TaxRateOrderColumn, ascending bool,
) (Connection[TaxRate], error) {
	p := page{
		table:     "taxrateentity",
		columns:   taxRateColumns,
		orderCols: order,
		ascending: ascending,
		first:     first,
		skip:      skip,
	}

	total, err := p.totalCount(ctx, s.pool)
	if err != nil {
		return Connection[TaxRate]{}, err
	}

	q, args := p.nodesSQL()
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return Connection[TaxRate]{}, fmt.Errorf("list tax rates: %w", err)
	}
	nodes, err := scanTaxRates(rows)
	if err != nil {
		return Connection[TaxRate]{}, err
	}

	next, err := p.hasNextPage(ctx, s.pool)
	if err != nil {
		return Connection[TaxRate]{}, err
	}

	return Connection[TaxRate]{Nodes: nodes, TotalCount: total, HasNextPage: next}, nil
}

// CreateTaxRate inserts a tax rate together with its initial version (version
// 1), sets the tax rate's current version to it, and returns both rows. All
// three writes run in one transaction; the caller publishes the events after a
// successful commit.
func (s *Store) CreateTaxRate(
	ctx context.Context, name, description string, rate float64, createdAt time.Time,
) (TaxRate, TaxRateVersion, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TaxRate{}, TaxRateVersion{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rateID := uuid.New()
	if _, err := tx.Exec(ctx,
		"INSERT INTO taxrateentity (id, name, description, currentversionid) VALUES ($1, $2, $3, NULL)",
		rateID, name, description,
	); err != nil {
		return TaxRate{}, TaxRateVersion{}, fmt.Errorf("insert tax rate: %w", err)
	}

	version, err := insertTaxRateVersion(ctx, tx, rateID, rate, createdAt)
	if err != nil {
		return TaxRate{}, TaxRateVersion{}, err
	}

	if _, err := tx.Exec(ctx,
		"UPDATE taxrateentity SET currentversionid = $1 WHERE id = $2", version.ID, rateID,
	); err != nil {
		return TaxRate{}, TaxRateVersion{}, fmt.Errorf("set current version: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return TaxRate{}, TaxRateVersion{}, err
	}

	return TaxRate{
		ID:               rateID,
		Name:             name,
		Description:      description,
		CurrentVersionID: version.ID,
	}, version, nil
}

// UpdateTaxRate applies a partial update (name and/or description) and returns
// the updated row. It errors if the tax rate does not exist. No event is
// published (matching the original).
func (s *Store) UpdateTaxRate(
	ctx context.Context, id uuid.UUID, name, description *string,
) (TaxRate, error) {
	// COALESCE keeps the existing value when the argument is NULL, giving a
	// single-statement partial update equivalent to the original's
	// read-modify-write.
	var t TaxRate
	err := s.pool.QueryRow(ctx, `
		UPDATE taxrateentity
		SET name = COALESCE($2, name),
		    description = COALESCE($3, description)
		WHERE id = $1
		RETURNING `+taxRateColumns,
		id, name, description,
	).Scan(&t.ID, &t.Name, &t.Description, &t.CurrentVersionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaxRate{}, fmt.Errorf("TaxRate with id %s does not exist.", id)
	}
	if err != nil {
		return TaxRate{}, fmt.Errorf("update tax rate %s: %w", id, err)
	}
	return t, nil
}

// scanTaxRates collects tax rate rows from an open pgx.Rows.
func scanTaxRates(rows pgx.Rows) ([]TaxRate, error) {
	defer rows.Close()
	var out []TaxRate
	for rows.Next() {
		var t TaxRate
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.CurrentVersionID); err != nil {
			return nil, fmt.Errorf("scan tax rate: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
