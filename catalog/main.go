// Command catalog is the MiSArch catalog subgraph: it owns products, product
// variants, versioned variant snapshots, categories, and category
// characteristics, serving a federated GraphQL API. It publishes product /
// variant / version / category domain events over Dapr pub/sub and subscribes
// to tax-rate-created and media-created events to maintain its local id-only
// mirror tables (taxrateentity / mediaentity).
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/google/uuid"
	"github.com/vektah/gqlparser/v2/ast"

	"misarch/catalog/events"
	"misarch/catalog/graph"
	"misarch/catalog/store"
	"misarch/pkg/dapr"
	"misarch/pkg/db"
	"misarch/pkg/server"
)

func main() {
	// server.Main handles the "healthcheck" self-probe argument BEFORE init
	// runs, so the database is only touched on a real start.
	server.Main(func(ctx context.Context) (server.Config, error) {
		pool, err := db.ConnectPostgres(ctx)
		if err != nil {
			return server.Config{}, err
		}
		if err := db.Migrate(ctx, pool, store.Migrations); err != nil {
			return server.Config{}, err
		}

		st := store.New(pool)
		resolver := &graph.Resolver{Store: st, Dapr: dapr.NewClient()}

		gql := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))
		gql.AddTransport(transport.POST{})
		gql.AddTransport(transport.GET{})
		gql.SetQueryCache(lru.New[*ast.QueryDocument](1000))
		gql.Use(extension.Introspection{})

		// Subscribe to the two id-only mirror events. Each handler reads only
		// data.id and does a plain (non-idempotent) INSERT: a duplicate id
		// errors → 500 → Dapr redelivers, matching the original.
		sub := dapr.NewSubscriber()
		sub.Subscribe(events.TopicTaxRateCreated, events.RouteTaxRateCreated,
			func(ctx context.Context, data json.RawMessage) error {
				id, err := parseEventID(data)
				if err != nil {
					return err
				}
				return st.RegisterTaxRate(ctx, id)
			})
		sub.Subscribe(events.TopicMediaCreated, events.RouteMediaCreated,
			func(ctx context.Context, data json.RawMessage) error {
				id, err := parseEventID(data)
				if err != nil {
					return err
				}
				return st.RegisterMedia(ctx, id)
			})

		return server.Config{ServiceName: "catalog", GraphQL: gql, Subscriber: sub}, nil
	})
}

// eventID is the minimal shape of the incoming CloudEvent data payloads
// (CreateTaxRateDTO / CreateMediaDTO): only the id is read; unknown fields are
// ignored.
type eventID struct {
	ID uuid.UUID `json:"id"`
}

// parseEventID extracts the id from a mirror-event data payload.
func parseEventID(data json.RawMessage) (uuid.UUID, error) {
	var e eventID
	if err := json.Unmarshal(data, &e); err != nil {
		return uuid.Nil, fmt.Errorf("decode event data: %w", err)
	}
	return e.ID, nil
}
