package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// page describes one offset page request, mirroring the original
// BaseConnection: an optional JOIN, a filter predicate (already
// parameterized), the ORDER BY column list with a single shared direction,
// and first/skip.
//
// The order columns are applied verbatim in sequence. All three shipment
// connections order only by the primary key (ID), so the column lists are
// single-element; the machinery still supports multi-column tiebreakers to
// match the tax reference pattern.
type page struct {
	table     string   // physical (lowercase) table name, possibly with alias/join base
	columns   string   // projection, e.g. "id, status, shipmentmethodid, ..."
	countCol  string   // column passed to COUNT(...) (the primary key)
	join      string   // "" or an "INNER JOIN ... ON ..." clause applied to every query
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

// from renders "<table>" or "<table> <join>".
func (p page) from() string {
	if p.join == "" {
		return p.table
	}
	return p.table + " " + p.join
}

// whereClause renders " WHERE <predicate>" or "".
func (p page) whereClause() string {
	if p.where == "" {
		return ""
	}
	return " WHERE " + p.where
}

// totalCount runs COUNT(countCol) with the join+predicate but no
// order/offset/limit.
func (p page) totalCount(ctx context.Context, pool querier) (int, error) {
	q := fmt.Sprintf("SELECT COUNT(%s) FROM %s%s", p.countCol, p.from(), p.whereClause())
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
		p.from(), p.whereClause(), p.orderBy(), len(args))
	var exists bool
	if err := pool.QueryRow(ctx, q, args...).Scan(&exists); err != nil {
		return false, fmt.Errorf("hasNextPage %s: %w", p.table, err)
	}
	return exists, nil
}

// nodesSQL renders the SELECT for the page body with OFFSET/LIMIT bound as the
// trailing placeholders, returning the SQL and its full arg list.
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
		p.columns, p.from(), p.whereClause(), p.orderBy(), len(p.whereArgs)+1, limit)
	return q, args
}

// querier is the subset of pgxpool.Pool the store methods need, so the same
// helpers work against a pool or a transaction if introduced later.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}
