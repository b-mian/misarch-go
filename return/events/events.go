// Package events defines the Dapr pub/sub payloads the return service emits and
// consumes, plus thin helpers. Topic strings and JSON field names/casing are
// copied verbatim from the original Kotlin service and must not drift — other
// services publish/subscribe these exact shapes.
package events

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Topic constants mirror org.misarch.returns.event.ReturnEvents.
const (
	// TopicReturnCreated is published once at the end of a successful
	// createReturn mutation (after the return row and order-item links commit).
	TopicReturnCreated = "return/return/created"

	// TopicShipmentCreated is subscribed: materializes shipments + order items.
	TopicShipmentCreated = "shipment/shipment/created"
	// TopicShipmentStatusUpdated is subscribed: sets deliveredAt on DELIVERED.
	TopicShipmentStatusUpdated = "shipment/shipment/status-updated"
	// TopicOrderCreated is subscribed: materializes orders + order items.
	TopicOrderCreated = "order/order/created"

	// TopicProductVariantVersionCreated is the intended (but NEVER subscribed —
	// see BUG-1) topic for product-variant-version return windows. The original
	// handler lacks its annotations, so the topic is not registered and the pvv
	// table stays empty. Kept here for documentation only; do NOT subscribe it
	// in a bug-for-bug port.
	TopicProductVariantVersionCreated = "catalog/product-variant-version/created"
)

// Route strings for the subscribed topics (POST /subscription/<topic>).
const (
	RouteShipmentCreated       = "/subscription/" + TopicShipmentCreated
	RouteShipmentStatusUpdated = "/subscription/" + TopicShipmentStatusUpdated
	RouteOrderCreated          = "/subscription/" + TopicOrderCreated
)

// Publisher is the subset of *dapr.Client this package needs, so callers can
// inject the real client (or a fake in tests).
type Publisher interface {
	Publish(ctx context.Context, topic string, payload any) error
}

// ReturnCreated is the payload of TopicReturnCreated. Mirrors ReturnDTO:
// Jackson default naming = the Kotlin property names (camelCase). Note the two
// widths of the refunded amount across the codebase: this event carries the
// full int64 (Kotlin Long), while the GraphQL Return.refundedAmount narrows to
// a 32-bit Int. createdAt is a STRING formatted with a numeric offset
// (ISO_OFFSET_DATE_TIME → "+00:00"), NOT the "Z" form the GraphQL scalar emits.
type ReturnCreated struct {
	ID             uuid.UUID   `json:"id"`
	OrderID        uuid.UUID   `json:"orderId"`
	OrderItemIds   []uuid.UUID `json:"orderItemIds"`
	Reason         string      `json:"reason"`
	RefundedAmount int64       `json:"refundedAmount"`
	CreatedAt      string      `json:"createdAt"`
}

// FormatEventCreatedAt renders a timestamp the way ReturnEntity.toEventDTO does:
// Java DateTimeFormatter.ISO_OFFSET_DATE_TIME, i.e. RFC-3339 with a NUMERIC UTC
// offset ("+00:00", not "Z") and variable sub-second precision (trailing zeros
// and the decimal point omitted when zero). Distinct from the GraphQL DateTime
// scalar, which emits "Z" for UTC.
func FormatEventCreatedAt(t time.Time) string {
	// -07:00 (rather than Z07:00) forces a numeric offset even for UTC, giving
	// "+00:00"; .999999999 trims trailing-zero fractional digits like the Java
	// ISO fraction printer.
	return t.UTC().Format("2006-01-02T15:04:05.999999999-07:00")
}

// PublishReturnCreated publishes a ReturnCreated event.
func PublishReturnCreated(ctx context.Context, p Publisher, e ReturnCreated) error {
	return p.Publish(ctx, TopicReturnCreated, e)
}

// --- Inbound event payloads (CloudEvent "data" bodies) ---

// ShipmentStatus is the status enum carried by shipment events; it is never
// persisted, only used to decide deliveredAt.
type ShipmentStatus string

const (
	ShipmentStatusPending    ShipmentStatus = "PENDING"
	ShipmentStatusInProgress ShipmentStatus = "IN_PROGRESS"
	ShipmentStatusDelivered  ShipmentStatus = "DELIVERED"
	ShipmentStatusFailed     ShipmentStatus = "FAILED"
)

// ShipmentCreated is the data of TopicShipmentCreated (ShipmentDTO). orderId is
// nullable: a null orderId means the shipment belongs to a return and is ignored.
type ShipmentCreated struct {
	ID           uuid.UUID      `json:"id"`
	OrderID      *uuid.UUID     `json:"orderId"`
	Status       ShipmentStatus `json:"status"`
	OrderItemIds []uuid.UUID    `json:"orderItemIds"`
}

// ShipmentStatusUpdated is the data of TopicShipmentStatusUpdated
// (ShipmentStatusUpdatedDTO).
type ShipmentStatusUpdated struct {
	ID     uuid.UUID      `json:"id"`
	Status ShipmentStatus `json:"status"`
}

// OrderCreated is the data of TopicOrderCreated (OrderDTO).
type OrderCreated struct {
	ID         uuid.UUID       `json:"id"`
	UserID     uuid.UUID       `json:"userId"`
	OrderItems []OrderItemData `json:"orderItems"`
}

// OrderItemData is one element of OrderCreated.OrderItems (OrderItemDTO).
type OrderItemData struct {
	ID                      uuid.UUID `json:"id"`
	ProductVariantVersionID uuid.UUID `json:"productVariantVersionId"`
	CompensatableAmount     int64     `json:"compensatableAmount"`
}

// ProductVariantVersionCreated is the data of TopicProductVariantVersionCreated
// (ProductVariantVersionDTO). Defined for completeness; the topic is not
// subscribed (BUG-1).
type ProductVariantVersionCreated struct {
	ID                   uuid.UUID `json:"id"`
	CanBeReturnedForDays *int      `json:"canBeReturnedForDays"`
}
