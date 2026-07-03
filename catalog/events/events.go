// Package events defines the Dapr pub/sub payloads the catalog service emits
// and thin helpers to publish them, plus the topics it subscribes to. Topic
// strings and JSON field names/casing are copied verbatim from the original
// Kotlin service (org.misarch.catalog.event) and must not drift — other
// services consume these exact shapes.
package events

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Published topics (org.misarch.catalog.event.CatalogEvents).
const (
	TopicProductCreated               = "catalog/product/created"
	TopicProductUpdated               = "catalog/product/updated"
	TopicProductVariantCreated        = "catalog/product-variant/created"
	TopicProductVariantUpdated        = "catalog/product-variant/updated"
	TopicProductVariantVersionCreated = "catalog/product-variant-version/created"
	TopicCategoryCreated              = "catalog/category/created"
)

// Subscribed topics and their delivery routes.
const (
	TopicTaxRateCreated = "tax/tax-rate/created"
	RouteTaxRateCreated = "/subscription/tax/tax-rate/created"
	TopicMediaCreated   = "media/media/created"
	RouteMediaCreated   = "/subscription/media/media/created"
)

// Publisher is the subset of *dapr.Client this package needs so callers can
// inject the real client (or a fake in tests).
type Publisher interface {
	Publish(ctx context.Context, topic string, payload any) error
}

// Product is the ProductDTO payload of TopicProductCreated / TopicProductUpdated.
// categoryIds is deduplicated, first-occurrence order.
type Product struct {
	ID                uuid.UUID   `json:"id"`
	InternalName      string      `json:"internalName"`
	IsPubliclyVisible bool        `json:"isPubliclyVisible"`
	DefaultVariantID  uuid.UUID   `json:"defaultVariantId"`
	CategoryIDs       []uuid.UUID `json:"categoryIds"`
}

// CreatedProductVariant is the CreatedProductVariantDTO payload of
// TopicProductVariantCreated. Note the JSON key currentVersionId (not currentVersion).
type CreatedProductVariant struct {
	ID                uuid.UUID `json:"id"`
	ProductID         uuid.UUID `json:"productId"`
	CurrentVersionID  uuid.UUID `json:"currentVersionId"`
	IsPubliclyVisible bool      `json:"isPubliclyVisible"`
}

// UpdatedProductVariant is the UpdatedProductVariantDTO payload of
// TopicProductVariantUpdated.
type UpdatedProductVariant struct {
	ID                uuid.UUID `json:"id"`
	IsPubliclyVisible bool      `json:"isPubliclyVisible"`
}

// ProductVariantVersion is the ProductVariantVersionDTO payload of
// TopicProductVariantVersionCreated. createdAt is an ISO-8601 offset STRING (the
// original formatted the OffsetDateTime with ISO_OFFSET_DATE_TIME); mediaIds is
// deduplicated, first-occurrence order. CanBeReturnedForDays is a pointer so a
// null value serializes as JSON null.
type ProductVariantVersion struct {
	ID                   uuid.UUID   `json:"id"`
	Name                 string      `json:"name"`
	Description          string      `json:"description"`
	Version              int         `json:"version"`
	RetailPrice          int         `json:"retailPrice"`
	CreatedAt            string      `json:"createdAt"`
	CanBeReturnedForDays *int        `json:"canBeReturnedForDays"`
	ProductVariantID     uuid.UUID   `json:"productVariantId"`
	TaxRateID            uuid.UUID   `json:"taxRateId"`
	Weight               float64     `json:"weight"`
	MediaIDs             []uuid.UUID `json:"mediaIds"`
}

// Category is the CategoryDTO payload of TopicCategoryCreated.
type Category struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
}

// FormatCreatedAt renders a timestamp the way the event payload expects: an
// ISO-8601 / RFC-3339 string in UTC with sub-second precision, matching the
// DateTime scalar wire format used across the platform.
func FormatCreatedAt(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// Publish helpers keep the topic strings in one place.

func PublishProductCreated(ctx context.Context, p Publisher, e Product) error {
	return p.Publish(ctx, TopicProductCreated, e)
}

func PublishProductUpdated(ctx context.Context, p Publisher, e Product) error {
	return p.Publish(ctx, TopicProductUpdated, e)
}

func PublishProductVariantCreated(ctx context.Context, p Publisher, e CreatedProductVariant) error {
	return p.Publish(ctx, TopicProductVariantCreated, e)
}

func PublishProductVariantUpdated(ctx context.Context, p Publisher, e UpdatedProductVariant) error {
	return p.Publish(ctx, TopicProductVariantUpdated, e)
}

func PublishProductVariantVersionCreated(ctx context.Context, p Publisher, e ProductVariantVersion) error {
	return p.Publish(ctx, TopicProductVariantVersionCreated, e)
}

func PublishCategoryCreated(ctx context.Context, p Publisher, e Category) error {
	return p.Publish(ctx, TopicCategoryCreated, e)
}
