package store

import (
	"regexp"
	"sort"
	"strconv"
	"testing"

	"github.com/google/uuid"
)

// assertSequentialPlaceholders checks that the $N placeholders used in sql are
// exactly 1..n with no gaps or duplicates beyond intentional reuse, and that the
// highest placeholder does not exceed argCount. Reused placeholders (e.g. the
// same product-variant id in three subqueries) are allowed; the invariant is
// that every referenced index is within [1, argCount].
func assertSequentialPlaceholders(t *testing.T, sql string, argCount int) {
	t.Helper()
	re := regexp.MustCompile(`\$(\d+)`)
	matches := re.FindAllStringSubmatch(sql, -1)
	seen := map[int]bool{}
	for _, m := range matches {
		n, _ := strconv.Atoi(m[1])
		if n < 1 || n > argCount {
			t.Fatalf("placeholder $%d out of range [1,%d] in SQL:\n%s", n, argCount, sql)
		}
		seen[n] = true
	}
	// Every arg index up to argCount must be referenced at least once (no unused
	// trailing args, which would signal a numbering bug).
	var idx []int
	for k := range seen {
		idx = append(idx, k)
	}
	sort.Ints(idx)
	for want := 1; want <= argCount; want++ {
		if !seen[want] {
			t.Fatalf("arg $%d never referenced (referenced: %v) in SQL:\n%s", want, idx, sql)
		}
	}
}

func TestPageNodesSQLPlaceholders(t *testing.T) {
	first, skip := 10, 5
	p := page{
		table:     "discountentity",
		pk:        "discountentity.id",
		columns:   discountColumns,
		joins:     " INNER JOIN discounttocategoryentity ON discounttocategoryentity.discountid = discountentity.id",
		where:     "discounttocategoryentity.categoryid = $1",
		whereArgs: []any{uuid.New()},
		orderCols: CategoryOrderByID,
		ascending: true,
		first:     &first,
		skip:      &skip,
	}
	sql, args := p.nodesSQL()
	// whereArgs(1) + skip + first = 3 args, placeholders $1..$3.
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(args))
	}
	assertSequentialPlaceholders(t, sql, len(args))
}

func TestApplicableDiscountsForVariantSQL(t *testing.T) {
	// Reconstruct the builder the store method uses, then assert the resulting
	// SQL uses sequential placeholders matching the arg count. Mirrors
	// ApplicableDiscountsForVariant's construction.
	var b condBuilder
	pv := uuid.New()
	user := uuid.New()
	couponIDs := []uuid.UUID{uuid.New()}

	phUser := b.placeholder(user)
	joins := " LEFT JOIN discountusageentity ON discountusageentity.discountid = discountentity.id AND discountusageentity.userid = " + phUser
	appliesCondition(&b, pv)
	noCoupon := noCouponsCondition()
	userHas := userHasCouponFromListCondition(&b, couponIDs, user)
	b.addRaw("(" + noCoupon + " OR " + userHas + ")")
	nowFrom := b.placeholder("now")
	nowUntil := b.placeholder("now")
	b.addRaw("discountentity.validfrom <= " + nowFrom + " AND discountentity.validuntil >= " + nowUntil)
	phAmount := b.placeholder(1000)
	b.addRaw("(discountentity.minorderamount IS NULL OR discountentity.minorderamount <= " + phAmount + ")")

	where, args := b.where()
	sql := "SELECT " + discountColumns + " FROM discountentity" + joins + " WHERE " + where
	// user, pv, couponIDs, user, now, now, amount = 7 args.
	if len(args) != 7 {
		t.Fatalf("expected 7 args, got %d", len(args))
	}
	assertSequentialPlaceholders(t, sql, len(args))
}

func TestApplicableDiscountsConnectionSQL(t *testing.T) {
	// Anonymous applicable discounts: no join, no coupon-user condition.
	var b condBuilder
	pv := uuid.New()
	appliesCondition(&b, pv)
	b.addRaw(noCouponsCondition())
	nowFrom := b.placeholder("now")
	nowUntil := b.placeholder("now")
	b.addRaw("discountentity.validfrom <= " + nowFrom + " AND discountentity.validuntil >= " + nowUntil)
	where, args := b.where()

	first := 3
	p := page{
		table:     "discountentity",
		pk:        "discountentity.id",
		columns:   discountColumns,
		where:     where,
		whereArgs: args,
		orderCols: DiscountOrderByID,
		ascending: true,
		first:     &first,
	}
	// pv, now, now = 3 whereArgs; nodesSQL adds skip + first = 5.
	nodesSQL, nodesArgs := p.nodesSQL()
	assertSequentialPlaceholders(t, nodesSQL, len(nodesArgs))

	countSQL := "SELECT COUNT(" + p.pk + ") FROM " + p.from() + p.whereClause()
	assertSequentialPlaceholders(t, countSQL, len(p.whereArgs))
}

func TestCouponConnectionAuthFilterSQL(t *testing.T) {
	// Non-employee: base predicate + auth filter (own coupons) -> 2 args.
	var b condBuilder
	b.eq("couponentity.discountid", uuid.New())
	authUser := &AuthUser{ID: uuid.New(), Employee: false}
	b.addRaw(couponAuthFilterSQL(&b, authUser))
	where, args := b.where()
	if len(args) != 2 {
		t.Fatalf("expected 2 args (discountid, authUser), got %d", len(args))
	}
	p := page{
		table:     "couponentity",
		pk:        "couponentity.id",
		columns:   couponColumns,
		where:     where,
		whereArgs: args,
		orderCols: CouponOrderByID,
		ascending: true,
	}
	countSQL := "SELECT COUNT(" + p.pk + ") FROM " + p.from() + p.whereClause()
	assertSequentialPlaceholders(t, countSQL, len(p.whereArgs))
}

func TestCouponAuthFilterAnonymousAndEmployee(t *testing.T) {
	var b condBuilder
	if got := couponAuthFilterSQL(&b, nil); got != "FALSE" {
		t.Fatalf("anonymous: want FALSE, got %q", got)
	}
	if got := couponAuthFilterSQL(&b, &AuthUser{ID: uuid.New(), Employee: true}); got != "" {
		t.Fatalf("employee: want empty, got %q", got)
	}
	if len(b.args) != 0 {
		t.Fatalf("anonymous/employee should bind no args, got %d", len(b.args))
	}
}

func TestOrderByTiebreakers(t *testing.T) {
	// Every non-ID order set must append id as a secondary column.
	cases := map[string][]string{
		"couponValidFrom":   CouponOrderByValidFrom,
		"couponValidUntil":  CouponOrderByValidUntil,
		"discountValidFrom": DiscountOrderByValidFrom,
		"usages":            DiscountUsageOrderByUsages,
	}
	for name, cols := range cases {
		if len(cols) != 2 {
			t.Fatalf("%s: expected [field, id], got %v", name, cols)
		}
		if cols[1] != "couponentity.id" && cols[1] != "discountentity.id" && cols[1] != "discountusageentity.id" {
			t.Fatalf("%s: expected id tiebreaker, got %q", name, cols[1])
		}
	}
	p := page{orderCols: DiscountUsageOrderByUsages, ascending: false}
	if got := p.orderBy(); got != "ORDER BY discountusageentity.usages DESC, discountusageentity.id DESC" {
		t.Fatalf("orderBy DESC applied to all columns; got %q", got)
	}
}
