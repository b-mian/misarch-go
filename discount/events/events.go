// Package events defines the Dapr pub/sub topics the discount service emits and
// consumes, the payload structs on the wire, and thin publish helpers. Topic
// strings and JSON field names/casing are copied verbatim from the original
// Kotlin service (org.misarch.discount.event) and must not drift — other
// services subscribe to these exact shapes, and this service subscribes to
// theirs.
package events

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Topic constants mirror org.misarch.discount.event.DiscountEvents.
const (
	// Published by this service on writes.
	TopicDiscountCreated = "discount/discount/created"
	TopicDiscountUpdated = "discount/discount/updated"
	TopicCouponCreated   = "discount/coupon/created"
	TopicCouponUpdated   = "discount/coupon/updated"
	// Published by this service from the order-validation saga.
	TopicValidationSucceeded = "discount/order/validation-succeeded"
	TopicValidationFailed    = "discount/order/validation-failed"

	// Subscribed by this service.
	TopicUserCreated                   = "user/user/created"
	TopicProductCreated                = "catalog/product/created"
	TopicProductVariantCreated         = "catalog/product-variant/created"
	TopicCategoryCreated               = "catalog/category/created"
	TopicInventoryReservationSucceeded = "inventory/product-item/reservation-succeeded"
)

// Publisher is the subset of *dapr.Client this package needs; *dapr.Client
// satisfies it. Keeps events decoupled and testable.
type Publisher interface {
	Publish(ctx context.Context, topic string, payload any) error
}

// FormatTimestamp renders a timestamp the way the event DTOs do: the original
// formats OffsetDateTime with DateTimeFormatter.ISO_OFFSET_DATE_TIME (an
// ISO-8601 / RFC-3339 offset string, typically Z for the UTC values coming from
// TIMESTAMPTZ), NOT via the GraphQL scalar. RFC3339Nano over a UTC time
// produces the same wire shape (and trims trailing zero fractional digits).
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// DiscountDTO is the payload of discount/discount/{created,updated}. Field names
// are the Kotlin property names (camelCase; no @JsonProperty renames).
// validUntil/validFrom are ISO-8601 offset strings; discount is a JSON number;
// maxUsagesPerUser/minOrderAmount are number-or-null. The three id lists reflect
// the full current applies-to set of the discount.
type DiscountDTO struct {
	ID                                 uuid.UUID   `json:"id"`
	Discount                           float64     `json:"discount"`
	MaxUsagesPerUser                   *int        `json:"maxUsagesPerUser"`
	ValidUntil                         string      `json:"validUntil"`
	ValidFrom                          string      `json:"validFrom"`
	MinOrderAmount                     *int        `json:"minOrderAmount"`
	DiscountAppliesToCategoryIds       []uuid.UUID `json:"discountAppliesToCategoryIds"`
	DiscountAppliesToProductIds        []uuid.UUID `json:"discountAppliesToProductIds"`
	DiscountAppliesToProductVariantIds []uuid.UUID `json:"discountAppliesToProductVariantIds"`
}

// CouponDTO is the payload of discount/coupon/{created,updated}. Note: there is
// NO `usages` field (only maxUsages), matching the original CouponDTO.
type CouponDTO struct {
	ID         uuid.UUID `json:"id"`
	MaxUsages  *int      `json:"maxUsages"`
	ValidUntil string    `json:"validUntil"`
	ValidFrom  string    `json:"validFrom"`
	Code       string    `json:"code"`
	DiscountID uuid.UUID `json:"discountId"`
}

// ValidationSucceededDTO is the payload of discount/order/validation-succeeded:
// just the echoed order.
type ValidationSucceededDTO struct {
	Order OrderDTO `json:"order"`
}

// ValidationFailedDTO is the payload of discount/order/validation-failed: the
// echoed order plus the failing discount ids (empty list on the exception path).
type ValidationFailedDTO struct {
	Order              OrderDTO    `json:"order"`
	FailingDiscountIds []uuid.UUID `json:"failingDiscountIds"`
}

// Publish helpers ----------------------------------------------------------

func PublishDiscountCreated(ctx context.Context, p Publisher, e DiscountDTO) error {
	return p.Publish(ctx, TopicDiscountCreated, e)
}

func PublishDiscountUpdated(ctx context.Context, p Publisher, e DiscountDTO) error {
	return p.Publish(ctx, TopicDiscountUpdated, e)
}

func PublishCouponCreated(ctx context.Context, p Publisher, e CouponDTO) error {
	return p.Publish(ctx, TopicCouponCreated, e)
}

func PublishCouponUpdated(ctx context.Context, p Publisher, e CouponDTO) error {
	return p.Publish(ctx, TopicCouponUpdated, e)
}

func PublishValidationSucceeded(ctx context.Context, p Publisher, e ValidationSucceededDTO) error {
	return p.Publish(ctx, TopicValidationSucceeded, e)
}

func PublishValidationFailed(ctx context.Context, p Publisher, e ValidationFailedDTO) error {
	return p.Publish(ctx, TopicValidationFailed, e)
}
