package graph

import (
	"misarch/review/store"
)

// This file is not regenerated automatically.
//
// It serves as dependency injection for the app: the resolvers reach MongoDB
// through Store. The review service publishes no events, so there is no Dapr
// publisher here.

// Resolver wires the GraphQL resolvers to their dependencies.
type Resolver struct {
	// Store is the review persistence layer.
	Store *store.Store
}
