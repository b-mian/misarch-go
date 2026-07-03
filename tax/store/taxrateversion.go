package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// taxRateVersionColumns is the projection for a full TaxRateVersion row.
const taxRateVersionColumns = "id, rate, version, createdat, taxrateid"

// TaxRateVersionOrderColumn is a validated ORDER BY column for the versions
// connection.
type TaxRateVersionOrderColumn []string

// Order-column sets for tax rate versions. CREATED_AT and VERSION carry a
// secondary `id` tiebreaker (as the original order fields did); ID is a single
// column.
var (
	TaxRateVersionOrderByID        = TaxRateVersionOrderColumn{"id"}
	TaxRateVersionOrderByCreatedAt = TaxRateVersionOrderColumn{"createdat", "id"}
	TaxRateVersionOrderByVersion   = TaxRateVersionOrderColumn{"version", "id"}
)

// execer is the subset of pgx used to run a version INSERT against either the
// pool or a transaction (both pgxpool.Pool and pgx.Tx satisfy it).
type execer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// GetTaxRateVersion loads a version by id, erroring if it does not exist.
func (s *Store) GetTaxRateVersion(ctx context.Context, id uuid.UUID) (TaxRateVersion, error) {
	var v TaxRateVersion
	err := s.pool.QueryRow(ctx,
		"SELECT "+taxRateVersionColumns+" FROM taxrateversionentity WHERE id = $1", id,
	).Scan(&v.ID, &v.Rate, &v.Version, &v.CreatedAt, &v.TaxRateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaxRateVersion{}, fmt.Errorf("TaxRateVersion with id %s does not exist.", id)
	}
	if err != nil {
		return TaxRateVersion{}, fmt.Errorf("get tax rate version %s: %w", id, err)
	}
	return v, nil
}

// TaxRateExists reports whether a tax rate with the given id exists.
func (s *Store) TaxRateExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM taxrateentity WHERE id = $1)", id,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("tax rate exists %s: %w", id, err)
	}
	return exists, nil
}

// ListTaxRateVersions returns a page of versions belonging to one tax rate.
func (s *Store) ListTaxRateVersions(
	ctx context.Context, taxRateID uuid.UUID, first, skip *int,
	order TaxRateVersionOrderColumn, ascending bool,
) (Connection[TaxRateVersion], error) {
	p := page{
		table:     "taxrateversionentity",
		columns:   taxRateVersionColumns,
		where:     "taxrateid = $1",
		whereArgs: []any{taxRateID},
		orderCols: order,
		ascending: ascending,
		first:     first,
		skip:      skip,
	}

	total, err := p.totalCount(ctx, s.pool)
	if err != nil {
		return Connection[TaxRateVersion]{}, err
	}

	q, args := p.nodesSQL()
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return Connection[TaxRateVersion]{}, fmt.Errorf("list tax rate versions: %w", err)
	}
	nodes, err := scanTaxRateVersions(rows)
	if err != nil {
		return Connection[TaxRateVersion]{}, err
	}

	next, err := p.hasNextPage(ctx, s.pool)
	if err != nil {
		return Connection[TaxRateVersion]{}, err
	}

	return Connection[TaxRateVersion]{Nodes: nodes, TotalCount: total, HasNextPage: next}, nil
}

// CreateTaxRateVersion inserts a new version for an existing tax rate, bumps
// the tax rate's current version to it, and returns the new version. It errors
// if the tax rate does not exist. The caller publishes the event after commit.
func (s *Store) CreateTaxRateVersion(
	ctx context.Context, taxRateID uuid.UUID, rate float64, createdAt time.Time,
) (TaxRateVersion, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TaxRateVersion{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists bool
	if err := tx.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM taxrateentity WHERE id = $1)", taxRateID,
	).Scan(&exists); err != nil {
		return TaxRateVersion{}, fmt.Errorf("tax rate exists %s: %w", taxRateID, err)
	}
	if !exists {
		return TaxRateVersion{}, fmt.Errorf("TaxRate with id %s does not exist.", taxRateID)
	}

	version, err := insertTaxRateVersion(ctx, tx, taxRateID, rate, createdAt)
	if err != nil {
		return TaxRateVersion{}, err
	}

	if _, err := tx.Exec(ctx,
		"UPDATE taxrateentity SET currentversionid = $1 WHERE id = $2", version.ID, taxRateID,
	); err != nil {
		return TaxRateVersion{}, fmt.Errorf("set current version: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return TaxRateVersion{}, err
	}
	return version, nil
}

// insertTaxRateVersion computes the next version number (max(version)+1, or 1
// when none exist) and inserts a version row, returning it. It runs on the
// provided execer so it participates in the caller's transaction.
func insertTaxRateVersion(
	ctx context.Context, q execer, taxRateID uuid.UUID, rate float64, createdAt time.Time,
) (TaxRateVersion, error) {
	// MAX(version) is NULL when the tax rate has no versions yet; COALESCE to 0
	// so the first version is 1, matching the original (`max?.plus(1) ?: 1`).
	var version int
	if err := q.QueryRow(ctx,
		"SELECT COALESCE(MAX(version), 0) + 1 FROM taxrateversionentity WHERE taxrateid = $1",
		taxRateID,
	).Scan(&version); err != nil {
		return TaxRateVersion{}, fmt.Errorf("next version for %s: %w", taxRateID, err)
	}

	id := uuid.New()
	if _, err := q.Exec(ctx,
		`INSERT INTO taxrateversionentity (id, rate, version, createdat, taxrateid)
		 VALUES ($1, $2, $3, $4, $5)`,
		id, rate, version, createdAt, taxRateID,
	); err != nil {
		return TaxRateVersion{}, fmt.Errorf("insert tax rate version: %w", err)
	}

	return TaxRateVersion{
		ID:        id,
		Rate:      rate,
		Version:   version,
		CreatedAt: createdAt,
		TaxRateID: taxRateID,
	}, nil
}

// scanTaxRateVersions collects version rows from an open pgx.Rows.
func scanTaxRateVersions(rows pgx.Rows) ([]TaxRateVersion, error) {
	defer rows.Close()
	var out []TaxRateVersion
	for rows.Next() {
		var v TaxRateVersion
		if err := rows.Scan(&v.ID, &v.Rate, &v.Version, &v.CreatedAt, &v.TaxRateID); err != nil {
			return nil, fmt.Errorf("scan tax rate version: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
