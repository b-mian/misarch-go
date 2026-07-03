package graph

import (
	"context"
	"fmt"

	"github.com/99designs/gqlgen/graphql"

	"misarch/inventory/store"
)

const (
	defaultSkip  = 0
	defaultFirst = 2147483647 // MAX_INT32, the schema default for `first`.
)

// paginationArgs normalizes skip/first to concrete values and validates them
// the way the original Nest ValidationPipe did (@Min(0) on skip, @Min(1) on
// first), returning an error BEFORE any DB work. The messages contain the
// phrases other services/tests match on (spec §3.0 / §3.5).
func paginationArgs(skip, first *int) (int, int, error) {
	s := defaultSkip
	if skip != nil {
		s = *skip
	}
	f := defaultFirst
	if first != nil {
		f = *first
	}
	if s < 0 {
		return 0, 0, fmt.Errorf("skip must not be less than 0")
	}
	if f < 1 {
		return 0, 0, fmt.Errorf("first must not be less than 1")
	}
	return s, f, nil
}

// buildConnection reproduces the original buildConnection lazy semantics
// (spec §3.0): it inspects which sub-fields of the connection were selected and
//   - loads `nodes` only if `nodes` was selected;
//   - computes `totalCount`/`hasNextPage` only if either was selected
//     (hasNextPage forces a count).
//
// The nodes filter and count filter differ inside the store (the count uses the
// correct field names; nodes reproduces the original buggy translation), so the
// caller passes the same *store.Filter and the store applies each translation.
func buildConnection(ctx context.Context, st *store.Store, filter *store.Filter, skip, first int, sortAsc bool) (*ProductItemConnection, error) {
	selected := selectedConnectionFields(ctx)
	conn := &ProductItemConnection{}

	if selected["nodes"] {
		items, err := st.FindNodes(ctx, filter, skip, first, sortAsc)
		if err != nil {
			return nil, err
		}
		conn.Nodes = toProductItems(items)
	}

	if selected["totalCount"] || selected["hasNextPage"] {
		total, err := st.Count(ctx, filter)
		if err != nil {
			return nil, err
		}
		conn.TotalCount = int(total)
		// 64-bit arithmetic to avoid overflow with the MAX_INT32 default first
		// (skip + first can exceed int32; the original used JS numbers).
		conn.HasNextPage = int64(skip)+int64(first) < total
	}

	return conn, nil
}

// selectedConnectionFields returns the set of immediate sub-field names
// selected on the connection field currently being resolved — the gqlgen
// equivalent of the original queryKeys(info).
func selectedConnectionFields(ctx context.Context) map[string]bool {
	set := map[string]bool{}
	for _, f := range graphql.CollectFieldsCtx(ctx, nil) {
		set[f.Name] = true
	}
	return set
}
