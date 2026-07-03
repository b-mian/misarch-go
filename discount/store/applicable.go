package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// MissingProductVariants returns the subset of ids (order preserved, deduped by
// the caller as a set) that do not exist, for verifyProductVariants.
func (s *Store) MissingProductVariants(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error) {
	return missingIDs(ctx, s.pool, "productvariantentity", ids)
}

// CouponsByIDs loads the coupons with the given ids, returned as a map keyed by
// id (used by findApplicableDiscounts for existence + discountId lookup).
func (s *Store) CouponsByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Coupon, error) {
	out := map[uuid.UUID]Coupon{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		"SELECT "+couponColumns+" FROM couponentity WHERE couponentity.id = ANY($1)", ids)
	if err != nil {
		return nil, fmt.Errorf("load coupons: %w", err)
	}
	coupons, err := scanCoupons(rows)
	if err != nil {
		return nil, err
	}
	for _, c := range coupons {
		out[c.ID] = c
	}
	return out, nil
}

// DiscountsByIDs loads the discounts with the given ids (used by validateOrder
// to resolve the discounts referenced by order items).
func (s *Store) DiscountsByIDs(ctx context.Context, ids []uuid.UUID) ([]Discount, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		"SELECT "+discountColumns+" FROM discountentity WHERE discountentity.id = ANY($1)", ids)
	if err != nil {
		return nil, fmt.Errorf("load discounts: %w", err)
	}
	return scanDiscounts(rows)
}

// RemainingUsages computes, for the given discounts and user, the remaining
// per-user usages of each CAPPED discount (maxUsagesPerUser != nil):
// maxUsagesPerUser - currentUsages (0 if the user has no usage row). Uncapped
// discounts are omitted (treated as unlimited by callers). Mirrors
// findRemainingUsagesForDiscounts.
func (s *Store) RemainingUsages(ctx context.Context, userID uuid.UUID, discounts []Discount) (map[uuid.UUID]int64, error) {
	capped := make([]uuid.UUID, 0, len(discounts))
	maxByID := map[uuid.UUID]int{}
	for _, d := range discounts {
		if d.MaxUsagesPerUser != nil {
			capped = append(capped, d.ID)
			maxByID[d.ID] = *d.MaxUsagesPerUser
		}
	}
	current := map[uuid.UUID]int64{}
	if len(capped) > 0 {
		rows, err := s.pool.Query(ctx,
			"SELECT discountid, usages FROM discountusageentity WHERE userid = $1 AND discountid = ANY($2)",
			userID, capped)
		if err != nil {
			return nil, fmt.Errorf("load current usages: %w", err)
		}
		func() {
			defer rows.Close()
			for rows.Next() {
				var did uuid.UUID
				var usages int64
				if err = rows.Scan(&did, &usages); err != nil {
					return
				}
				current[did] = usages
			}
			err = rows.Err()
		}()
		if err != nil {
			return nil, err
		}
	}
	remaining := make(map[uuid.UUID]int64, len(capped))
	for _, id := range capped {
		remaining[id] = int64(maxByID[id]) - current[id]
	}
	return remaining, nil
}

// ApplicableDiscountsForVariant runs the per-product-variant applicability query
// of findApplicableDiscounts: the discount applies to the variant/product/
// category, AND (no coupon required OR the user owns one of couponIDs for it),
// AND is currently valid, AND satisfies minOrderAmount. now is the SQL
// evaluation time (server clock), bound as a parameter. Returns the matching
// discounts (LEFT JOIN on the user's usage row is a faithful no-op).
func (s *Store) ApplicableDiscountsForVariant(
	ctx context.Context, productVariantID uuid.UUID, couponIDs []uuid.UUID,
	userID uuid.UUID, orderAmount int, now time.Time,
) ([]Discount, error) {
	var b condBuilder

	// LEFT JOIN discountusageentity ON discountid = discount.id AND userid = :userId
	phUser := b.placeholder(userID)
	joins := " LEFT JOIN discountusageentity ON discountusageentity.discountid = discountentity.id AND discountusageentity.userid = " + phUser

	appliesCondition(&b, productVariantID)

	// (no coupon required) OR (user owns one of couponIDs for this discount)
	noCoupon := noCouponsCondition()
	userHas := userHasCouponFromListCondition(&b, couponIDs, userID)
	b.addRaw("(" + noCoupon + " OR " + userHas + ")")

	// currently valid
	nowFrom := b.placeholder(now)
	nowUntil := b.placeholder(now)
	b.addRaw("discountentity.validfrom <= " + nowFrom + " AND discountentity.validuntil >= " + nowUntil)

	// min order amount: minorderamount IS NULL OR minorderamount <= orderAmount
	phAmount := b.placeholder(orderAmount)
	b.addRaw("(discountentity.minorderamount IS NULL OR discountentity.minorderamount <= " + phAmount + ")")

	where, args := b.where()
	sql := "SELECT " + discountColumns + " FROM discountentity" + joins + " WHERE " + where
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("applicable discounts for variant: %w", err)
	}
	return scanDiscounts(rows)
}

// userHasCouponFromListCondition renders the generateUserHasCouponCondition:
// the discount id is among discounts of a coupon in couponIDs that userID has
// redeemed. An empty couponIDs list yields "IN (NULL)" semantics — never true —
// matching an empty QueryDSL `in`.
func userHasCouponFromListCondition(b *condBuilder, couponIDs []uuid.UUID, userID uuid.UUID) string {
	phCoupons := b.placeholder(couponIDs)
	phUser := b.placeholder(userID)
	return "discountentity.id IN (SELECT couponentity.discountid FROM couponentity " +
		"JOIN couponredemptionentity ON couponredemptionentity.couponid = couponentity.id " +
		"WHERE couponentity.id = ANY(" + phCoupons + ") AND couponredemptionentity.userid = " + phUser + ")"
}
