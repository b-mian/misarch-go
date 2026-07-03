// Command address is the MiSArch address subgraph: it owns user and vendor
// addresses, serves a federated GraphQL API (extending the User entity with an
// addresses connection), publishes address creation/archival events over Dapr
// pub/sub, and keeps a local replica of user ids fed by user/user/created
// events so it can enforce the "address belongs to an existing user" rule.
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

	"misarch/address/graph"
	"misarch/address/store"
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

		// Replicate user ids from the user service. The event carries a richer
		// user DTO, but only its id is used; unknown fields are ignored. A
		// duplicate delivery violates the primary key and returns 500 so Dapr
		// redelivers — matching the original (no idempotency guard).
		sub := dapr.NewSubscriber()
		sub.Subscribe("user/user/created", "/subscription/user/user/created",
			func(ctx context.Context, data json.RawMessage) error {
				var payload struct {
					ID uuid.UUID `json:"id"`
				}
				if err := json.Unmarshal(data, &payload); err != nil {
					return fmt.Errorf("decode user/user/created: %w", err)
				}
				return st.RegisterUser(ctx, payload.ID)
			})

		return server.Config{ServiceName: "address", GraphQL: gql, Subscriber: sub}, nil
	})
}
