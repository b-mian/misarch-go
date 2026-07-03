package graph

import (
	"github.com/99designs/gqlgen/graphql"

	"misarch/discount/events"
	"misarch/discount/store"
)

// optionalInt translates a gqlgen Omittable[*int] to the store's tri-state
// OptionalInt: absent -> Set:false (leave unchanged); explicit null -> Set:true,
// Value:nil (clear); a value -> Set:true, Value:&v. This is the load-bearing
// OptionalInput semantics for updateDiscount.maxUsagesPerUser/minOrderAmount and
// updateCoupon.maxUsages.
func optionalInt(o graphql.Omittable[*int]) store.OptionalInt {
	return store.OptionalInt{Set: o.IsSet(), Value: o.Value()}
}

// discountEvent builds the DiscountDTO event payload for a discount and its
// applies-to id sets, formatting the timestamps as ISO-8601 offset strings.
func discountEvent(d store.Discount, applies store.AppliesTo) events.DiscountDTO {
	return events.DiscountDTO{
		ID:                                 d.ID,
		Discount:                           d.Discount,
		MaxUsagesPerUser:                   d.MaxUsagesPerUser,
		ValidUntil:                         events.FormatTimestamp(d.ValidUntil),
		ValidFrom:                          events.FormatTimestamp(d.ValidFrom),
		MinOrderAmount:                     d.MinOrderAmount,
		DiscountAppliesToCategoryIds:       applies.CategoryIDs,
		DiscountAppliesToProductIds:        applies.ProductIDs,
		DiscountAppliesToProductVariantIds: applies.ProductVariantIDs,
	}
}

// couponEvent builds the CouponDTO event payload for a coupon (no usages field),
// formatting the timestamps as ISO-8601 offset strings.
func couponEvent(c store.Coupon) events.CouponDTO {
	return events.CouponDTO{
		ID:         c.ID,
		MaxUsages:  c.MaxUsages,
		ValidUntil: events.FormatTimestamp(c.ValidUntil),
		ValidFrom:  events.FormatTimestamp(c.ValidFrom),
		Code:       c.Code,
		DiscountID: c.DiscountID,
	}
}
