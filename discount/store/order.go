package store

// Order-column sets for every connection, mirroring the original *OrderField
// enums. Each non-ID field carries a secondary `id` tiebreaker; the chosen
// direction is applied to every column in the list (see BaseOrder.toOrderSpecifier
// and the *OrderField enum expression arrays). Columns are qualified with the
// entity alias so they are unambiguous under the connection joins.
//
// The graph layer selects one of these from the GraphQL order enum + a bool
// direction, then hands them to a connection query.

// CommonOrderField: only ID (used by CategoryConnection, ProductConnection,
// ProductVariantConnection, UserConnection).
var (
	CategoryOrderByID       = []string{"categoryentity.id"}
	ProductOrderByID        = []string{"productentity.id"}
	ProductVariantOrderByID = []string{"productvariantentity.id"}
	UserOrderByID           = []string{"userentity.id"}
)

// CouponOrderField: ID, VALID_FROM, VALID_UNTIL.
var (
	CouponOrderByID         = []string{"couponentity.id"}
	CouponOrderByValidFrom  = []string{"couponentity.validfrom", "couponentity.id"}
	CouponOrderByValidUntil = []string{"couponentity.validuntil", "couponentity.id"}
)

// DiscountOrderField: ID, VALID_FROM, VALID_UNTIL.
var (
	DiscountOrderByID         = []string{"discountentity.id"}
	DiscountOrderByValidFrom  = []string{"discountentity.validfrom", "discountentity.id"}
	DiscountOrderByValidUntil = []string{"discountentity.validuntil", "discountentity.id"}
)

// DiscountUsageOrderField: ID, USAGES.
var (
	DiscountUsageOrderByID     = []string{"discountusageentity.id"}
	DiscountUsageOrderByUsages = []string{"discountusageentity.usages", "discountusageentity.id"}
)
