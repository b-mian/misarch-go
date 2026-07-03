package events

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"misarch/order/store"
)

// compensateOrder reproduces the Rust compensate_order saga triggered by
// shipment/shipment/creation-failed:
//
//  1. validate_object(orders, orderId) — order must exist, else error → 500.
//  2. verify_items_uncompensated — the (apparently inverted) $not/$elemMatch/$in
//     probe: if it matches ANY compensation doc → error → 500 (compensation
//     blocked). Reproduced exactly.
//  3. calculate_amount_to_compensate — re-read the order; sum
//     compensatable_amount of embedded items whose _id ∈ orderItemIds.
//  4. build + insert an OrderCompensation.
//  5. publish order/order-compensation/created (transport error only).
//
// No order-status change occurs here. Any error → the handler returns 500 and
// Dapr retries.
func (svc *Service) compensateOrder(ctx context.Context, data ShipmentFailedEventData) error {
	// 1. Order must exist.
	if err := svc.store.ValidateObjectOrder(ctx, data.OrderID); err != nil {
		if errors.Is(err, store.ErrNoDocuments) {
			return fmt.Errorf("Order with UUID: `%s` not found.", data.OrderID)
		}
		return err
	}

	// 2. verify_items_uncompensated: a non-zero probe count blocks compensation.
	count, err := svc.store.CountUncompensatedProbe(ctx, data.OrderItemIDs)
	if err != nil {
		return fmt.Errorf("Order items of UUIDs: `%v` could not be verfied.", data.OrderItemIDs)
	}
	if count != 0 {
		return fmt.Errorf("Order items of UUIDs: `%v` could not be verfied.", data.OrderItemIDs)
	}

	// 3. Sum the compensatable amounts of the referenced embedded order items.
	amount, err := svc.calculateAmountToCompensate(ctx, data)
	if err != nil {
		return err
	}

	// 4. Insert the compensation log.
	compensation := store.OrderCompensation{
		ID:                 uuid.New(),
		OrderID:            data.OrderID,
		OrderItemIDs:       data.OrderItemIDs,
		TriggeredAt:        time.Now().UTC(),
		AmountToCompensate: amount,
	}
	if err := svc.store.InsertOrderCompensation(ctx, compensation); err != nil {
		return errors.New("Adding order compensation failed in MongoDB.")
	}

	// 5. Publish the compensation event (status not checked).
	return svc.pub.PublishOrderCompensationCreated(ctx, OrderCompensationDTO{
		ID:                 compensation.ID,
		AmountToCompensate: compensation.AmountToCompensate,
	})
}

// calculateAmountToCompensate re-reads the order and sums compensatable_amount
// over the embedded internal_order_items whose _id is in orderItemIDs. Mirrors
// calculate_amount_to_compensate.
func (svc *Service) calculateAmountToCompensate(ctx context.Context, data ShipmentFailedEventData) (uint64, error) {
	order, err := svc.store.GetOrder(ctx, data.OrderID)
	if err != nil {
		if errors.Is(err, store.ErrNoDocuments) {
			return 0, fmt.Errorf("Order with UUID: `%s` not found.", data.OrderID)
		}
		return 0, err
	}
	wanted := make(map[uuid.UUID]struct{}, len(data.OrderItemIDs))
	for _, id := range data.OrderItemIDs {
		wanted[id] = struct{}{}
	}
	var sum uint64
	for _, item := range order.InternalOrderItems {
		if _, ok := wanted[item.ID]; ok {
			sum += item.CompensatableAmount
		}
	}
	return sum, nil
}
