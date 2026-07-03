package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"misarch/shipment/events"
	"misarch/shipment/provider"
	"misarch/shipment/store"
)

// statusPending is the ShipmentStatus NAME string every shipment is created
// with.
const statusPending = "PENDING"

// OrderItemInput is one order item flowing into a shipment (id +
// productVariantVersionId + quantity), mirroring service.OrderItemInput.
type OrderItemInput struct {
	ID                      uuid.UUID
	ProductVariantVersionID uuid.UUID
	Quantity                int
}

// createShipmentInput is the internal input to the create-shipment saga
// (mirrors service.CreateShipmentInput). Exactly one of OrderID/ReturnID is
// set.
type createShipmentInput struct {
	OrderID          *uuid.UUID
	ReturnID         *uuid.UUID
	ShipmentMethodID uuid.UUID
	AddressID        uuid.UUID
	OrderItems       []OrderItemInput
}

// validate reproduces the CreateShipmentInput constructor require()s. These are
// evaluated before the try/catch in createShipment, so a violation propagates
// out (rather than becoming a creation-failed event). In practice the callers
// always satisfy them.
func (in createShipmentInput) validate() error {
	if (in.OrderID != nil) == (in.ReturnID != nil) {
		return fmt.Errorf("orderId xor returnId must be set")
	}
	if len(in.OrderItems) == 0 {
		return fmt.Errorf("orderItems must not be empty")
	}
	return nil
}

// initOrderItems is the callback that initializes order-item rows for a
// shipment (upsert for orders, no-op for returns), mirroring the Kotlin
// InitOrderItems typealias.
type initOrderItems func(ctx context.Context, items []OrderItemInput, shipmentID uuid.UUID) error

// ShipmentSaga wires the create-shipment saga to its dependencies: the store,
// the event publisher, and the external-provider client.
type ShipmentSaga struct {
	*Service
	Publisher events.Publisher
	Provider  *provider.Client
}

// NewShipmentSaga builds a ShipmentSaga.
func NewShipmentSaga(st *store.Store, pub events.Publisher, prov *provider.Client) *ShipmentSaga {
	return &ShipmentSaga{Service: NewService(st), Publisher: pub, Provider: prov}
}

// CreateShipmentForOrder groups the order's items by shipment method and
// creates one shipment per group (order path). Each group's failure is
// isolated by createShipment's try/catch (→ creation-failed event); one bad
// group does not abort the others. Mirrors createShipmentForOrder.
func (s *ShipmentSaga) CreateShipmentForOrder(ctx context.Context, order OrderInput) error {
	// Group by shipmentMethodId preserving first-seen order.
	groups := map[uuid.UUID][]OrderItemInput{}
	var methodOrder []uuid.UUID
	for _, it := range order.OrderItems {
		if _, seen := groups[it.ShipmentMethodID]; !seen {
			methodOrder = append(methodOrder, it.ShipmentMethodID)
		}
		groups[it.ShipmentMethodID] = append(groups[it.ShipmentMethodID], OrderItemInput{
			ID:                      it.ID,
			ProductVariantVersionID: it.ProductVariantVersionID,
			Quantity:                it.Count, // count (Long) -> quantity (.toInt())
		})
	}

	orderID := order.ID
	for _, methodID := range methodOrder {
		items := groups[methodID]
		in := createShipmentInput{
			OrderID:          &orderID,
			ReturnID:         nil,
			ShipmentMethodID: methodID,
			AddressID:        order.ShipmentAddressID,
			OrderItems:       items,
		}
		if err := s.createShipment(ctx, in, s.upsertOrderItems); err != nil {
			return err
		}
	}
	return nil
}

// CreateShipmentForReturn creates the single return shipment: it uses the
// current vendor address as the destination and the least-expensive shipment
// method. A missing vendor address ("No vendor address found") or zero shipment
// methods ("No shipment methods available") propagates OUT of this function
// (→ 500 → Dapr retry), BEFORE createShipment's try/catch — the return path's
// asymmetry vs the order path. Failures inside createShipment become
// creation-failed events. Mirrors createShipmentForReturn.
func (s *ShipmentSaga) CreateShipmentForReturn(ctx context.Context, ret ReturnInput) error {
	vendorAddress, ok, err := s.Store.FindCurrentVendorAddress(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("No vendor address found")
	}

	orderItems, err := s.Store.FindOrderItemsByIDs(ctx, ret.OrderItemIDs)
	if err != nil {
		return err
	}
	method, err := s.FindLeastExpensiveShipmentMethod(ctx, orderItems)
	if err != nil {
		return err
	}

	items := make([]OrderItemInput, 0, len(orderItems))
	for _, it := range orderItems {
		items = append(items, OrderItemInput{
			ID:                      it.ID,
			ProductVariantVersionID: it.ProductVariantVersionID,
			Quantity:                it.Quantity,
		})
	}

	returnID := ret.ID
	in := createShipmentInput{
		OrderID:          nil,
		ReturnID:         &returnID,
		ShipmentMethodID: method.ID,
		AddressID:        vendorAddress.ID,
		OrderItems:       items,
	}
	// initOrderItems is a no-op for returns (the order items already exist).
	return s.createShipment(ctx, in, noopInitOrderItems)
}

// createShipment wraps tryCreateShipment in the failure-to-event behavior:
// input-invariant violations propagate; any error from tryCreateShipment is
// caught and published as creation-failed (reason = err message or
// "Unknown reason"), and the partial DB writes are NOT rolled back. Mirrors
// ShipmentService.createShipment.
func (s *ShipmentSaga) createShipment(ctx context.Context, in createShipmentInput, init initOrderItems) error {
	if err := in.validate(); err != nil {
		return err
	}
	if err := s.tryCreateShipment(ctx, in, init); err != nil {
		reason := err.Error()
		if reason == "" {
			reason = "Unknown reason"
		}
		orderItemIDs := make([]uuid.UUID, 0, len(in.OrderItems))
		for _, it := range in.OrderItems {
			orderItemIDs = append(orderItemIDs, it.ID)
		}
		return events.PublishShipmentCreationFailed(ctx, s.Publisher, events.ShipmentCreationFailed{
			OrderID:           in.OrderID,
			ReturnID:          in.ReturnID,
			OrderItemIDs:      orderItemIDs,
			ShipmentMethodID:  in.ShipmentMethodID,
			ShipmentAddressID: in.AddressID,
			Reason:            reason,
		})
	}
	return nil
}

// tryCreateShipment performs the create-shipment steps and publishes the
// created event. Any error is returned to createShipment (which converts it to
// a creation-failed event). Steps mirror ShipmentService.tryCreateShipment.
func (s *ShipmentSaga) tryCreateShipment(ctx context.Context, in createShipmentInput, init initOrderItems) error {
	// 1. Address must exist.
	exists, err := s.Store.AddressExists(ctx, in.AddressID)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("Address does not exist")
	}
	// 2. Shipment method must exist.
	method, err := s.Store.GetShipmentMethod(ctx, in.ShipmentMethodID)
	if err != nil {
		return err
	}
	// 3. Save the shipment (DB assigns id), status PENDING.
	shipmentID, err := s.Store.CreateShipment(ctx, statusPending, in.ShipmentMethodID, in.AddressID, in.OrderID, in.ReturnID)
	if err != nil {
		return err
	}
	// 4. Initialize order items (upsert for orders, no-op for returns).
	if err := init(ctx, in.OrderItems, shipmentID); err != nil {
		return err
	}
	// 5. One join row per order item.
	for _, it := range in.OrderItems {
		if err := s.Store.CreateShipmentToOrderItem(ctx, shipmentID, it.ID); err != nil {
			return err
		}
	}
	// 6. Send to the external provider (compute qty/weight, load address, POST).
	if err := s.sendToProvider(ctx, in, shipmentID, method); err != nil {
		return err
	}
	// 7. Publish the created event.
	orderItemIDs := make([]uuid.UUID, 0, len(in.OrderItems))
	for _, it := range in.OrderItems {
		orderItemIDs = append(orderItemIDs, it.ID)
	}
	return events.PublishShipmentCreated(ctx, s.Publisher, events.ShipmentCreated{
		ID:                shipmentID,
		OrderID:           in.OrderID,
		ReturnID:          in.ReturnID,
		Status:            statusPending,
		OrderItemIDs:      orderItemIDs,
		ShipmentMethodID:  in.ShipmentMethodID,
		ShipmentAddressID: in.AddressID,
	})
}

// sendToProvider builds the provider payload and POSTs it with retries.
// Mirrors ShipmentService.sendShipmentToExternalProvider.
func (s *ShipmentSaga) sendToProvider(ctx context.Context, in createShipmentInput, shipmentID uuid.UUID, method store.ShipmentMethod) error {
	items := make([]ItemWithQuantity, 0, len(in.OrderItems))
	for _, it := range in.OrderItems {
		items = append(items, ItemWithQuantity{
			ProductVariantVersionID: it.ProductVariantVersionID,
			Quantity:                it.Quantity,
		})
	}
	quantity, weight, err := s.CalculateQuantityAndWeight(ctx, items)
	if err != nil {
		return err
	}
	address, err := s.Store.GetAddress(ctx, in.AddressID)
	if err != nil {
		return err
	}
	def := provider.ShipmentDefinition{
		ShipmentID: shipmentID,
		Ref:        method.ExternalReference,
		Quantity:   quantity,
		Weight:     weight,
		Address: provider.AddressDefinition{
			Street1:     address.Street1,
			Street2:     address.Street2,
			City:        address.City,
			PostalCode:  address.PostalCode,
			Country:     address.Country,
			CompanyName: address.CompanyName,
		},
	}
	return s.Provider.Send(ctx, def)
}

// upsertOrderItems is the order-path initOrderItems: create each order-item row
// (ON CONFLICT DO NOTHING) linked to the new shipment.
func (s *ShipmentSaga) upsertOrderItems(ctx context.Context, items []OrderItemInput, shipmentID uuid.UUID) error {
	for _, it := range items {
		if err := s.Store.CreateOrderItem(ctx, it.ID, shipmentID, it.ProductVariantVersionID, it.Quantity); err != nil {
			return err
		}
	}
	return nil
}

// noopInitOrderItems is the return-path initOrderItems: the order items already
// exist, so only the join rows are added (by tryCreateShipment).
func noopInitOrderItems(ctx context.Context, items []OrderItemInput, shipmentID uuid.UUID) error {
	return nil
}

// UpdateShipmentStatus loads the shipment (missing → error), sets the new
// status, saves, and publishes status-updated (with orderItemIds from the join
// rows). No state-machine validation and no idempotency, matching
// ShipmentService.updateShipmentStatus.
func (s *ShipmentSaga) UpdateShipmentStatus(ctx context.Context, shipmentID uuid.UUID, status string) error {
	shipment, err := s.Store.UpdateShipmentStatus(ctx, shipmentID, status)
	if err != nil {
		return err
	}
	orderItemIDs, err := s.Store.FindOrderItemIDsByShipmentID(ctx, shipmentID)
	if err != nil {
		return err
	}
	return events.PublishShipmentStatusUpdated(ctx, s.Publisher, events.ShipmentStatusUpdated{
		ID:           shipmentID,
		OrderItemIDs: orderItemIDs,
		OrderID:      shipment.OrderID,
		ReturnID:     shipment.ReturnID,
		Status:       shipment.Status,
	})
}

// OrderInput is the subset of the payment-enabled OrderDTO the saga reads.
type OrderInput struct {
	ID                uuid.UUID
	ShipmentAddressID uuid.UUID
	OrderItems        []OrderItemFromEvent
}

// OrderItemFromEvent is the subset of the payment-enabled OrderItemDTO the saga
// reads. Count is the Long quantity used as the order-item quantity.
type OrderItemFromEvent struct {
	ID                      uuid.UUID
	ProductVariantVersionID uuid.UUID
	Count                   int
	ShipmentMethodID        uuid.UUID
}

// ReturnInput is the subset of the return-created ReturnDTO the saga reads.
type ReturnInput struct {
	ID           uuid.UUID
	OrderItemIDs []uuid.UUID
}
