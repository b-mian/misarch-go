package graph

import (
	"misarch/discount/store"
)

// This file maps store rows to GraphQL models and GraphQL order inputs to store
// order-column sets + direction. Relationship fields (Discount, User, the
// connections, and Coupon.usages/maxUsages) are intentionally left for their own
// field resolvers; the extraFields (DiscountID, UserID, UsagesValue,
// MaxUsagesValue) carry the values/FKs those resolvers need.

// --- row -> model ---------------------------------------------------------

func toDiscount(d store.Discount) *Discount {
	return &Discount{
		ID:               d.ID,
		Discount:         d.Discount,
		MaxUsagesPerUser: d.MaxUsagesPerUser,
		MinOrderAmount:   d.MinOrderAmount,
		ValidFrom:        d.ValidFrom,
		ValidUntil:       d.ValidUntil,
	}
}

func toCoupon(c store.Coupon) *Coupon {
	return &Coupon{
		ID:         c.ID,
		Code:       c.Code,
		ValidFrom:  c.ValidFrom,
		ValidUntil: c.ValidUntil,
		// usages/maxUsages are employee-gated field resolvers; the raw values
		// ride along as extraFields. discount is a field resolver keyed by
		// DiscountID.
		UsagesValue:    c.Usages,
		MaxUsagesValue: c.MaxUsages,
		DiscountID:     c.DiscountID,
	}
}

func toDiscountUsage(u store.DiscountUsage) *DiscountUsage {
	return &DiscountUsage{
		ID: u.ID,
		// BIGINT narrowed to Int, matching the original .toInt().
		Usages:     int(u.Usages),
		DiscountID: u.DiscountID,
		UserID:     u.UserID,
	}
}

func toCategory(c store.CategoryRow) Category { return Category{ID: c.ID} }
func toProduct(p store.ProductRow) Product    { return Product{ID: p.ID} }
func toUser(u store.UserRow) User             { return User{ID: u.ID} }
func toProductVariant(p store.ProductVariantRow) ProductVariant {
	return ProductVariant{ID: p.ID}
}

// --- connection row -> model connection -----------------------------------

func toDiscountConnection(c store.Connection[store.Discount]) *DiscountConnection {
	nodes := make([]Discount, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toDiscount(n)
	}
	return &DiscountConnection{Nodes: nodes, TotalCount: c.TotalCount, HasNextPage: c.HasNextPage}
}

func toCouponConnection(c store.Connection[store.Coupon]) *CouponConnection {
	nodes := make([]Coupon, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toCoupon(n)
	}
	return &CouponConnection{Nodes: nodes, TotalCount: c.TotalCount, HasNextPage: c.HasNextPage}
}

func toDiscountUsageConnection(c store.Connection[store.DiscountUsage]) *DiscountUsageConnection {
	nodes := make([]DiscountUsage, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toDiscountUsage(n)
	}
	return &DiscountUsageConnection{Nodes: nodes, TotalCount: c.TotalCount, HasNextPage: c.HasNextPage}
}

func toCategoryConnection(c store.Connection[store.CategoryRow]) *CategoryConnection {
	nodes := make([]Category, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = toCategory(n)
	}
	return &CategoryConnection{Nodes: nodes, TotalCount: c.TotalCount, HasNextPage: c.HasNextPage}
}

func toProductConnection(c store.Connection[store.ProductRow]) *ProductConnection {
	nodes := make([]Product, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = toProduct(n)
	}
	return &ProductConnection{Nodes: nodes, TotalCount: c.TotalCount, HasNextPage: c.HasNextPage}
}

func toProductVariantConnection(c store.Connection[store.ProductVariantRow]) *ProductVariantConnection {
	nodes := make([]ProductVariant, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = toProductVariant(n)
	}
	return &ProductVariantConnection{Nodes: nodes, TotalCount: c.TotalCount, HasNextPage: c.HasNextPage}
}

func toUserConnection(c store.Connection[store.UserRow]) *UserConnection {
	nodes := make([]User, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = toUser(n)
	}
	return &UserConnection{Nodes: nodes, TotalCount: c.TotalCount, HasNextPage: c.HasNextPage}
}

// --- order input -> store columns + direction -----------------------------

// ascending reports whether an OrderDirection means ascending; nil defaults to
// ASC (matching OrderDirection.ASC default in BaseOrder).
func ascending(d *OrderDirection) bool {
	return d == nil || *d != OrderDirectionDesc
}

// Order/filter input fields are graphql.Omittable[*T] (a side effect of the
// nullable_input_omittable setting needed for the update mutations' tri-state).
// For ordering and the coupon filter, "absent" and "explicit null" both mean
// "use the default", so .Value() (nil for either) is exactly the right unwrap.

// commonOrder resolves a CommonOrderInput (only ID) for a connection whose
// id column set is given by idCols.
func commonOrder(in *CommonOrderInput, idCols []string) ([]string, bool) {
	if in == nil {
		return idCols, true
	}
	return idCols, ascending(in.Direction.Value())
}

// couponOrder resolves a CouponOrderInput to a coupon order-column set.
func couponOrder(in *CouponOrderInput) ([]string, bool) {
	if in == nil {
		return store.CouponOrderByID, true
	}
	cols := store.CouponOrderByID
	if field := in.Field.Value(); field != nil {
		switch *field {
		case CouponOrderFieldValidFrom:
			cols = store.CouponOrderByValidFrom
		case CouponOrderFieldValidUntil:
			cols = store.CouponOrderByValidUntil
		}
	}
	return cols, ascending(in.Direction.Value())
}

// discountOrder resolves a DiscountOrderInput to a discount order-column set.
func discountOrder(in *DiscountOrderInput) ([]string, bool) {
	if in == nil {
		return store.DiscountOrderByID, true
	}
	cols := store.DiscountOrderByID
	if field := in.Field.Value(); field != nil {
		switch *field {
		case DiscountOrderFieldValidFrom:
			cols = store.DiscountOrderByValidFrom
		case DiscountOrderFieldValidUntil:
			cols = store.DiscountOrderByValidUntil
		}
	}
	return cols, ascending(in.Direction.Value())
}

// discountUsageOrder resolves a DiscountUsageOrderInput to a usage order-column
// set.
func discountUsageOrder(in *DiscountUsageOrderInput) ([]string, bool) {
	if in == nil {
		return store.DiscountUsageOrderByID, true
	}
	cols := store.DiscountUsageOrderByID
	if field := in.Field.Value(); field != nil && *field == DiscountUsageOrderFieldUsages {
		cols = store.DiscountUsageOrderByUsages
	}
	return cols, ascending(in.Direction.Value())
}

// couponFilter maps a GraphQL CouponFilterInput to the store filter.
func couponFilter(in *CouponFilterInput) *store.CouponFilter {
	if in == nil {
		return nil
	}
	return &store.CouponFilter{UserHasCoupon: in.UserHasCoupon.Value()}
}
