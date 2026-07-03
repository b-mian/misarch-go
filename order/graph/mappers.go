package graph

import (
	"github.com/google/uuid"

	"misarch/order/store"
)

// This file maps store documents to GraphQL models. Relationship fields (User,
// InvoiceAddress, OrderItems, the OrderItem foreign types, Discounts, Orders)
// are left nil here — they are populated lazily by their own field resolvers
// from the extraFields carried on the model (all in-memory except User.orders).

// toOrderModel maps a store.Order to the GraphQL Order model, carrying the
// internal data (owner id, invoice-address id, embedded order items) the field
// resolvers need on the extraFields.
func toOrderModel(o store.Order) *Order {
	items := make([]*OrderItem, len(o.InternalOrderItems))
	for i := range o.InternalOrderItems {
		items[i] = toOrderItemModel(o.InternalOrderItems[i])
	}
	m := &Order{
		ID:                       o.ID,
		CreatedAt:                o.CreatedAt,
		OrderStatus:              OrderStatus(o.OrderStatus),
		CompensatableOrderAmount: int(o.CompensatableOrderAmount),
		PaymentInformationID:     o.PaymentInformationID,
		// extraFields:
		UserID:             o.User.ID,
		InvoiceAddressID:   o.InvoiceAddress.ID,
		InternalOrderItems: items,
	}
	if o.PlacedAt != nil {
		t := *o.PlacedAt
		m.PlacedAt = &t
	}
	if o.RejectionReason != nil {
		rr := RejectionReason(*o.RejectionReason)
		m.RejectionReason = &rr
	}
	return m
}

// toOrderItemModel maps an embedded store.OrderItem to the GraphQL OrderItem
// model, carrying the foreign-type ids and the applied-discount ids on the
// extraFields.
func toOrderItemModel(oi store.OrderItem) *OrderItem {
	discountIDs := make([]uuid.UUID, len(oi.InternalDiscounts))
	for i := range oi.InternalDiscounts {
		discountIDs[i] = oi.InternalDiscounts[i].ID
	}
	return &OrderItem{
		ID:                  oi.ID,
		CreatedAt:           oi.CreatedAt,
		Count:               int(oi.Count),
		CompensatableAmount: int(oi.CompensatableAmount),
		// extraFields:
		ProductVariantID:        oi.ProductVariant.ID,
		ProductVariantVersionID: oi.ProductVariantVersion.ID,
		TaxRateVersionID:        oi.TaxRateVersion.ID,
		ShoppingCartItemID:      oi.ShoppingCartItem.ID,
		ShipmentMethodID:        oi.ShipmentMethod.ID,
		InternalDiscounts:       discountIDs,
	}
}

// toUserModel maps a store.User to the GraphQL User model (only id is exposed;
// User.orders is resolved from obj.ID).
func toUserModel(u store.User) *User {
	return &User{ID: u.ID}
}
