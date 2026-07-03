// Package events defines the Dapr pub/sub payloads the tax service emits and
// thin helpers to publish them. Topic strings and JSON field names/casing are
// copied verbatim from the original Kotlin service and must not drift — other
// services subscribe to these exact shapes.
package events

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Topic constants mirror org.misarch.tax.event.TaxEvents.
const (
	// TopicTaxRateCreated is published once when a tax rate is created.
	TopicTaxRateCreated = "tax/tax-rate/created"
	// TopicTaxRateVersionCreated is published whenever a new version is
	// created — both for a brand-new tax rate's initial version and for
	// versions added to an existing tax rate.
	TopicTaxRateVersionCreated = "tax/tax-rate-version/created"
)

// Publisher is the subset of *dapr.Client this package needs, so callers can
// inject the real client (or a fake in tests).
type Publisher interface {
	Publish(ctx context.Context, topic string, payload any) error
}

// TaxRateCreated is the payload of TopicTaxRateCreated. Mirrors TaxRateDTO:
// every field is a UUID/string, and currentVersionId is always populated
// because the event is emitted only after the initial version is linked.
type TaxRateCreated struct {
	ID               uuid.UUID `json:"id"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	CurrentVersionID uuid.UUID `json:"currentVersionId"`
}

// TaxRateVersionCreated is the payload of TopicTaxRateVersionCreated. Mirrors
// TaxRateVersionDTO: createdAt is a STRING (the original formatted the
// OffsetDateTime with ISO_OFFSET_DATE_TIME), not a JSON timestamp object.
type TaxRateVersionCreated struct {
	ID        uuid.UUID `json:"id"`
	Rate      float64   `json:"rate"`
	Version   int       `json:"version"`
	CreatedAt string    `json:"createdAt"`
	TaxRateID uuid.UUID `json:"taxRateId"`
}

// FormatCreatedAt renders a timestamp the way the event payload expects: an
// ISO-8601 / RFC-3339 string in UTC with sub-second precision, matching the
// DateTime scalar wire format used everywhere else in the platform.
func FormatCreatedAt(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// PublishTaxRateCreated publishes a TaxRateCreated event.
func PublishTaxRateCreated(ctx context.Context, p Publisher, e TaxRateCreated) error {
	return p.Publish(ctx, TopicTaxRateCreated, e)
}

// PublishTaxRateVersionCreated publishes a TaxRateVersionCreated event.
func PublishTaxRateVersionCreated(ctx context.Context, p Publisher, e TaxRateVersionCreated) error {
	return p.Publish(ctx, TopicTaxRateVersionCreated, e)
}
