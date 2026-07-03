package graph

import (
	"misarch/address/store"
	"misarch/pkg/dapr"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for the app: the resolvers reach the
// database through Store and publish domain events through Dapr.

// Resolver wires the GraphQL resolvers to their dependencies.
type Resolver struct {
	// Store is the address persistence layer.
	Store *store.Store
	// Dapr publishes pub/sub events (its *Client satisfies events.Publisher).
	Dapr *dapr.Client
}
