package graph

import (
	"misarch/pkg/dapr"
	"misarch/shipment/service"
	"misarch/shipment/store"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for the app: the resolvers reach the
// database through Store, run shared domain logic (fee math) through Service,
// and publish shipment-method events through Dapr.

// Resolver wires the GraphQL resolvers to their dependencies.
type Resolver struct {
	// Store is the shipment persistence layer.
	Store *store.Store
	// Service holds the shared fee/weight domain logic used by the
	// calculateShipmentFees query and the ShipmentMethod.calculateFees field.
	Service *service.Service
	// Dapr publishes pub/sub events (its *Client satisfies events.Publisher).
	Dapr *dapr.Client
}
