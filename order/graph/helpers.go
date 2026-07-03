package graph

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"misarch/order/events"
	"misarch/order/store"
)

// pendingTimeout is the maximum age a PENDING order may reach before placeOrder
// rejects it (PENDING_TIMEOUT in the original).
const pendingTimeout = 3600 * time.Second

// setStatusPlaced is the pending-timeout state machine (set_status_placed):
//   - if created_at + 3600s >= now:
//   - PENDING → set PLACED + placed_at=now, Ok;
//   - else → error (already placed/rejected), no change;
//   - else (stale) → set REJECTED and ALWAYS return an error (no placed_at, no
//     rejection_reason, no event).
func (r *mutationResolver) setStatusPlaced(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	order, err := r.Store.GetOrder(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNoDocuments) {
			return orderNotFound(id)
		}
		return orderNotFound(id)
	}
	if !order.CreatedAt.Add(pendingTimeout).Before(now) {
		// created_at + 3600s >= now
		if order.OrderStatus == "PENDING" {
			return r.Store.SetStatusPlaced(ctx, id, now)
		}
		return fmt.Errorf("`%s` must be `OrderStatus::Pending` to be able to be placed. Order was already placed or rejected.", order.OrderStatus)
	}
	// Stale: reject and always return an error.
	if err := r.Store.SetStatusRejected(ctx, id); err != nil {
		return fmt.Errorf("Order should be rejected as it is `OrderStatus::Pending` for too long. Rejecting order of id: `%s` failed in MongoDB.", id)
	}
	return fmt.Errorf("Order of id: `%s` was rejected as it is `OrderStatus::Pending` for too long.", id)
}

// orderNotFound / orderItemNotFound / userNotFound produce the "not found"
// GraphQL errors. The original leaked Rust type-path strings via type_name; a
// stable clean name is used here (callers must not depend on the exact text).
func orderNotFound(id uuid.UUID) error {
	return fmt.Errorf("Order with UUID: `%s` not found.", id)
}

func orderItemNotFound(id uuid.UUID) error {
	return fmt.Errorf("OrderItem with UUID: `%s` not found.", id)
}

func userNotFound(id uuid.UUID) error {
	return fmt.Errorf("User with UUID: `%s` not found.", id)
}

// dedupeOrderItemInputs reproduces the BTreeSet<OrderItemInput> semantics:
// dedupe by shoppingCartItemId (first occurrence wins), then order ascending by
// that id. couponIds within each input are deduped (HashSet semantics).
func dedupeOrderItemInputs(in []OrderItemInput) []orderItemInput {
	seen := make(map[uuid.UUID]struct{}, len(in))
	out := make([]orderItemInput, 0, len(in))
	for _, oi := range in {
		if _, ok := seen[oi.ShoppingCartItemID]; ok {
			continue
		}
		seen[oi.ShoppingCartItemID] = struct{}{}
		out = append(out, orderItemInput{
			shoppingCartItemID: oi.ShoppingCartItemID,
			shipmentMethodID:   oi.ShipmentMethodID,
			couponIDs:          dedupeUUIDs(oi.CouponIds),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return uuidLess(out[i].shoppingCartItemID, out[j].shoppingCartItemID) })
	return out
}

// dedupeUUIDs removes duplicate UUIDs, preserving first-occurrence order
// (HashSet<Uuid> membership; wire order is unordered so first-occurrence is a
// valid representative).
func dedupeUUIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// validateOrderInput checks the order input against the LOCAL read models:
// user exists, all shipment methods present, all coupons present, and both
// addresses "valid" (user-existence only). Mirrors validate_order_input.
func (r *mutationResolver) validateOrderInput(ctx context.Context, input CreateOrderInput, inputs []orderItemInput) error {
	// User must exist.
	if err := r.Store.ValidateObjectUser(ctx, input.UserID); err != nil {
		return fmt.Errorf("User with UUID: `%s` not found.", input.UserID)
	}

	// All shipment methods present.
	shipmentMethodIDs := make([]uuid.UUID, 0, len(inputs))
	for _, oi := range inputs {
		shipmentMethodIDs = append(shipmentMethodIDs, oi.shipmentMethodID)
	}
	if err := r.validateObjectsPresent(ctx, r.Store.ShipmentMethodsPresent, shipmentMethodIDs, "ShipmentMethod"); err != nil {
		return err
	}

	// All coupons present (flattened over the inputs).
	var couponIDs []uuid.UUID
	for _, oi := range inputs {
		couponIDs = append(couponIDs, oi.couponIDs...)
	}
	if err := r.validateObjectsPresent(ctx, r.Store.CouponsPresent, couponIDs, "Coupon"); err != nil {
		return err
	}

	// Addresses: validate_user_address only checks that the USER exists (it does
	// NOT verify address ownership). Both addresses use the same check.
	if err := r.validateUserAddress(ctx, input.ShipmentAddressID, input.UserID); err != nil {
		return err
	}
	return r.validateUserAddress(ctx, input.InvoiceAddressID, input.UserID)
}

// validateObjectsPresent runs the given presence lookup and errors on the first
// id not present, mirroring validate_objects.
func (r *mutationResolver) validateObjectsPresent(ctx context.Context, lookup func(context.Context, []uuid.UUID) (map[uuid.UUID]struct{}, error), ids []uuid.UUID, typeName string) error {
	present, err := lookup(ctx, ids)
	if err != nil {
		return fmt.Errorf("%s with specified UUIDs are not present in the system.", typeName)
	}
	for _, id := range ids {
		if _, ok := present[id]; !ok {
			return fmt.Errorf("%s with UUID: `%s` is not present in the system.", typeName, id)
		}
	}
	return nil
}

// validateUserAddress reproduces validate_user_address: it only checks that the
// user exists (the address id is used only in the error message — ownership is
// NOT verified).
func (r *mutationResolver) validateUserAddress(ctx context.Context, addressID, userID uuid.UUID) error {
	if err := r.Store.ValidateObjectUser(ctx, userID); err != nil {
		return fmt.Errorf("User address with UUID: `%s` of user with UUID: `%s` not found.", addressID, userID)
	}
	return nil
}

// buildPaymentAuthorization mirrors build_payment_authorization: a CVC produces
// PaymentAuthorization::CVC; an absent authorization or a null cvc yields nil.
func buildPaymentAuthorization(in *PaymentAuthorizationInput) *events.PaymentAuthorization {
	if in == nil || in.Cvc == nil {
		return nil
	}
	return &events.PaymentAuthorization{CVC: uint16(*in.Cvc)}
}

// toOrderDTO builds the order/order/created payload from a placed order.
// Requires placed_at to be non-null (it is, just set), else error. Mirrors
// OrderDTO::try_from.
func toOrderDTO(o store.Order, auth *events.PaymentAuthorization) (events.OrderDTO, error) {
	if o.PlacedAt == nil {
		return events.OrderDTO{}, errors.New("OrderDTO cannot be created, `placed_at` of the given Order is `None`")
	}
	itemDTOs := make([]events.OrderItemDTO, len(o.InternalOrderItems))
	for i := range o.InternalOrderItems {
		itemDTOs[i] = toOrderItemDTO(o.InternalOrderItems[i])
	}
	return events.OrderDTO{
		ID:                       o.ID,
		UserID:                   o.User.ID,
		CreatedAt:                events.NewTime(o.CreatedAt),
		OrderStatus:              o.OrderStatus,
		PlacedAt:                 events.NewTime(*o.PlacedAt),
		RejectionReason:          o.RejectionReason,
		OrderItems:               itemDTOs,
		ShipmentAddressID:        o.ShipmentAddress.ID,
		InvoiceAddressID:         o.InvoiceAddress.ID,
		CompensatableOrderAmount: o.CompensatableOrderAmount,
		PaymentInformationID:     o.PaymentInformationID,
		PaymentAuthorization:     auth,
		VatNumber:                o.VatNumber,
	}, nil
}

// toOrderItemDTO builds one order-item event DTO from an embedded order item.
// discountIds are the ids of the applied discounts. Mirrors OrderItemDTO::from.
func toOrderItemDTO(oi store.OrderItem) events.OrderItemDTO {
	discountIDs := make([]uuid.UUID, len(oi.InternalDiscounts))
	for i := range oi.InternalDiscounts {
		discountIDs[i] = oi.InternalDiscounts[i].ID
	}
	return events.OrderItemDTO{
		ID:                      oi.ID,
		CreatedAt:               events.NewTime(oi.CreatedAt),
		ProductVariantID:        oi.ProductVariant.ID,
		ProductVariantVersionID: oi.ProductVariantVersion.ID,
		TaxRateVersionID:        oi.TaxRateVersion.ID,
		ShoppingCartItemID:      oi.ShoppingCartItem.ID,
		Count:                   oi.Count,
		CompensatableAmount:     oi.CompensatableAmount,
		ShipmentMethodID:        oi.ShipmentMethod.ID,
		DiscountIDs:             discountIDs,
	}
}
