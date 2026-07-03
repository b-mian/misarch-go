// Package events defines the Dapr pub/sub topics the shipment service both
// publishes and subscribes to, the payload structs it publishes, and thin
// publish helpers. Topic strings and JSON field names/casing are copied
// verbatim from the original Kotlin service (org.misarch.shipment.event) and
// must not drift — other services subscribe to these exact shapes.
//
// JSON casing: all published DTOs are plain Kotlin data classes serialized
// with Jackson defaults → property names verbatim (camelCase), no renames.
// UUIDs → strings; ShipmentStatus enum → its NAME string; no timestamps in any
// published payload. Nullable UUID fields (orderId/returnId) are serialized as
// JSON null when absent (NOT omitted), so they use *uuid.UUID without
// omitempty to match Jackson's data-class output.
package events

import (
	"context"

	"github.com/google/uuid"
)

// Published topics (mirrors ShipmentEvents; a shipment is created only in
// response to the subscribed topics below).
const (
	// TopicShipmentMethodCreated fires at the end of createShipmentMethod.
	TopicShipmentMethodCreated = "shipment/shipment-method/created"
	// TopicShipmentMethodArchived fires at the end of archiveShipmentMethod.
	TopicShipmentMethodArchived = "shipment/shipment-method/archived"
	// TopicShipmentCreated fires once per successfully created shipment.
	TopicShipmentCreated = "shipment/shipment/created"
	// TopicShipmentCreationFailed fires when a shipment creation attempt
	// throws (order path only converts failures to this event).
	TopicShipmentCreationFailed = "shipment/shipment/creation-failed"
	// TopicShipmentStatusUpdated fires at the end of updateShipmentStatus.
	TopicShipmentStatusUpdated = "shipment/shipment/status-updated"
)

// Subscribed topics (mirrors the five @Topic annotations on EventController).
const (
	TopicUserAddressCreated           = "address/user-address/created"
	TopicVendorAddressCreated         = "address/vendor-address/created"
	TopicProductVariantVersionCreated = "catalog/product-variant-version/created"
	TopicPaymentEnabled               = "payment/payment/payment-enabled"
	TopicReturnCreated                = "return/return/created"
)

// Route strings for the subscribed topics: the Dapr Spring SDK maps each
// @PostMapping("/subscription/${TOPIC}") with the topic string (slashes and
// all) embedded in the path. Reproduce exactly.
const (
	RouteUserAddressCreated           = "/subscription/address/user-address/created"
	RouteVendorAddressCreated         = "/subscription/address/vendor-address/created"
	RouteProductVariantVersionCreated = "/subscription/catalog/product-variant-version/created"
	RoutePaymentEnabled               = "/subscription/payment/payment/payment-enabled"
	RouteReturnCreated                = "/subscription/return/return/created"
)

// Publisher is the subset of *dapr.Client this package needs, so callers can
// inject the real client (or a fake in tests). *dapr.Client satisfies it.
type Publisher interface {
	Publish(ctx context.Context, topic string, payload any) error
}

// ShipmentMethodCreated is the payload of TopicShipmentMethodCreated
// (ShipmentMethodDTO).
type ShipmentMethodCreated struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	ExternalReference string    `json:"externalReference"`
	BaseFees          int       `json:"baseFees"`
	FeesPerItem       int       `json:"feesPerItem"`
	FeesPerKg         int       `json:"feesPerKg"`
}

// ShipmentMethodArchived is the payload of TopicShipmentMethodArchived
// (ArchiveShipmentMethodDTO).
type ShipmentMethodArchived struct {
	ID uuid.UUID `json:"id"`
}

// ShipmentCreated is the payload of TopicShipmentCreated (ShipmentDTO). Exactly
// one of OrderID/ReturnID is non-null; the other serializes as JSON null.
// Status is always "PENDING" at creation.
type ShipmentCreated struct {
	ID                uuid.UUID   `json:"id"`
	OrderID           *uuid.UUID  `json:"orderId"`
	ReturnID          *uuid.UUID  `json:"returnId"`
	Status            string      `json:"status"`
	OrderItemIDs      []uuid.UUID `json:"orderItemIds"`
	ShipmentMethodID  uuid.UUID   `json:"shipmentMethodId"`
	ShipmentAddressID uuid.UUID   `json:"shipmentAddressId"`
}

// ShipmentCreationFailed is the payload of TopicShipmentCreationFailed
// (ShipmentCreationFailedDTO). Field order mirrors the data class (irrelevant
// to JSON consumers). ShipmentAddressID is the input address id; Reason is
// e.message or "Unknown reason".
type ShipmentCreationFailed struct {
	OrderID           *uuid.UUID  `json:"orderId"`
	ReturnID          *uuid.UUID  `json:"returnId"`
	OrderItemIDs      []uuid.UUID `json:"orderItemIds"`
	ShipmentMethodID  uuid.UUID   `json:"shipmentMethodId"`
	ShipmentAddressID uuid.UUID   `json:"shipmentAddressId"`
	Reason            string      `json:"reason"`
}

// ShipmentStatusUpdated is the payload of TopicShipmentStatusUpdated
// (ShipmentStatusUpdatedDTO). Field order mirrors the data class: id,
// orderItemIds, orderId, returnId, status.
type ShipmentStatusUpdated struct {
	ID           uuid.UUID   `json:"id"`
	OrderItemIDs []uuid.UUID `json:"orderItemIds"`
	OrderID      *uuid.UUID  `json:"orderId"`
	ReturnID     *uuid.UUID  `json:"returnId"`
	Status       string      `json:"status"`
}

// PublishShipmentMethodCreated publishes a ShipmentMethodCreated event.
func PublishShipmentMethodCreated(ctx context.Context, p Publisher, e ShipmentMethodCreated) error {
	return p.Publish(ctx, TopicShipmentMethodCreated, e)
}

// PublishShipmentMethodArchived publishes a ShipmentMethodArchived event.
func PublishShipmentMethodArchived(ctx context.Context, p Publisher, e ShipmentMethodArchived) error {
	return p.Publish(ctx, TopicShipmentMethodArchived, e)
}

// PublishShipmentCreated publishes a ShipmentCreated event.
func PublishShipmentCreated(ctx context.Context, p Publisher, e ShipmentCreated) error {
	return p.Publish(ctx, TopicShipmentCreated, e)
}

// PublishShipmentCreationFailed publishes a ShipmentCreationFailed event.
func PublishShipmentCreationFailed(ctx context.Context, p Publisher, e ShipmentCreationFailed) error {
	return p.Publish(ctx, TopicShipmentCreationFailed, e)
}

// PublishShipmentStatusUpdated publishes a ShipmentStatusUpdated event.
func PublishShipmentStatusUpdated(ctx context.Context, p Publisher, e ShipmentStatusUpdated) error {
	return p.Publish(ctx, TopicShipmentStatusUpdated, e)
}
