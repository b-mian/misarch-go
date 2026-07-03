package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// page describes one offset page request over the addressentity table,
// mirroring the original BaseConnection: an AND-combined set of already
// parameterized predicates, the ORDER BY column list with a single shared
// direction, and first/skip.
//
// The order columns are applied verbatim in sequence. The only order field the
// address schema exposes is ID (a single `id` column with no secondary
// tiebreaker), matching UserAddressOrderField.ID.
type page struct {
	columns    string   // projection
	predicates []string // AND-combined predicate fragments (already parameterized)
	args       []any    // args bound to the predicate placeholders, in order
	orderCols  []string // ORDER BY columns in priority order
	ascending  bool     // shared direction for every order column
	first      *int     // nil = unbounded
	skip       *int     // nil = 0
}

// direction renders the SQL sort direction.
func (p page) direction() string {
	if p.ascending {
		return "ASC"
	}
	return "DESC"
}

// orderBy renders the ORDER BY clause (columns share one direction).
func (p page) orderBy() string {
	parts := make([]string, len(p.orderCols))
	dir := p.direction()
	for i, c := range p.orderCols {
		parts[i] = c + " " + dir
	}
	return "ORDER BY " + strings.Join(parts, ", ")
}

// whereClause renders " WHERE <p1 AND p2 ...>" or "".
func (p page) whereClause() string {
	if len(p.predicates) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(p.predicates, " AND ")
}

// totalCount runs COUNT(id) with the predicates but no order/offset/limit,
// exactly as the original BaseConnection.totalCount (which ignores first/skip).
func (p page) totalCount(ctx context.Context, pool querier) (int, error) {
	q := fmt.Sprintf("SELECT COUNT(id) FROM addressentity%s", p.whereClause())
	var n int
	if err := pool.QueryRow(ctx, q, p.args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count addressentity: %w", err)
	}
	return n, nil
}

// hasNextPage reports whether a row exists beyond the current page. It mirrors
// the original: unbounded pages (first == nil) never have a next page;
// otherwise EXISTS a row at OFFSET (first + skip).
func (p page) hasNextPage(ctx context.Context, pool querier) (bool, error) {
	if p.first == nil {
		return false, nil
	}
	skip := 0
	if p.skip != nil {
		skip = *p.skip
	}
	offset := *p.first + skip
	args := append(append([]any{}, p.args...), offset)
	q := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM addressentity%s %s OFFSET $%d LIMIT 1)",
		p.whereClause(), p.orderBy(), len(args))
	var exists bool
	if err := pool.QueryRow(ctx, q, args...).Scan(&exists); err != nil {
		return false, fmt.Errorf("hasNextPage addressentity: %w", err)
	}
	return exists, nil
}

// nodesSQL renders the SELECT for the page body with OFFSET/LIMIT bound as the
// trailing placeholders, returning the SQL and its full arg list. A nil first
// maps to LIMIT ALL (Long.MAX_VALUE in the original), a nil skip to OFFSET 0.
func (p page) nodesSQL() (string, []any) {
	skip := 0
	if p.skip != nil {
		skip = *p.skip
	}
	args := append(append([]any{}, p.args...), skip)
	limit := "ALL" // Postgres accepts LIMIT ALL for "no limit"
	if p.first != nil {
		args = append(args, *p.first)
		limit = fmt.Sprintf("$%d", len(args))
	}
	q := fmt.Sprintf("SELECT %s FROM addressentity%s %s OFFSET $%d LIMIT %s",
		p.columns, p.whereClause(), p.orderBy(), len(p.args)+1, limit)
	return q, args
}

// querier is the subset of pgxpool.Pool the store methods need, so the same
// helpers work against a pool or a transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}
