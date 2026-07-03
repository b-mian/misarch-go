package graph

import (
	"misarch/shoppingcart/store"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for the app: the resolvers reach MongoDB
// through Store. The shoppingcart service publishes no events, so no Dapr
// client is wired into the resolver.

// Resolver wires the GraphQL resolvers to their dependencies.
type Resolver struct {
	// Store is the shoppingcart persistence layer.
	Store *store.Store
}
