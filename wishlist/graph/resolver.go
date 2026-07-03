package graph

import (
	"misarch/wishlist/store"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for the app: the resolvers reach MongoDB
// through Store. The wishlist service publishes no events, so no Dapr client is
// wired into the resolver (the two consumed events are handled by the events
// package via server.Config.ExtraRoutes, not through a resolver dependency).

// Resolver wires the GraphQL resolvers to their dependencies.
type Resolver struct {
	// Store is the wishlist persistence layer (MongoDB).
	Store *store.Store
}
