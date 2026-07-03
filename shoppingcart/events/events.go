// Package events defines the Dapr pub/sub topics the shoppingcart service
// subscribes to and the payload shapes it reads from each. The service
// publishes nothing — it only consumes these events to maintain its local
// read-model (users, product_variants) and to empty carts on checkout.
//
// Topic strings and JSON field names/casing are copied verbatim from the
// original Rust service and must not drift.
package events

import (
	"github.com/google/uuid"
)

// Subscribed topics (Rust list_topic_subscriptions).
const (
	// TopicUserCreated creates a local user document (with an empty cart).
	TopicUserCreated = "user/user/created"
	// TopicProductVariantCreated records a product variant id for existence
	// validation.
	TopicProductVariantCreated = "catalog/product-variant/created"
	// TopicOrderCreated empties the ordered items from the buyer's cart.
	TopicOrderCreated = "order/order/created"
)

// Routes registered for each subscription. The original Rust service served
// user-created and product-variant-created on a single shared route
// (/on-topic-event) and dispatched on the CloudEvent topic. The Go pkg/dapr
// subscriber routes by URL and hands the handler only the event data (not the
// topic), so we register a distinct route per topic to disambiguate. The
// /dapr/subscribe list is self-describing, so Dapr behavior is unchanged.
const (
	RouteUserCreated           = "/on-topic-user-created"
	RouteProductVariantCreated = "/on-topic-product-variant-created"
	RouteOrderCreated          = "/on-order-creation-event"
)

// IDEvent is the payload of both user/user/created and
// catalog/product-variant/created: only the id is read (Rust EventData).
type IDEvent struct {
	ID uuid.UUID `json:"id"`
}

// OrderCreated is the payload of order/order/created (Rust OrderEventData,
// camelCase). Only UserID and the item ids are used; ID and per-item Count are
// read but unused by the cart mutation.
type OrderCreated struct {
	ID         uuid.UUID        `json:"id"`
	UserID     uuid.UUID        `json:"userId"`
	OrderItems []OrderItemEvent `json:"orderItems"`
}

// OrderItemEvent is one order line in an OrderCreated event. The whole cart
// item is pulled regardless of Count.
type OrderItemEvent struct {
	ShoppingCartItemID uuid.UUID `json:"shoppingCartItemId"`
	Count              uint64    `json:"count"`
}
