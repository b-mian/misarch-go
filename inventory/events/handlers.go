package events

import (
	"context"
	"encoding/json"
	"log/slog"

	"misarch/inventory/store"
)

// Inventory is the subset of the store the event handlers drive. It lets the
// handlers be constructed with the concrete *store.Store (which satisfies it)
// and keeps this package independently testable.
type Inventory interface {
	CreateVariantPartial(ctx context.Context, id string) error
	ReserveBatch(ctx context.Context, productVariantID string, number int, orderID string) ([]store.ProductItem, error)
	ReleaseBatch(ctx context.Context, orderID string) ([]store.ProductItem, error)
	UpdateOrderStatus(ctx context.Context, orderID, status string) error
}

// Shipment status strings (spec §4 shipment DTO). The service validates a
// shipment event's status against these before acting.
const (
	shipmentDelivered = "DELIVERED"
	shipmentFailed    = "FAILED"
)

// Handlers holds the dependencies the Dapr event handlers need: the store and
// the event publisher.
type Handlers struct {
	Store     Inventory
	Publisher Publisher
}

// NewHandlers builds an event Handlers set.
func NewHandlers(st Inventory, pub Publisher) *Handlers {
	return &Handlers{Store: st, Publisher: pub}
}

// --- Incoming payload shapes (only the fields the logic uses are typed). ---

// productVariantCreated is the data of catalog/product-variant/created; only id
// is used.
type productVariantCreated struct {
	ID string `json:"id"`
}

// orderItem is one entry of an order's orderItems; only productVariantId and
// count drive the reservation logic.
type orderItem struct {
	ProductVariantID string `json:"productVariantId"`
	Count            int    `json:"count"`
}

// orderCreated extracts the id and order items from an order-created event
// while keeping the ENTIRE raw order for lossless echo in the published
// reservation events (spec §4 fidelity point).
type orderCreated struct {
	ID         string      `json:"id"`
	OrderItems []orderItem `json:"orderItems"`
}

// orderEnvelope is the { order: OrderDTO } wrapper used by the payment and
// discount events; only order.id is used, but the whole order is retained raw.
type orderEnvelope struct {
	Order json.RawMessage `json:"order"`
}

// shipmentEvent is the shipment-created / shipment-status-updated payload.
type shipmentEvent struct {
	OrderID string `json:"orderId"`
	Status  string `json:"status"`
}

// orderIDOnly pulls just the id out of a raw order object.
type orderIDOnly struct {
	ID string `json:"id"`
}

// ProductVariantCreated handles catalog/product-variant/created: insert the
// variant id into the local replica. Duplicate keys are treated as success by
// the store ([BUG-COMPAT product-variant-created] FIXED — no process crash).
func (h *Handlers) ProductVariantCreated(ctx context.Context, data json.RawMessage) error {
	var dto productVariantCreated
	if err := json.Unmarshal(data, &dto); err != nil {
		return err
	}
	slog.Info("received product-variant-created", "id", dto.ID)
	return h.Store.CreateVariantPartial(ctx, dto.ID)
}

// OrderCreated handles order/order/created: the reservation saga step. It
// reserves one batch per order item, publishes reservation-succeeded when all
// succeed, or reservation-failed followed by a release of everything already
// reserved for the order when any fails. It ALWAYS returns nil (2xx) — matching
// the original, which swallows every error and is thus never retried by Dapr
// (spec §4 order-created step 4).
func (h *Handlers) OrderCreated(ctx context.Context, data json.RawMessage) error {
	var order orderCreated
	if err := json.Unmarshal(data, &order); err != nil {
		// Malformed order payload: the original's class-validator would 400 and
		// Dapr would retry. A malformed CloudEvent is already dropped upstream;
		// a structurally-valid-but-unparseable order simply cannot be processed.
		slog.Error("order-created: invalid order payload", "error", err)
		return nil
	}
	slog.Info("received order-created", "orderId", order.ID)

	// Reserve each order item; collect the productVariantIds that failed, in
	// order-item order. A reservation "fails" iff ReserveBatch returns an error
	// (insufficient stock or a transaction/write error) — the original's buggy
	// empty return still counts as success (spec §4 step 1).
	var failed []string
	for _, it := range order.OrderItems {
		if _, err := h.Store.ReserveBatch(ctx, it.ProductVariantID, it.Count, order.ID); err != nil {
			slog.Error("order-created: reservation failed", "productVariantId", it.ProductVariantID, "error", err)
			failed = append(failed, it.ProductVariantID)
		}
	}

	if len(failed) > 0 {
		// Publish reservation-failed BEFORE releasing (spec §9.2), echoing the
		// order verbatim, then release everything reserved for this order.
		PublishReservationFailed(ctx, h.Publisher, data, failed)
		if _, err := h.Store.ReleaseBatch(ctx, order.ID); err != nil {
			slog.Error("order-created: release after failure failed", "orderId", order.ID, "error", err)
		}
		return nil
	}

	PublishReservationSucceeded(ctx, h.Publisher, data)
	return nil
}

// PaymentEnabled handles payment/payment/payment-enabled: set every item of the
// order to IN_FULFILLMENT (unconditional bulk update keyed on orderId).
func (h *Handlers) PaymentEnabled(ctx context.Context, data json.RawMessage) error {
	id, err := orderIDFromEnvelope(data)
	if err != nil {
		return err
	}
	slog.Info("received payment-enabled", "orderId", id)
	return h.Store.UpdateOrderStatus(ctx, id, store.StatusInFulfillment)
}

// PaymentFailed handles payment/payment/payment-failed: release all items of
// the order (status → IN_STORAGE, orderId → null).
func (h *Handlers) PaymentFailed(ctx context.Context, data json.RawMessage) error {
	id, err := orderIDFromEnvelope(data)
	if err != nil {
		return err
	}
	slog.Info("received payment-failed", "orderId", id)
	_, err = h.Store.ReleaseBatch(ctx, id)
	return err
}

// ShipmentCreated handles shipment/shipment/created: mark ALL items of the
// order SHIPPED. A shipment with no orderId is a return shipment and is ignored
// (spec §4). orderItemIds are ignored (partial shipments still mark all items).
func (h *Handlers) ShipmentCreated(ctx context.Context, data json.RawMessage) error {
	var ev shipmentEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return err
	}
	if ev.OrderID == "" {
		return nil
	}
	slog.Info("received shipment-created", "orderId", ev.OrderID)
	return h.Store.UpdateOrderStatus(ctx, ev.OrderID, store.StatusShipped)
}

// ShipmentStatusUpdated handles shipment/shipment/status-updated: DELIVERED →
// DELIVERED, FAILED → LOST, others no-op. Returns for shipments have no orderId
// and are ignored (spec §4).
func (h *Handlers) ShipmentStatusUpdated(ctx context.Context, data json.RawMessage) error {
	var ev shipmentEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return err
	}
	if ev.OrderID == "" {
		return nil
	}
	slog.Info("received shipment-status-updated", "orderId", ev.OrderID, "status", ev.Status)
	switch ev.Status {
	case shipmentDelivered:
		return h.Store.UpdateOrderStatus(ctx, ev.OrderID, store.StatusDelivered)
	case shipmentFailed:
		return h.Store.UpdateOrderStatus(ctx, ev.OrderID, store.StatusLost)
	default:
		return nil
	}
}

// DiscountValidationFailed handles discount/order/validation-failed: release all
// items of the order (status → IN_STORAGE, orderId → null).
func (h *Handlers) DiscountValidationFailed(ctx context.Context, data json.RawMessage) error {
	id, err := orderIDFromEnvelope(data)
	if err != nil {
		return err
	}
	slog.Info("received discount-validation-failed", "orderId", id)
	_, err = h.Store.ReleaseBatch(ctx, id)
	return err
}

// orderIDFromEnvelope extracts order.id from a { order: {...} } payload.
func orderIDFromEnvelope(data json.RawMessage) (string, error) {
	var env orderEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return "", err
	}
	var o orderIDOnly
	if err := json.Unmarshal(env.Order, &o); err != nil {
		return "", err
	}
	return o.ID, nil
}
