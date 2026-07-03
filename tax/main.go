// Command tax is the MiSArch tax subgraph: it manages tax rates and their
// versioned rates, serving a federated GraphQL API and publishing tax-rate /
// tax-rate-version creation events over Dapr pub/sub. It is publish-only (no
// subscriptions), so no Subscriber is wired.
package main

import (
	"context"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/ast"

	"misarch/pkg/dapr"
	"misarch/pkg/db"
	"misarch/pkg/server"
	"misarch/tax/graph"
	"misarch/tax/store"
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

		// Tax is publish-only: no Dapr subscriptions.
		return server.Config{ServiceName: "tax", GraphQL: gql}, nil
	})
}
