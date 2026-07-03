package graph

import (
	"misarch/invoice/store"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for the app: the resolvers reach MongoDB
// through Store. The invoice subgraph has no mutations and publishes no events
// from the GraphQL side (all writes happen via Dapr event handlers), so no Dapr
// client is wired into the resolver.

// Resolver wires the GraphQL resolvers to their dependencies.
type Resolver struct {
	// Store is the invoice persistence layer (MongoDB).
	Store *store.Store
}
