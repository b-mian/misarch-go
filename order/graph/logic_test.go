package graph

import (
	"testing"

	"github.com/google/uuid"

	"misarch/order/store"
)

// TestCalculateCompensatableAmount checks the multiplicative discount fold with
// a truncating float→uint64 cast (matching calculate_compensatable_amount).
func TestCalculateCompensatableAmount(t *testing.T) {
	pvv := store.ProductVariantVersion{Price: 1000}
	// 1000 * 0.9 * 0.85 = 765.0 → 765
	d1 := store.Discount{ID: uuid.New(), Discount: 0.9}
	d2 := store.Discount{ID: uuid.New(), Discount: 0.85}
	if got := calculateCompensatableAmount(pvv, []store.Discount{d1, d2}); got != 765 {
		t.Errorf("got %d, want 765", got)
	}
	// Truncation: 1000 * 0.333 = 333.0 → but 999 * 0.334 = 333.666 → 333.
	pvv2 := store.ProductVariantVersion{Price: 999}
	d3 := store.Discount{ID: uuid.New(), Discount: 0.334}
	if got := calculateCompensatableAmount(pvv2, []store.Discount{d3}); got != 333 {
		t.Errorf("truncation: got %d, want 333", got)
	}
	// No discounts → undiscounted price.
	if got := calculateCompensatableAmount(store.ProductVariantVersion{Price: 42}, nil); got != 42 {
		t.Errorf("no discounts: got %d, want 42", got)
	}
}

// TestPaginateInMemory checks the sort/skip/first/hasNextPage algorithm shared
// by Order.orderItems and OrderItem.discounts.
func TestPaginateInMemory(t *testing.T) {
	ids := []uuid.UUID{
		uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		uuid.MustParse("00000000-0000-0000-0000-000000000003"),
	}
	idf := func(u uuid.UUID) uuid.UUID { return u }

	// Default ASC, no paging → all, sorted ascending, hasNext=false.
	nodes, hasNext, total := paginateInMemory([]uuid.UUID{ids[2], ids[0], ids[1]}, nil, nil, nil, idf)
	if total != 3 || hasNext {
		t.Errorf("total=%d hasNext=%v, want 3/false", total, hasNext)
	}
	if nodes[0] != ids[0] || nodes[1] != ids[1] || nodes[2] != ids[2] {
		t.Errorf("ascending sort wrong: %v", nodes)
	}

	// first=2, skip=0 → first two, hasNext=true (3 > 2+0).
	first := 2
	nodes, hasNext, total = paginateInMemory(ids, &first, nil, nil, idf)
	if len(nodes) != 2 || !hasNext || total != 3 {
		t.Errorf("first=2: len=%d hasNext=%v total=%d, want 2/true/3", len(nodes), hasNext, total)
	}

	// skip=2, first big → last one, hasNext=false (3 > 1+2 is false).
	skip := 2
	big := 100
	nodes, hasNext, _ = paginateInMemory(ids, &big, &skip, nil, idf)
	if len(nodes) != 1 || hasNext {
		t.Errorf("skip=2: len=%d hasNext=%v, want 1/false", len(nodes), hasNext)
	}
	if nodes[0] != ids[2] {
		t.Errorf("skip=2 node = %s, want %s", nodes[0], ids[2])
	}

	// skip beyond end → empty, hasNext=false.
	skip = 10
	nodes, hasNext, _ = paginateInMemory(ids, nil, &skip, nil, idf)
	if len(nodes) != 0 || hasNext {
		t.Errorf("skip beyond end: len=%d hasNext=%v, want 0/false", len(nodes), hasNext)
	}

	// DESC direction.
	desc := OrderDirectionDesc
	nodes, _, _ = paginateInMemory(ids, nil, nil, &CommonOrderInput{Direction: &desc}, idf)
	if nodes[0] != ids[2] || nodes[2] != ids[0] {
		t.Errorf("descending sort wrong: %v", nodes)
	}
}

// TestSortDedupeDiscounts checks BTreeSet-by-id semantics (sort asc, dedupe).
func TestSortDedupeDiscounts(t *testing.T) {
	a := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	b := uuid.MustParse("00000000-0000-0000-0000-0000000000bb")
	in := []store.Discount{
		{ID: b, Discount: 0.5},
		{ID: a, Discount: 0.9},
		{ID: b, Discount: 0.5}, // duplicate id
	}
	out := sortDedupeDiscounts(in)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2 (deduped)", len(out))
	}
	if out[0].ID != a || out[1].ID != b {
		t.Errorf("order = %v, want [a, b] ascending", out)
	}
}

// TestDedupeOrderItemInputs checks dedup-by-cart-item-id + ascending sort.
func TestDedupeOrderItemInputs(t *testing.T) {
	a := uuid.MustParse("00000000-0000-0000-0000-0000000000a0")
	b := uuid.MustParse("00000000-0000-0000-0000-0000000000b0")
	sm := uuid.New()
	in := []OrderItemInput{
		{ShoppingCartItemID: b, ShipmentMethodID: sm, CouponIds: []uuid.UUID{}},
		{ShoppingCartItemID: a, ShipmentMethodID: sm, CouponIds: []uuid.UUID{}},
		{ShoppingCartItemID: b, ShipmentMethodID: sm, CouponIds: []uuid.UUID{}}, // dup
	}
	out := dedupeOrderItemInputs(in)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	if out[0].shoppingCartItemID != a || out[1].shoppingCartItemID != b {
		t.Errorf("order = %v, want ascending by cart item id", out)
	}
}
