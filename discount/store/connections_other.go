package store

import (
	"context"

	"github.com/google/uuid"
)

// CategoriesByDiscount lists categories a discount directly applies to
// (Discount.discountAppliesToCategories): categoryentity JOIN
// discounttocategoryentity ON categoryid = category.id WHERE discountid = <id>.
func (s *Store) CategoriesByDiscount(
	ctx context.Context, discountID uuid.UUID, first, skip *int, order []string, asc bool,
) (Connection[CategoryRow], error) {
	var b condBuilder
	b.eq("discounttocategoryentity.discountid", discountID)
	where, args := b.where()
	p := page{
		table:     "categoryentity",
		pk:        "categoryentity.id",
		columns:   categoryColumns,
		joins:     " INNER JOIN discounttocategoryentity ON discounttocategoryentity.categoryid = categoryentity.id",
		where:     where,
		whereArgs: args,
		orderCols: order,
		ascending: asc,
		first:     first,
		skip:      skip,
	}
	return paginate(ctx, s.pool, p, scanCategories)
}

// ProductsByDiscount lists products a discount directly applies to
// (Discount.discountAppliesToProducts).
func (s *Store) ProductsByDiscount(
	ctx context.Context, discountID uuid.UUID, first, skip *int, order []string, asc bool,
) (Connection[ProductRow], error) {
	var b condBuilder
	b.eq("discounttoproductentity.discountid", discountID)
	where, args := b.where()
	p := page{
		table:     "productentity",
		pk:        "productentity.id",
		columns:   productColumns,
		joins:     " INNER JOIN discounttoproductentity ON discounttoproductentity.productid = productentity.id",
		where:     where,
		whereArgs: args,
		orderCols: order,
		ascending: asc,
		first:     first,
		skip:      skip,
	}
	return paginate(ctx, s.pool, p, scanProducts)
}

// ProductVariantsByDiscount lists product variants a discount directly applies
// to (Discount.discountAppliesToProductVariants).
func (s *Store) ProductVariantsByDiscount(
	ctx context.Context, discountID uuid.UUID, first, skip *int, order []string, asc bool,
) (Connection[ProductVariantRow], error) {
	var b condBuilder
	b.eq("discounttoproductvariantentity.discountid", discountID)
	where, args := b.where()
	p := page{
		table:     "productvariantentity",
		pk:        "productvariantentity.id",
		columns:   productVariantColumns,
		joins:     " INNER JOIN discounttoproductvariantentity ON discounttoproductvariantentity.productvariantid = productvariantentity.id",
		where:     where,
		whereArgs: args,
		orderCols: order,
		ascending: asc,
		first:     first,
		skip:      skip,
	}
	return paginate(ctx, s.pool, p, scanProductVariants)
}

// UsersByCoupon lists users who claimed a coupon (Coupon.users): userentity
// JOIN couponredemptionentity ON userid = user.id WHERE couponid = <couponID>.
func (s *Store) UsersByCoupon(
	ctx context.Context, couponID uuid.UUID, first, skip *int, order []string, asc bool,
) (Connection[UserRow], error) {
	var b condBuilder
	b.eq("couponredemptionentity.couponid", couponID)
	where, args := b.where()
	p := page{
		table:     "userentity",
		pk:        "userentity.id",
		columns:   userColumns,
		joins:     " INNER JOIN couponredemptionentity ON couponredemptionentity.userid = userentity.id",
		where:     where,
		whereArgs: args,
		orderCols: order,
		ascending: asc,
		first:     first,
		skip:      skip,
	}
	return paginate(ctx, s.pool, p, scanUsers)
}

// DiscountUsagesByDiscount lists usages of a discount (Discount.discountUsages):
// discountusageentity WHERE discountid = <discountID>.
func (s *Store) DiscountUsagesByDiscount(
	ctx context.Context, discountID uuid.UUID, first, skip *int, order []string, asc bool,
) (Connection[DiscountUsage], error) {
	var b condBuilder
	b.eq("discountusageentity.discountid", discountID)
	where, args := b.where()
	p := page{
		table:     "discountusageentity",
		pk:        "discountusageentity.id",
		columns:   discountUsageColumns,
		where:     where,
		whereArgs: args,
		orderCols: order,
		ascending: asc,
		first:     first,
		skip:      skip,
	}
	return paginate(ctx, s.pool, p, scanDiscountUsages)
}

// DiscountUsagesByUser lists a user's discount usages (User.discountUsages):
// discountusageentity WHERE userid = <userID>.
func (s *Store) DiscountUsagesByUser(
	ctx context.Context, userID uuid.UUID, first, skip *int, order []string, asc bool,
) (Connection[DiscountUsage], error) {
	var b condBuilder
	b.eq("discountusageentity.userid", userID)
	where, args := b.where()
	p := page{
		table:     "discountusageentity",
		pk:        "discountusageentity.id",
		columns:   discountUsageColumns,
		where:     where,
		whereArgs: args,
		orderCols: order,
		ascending: asc,
		first:     first,
		skip:      skip,
	}
	return paginate(ctx, s.pool, p, scanDiscountUsages)
}
