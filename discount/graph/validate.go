package graph

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"misarch/discount/events"
	"misarch/discount/store"
)

// HandleReservationSucceeded is the Dapr subscription handler for
// inventory/product-item/reservation-succeeded. It parses the CloudEvent data
// into the typed ReservationSucceeded DTO and runs the order-validation saga.
// An error return becomes a 500 so Dapr redelivers (matching the original
// handler, which re-throws on failure).
func (r *Resolver) HandleReservationSucceeded(ctx context.Context, data json.RawMessage) error {
	var msg events.ReservationSucceeded
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("decode reservation-succeeded: %w", err)
	}
	return r.validateOrder(ctx, msg.Order)
}

// validateOrder wraps validateOrderInternal in the original's try/catch: on ANY
// error it publishes a validation-failed event with an EMPTY failingDiscountIds
// list and then re-surfaces the error (=> 500 => Dapr retry). This is distinct
// from the normal per-user-limit failure path, which publishes the failing ids
// and returns success (200, consumed).
func (r *Resolver) validateOrder(ctx context.Context, order events.OrderDTO) error {
	if err := r.validateOrderInternal(ctx, order); err != nil {
		// Best-effort: mirror the catch block's publish, then return the error.
		_ = events.PublishValidationFailed(ctx, r.Dapr, events.ValidationFailedDTO{
			Order:              order,
			FailingDiscountIds: []uuid.UUID{},
		})
		return err
	}
	return nil
}

// validateOrderInternal ports DiscountService.validateOrderInternal (§9.7):
// group the order's discount usages, compute each discount's remaining per-user
// budget, and either publish validation-failed with the over-budget discount ids
// (no usages recorded) or upsert the usages and publish validation-succeeded.
// The upserts and the success publish share one transaction so a publish failure
// rolls back the increments.
func (r *Resolver) validateOrderInternal(ctx context.Context, order events.OrderDTO) error {
	// Group order items by discount id, preserving first-encounter order and
	// summing counts. An item with N discountIds contributes its full count to
	// each referenced discount.
	discountOrder := []uuid.UUID{}
	totalByDiscount := map[uuid.UUID]int64{}
	for _, item := range order.OrderItems {
		for _, did := range item.DiscountIds {
			if _, seen := totalByDiscount[did]; !seen {
				discountOrder = append(discountOrder, did)
			}
			totalByDiscount[did] += item.Count
		}
	}

	discounts, err := r.Store.DiscountsByIDs(ctx, discountOrder)
	if err != nil {
		return err
	}
	remaining, err := r.Store.RemainingUsages(ctx, order.UserID, discounts)
	if err != nil {
		return err
	}

	// Collect discounts whose requested total exceeds the remaining budget
	// (uncapped discounts are absent from the map and treated as unlimited).
	var failed []uuid.UUID
	for _, did := range discountOrder {
		limit, tracked := remaining[did]
		if tracked && totalByDiscount[did] > limit {
			failed = append(failed, did)
		}
	}

	if len(failed) > 0 {
		return events.PublishValidationFailed(ctx, r.Dapr, events.ValidationFailedDTO{
			Order:              order,
			FailingDiscountIds: failed,
		})
	}

	// Success: upsert usages and publish, atomically.
	return r.Store.RunInTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		for _, did := range discountOrder {
			if err := store.UpsertDiscountUsage(ctx, tx, did, order.UserID, totalByDiscount[did]); err != nil {
				return err
			}
		}
		return events.PublishValidationSucceeded(ctx, r.Dapr, events.ValidationSucceededDTO{Order: order})
	})
}
