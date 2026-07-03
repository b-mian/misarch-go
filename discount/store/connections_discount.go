package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// DiscountsByCategory lists discounts applying directly to a category:
// discountentity JOIN discounttocategoryentity ON discountid = discount.id
// WHERE discounttocategoryentity.categoryid = <categoryID>.
func (s *Store) DiscountsByCategory(
	ctx context.Context, categoryID uuid.UUID, first, skip *int, order []string, asc bool,
) (Connection[Discount], error) {
	var b condBuilder
	b.eq("discounttocategoryentity.categoryid", categoryID)
	where, args := b.where()
	p := page{
		table:     "discountentity",
		pk:        "discountentity.id",
		columns:   discountColumns,
		joins:     " INNER JOIN discounttocategoryentity ON discounttocategoryentity.discountid = discountentity.id",
		where:     where,
		whereArgs: args,
		orderCols: order,
		ascending: asc,
		first:     first,
		skip:      skip,
	}
	return paginate(ctx, s.pool, p, scanDiscounts)
}

// DiscountsByProduct lists discounts applying directly to a product.
func (s *Store) DiscountsByProduct(
	ctx context.Context, productID uuid.UUID, first, skip *int, order []string, asc bool,
) (Connection[Discount], error) {
	var b condBuilder
	b.eq("discounttoproductentity.productid", productID)
	where, args := b.where()
	p := page{
		table:     "discountentity",
		pk:        "discountentity.id",
		columns:   discountColumns,
		joins:     " INNER JOIN discounttoproductentity ON discounttoproductentity.discountid = discountentity.id",
		where:     where,
		whereArgs: args,
		orderCols: order,
		ascending: asc,
		first:     first,
		skip:      skip,
	}
	return paginate(ctx, s.pool, p, scanDiscounts)
}

// DiscountsByProductVariant lists discounts applying directly to a product
// variant.
func (s *Store) DiscountsByProductVariant(
	ctx context.Context, productVariantID uuid.UUID, first, skip *int, order []string, asc bool,
) (Connection[Discount], error) {
	var b condBuilder
	b.eq("discounttoproductvariantentity.productvariantid", productVariantID)
	where, args := b.where()
	p := page{
		table:     "discountentity",
		pk:        "discountentity.id",
		columns:   discountColumns,
		joins:     " INNER JOIN discounttoproductvariantentity ON discounttoproductvariantentity.discountid = discountentity.id",
		where:     where,
		whereArgs: args,
		orderCols: order,
		ascending: asc,
		first:     first,
		skip:      skip,
	}
	return paginate(ctx, s.pool, p, scanDiscounts)
}

// ApplicableDiscounts lists the discounts that currently apply to a product
// variant for the (possibly anonymous) authorized user, mirroring
// generateFullDiscountAppliesCondition: the discount targets the variant, its
// product, or any category of that product; AND either requires no coupon or
// (if authenticated) the user owns a coupon for it; AND is currently valid. It
// does NOT consult minOrderAmount or per-user usage limits, and (matching the
// ProductVariant.applicableDiscounts DiscountConnection, which passes no join)
// there is NO join — the coupon check uses correlated EXISTS subqueries. now is
// the SQL evaluation time (server clock), bound as a parameter.
func (s *Store) ApplicableDiscounts(
	ctx context.Context, productVariantID uuid.UUID, authUser *AuthUser,
	now time.Time, first, skip *int, order []string, asc bool,
) (Connection[Discount], error) {
	var b condBuilder

	appliesCondition(&b, productVariantID)

	// coupon condition
	noCoupon := noCouponsCondition()
	if authUser == nil {
		b.addRaw(noCoupon)
	} else {
		userHas := userHasCouponForDiscountCondition(&b, authUser.ID)
		b.addRaw("(" + noCoupon + " OR " + userHas + ")")
	}

	// currently valid
	nowFrom := b.placeholder(now)
	nowUntil := b.placeholder(now)
	b.addRaw("discountentity.validfrom <= " + nowFrom + " AND discountentity.validuntil >= " + nowUntil)

	where, args := b.where()
	p := page{
		table:     "discountentity",
		pk:        "discountentity.id",
		columns:   discountColumns,
		where:     where,
		whereArgs: args,
		orderCols: order,
		ascending: asc,
		first:     first,
		skip:      skip,
	}
	return paginate(ctx, s.pool, p, scanDiscounts)
}

// appliesCondition appends the three-way "discount applies to this product
// variant" predicate (direct variant, owning product, or any category of that
// product) to b, mirroring generateDiscountAppliesCondition.
func appliesCondition(b *condBuilder, productVariantID uuid.UUID) {
	ph := b.placeholder(productVariantID)
	// Same product-variant id is used by all three subqueries; reuse one
	// placeholder to keep the SQL close to the original.
	direct := "discountentity.id IN (SELECT discounttoproductvariantentity.discountid FROM discounttoproductvariantentity WHERE discounttoproductvariantentity.productvariantid = " + ph + ")"
	viaProduct := "discountentity.id IN (SELECT discounttoproductentity.discountid FROM discounttoproductentity " +
		"JOIN productvariantentity ON discounttoproductentity.productid = productvariantentity.productid " +
		"WHERE productvariantentity.id = " + ph + ")"
	viaCategory := "discountentity.id IN (SELECT discounttocategoryentity.discountid FROM discounttocategoryentity " +
		"JOIN producttocategoryentity ON discounttocategoryentity.categoryid = producttocategoryentity.categoryid " +
		"JOIN productvariantentity ON producttocategoryentity.productid = productvariantentity.productid " +
		"WHERE productvariantentity.id = " + ph + ")"
	b.addRaw("(" + direct + " OR " + viaProduct + " OR " + viaCategory + ")")
}

// noCouponsCondition renders "the discount requires no coupon" (NOT EXISTS a
// coupon for it).
func noCouponsCondition() string {
	return "NOT EXISTS (SELECT 1 FROM couponentity WHERE couponentity.discountid = discountentity.id)"
}

// userHasCouponForDiscountCondition renders "the user owns a coupon for this
// discount" (correlated to discountentity.id), binding userID.
func userHasCouponForDiscountCondition(b *condBuilder, userID uuid.UUID) string {
	ph := b.placeholder(userID)
	return "EXISTS (SELECT 1 FROM couponentity JOIN couponredemptionentity ON couponentity.id = couponredemptionentity.couponid " +
		"WHERE couponentity.discountid = discountentity.id AND couponredemptionentity.userid = " + ph + ")"
}
