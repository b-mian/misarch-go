// Package events defines the Dapr pub/sub payloads the payment service emits
// and thin helpers to publish them. Topic strings and JSON field names/casing
// are copied verbatim from the original NestJS service and must not drift —
// the order-orchestration saga subscribes to these exact shapes.
package events

import (
	"context"
	"encoding/json"
)

// Topic constants mirror the string literals used in EventService.
const (
	// TopicPaymentEnabled: all prerequisites to later capture the payment are
	// in place (emitted by credit-card/invoice create, and prepayment
	// success-update).
	TopicPaymentEnabled = "payment/payment/payment-enabled"
	// TopicPaymentFailed: payment failed permanently (saga-start failure,
	// credit-card retries exhausted, or overdue cron).
	TopicPaymentFailed = "payment/payment/payment-failed"
	// TopicPaymentProcessed: DEAD CODE in the original — the builder/publisher
	// exist but are never called. Defined here for completeness; nothing in
	// this service publishes it.
	TopicPaymentProcessed = "payment/payment/payment-processed"
)

// Publisher is the subset of *dapr.Client this package needs, so callers can
// inject the real client (or a fake in tests).
type Publisher interface {
	Publish(ctx context.Context, topic string, payload any) error
}

// orderEnvelope is the payload wrapper for every saga event: { "order": <...> }.
// The order is carried as raw JSON so it is re-emitted byte-for-byte exactly as
// it was received in the inbound validation-succeeded event.
type orderEnvelope struct {
	Order json.RawMessage `json:"order"`
}

// PublishPaymentEnabled publishes payment-enabled with { order }.
func PublishPaymentEnabled(ctx context.Context, p Publisher, order json.RawMessage) error {
	return p.Publish(ctx, TopicPaymentEnabled, orderEnvelope{Order: order})
}

// PublishPaymentFailed publishes payment-failed with { order }.
func PublishPaymentFailed(ctx context.Context, p Publisher, order json.RawMessage) error {
	return p.Publish(ctx, TopicPaymentFailed, orderEnvelope{Order: order})
}
