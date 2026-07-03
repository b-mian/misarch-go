package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"misarch/shipment/ecs"
	"misarch/shipment/service"
	"misarch/shipment/store"
)

// Handlers bundles the dependencies the event and REST handlers need.
type Handlers struct {
	Store *store.Store
	Saga  *service.ShipmentSaga
	ECS   *ecs.Config
}

// New builds a Handlers.
func New(st *store.Store, saga *service.ShipmentSaga, cfg *ecs.Config) *Handlers {
	return &Handlers{Store: st, Saga: saga, ECS: cfg}
}

// OnUserAddressCreated handles address/user-address/created: insert a user
// address (plain insert; a duplicate → error → 500 → Dapr retry).
func (h *Handlers) OnUserAddressCreated(ctx context.Context, data json.RawMessage) error {
	var dto userAddressDTO
	if err := json.Unmarshal(data, &dto); err != nil {
		return fmt.Errorf("decode user-address: %w", err)
	}
	userID := dto.UserID
	return h.Store.CreateAddress(ctx, dto.ID, &userID, dto.Street1, dto.Street2, dto.City, dto.PostalCode, dto.Country, dto.CompanyName)
}

// OnVendorAddressCreated handles address/vendor-address/created: insert a
// vendor address (userId NULL; plain insert).
func (h *Handlers) OnVendorAddressCreated(ctx context.Context, data json.RawMessage) error {
	var dto vendorAddressDTO
	if err := json.Unmarshal(data, &dto); err != nil {
		return fmt.Errorf("decode vendor-address: %w", err)
	}
	return h.Store.CreateAddress(ctx, dto.ID, nil, dto.Street1, dto.Street2, dto.City, dto.PostalCode, dto.Country, dto.CompanyName)
}

// OnProductVariantVersionCreated handles catalog/product-variant-version/created:
// insert the pvv read-model {id, weight} (plain insert). Unknown fields on the
// incoming event are ignored.
func (h *Handlers) OnProductVariantVersionCreated(ctx context.Context, data json.RawMessage) error {
	var dto productVariantVersionDTO
	if err := json.Unmarshal(data, &dto); err != nil {
		return fmt.Errorf("decode product-variant-version: %w", err)
	}
	return h.Store.CreateProductVariantVersion(ctx, dto.ID, dto.Weight)
}

// OnPaymentEnabled handles payment/payment/payment-enabled: create one shipment
// per shipment-method group of the order (order path). Per-group failures
// become creation-failed events; the handler only errors (→ 500) on an
// invariant violation that escapes createShipment.
func (h *Handlers) OnPaymentEnabled(ctx context.Context, data json.RawMessage) error {
	var dto paymentEnabledDTO
	if err := json.Unmarshal(data, &dto); err != nil {
		return fmt.Errorf("decode payment-enabled: %w", err)
	}
	items := make([]service.OrderItemFromEvent, len(dto.Order.OrderItems))
	for i, it := range dto.Order.OrderItems {
		items[i] = service.OrderItemFromEvent{
			ID:                      it.ID,
			ProductVariantVersionID: it.ProductVariantVersionID,
			Count:                   int(it.Count),
			ShipmentMethodID:        it.ShipmentMethodID,
		}
	}
	return h.Saga.CreateShipmentForOrder(ctx, service.OrderInput{
		ID:                dto.Order.ID,
		ShipmentAddressID: dto.Order.ShipmentAddressID,
		OrderItems:        items,
	})
}

// OnReturnCreated handles return/return/created: create the single return
// shipment (return path). A missing vendor address / zero shipment methods
// escapes as an error (→ 500 → Dapr retry); failures inside createShipment
// become creation-failed events.
func (h *Handlers) OnReturnCreated(ctx context.Context, data json.RawMessage) error {
	var dto returnDTO
	if err := json.Unmarshal(data, &dto); err != nil {
		return fmt.Errorf("decode return-created: %w", err)
	}
	orderItemIDs := make([]uuid.UUID, len(dto.OrderItemIDs))
	copy(orderItemIDs, dto.OrderItemIDs)
	return h.Saga.CreateShipmentForReturn(ctx, service.ReturnInput{
		ID:           dto.ID,
		OrderItemIDs: orderItemIDs,
	})
}
