package graph

import (
	"misarch/notification/store"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for the app: the resolvers reach the
// database through Store. The notification service publishes no events, so no
// Dapr client is wired into the resolvers.

// Resolver wires the GraphQL resolvers to their dependencies.
type Resolver struct {
	// Store is the notification persistence layer.
	Store *store.Store
}
