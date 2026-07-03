package graph

import (
	"misarch/media/store"
	"misarch/pkg/dapr"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for the app: the resolvers reach the
// MinIO object store through Store and publish the media-created event through
// Dapr. There is no relational or document database — the object store is the
// single source of truth (see the media spec §5).

// Resolver wires the GraphQL resolvers to their dependencies.
type Resolver struct {
	// Store is the media persistence layer (MinIO bucket "media-data").
	Store *store.Store
	// Dapr publishes pub/sub events (its *Client satisfies events.Publisher).
	Dapr *dapr.Client
}
