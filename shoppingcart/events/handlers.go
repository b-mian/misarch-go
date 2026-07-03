package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Store is the subset of the persistence layer the event handlers drive. The
// concrete *store.Store satisfies it; the interface keeps this package
// independently testable and decoupled from the store package.
type Store interface {
	InsertUser(ctx context.Context, id uuid.UUID, now time.Time) error
	InsertProductVariant(ctx context.Context, id uuid.UUID) error
	PullItems(ctx context.Context, userID uuid.UUID, itemIDs []uuid.UUID) error
}

// Handlers holds the dependencies the Dapr event handlers need. The
// shoppingcart service publishes nothing, so only the store is required.
type Handlers struct {
	Store Store
}

// NewHandlers builds an event Handlers set.
func NewHandlers(st Store) *Handlers {
	return &Handlers{Store: st}
}

// UserCreated handles user/user/created: insert a new user document with an
// empty cart. NOT idempotent — a duplicate user id causes an insert error
// (duplicate _id) which is returned so the handler responds 500 and Dapr
// redelivers, exactly matching the original Rust service.
func (h *Handlers) UserCreated(ctx context.Context, data json.RawMessage) error {
	var ev IDEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return err
	}
	slog.Info("received user-created", "id", ev.ID)
	return h.Store.InsertUser(ctx, ev.ID, time.Now().UTC())
}

// ProductVariantCreated handles catalog/product-variant/created: insert the
// variant id into the local existence mirror. NOT idempotent, same as
// UserCreated — a duplicate returns an error → 500 → redeliver.
func (h *Handlers) ProductVariantCreated(ctx context.Context, data json.RawMessage) error {
	var ev IDEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return err
	}
	slog.Info("received product-variant-created", "id", ev.ID)
	return h.Store.InsertProductVariant(ctx, ev.ID)
}

// OrderCreated handles order/order/created: pull every ordered cart item out of
// the buyer's cart (checkout empties what was bought). The per-item count is
// ignored — the whole line item is removed regardless of ordered quantity.
// Idempotent in effect: a second delivery removes nothing and still succeeds.
// A driver error is returned → 500 → redeliver.
func (h *Handlers) OrderCreated(ctx context.Context, data json.RawMessage) error {
	var ev OrderCreated
	if err := json.Unmarshal(data, &ev); err != nil {
		return err
	}
	slog.Info("received order-created", "orderId", ev.ID, "userId", ev.UserID)
	itemIDs := make([]uuid.UUID, len(ev.OrderItems))
	for i, it := range ev.OrderItems {
		itemIDs[i] = it.ShoppingCartItemID
	}
	return h.Store.PullItems(ctx, ev.UserID, itemIDs)
}
