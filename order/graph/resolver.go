package graph

import (
	"misarch/order/events"
	"misarch/order/store"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for the app: the resolvers reach MongoDB
// through Store, publish domain events through Publisher, and issue
// cross-service GraphQL queries (the createOrder saga fan-out) through gql.

// Resolver wires the GraphQL resolvers to their dependencies.
type Resolver struct {
	// Store is the order persistence layer (MongoDB).
	Store *store.Store
	// Publisher publishes order/order/created and
	// order/order-compensation/created events over Dapr pub/sub.
	Publisher *events.Publisher
	// gql issues cross-service GraphQL queries (shoppingcart, inventory,
	// discount, shipment) through the Dapr sidecar.
	gql *gqlClient
}

// NewResolver builds a Resolver with its dependencies. The cross-service
// GraphQL client is constructed internally.
func NewResolver(st *store.Store, pub *events.Publisher) *Resolver {
	return &Resolver{Store: st, Publisher: pub, gql: newGQLClient()}
}
