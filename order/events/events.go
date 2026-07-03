// Package events defines the Dapr pub/sub surface of the order service: the
// topic/route constants, the incoming CloudEvent-data shapes, the outgoing
// event DTOs, and the compensation saga triggered by the shipment-failed event.
//
// Topic strings, route strings and JSON field names/casing are copied VERBATIM
// from the original Rust service, including its idiosyncrasies (the
// order/order-compensation/created payload is snake_case while
// order/order/created is camelCase; the shipment-failed route is served but not
// advertised in /dapr/subscribe; publish responses are not status-checked).
package events

import (
	"github.com/google/uuid"
)

// Subscribed / published topic strings, verbatim from the Rust service.
const (
	// Subscribed.
	TopicProductVariantUpdated        = "catalog/product-variant/updated"
	TopicProductVariantVersionCreated = "catalog/product-variant-version/created"
	TopicCouponCreated                = "discount/coupon/created"
	TopicTaxRateVersionCreated        = "tax/tax-rate-version/created"
	TopicShipmentMethodCreated        = "shipment/shipment-method/created"
	TopicUserCreated                  = "user/user/created"
	TopicUserAddressCreated           = "address/user-address/created"
	TopicUserAddressArchived          = "address/user-address/archived"
	// Subscribed via a declarative subscription (NOT in /dapr/subscribe), but
	// its route is served and the handler asserts this topic.
	TopicShipmentCreationFailed = "shipment/shipment/creation-failed"

	// Published.
	TopicOrderCreated             = "order/order/created"
	TopicOrderCompensationCreated = "order/order-compensation/created"
)

// Dapr delivery route paths served by the service. Several topics share
// /on-id-creation-event (coupon, shipment-method, user).
const (
	RouteIDCreation             = "/on-id-creation-event"
	RouteProductVariantVersion  = "/on-product-variant-version-creation-event"
	RouteProductVariantUpdated  = "/on-product-variant-updated-event"
	RouteTaxRateVersion         = "/on-tax-rate-version-creation-event"
	RouteUserAddressCreated     = "/on-user-address-creation-event"
	RouteUserAddressArchived    = "/on-user-address-archived-event"
	RouteShipmentCreationFailed = "/on-shipment-creation-failed-event"
)

// Envelope is the CloudEvent subset every handler reads: the topic (asserted
// against the expected value) and the typed data payload.
type Envelope[T any] struct {
	Topic string `json:"topic"`
	Data  T      `json:"data"`
}

// ---------------------------------------------------------------------------
// Incoming event-data shapes (spec §4).
// ---------------------------------------------------------------------------

// UUIDEventData is the payload of /on-id-creation-event (coupon, shipment
// method, user creation): {"id": UUID}.
type UUIDEventData struct {
	ID uuid.UUID `json:"id"`
}

// ProductVariantVersionEventData is the payload of
// catalog/product-variant-version/created (camelCase).
type ProductVariantVersionEventData struct {
	ID               uuid.UUID `json:"id"`
	RetailPrice      uint32    `json:"retailPrice"`
	TaxRateID        uuid.UUID `json:"taxRateId"`
	ProductVariantID uuid.UUID `json:"productVariantId"`
}

// UpdateProductVariantEventData is the payload of catalog/product-variant/updated
// (camelCase). isPubliclyVisible is deserialized as a STRING (not bool),
// matching the original — the value is stored verbatim (as a string).
type UpdateProductVariantEventData struct {
	ID                uuid.UUID `json:"id"`
	IsPubliclyVisible string    `json:"isPubliclyVisible"`
}

// TaxRateVersionEventData is the payload of tax/tax-rate-version/created
// (camelCase).
type TaxRateVersionEventData struct {
	ID        uuid.UUID `json:"id"`
	Rate      float64   `json:"rate"`
	Version   uint32    `json:"version"`
	TaxRateID uuid.UUID `json:"taxRateId"`
}

// UserAddressEventData is the payload of address/user-address/created and
// address/user-address/archived (camelCase).
type UserAddressEventData struct {
	ID     uuid.UUID `json:"id"`
	UserID uuid.UUID `json:"userId"`
}

// ShipmentFailedEventData is the payload of shipment/shipment/creation-failed
// (camelCase).
type ShipmentFailedEventData struct {
	OrderID      uuid.UUID   `json:"orderId"`
	OrderItemIDs []uuid.UUID `json:"orderItemIds"`
}

// ---------------------------------------------------------------------------
// Outgoing event DTOs (spec §4 "Published").
// ---------------------------------------------------------------------------

// PaymentAuthorization is the externally-tagged Rust enum
// PaymentAuthorization::CVC(u16) whose serde camelCase form is the single-key
// object {"cVC": <int>}. Modeled as a pointer-to-struct so an absent
// authorization serializes as JSON null.
type PaymentAuthorization struct {
	CVC uint16 `json:"cVC"`
}

// OrderItemDTO is one element of OrderDTO.orderItems (serde camelCase).
type OrderItemDTO struct {
	ID                      uuid.UUID   `json:"id"`
	CreatedAt               Time        `json:"createdAt"`
	ProductVariantID        uuid.UUID   `json:"productVariantId"`
	ProductVariantVersionID uuid.UUID   `json:"productVariantVersionId"`
	TaxRateVersionID        uuid.UUID   `json:"taxRateVersionId"`
	ShoppingCartItemID      uuid.UUID   `json:"shoppingCartItemId"`
	Count                   uint64      `json:"count"`
	CompensatableAmount     uint64      `json:"compensatableAmount"`
	ShipmentMethodID        uuid.UUID   `json:"shipmentMethodId"`
	DiscountIDs             []uuid.UUID `json:"discountIds"`
}

// OrderDTO is the payload of order/order/created (serde camelCase). createdAt /
// placedAt use the chrono serde-default form (UTC 'Z'); placedAt is non-null
// (placeOrder has just set it). rejectionReason is always null in practice.
type OrderDTO struct {
	ID                       uuid.UUID             `json:"id"`
	UserID                   uuid.UUID             `json:"userId"`
	CreatedAt                Time                  `json:"createdAt"`
	OrderStatus              string                `json:"orderStatus"`
	PlacedAt                 Time                  `json:"placedAt"`
	RejectionReason          *string               `json:"rejectionReason"`
	OrderItems               []OrderItemDTO        `json:"orderItems"`
	ShipmentAddressID        uuid.UUID             `json:"shipmentAddressId"`
	InvoiceAddressID         uuid.UUID             `json:"invoiceAddressId"`
	CompensatableOrderAmount uint64                `json:"compensatableOrderAmount"`
	PaymentInformationID     uuid.UUID             `json:"paymentInformationId"`
	PaymentAuthorization     *PaymentAuthorization `json:"paymentAuthorization"`
	VatNumber                *string               `json:"vatNumber"`
}

// OrderCompensationDTO is the payload of order/order-compensation/created.
// NOTE: unlike OrderDTO, this DTO has NO camelCase rename, so
// amount_to_compensate stays snake_case on the wire. Do not camelCase it.
type OrderCompensationDTO struct {
	ID                 uuid.UUID `json:"id"`
	AmountToCompensate uint64    `json:"amount_to_compensate"`
}
