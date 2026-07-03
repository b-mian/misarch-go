package store

import (
	"context"

	"github.com/google/uuid"
)

// CouponFilter models CouponFilterInput. UserHasCoupon, when set, restricts to
// coupons redeemed by that user (with an employee check performed in the graph
// layer before the query, matching the original toExpression).
type CouponFilter struct {
	UserHasCoupon *uuid.UUID
}

// CouponsByDiscount lists the coupons of a discount (Discount.discountRequiresCoupon).
// The base predicate is couponentity.discountid = <discountID>; the filter and
// the CouponConnection authorizedUserFilter are AND-ed on top (§6). There is no
// join.
func (s *Store) CouponsByDiscount(
	ctx context.Context, discountID uuid.UUID, filter *CouponFilter, authUser *AuthUser,
	first, skip *int, order []string, asc bool,
) (Connection[Coupon], error) {
	var b condBuilder
	b.eq("couponentity.discountid", discountID)
	return s.couponConnection(ctx, &b, "", filter, authUser, first, skip, order, asc)
}

// CouponsByUser lists the coupons a user has claimed (User.coupons):
// couponentity JOIN couponredemptionentity ON couponid = coupon.id WHERE
// couponredemptionentity.userid = <userID>, plus filter + authorizedUserFilter.
func (s *Store) CouponsByUser(
	ctx context.Context, userID uuid.UUID, filter *CouponFilter, authUser *AuthUser,
	first, skip *int, order []string, asc bool,
) (Connection[Coupon], error) {
	var b condBuilder
	b.eq("couponredemptionentity.userid", userID)
	joins := " INNER JOIN couponredemptionentity ON couponredemptionentity.couponid = couponentity.id"
	return s.couponConnection(ctx, &b, joins, filter, authUser, first, skip, order, asc)
}

// couponConnection finishes a coupon connection: it appends the filter and the
// authorizedUserFilter to the (already seeded) condition builder, then paginates.
func (s *Store) couponConnection(
	ctx context.Context, b *condBuilder, joins string, filter *CouponFilter, authUser *AuthUser,
	first, skip *int, order []string, asc bool,
) (Connection[Coupon], error) {
	if filter != nil && filter.UserHasCoupon != nil {
		b.addRaw(userHasCouponSQL(b, *filter.UserHasCoupon))
	}
	b.addRaw(couponAuthFilterSQL(b, authUser))

	where, args := b.where()
	p := page{
		table:     "couponentity",
		pk:        "couponentity.id",
		columns:   couponColumns,
		joins:     joins,
		where:     where,
		whereArgs: args,
		orderCols: order,
		ascending: asc,
		first:     first,
		skip:      skip,
	}
	return paginate(ctx, s.pool, p, scanCoupons)
}
