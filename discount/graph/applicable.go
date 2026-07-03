package graph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"misarch/discount/store"
)

// findApplicableDiscounts ports DiscountService.findApplicableDiscounts (§9.6):
// per product-variant it finds the applicable discounts, then filters them
// against the user's remaining per-user usage budget (mutated across variants in
// input order, so earlier variants consume budget first), and validates the
// passed coupons. Returns one DiscountsForProductVariant per input variant, in
// input order.
func (r *Resolver) findApplicableDiscounts(ctx context.Context, input FindApplicableDiscountsInput) ([]DiscountsForProductVariant, error) {
	// 1. verify all product variants exist.
	pvIDs := uniqueUUIDs(mapProductVariantIDs(input.ProductVariants))
	missing, err := r.Store.MissingProductVariants(ctx, pvIDs)
	if err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("Product variant(s) with id(s) %s do(es) not exist", formatUUIDs(missing))
	}

	// now is evaluated once per applicability query (server clock), matching the
	// per-query OffsetDateTime.now() constant.
	now := time.Now().UTC()

	// 2. per-variant applicable discounts.
	perVariant := make([][]store.Discount, len(input.ProductVariants))
	for i, pv := range input.ProductVariants {
		discounts, err := r.Store.ApplicableDiscountsForVariant(
			ctx, pv.ProductVariantID, uniqueUUIDs(pv.CouponIds), input.UserID, input.OrderAmount, now)
		if err != nil {
			return nil, err
		}
		perVariant[i] = discounts
	}

	// 3. remaining per-user usages for the union of discounts found (capped
	// discounts only; uncapped are unlimited and absent from the map).
	allDiscounts := dedupeDiscountsByID(perVariant)
	remaining, err := r.Store.RemainingUsages(ctx, input.UserID, allDiscounts)
	if err != nil {
		return nil, err
	}

	// 4. verify every passed coupon exists.
	couponIDs := uniqueUUIDs(flattenCouponIDs(input.ProductVariants))
	couponsByID, err := r.Store.CouponsByIDs(ctx, couponIDs)
	if err != nil {
		return nil, err
	}
	var missingCoupons []uuid.UUID
	for _, id := range couponIDs {
		if _, ok := couponsByID[id]; !ok {
			missingCoupons = append(missingCoupons, id)
		}
	}
	if len(missingCoupons) > 0 {
		return nil, fmt.Errorf("Coupon(s) with id(s) %s do(es) not exist", formatUUIDs(missingCoupons))
	}

	// 5. filter per variant, mutating the remaining-usage budget in input order.
	result := make([]DiscountsForProductVariant, len(input.ProductVariants))
	for i, pv := range input.ProductVariants {
		dfpv, err := filterApplicableDiscounts(perVariant[i], remaining, pv, couponsByID)
		if err != nil {
			return nil, err
		}
		result[i] = dfpv
	}
	return result, nil
}

// filterApplicableDiscounts ports DiscountService.filterApplicableDiscounts:
// keep the discounts the user can still use for this variant's count (untracked/
// unlimited always kept), decrement the tracked budget, then validate that every
// passed coupon maps to a kept discount and that no discount has more than one
// coupon.
func filterApplicableDiscounts(
	discounts []store.Discount, remaining map[uuid.UUID]int64,
	pv FindApplicableDiscountsProductVariantInput, couponsByID map[uuid.UUID]store.Coupon,
) (DiscountsForProductVariant, error) {
	count := int64(pv.Count)

	usable := make([]store.Discount, 0, len(discounts))
	for _, d := range discounts {
		if rem, tracked := remaining[d.ID]; !tracked || rem >= count {
			usable = append(usable, d)
		}
	}
	// Decrement the tracked budget for each kept discount (uncapped discounts
	// are not in the map and stay unlimited).
	for _, d := range usable {
		if _, tracked := remaining[d.ID]; tracked {
			remaining[d.ID] -= count
		}
	}

	keptByDiscount := map[uuid.UUID]struct{}{}
	for _, d := range usable {
		keptByDiscount[d.ID] = struct{}{}
	}

	// Every coupon must map to a kept discount (existence already verified).
	for _, cid := range pv.CouponIds {
		coupon := couponsByID[cid]
		if _, ok := keptByDiscount[coupon.DiscountID]; !ok {
			return DiscountsForProductVariant{}, fmt.Errorf(
				"Coupon with id %s could not be used, either due to insufficient remaining usages, the user not owning the coupon, or the coupon not being applicable", cid)
		}
	}

	// No discount may have more than one coupon in this variant.
	couponsByDiscount := map[uuid.UUID][]uuid.UUID{}
	order := []uuid.UUID{}
	for _, cid := range pv.CouponIds {
		did := couponsByID[cid].DiscountID
		if _, seen := couponsByDiscount[did]; !seen {
			order = append(order, did)
		}
		couponsByDiscount[did] = append(couponsByDiscount[did], cid)
	}
	for _, did := range order {
		if ids := couponsByDiscount[did]; len(ids) > 1 {
			return DiscountsForProductVariant{}, fmt.Errorf(
				"Coupons with ids %s are used multiple times for the same discount %s", formatUUIDs(ids), did)
		}
	}

	nodes := make([]Discount, len(usable))
	for i, d := range usable {
		nodes[i] = *toDiscount(d)
	}
	return DiscountsForProductVariant{
		ProductVariantID: pv.ProductVariantID,
		Count:            pv.Count,
		Discounts:        nodes,
	}, nil
}

// --- small slice helpers --------------------------------------------------

func mapProductVariantIDs(pvs []FindApplicableDiscountsProductVariantInput) []uuid.UUID {
	out := make([]uuid.UUID, len(pvs))
	for i, pv := range pvs {
		out[i] = pv.ProductVariantID
	}
	return out
}

func flattenCouponIDs(pvs []FindApplicableDiscountsProductVariantInput) []uuid.UUID {
	var out []uuid.UUID
	for _, pv := range pvs {
		out = append(out, pv.CouponIds...)
	}
	return out
}

// uniqueUUIDs dedupes preserving first-occurrence order (Kotlin toSet()).
func uniqueUUIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// dedupeDiscountsByID flattens per-variant discounts into a unique-by-id slice.
func dedupeDiscountsByID(perVariant [][]store.Discount) []store.Discount {
	seen := map[uuid.UUID]struct{}{}
	var out []store.Discount
	for _, ds := range perVariant {
		for _, d := range ds {
			if _, ok := seen[d.ID]; ok {
				continue
			}
			seen[d.ID] = struct{}{}
			out = append(out, d)
		}
	}
	return out
}

// formatUUIDs renders a UUID slice as a Kotlin list literal "[a, b]" for the
// error messages (canonical lowercase, comma-space). Empty renders "[]".
func formatUUIDs(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
