package graph

import (
	"testing"

	"github.com/google/uuid"

	"misarch/discount/store"
)

func TestFilterApplicableDiscountsBudgetAcrossVariants(t *testing.T) {
	// A capped discount with 3 remaining, consumed across two variants of count
	// 2 then 2: the first keeps it (3 >= 2, remaining -> 1), the second drops it
	// (1 < 2). Mirrors the cross-variant budget mutation in input order.
	d := store.Discount{ID: uuid.New(), MaxUsagesPerUser: intptr(3)}
	remaining := map[uuid.UUID]int64{d.ID: 3}

	pv1 := FindApplicableDiscountsProductVariantInput{ProductVariantID: uuid.New(), Count: 2}
	res1, err := filterApplicableDiscounts([]store.Discount{d}, remaining, pv1, map[uuid.UUID]store.Coupon{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res1.Discounts) != 1 {
		t.Fatalf("variant 1 should keep the discount, got %d", len(res1.Discounts))
	}
	if remaining[d.ID] != 1 {
		t.Fatalf("remaining should be decremented to 1, got %d", remaining[d.ID])
	}

	pv2 := FindApplicableDiscountsProductVariantInput{ProductVariantID: uuid.New(), Count: 2}
	res2, err := filterApplicableDiscounts([]store.Discount{d}, remaining, pv2, map[uuid.UUID]store.Coupon{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Discounts) != 0 {
		t.Fatalf("variant 2 should drop the discount (budget exhausted), got %d", len(res2.Discounts))
	}
}

func TestFilterApplicableDiscountsUncappedUnlimited(t *testing.T) {
	// An uncapped discount (absent from the remaining map) is always kept and
	// never decrements anything, even for a large count.
	d := store.Discount{ID: uuid.New(), MaxUsagesPerUser: nil}
	remaining := map[uuid.UUID]int64{} // uncapped -> not present
	pv := FindApplicableDiscountsProductVariantInput{ProductVariantID: uuid.New(), Count: 1000}
	res, err := filterApplicableDiscounts([]store.Discount{d}, remaining, pv, map[uuid.UUID]store.Coupon{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Discounts) != 1 {
		t.Fatalf("uncapped discount should always be kept, got %d", len(res.Discounts))
	}
	if len(remaining) != 0 {
		t.Fatalf("uncapped discount must not touch the budget map, got %v", remaining)
	}
}

func TestFilterApplicableDiscountsCouponNotApplicable(t *testing.T) {
	// A coupon whose discount was dropped (insufficient budget) triggers the
	// "could not be used" error.
	d := store.Discount{ID: uuid.New(), MaxUsagesPerUser: intptr(0)}
	remaining := map[uuid.UUID]int64{d.ID: 0}
	couponID := uuid.New()
	coupons := map[uuid.UUID]store.Coupon{couponID: {ID: couponID, DiscountID: d.ID}}
	pv := FindApplicableDiscountsProductVariantInput{
		ProductVariantID: uuid.New(), Count: 1, CouponIds: []uuid.UUID{couponID},
	}
	_, err := filterApplicableDiscounts([]store.Discount{d}, remaining, pv, coupons)
	if err == nil {
		t.Fatal("expected error for inapplicable coupon")
	}
	want := "Coupon with id " + couponID.String() + " could not be used, either due to insufficient remaining usages, the user not owning the coupon, or the coupon not being applicable"
	if err.Error() != want {
		t.Fatalf("error mismatch:\n got: %s\nwant: %s", err.Error(), want)
	}
}

func TestFilterApplicableDiscountsDuplicateCouponForDiscount(t *testing.T) {
	// Two coupons for the same (kept) discount in one variant is an error.
	d := store.Discount{ID: uuid.New(), MaxUsagesPerUser: nil}
	remaining := map[uuid.UUID]int64{}
	c1, c2 := uuid.New(), uuid.New()
	coupons := map[uuid.UUID]store.Coupon{
		c1: {ID: c1, DiscountID: d.ID},
		c2: {ID: c2, DiscountID: d.ID},
	}
	pv := FindApplicableDiscountsProductVariantInput{
		ProductVariantID: uuid.New(), Count: 1, CouponIds: []uuid.UUID{c1, c2},
	}
	_, err := filterApplicableDiscounts([]store.Discount{d}, remaining, pv, coupons)
	if err == nil {
		t.Fatal("expected duplicate-coupon error")
	}
	want := "Coupons with ids " + formatUUIDs([]uuid.UUID{c1, c2}) + " are used multiple times for the same discount " + d.ID.String()
	if err.Error() != want {
		t.Fatalf("error mismatch:\n got: %s\nwant: %s", err.Error(), want)
	}
}

func TestFormatUUIDs(t *testing.T) {
	if got := formatUUIDs(nil); got != "[]" {
		t.Fatalf("empty want [], got %q", got)
	}
	a := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	b := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	if got := formatUUIDs([]uuid.UUID{a, b}); got != "[00000000-0000-0000-0000-000000000001, 00000000-0000-0000-0000-000000000002]" {
		t.Fatalf("format mismatch, got %q", got)
	}
}

func intptr(i int) *int { return &i }
