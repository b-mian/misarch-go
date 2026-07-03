package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// page describes one offset page request, mirroring the original
// BaseConnection: an optional join clause, a filter predicate (already
// parameterized), the ORDER BY column list with a single shared direction, and
// first/skip.
//
// The order columns are applied verbatim in sequence, reproducing the Kotlin
// order fields that carry a secondary `id` tiebreaker (NAME, INTERNAL_NAME,
// VERSION, CREATED_AT) as well as single-column ID/VALUE orderings; the chosen
// direction applies to every order column.
//
// countCol is the fully-qualified primary-key column counted by totalCount —
// the original always did COUNT(<entity table>.id) even across joins, so with
// a join the base-table id must be qualified to stay unambiguous.
type page struct {
	table     string   // physical (lowercase) table name (the FROM base table)
	join      string   // "" or " INNER JOIN ... ON ..." applied to every query
	countCol  string   // fully-qualified PK column for COUNT, e.g. "productentity.id"
	columns   string   // projection for nodes, qualified where a join is present
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

// countColumn returns the column COUNT() is applied to (defaults to id).
func (p page) countColumn() string {
	if p.countCol == "" {
		return "id"
	}
	return p.countCol
}

// totalCount runs COUNT(<primary key>) with the join and predicate but no
// order/offset/limit. It ignores first/skip, matching the original.
func (p page) totalCount(ctx context.Context, q querier) (int, error) {
	sql := fmt.Sprintf("SELECT COUNT(%s) FROM %s%s%s",
		p.countColumn(), p.table, p.join, p.whereClause())
	var n int
	if err := q.QueryRow(ctx, sql, p.whereArgs...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count %s: %w", p.table, err)
	}
	return n, nil
}

// hasNextPage reports whether a row exists beyond the current page. It mirrors
// the original: unbounded pages (first == nil) never have a next page;
// otherwise EXISTS a row at OFFSET (first + skip). The probe applies no ORDER
// BY (irrelevant to whether a further row exists).
func (p page) hasNextPage(ctx context.Context, q querier) (bool, error) {
	if p.first == nil {
		return false, nil
	}
	skip := 0
	if p.skip != nil {
		skip = *p.skip
	}
	offset := *p.first + skip
	args := append(append([]any{}, p.whereArgs...), offset)
	sql := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s%s%s OFFSET $%d LIMIT 1)",
		p.table, p.join, p.whereClause(), len(args))
	var exists bool
	if err := q.QueryRow(ctx, sql, args...).Scan(&exists); err != nil {
		return false, fmt.Errorf("hasNextPage %s: %w", p.table, err)
	}
	return exists, nil
}

// nodesSQL renders the SELECT for the page body with OFFSET/LIMIT bound as the
// trailing placeholders, returning the SQL and its full arg list. first == nil
// yields LIMIT ALL (unbounded), matching the original's LIMIT Long.MAX_VALUE.
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
	sql := fmt.Sprintf("SELECT %s FROM %s%s%s %s OFFSET $%d LIMIT %s",
		p.columns, p.table, p.join, p.whereClause(), p.orderBy(), len(p.whereArgs)+1, limit)
	return sql, args
}

// querier is the subset of pgxpool.Pool the store methods need, so the same
// helpers work against a pool or a transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}
