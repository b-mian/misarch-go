package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// page describes one offset page request, mirroring the original
// BaseConnection: an optional JOIN clause, a combined filter predicate (already
// parameterized), the ORDER BY column list with a single shared direction, and
// first/skip.
//
// The order columns are applied verbatim in sequence, reproducing the Kotlin
// order fields that carry a secondary `id` tiebreaker (VALID_FROM, VALID_UNTIL,
// USAGES) as well as the single-column ID ordering. The chosen direction is
// applied to every column.
type page struct {
	table     string   // physical (lowercase) table name, aliased as the entity name
	pk        string   // primary key column for COUNT(pk), e.g. "couponentity.id"
	columns   string   // projection for node rows
	joins     string   // "" or one/more " INNER JOIN ... ON ..." fragments
	where     string   // "" or a combined predicate (no WHERE keyword)
	whereArgs []any    // args bound to the predicate placeholders ($1..$n)
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

// from renders "<table>[ joins]".
func (p page) from() string {
	return p.table + p.joins
}

// totalCount runs COUNT(pk) with the joins and predicate but no
// order/offset/limit.
func (p page) totalCount(ctx context.Context, q querier) (int, error) {
	sql := fmt.Sprintf("SELECT COUNT(%s) FROM %s%s", p.pk, p.from(), p.whereClause())
	var n int
	if err := q.QueryRow(ctx, sql, p.whereArgs...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count %s: %w", p.table, err)
	}
	return n, nil
}

// hasNextPage reports whether a row exists beyond the current page. It mirrors
// the original: unbounded pages (first == nil) never have a next page;
// otherwise EXISTS a row at OFFSET (first + skip). The original still applies
// the ORDER BY before offsetting.
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
	sql := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s%s %s OFFSET $%d LIMIT 1)",
		p.from(), p.whereClause(), p.orderBy(), len(args))
	var exists bool
	if err := q.QueryRow(ctx, sql, args...).Scan(&exists); err != nil {
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
	limit := "ALL" // Postgres accepts LIMIT ALL for "no limit" (Long.MAX_VALUE)
	if p.first != nil {
		args = append(args, *p.first)
		limit = "$" + strconv.Itoa(len(args))
	}
	sql := fmt.Sprintf("SELECT %s FROM %s%s %s OFFSET $%d LIMIT %s",
		p.columns, p.from(), p.whereClause(), p.orderBy(), len(p.whereArgs)+1, limit)
	return sql, args
}

// paginate executes the three connection queries (totalCount, nodes,
// hasNextPage) and assembles the Connection. scanNodes maps the open nodes-query
// rows to the typed slice (and is responsible for closing them).
func paginate[T any](
	ctx context.Context, q querier, p page, scanNodes func(pgx.Rows) ([]T, error),
) (Connection[T], error) {
	total, err := p.totalCount(ctx, q)
	if err != nil {
		return Connection[T]{}, err
	}
	sql, args := p.nodesSQL()
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return Connection[T]{}, fmt.Errorf("list %s: %w", p.table, err)
	}
	nodes, err := scanNodes(rows)
	if err != nil {
		return Connection[T]{}, err
	}
	next, err := p.hasNextPage(ctx, q)
	if err != nil {
		return Connection[T]{}, err
	}
	return Connection[T]{Nodes: nodes, TotalCount: total, HasNextPage: next}, nil
}
