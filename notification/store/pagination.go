package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// page describes one keyset-free offset page request, mirroring the original
// BaseConnection: a filter predicate (already parameterized), the ORDER BY
// column list with a single shared direction, and first/skip.
//
// Unlike the tax service (which computed all three connection fields eagerly),
// the notification connection resolves nodes / totalCount / hasNextPage as
// independent, lazily-evaluated queries — so these helpers are invoked
// separately per selected field.
type page struct {
	table     string   // physical (lowercase) table name
	columns   string   // projection, e.g. "id, title, body, datesent, dateread, userid"
	where     string   // "" or a "col = $1" style predicate (no WHERE keyword)
	whereArgs []any    // args bound to the predicate placeholders
	orderCols []string // ORDER BY columns in priority order
	ascending bool     // shared direction for every order column
	first     *int     // nil = unbounded
	skip      *int     // nil = 0
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

// whereClause renders " WHERE <predicate>" or "".
func (p page) whereClause() string {
	if p.where == "" {
		return ""
	}
	return " WHERE " + p.where
}

// totalCount runs COUNT(id) with the predicate but no order/offset/limit,
// exactly mirroring the original totalCount() (ignores first/skip).
func (p page) totalCount(ctx context.Context, pool querier) (int, error) {
	q := fmt.Sprintf("SELECT COUNT(id) FROM %s%s", p.table, p.whereClause())
	var n int
	if err := pool.QueryRow(ctx, q, p.whereArgs...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count %s: %w", p.table, err)
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
	args := append(append([]any{}, p.whereArgs...), offset)
	q := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s%s %s OFFSET $%d LIMIT 1)",
		p.table, p.whereClause(), p.orderBy(), len(args))
	var exists bool
	if err := pool.QueryRow(ctx, q, args...).Scan(&exists); err != nil {
		return false, fmt.Errorf("hasNextPage %s: %w", p.table, err)
	}
	return exists, nil
}

// nodesSQL renders the SELECT for the page body with OFFSET/LIMIT bound as the
// trailing placeholders, returning the SQL and its full arg list. first == nil
// maps to LIMIT ALL (no limit), skip == nil to OFFSET 0.
func (p page) nodesSQL() (string, []any) {
	skip := 0
	if p.skip != nil {
		skip = *p.skip
	}
	args := append(append([]any{}, p.whereArgs...), skip)
	limit := "ALL" // Postgres accepts LIMIT ALL for "no limit"
	if p.first != nil {
		args = append(args, *p.first)
		limit = fmt.Sprintf("$%d", len(args))
	}
	q := fmt.Sprintf("SELECT %s FROM %s%s %s OFFSET $%d LIMIT %s",
		p.columns, p.table, p.whereClause(), p.orderBy(), len(p.whereArgs)+1, limit)
	return q, args
}

// querier is the subset of pgxpool.Pool the store methods need, so the same
// helpers work against a pool or a transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}
