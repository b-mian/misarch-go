package graph

import (
	"bytes"
	"sort"
	"testing"

	"github.com/google/uuid"
)

func item(id uuid.UUID) ShoppingCartItem { return ShoppingCartItem{ID: id} }

func ids(items []ShoppingCartItem) []uuid.UUID {
	out := make([]uuid.UUID, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func ptr[T any](v T) *T { return &v }

// makeItems builds n items with distinct random ids.
func makeItems(n int) []ShoppingCartItem {
	items := make([]ShoppingCartItem, n)
	for i := range items {
		items[i] = item(uuid.New())
	}
	return items
}

// sortedByBytes returns the ids sorted ascending by raw UUID bytes.
func sortedByBytes(items []ShoppingCartItem, asc bool) []uuid.UUID {
	cp := make([]ShoppingCartItem, len(items))
	copy(cp, items)
	sort.Slice(cp, func(i, j int) bool {
		c := bytes.Compare(cp[i].ID[:], cp[j].ID[:])
		if asc {
			return c < 0
		}
		return c > 0
	})
	return ids(cp)
}

// TestPaginateSortsByUUIDBytesAsc confirms the default (nil orderBy) sorts
// ascending by UUID bytes and returns all items with the correct totalCount and
// hasNextPage=false.
func TestPaginateSortsByUUIDBytesAsc(t *testing.T) {
	items := makeItems(5)
	conn := paginateItems(items, nil, nil, nil)

	if conn.TotalCount != 5 {
		t.Fatalf("totalCount = %d, want 5", conn.TotalCount)
	}
	if conn.HasNextPage {
		t.Fatalf("hasNextPage = true, want false (all returned)")
	}
	want := sortedByBytes(items, true)
	if got := ids(conn.Nodes); !equalIDs(got, want) {
		t.Fatalf("asc order:\n got %v\nwant %v", got, want)
	}
}

// TestPaginateDescIgnoresField confirms DESC direction reverses the byte order
// and that a non-nil field is ignored (only direction matters).
func TestPaginateDescIgnoresField(t *testing.T) {
	items := makeItems(4)
	orderBy := &CommonOrderInput{
		Direction: ptr(OrderDirectionDesc),
		Field:     ptr(CommonOrderFieldID), // must be ignored
	}
	conn := paginateItems(items, nil, nil, orderBy)
	want := sortedByBytes(items, false)
	if got := ids(conn.Nodes); !equalIDs(got, want) {
		t.Fatalf("desc order:\n got %v\nwant %v", got, want)
	}
}

// TestPaginateSkipTake exercises skip/first with hasNextPage math:
// hasNextPage = total > len(page) + skip.
func TestPaginateSkipTake(t *testing.T) {
	items := makeItems(10)
	full := sortedByBytes(items, true)

	// skip 2, take 3 → items [2,3,4]; hasNextPage = 10 > 3+2 = true.
	conn := paginateItems(items, ptr(3), ptr(2), nil)
	if conn.TotalCount != 10 {
		t.Fatalf("totalCount = %d, want 10", conn.TotalCount)
	}
	if !conn.HasNextPage {
		t.Fatalf("hasNextPage = false, want true")
	}
	want := full[2:5]
	if got := ids(conn.Nodes); !equalIDs(got, want) {
		t.Fatalf("skip/take page:\n got %v\nwant %v", got, want)
	}
}

// TestPaginateSkipTakeLastPage confirms hasNextPage=false when the page reaches
// the end.
func TestPaginateSkipTakeLastPage(t *testing.T) {
	items := makeItems(6)
	full := sortedByBytes(items, true)

	// skip 4, take 10 → items [4,5]; hasNextPage = 6 > 2+4 = false.
	conn := paginateItems(items, ptr(10), ptr(4), nil)
	if conn.HasNextPage {
		t.Fatalf("hasNextPage = true, want false")
	}
	want := full[4:]
	if got := ids(conn.Nodes); !equalIDs(got, want) {
		t.Fatalf("last page:\n got %v\nwant %v", got, want)
	}
}

// TestPaginateSkipBeyondEnd confirms an out-of-range skip yields an empty page
// and hasNextPage=false.
func TestPaginateSkipBeyondEnd(t *testing.T) {
	items := makeItems(3)
	conn := paginateItems(items, nil, ptr(10), nil)
	if len(conn.Nodes) != 0 {
		t.Fatalf("nodes = %d, want 0", len(conn.Nodes))
	}
	if conn.HasNextPage {
		t.Fatalf("hasNextPage = true, want false")
	}
	if conn.TotalCount != 3 {
		t.Fatalf("totalCount = %d, want 3", conn.TotalCount)
	}
}

// TestPaginateFirstOmittedReturnsAllRemaining confirms omitting first returns
// everything after skip.
func TestPaginateFirstOmittedReturnsAllRemaining(t *testing.T) {
	items := makeItems(5)
	full := sortedByBytes(items, true)
	conn := paginateItems(items, nil, ptr(2), nil)
	want := full[2:]
	if got := ids(conn.Nodes); !equalIDs(got, want) {
		t.Fatalf("first-omitted:\n got %v\nwant %v", got, want)
	}
	if conn.HasNextPage {
		t.Fatalf("hasNextPage = true, want false")
	}
}

// TestDedupItemInputs confirms identical {count, variant} pairs collapse while
// same-variant/different-count entries both persist, preserving order.
func TestDedupItemInputs(t *testing.T) {
	v1 := uuid.New()
	v2 := uuid.New()
	in := []ShoppingCartItemInput{
		{Count: 1, ProductVariantID: v1},
		{Count: 1, ProductVariantID: v1}, // exact dup → dropped
		{Count: 2, ProductVariantID: v1}, // same variant, diff count → kept
		{Count: 1, ProductVariantID: v2}, // diff variant → kept
	}
	out := dedupItemInputs(in)
	if len(out) != 3 {
		t.Fatalf("len = %d, want 3 (%+v)", len(out), out)
	}
	// Order preserved: (1,v1), (2,v1), (1,v2).
	if out[0].Count != 1 || out[0].ProductVariantID != v1 ||
		out[1].Count != 2 || out[1].ProductVariantID != v1 ||
		out[2].Count != 1 || out[2].ProductVariantID != v2 {
		t.Fatalf("unexpected dedup result: %+v", out)
	}
}

func equalIDs(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
