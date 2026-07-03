// Package events defines the Dapr pub/sub topics, routes and payloads the
// inventory service consumes and emits. Topic strings, route strings and JSON
// field names are copied verbatim from the original NestJS service (spec §4)
// and must not drift — the order and discount sagas depend on these exact
// shapes.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
)

// Subscribed topics (spec §4 "Subscribed").
const (
	TopicProductVariantCreated    = "catalog/product-variant/created"
	TopicOrderCreated             = "order/order/created"
	TopicPaymentEnabled           = "payment/payment/payment-enabled"
	TopicPaymentFailed            = "payment/payment/payment-failed"
	TopicShipmentStatusUpdated    = "shipment/shipment/status-updated"
	TopicShipmentCreated          = "shipment/shipment/created"
	TopicDiscountValidationFailed = "discount/order/validation-failed"
)

// Delivery routes for each subscribed topic. net/http mux patterns require a
// leading slash; the original registered them without one (Dapr accepts both).
const (
	RouteProductVariantCreated    = "/product-variant-created"
	RouteOrderCreated             = "/order-created"
	RoutePaymentEnabled           = "/payment-enabled"
	RoutePaymentFailed            = "/payment-failed"
	RouteShipmentStatusUpdated    = "/shipment-status-updated"
	RouteShipmentCreated          = "/shipment-created"
	RouteDiscountValidationFailed = "/discount-validation-failed"
)

// Published topics (spec §4 "Published").
const (
	TopicReservationSucceeded = "inventory/product-item/reservation-succeeded"
	TopicReservationFailed    = "inventory/product-item/reservation-failed"
)

// Publisher is the subset of *dapr.Client this package needs.
type Publisher interface {
	Publish(ctx context.Context, topic string, payload any) error
}

// ReservationSucceeded is the payload of TopicReservationSucceeded: the order
// echoed back verbatim as received.
type ReservationSucceeded struct {
	Order json.RawMessage `json:"order"`
}

// ReservationFailed is the payload of TopicReservationFailed: the echoed order
// plus the productVariantIds whose reservation failed, in order-item order.
type ReservationFailed struct {
	Order                   json.RawMessage `json:"order"`
	FailedProductVariantIDs []string        `json:"failedProductVariantIds"`
}

// PublishReservationSucceeded publishes a reservation-succeeded event. Publish
// failures are logged and swallowed — they must never fail the event handler
// (spec §4 "Publish failures are caught and logged, never propagated").
func PublishReservationSucceeded(ctx context.Context, p Publisher, order json.RawMessage) {
	if err := p.Publish(ctx, TopicReservationSucceeded, ReservationSucceeded{Order: order}); err != nil {
		slog.Error("publish reservation-succeeded failed", "error", err)
	}
}

// PublishReservationFailed publishes a reservation-failed event. Publish
// failures are logged and swallowed.
func PublishReservationFailed(ctx context.Context, p Publisher, order json.RawMessage, failedIDs []string) {
	payload := ReservationFailed{Order: order, FailedProductVariantIDs: failedIDs}
	if err := p.Publish(ctx, TopicReservationFailed, payload); err != nil {
		slog.Error("publish reservation-failed failed", "error", err)
	}
}
