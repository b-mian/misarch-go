package graph

import (
	"misarch/shipment/store"
)

// This file maps store rows to GraphQL models and GraphQL order/filter inputs
// to store parameters. Relationship fields (Shipment.order/return/sentItems/
// shipmentAddress/shipmentMethod, Order.shipments, Return.shipment,
// OrderItem.sentWith, ShipmentMethod.externalReference/calculateFees) are
// intentionally left at their zero value here: they are populated lazily by
// their own field resolvers. The extraFields carry the FKs / internal values
// those resolvers need. Scalar-only fields are populated directly.

// toShipmentMethod maps a store.ShipmentMethod to the GraphQL model. IsArchived
// is computed (archivedAt != nil), not stored. ExternalRef carries the value
// the auth-guarded ExternalReference resolver returns.
func toShipmentMethod(m store.ShipmentMethod) *ShipmentMethod {
	return &ShipmentMethod{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		BaseFees:    m.BaseFees,
		FeesPerItem: m.FeesPerItem,
		FeesPerKg:   m.FeesPerKg,
		ArchivedAt:  m.ArchivedAt,
		IsArchived:  m.ArchivedAt != nil,
		ExternalRef: m.ExternalReference,
	}
}

// toShipment maps a store.Shipment to the GraphQL model. Status is the enum
// NAME string; the FK extraFields feed the relationship resolvers.
func toShipment(sh store.Shipment) *Shipment {
	return &Shipment{
		ID:                sh.ID,
		Status:            ShipmentStatus(sh.Status),
		ShipmentMethodID:  sh.ShipmentMethodID,
		ShipmentAddressID: sh.ShipmentAddressID,
		OrderID:           sh.OrderID,
		ReturnID:          sh.ReturnID,
	}
}

// toOrderItem maps a store.OrderItem to the GraphQL model. SentWithID (the
// shipment the item was first sent with) feeds the sentWith resolver.
func toOrderItem(it store.OrderItem) *OrderItem {
	sentWith := it.SentWithID
	return &OrderItem{
		ID:         it.ID,
		SentWithID: &sentWith,
	}
}

// toAddress maps a store.Address to the Address interface implementation: a
// VendorAddress when userId is null, else a UserAddress (the AddressEntity
// discriminator).
func toAddress(a store.Address) Address {
	if a.UserID == nil {
		return &VendorAddress{ID: a.ID}
	}
	return &UserAddress{ID: a.ID}
}

// toShipmentMethodConnection maps a paginated store result to the GraphQL
// connection.
func toShipmentMethodConnection(c store.Connection[store.ShipmentMethod]) *ShipmentMethodConnection {
	nodes := make([]ShipmentMethod, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toShipmentMethod(n)
	}
	return &ShipmentMethodConnection{
		Nodes:       nodes,
		TotalCount:  c.TotalCount,
		HasNextPage: c.HasNextPage,
	}
}

// toShipmentConnection maps a paginated store result to the GraphQL connection.
func toShipmentConnection(c store.Connection[store.Shipment]) *ShipmentConnection {
	nodes := make([]Shipment, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toShipment(n)
	}
	return &ShipmentConnection{
		Nodes:       nodes,
		TotalCount:  c.TotalCount,
		HasNextPage: c.HasNextPage,
	}
}

// toOrderItemConnection maps a paginated store result to the GraphQL
// connection.
func toOrderItemConnection(c store.Connection[store.OrderItem]) *OrderItemConnection {
	nodes := make([]OrderItem, len(c.Nodes))
	for i, n := range c.Nodes {
		nodes[i] = *toOrderItem(n)
	}
	return &OrderItemConnection{
		Nodes:       nodes,
		TotalCount:  c.TotalCount,
		HasNextPage: c.HasNextPage,
	}
}

// ascending reports whether an OrderDirection means ascending. The default
// (nil) is ASC, matching the original OrderDirection default.
func ascending(d *OrderDirection) bool {
	return d == nil || *d != OrderDirectionDesc
}

// shipmentMethodOrder resolves a ShipmentMethodOrderInput to a store order
// column set and direction. Default: field ID, direction ASC. ID is the only
// order field.
func shipmentMethodOrder(in *ShipmentMethodOrderInput) (store.ShipmentMethodOrderColumn, bool) {
	if in == nil {
		return store.ShipmentMethodOrderByID, true
	}
	return store.ShipmentMethodOrderByID, ascending(in.Direction)
}

// shipmentOrder resolves a ShipmentOrderInput to a store order column set and
// direction. Default: field ID, direction ASC. ID is the only order field.
func shipmentOrder(in *ShipmentOrderInput) (store.ShipmentOrderColumn, bool) {
	if in == nil {
		return store.ShipmentOrderByID, true
	}
	return store.ShipmentOrderByID, ascending(in.Direction)
}

// orderItemOrder resolves an OrderItemOrderInput to a store order column set
// and direction. Default: field ID, direction ASC. ID is the only order field.
func orderItemOrder(in *OrderItemOrderInput) (store.OrderItemOrderColumn, bool) {
	if in == nil {
		return store.OrderItemOrderByID, true
	}
	return store.OrderItemOrderByID, ascending(in.Direction)
}

// shipmentStatusFilter converts a nullable GraphQL ShipmentStatus filter to the
// store's *string status parameter.
func shipmentStatusFilter(s *ShipmentStatus) *string {
	if s == nil {
		return nil
	}
	v := string(*s)
	return &v
}
