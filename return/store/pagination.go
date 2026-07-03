package store

import (
	"context"
	"fmt"
	"strings"
)

// page describes one offset/limit page request, mirroring the original
// BaseConnection: an optional JOIN, a set of AND-combined conditions (each
// already parameterized), the ORDER BY column list with a single shared
// direction, and first/skip.
//
// ReturnConnection joins OrderEntity and filters on orderentity.userid (both
// the User.returns predicate and the non-employee authorizedUserFilter);
// OrderItemConnection has neither a join nor a filter. Conditions are combined
// with AND in declaration order, exactly like BaseConnection.buildCondition
// (predicate, then authorizedUserFilter — the always-null filter is omitted).
type page struct {
	table      string   // physical (lowercase) table name, e.g. "returnentity"
	columns    string   // projection, qualified when joining (e.g. "returnentity.id, ...")
	join       string   // "" or a full "JOIN ... ON ..." clause
	conditions []string // AND-combined predicates ("col = $1"), no WHERE keyword
	condArgs   []any    // args bound to the condition placeholders, in order
	primaryKey string   // COUNT() target, qualified when joining
	orderCols  []string // ORDER BY columns in priority order
	ascending  bool     // shared direction for every order column
	first      *int     // nil = unbounded (Long.MAX_VALUE)
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

// fromClause renders "FROM <table>[ <join>]".
func (p page) fromClause() string {
	if p.join == "" {
		return "FROM " + p.table
	}
	return "FROM " + p.table + " " + p.join
}

// whereClause renders " WHERE <c1> AND <c2> ..." or "".
func (p page) whereClause() string {
	if len(p.conditions) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(p.conditions, " AND ")
}

// totalCount runs COUNT(primaryKey) with the join and conditions but no
// order/offset/limit (matching BaseConnection.totalCount).
func (p page) totalCount(ctx context.Context, q querier) (int, error) {
	pk := p.primaryKey
	if pk == "" {
		pk = "id"
	}
	sql := fmt.Sprintf("SELECT COUNT(%s) %s%s", pk, p.fromClause(), p.whereClause())
	var n int
	if err := q.QueryRow(ctx, sql, p.condArgs...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count %s: %w", p.table, err)
	}
	return n, nil
}

// hasNextPage reports whether a row exists beyond the current page. It mirrors
// the original: unbounded pages (first == nil) never have a next page;
// otherwise EXISTS a row at OFFSET (first + skip). No ORDER BY is applied (the
// original BaseConnection.hasNextPage omits it; the boolean is order-invariant).
func (p page) hasNextPage(ctx context.Context, q querier) (bool, error) {
	if p.first == nil {
		return false, nil
	}
	skip := 0
	if p.skip != nil {
		skip = *p.skip
	}
	offset := *p.first + skip
	args := append(append([]any{}, p.condArgs...), offset)
	sql := fmt.Sprintf("SELECT EXISTS(SELECT 1 %s%s OFFSET $%d LIMIT 1)",
		p.fromClause(), p.whereClause(), len(args))
	var exists bool
	if err := q.QueryRow(ctx, sql, args...).Scan(&exists); err != nil {
		return false, fmt.Errorf("hasNextPage %s: %w", p.table, err)
	}
	return exists, nil
}

// nodesSQL renders the SELECT for the page body with OFFSET/LIMIT bound as the
// trailing placeholders, returning the SQL and its full arg list. first == nil
// yields LIMIT ALL (unbounded), matching Long.MAX_VALUE in the original.
func (p page) nodesSQL() (string, []any) {
	skip := 0
	if p.skip != nil {
		skip = *p.skip
	}
	args := append(append([]any{}, p.condArgs...), skip)
	limit := "ALL" // Postgres accepts LIMIT ALL for "no limit"
	if p.first != nil {
		args = append(args, *p.first)
		limit = fmt.Sprintf("$%d", len(args))
	}
	sql := fmt.Sprintf("SELECT %s %s%s %s OFFSET $%d LIMIT %s",
		p.columns, p.fromClause(), p.whereClause(), p.orderBy(), len(p.condArgs)+1, limit)
	return sql, args
}
