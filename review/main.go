// Command review is the MiSArch review subgraph: it owns customer reviews
// (one user × one product variant, with a body, 1–5 star rating, timestamps and
// a visibility flag) and serves a federated GraphQL API. It maintains local
// shadow copies of users, products and product variants materialized purely
// from Dapr pub/sub create events, which are the referential-integrity source
// for review creation and the join keys for the federated reviews fields. It is
// MongoDB-backed and publishes no events — it only subscribes.
package main

import (
	"context"
	"net/http"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/ast"

	"misarch/pkg/db"
	"misarch/pkg/server"
	"misarch/review/events"
	"misarch/review/graph"
	"misarch/review/store"
)

func main() {
	// server.Main handles the "healthcheck" self-probe argument BEFORE init
	// runs, so the database is only touched on a real start.
	server.Main(func(ctx context.Context) (server.Config, error) {
		// Database name is hard-coded "review-database" in the original service.
		database, err := db.ConnectMongo(ctx, "review-database")
		if err != nil {
			return server.Config{}, err
		}

		st := store.New(database)
		resolver := &graph.Resolver{Store: st}

		gql := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))
		gql.AddTransport(transport.POST{})
		gql.AddTransport(transport.GET{})
		gql.SetQueryCache(lru.New[*ast.QueryDocument](1000))
		gql.Use(extension.Introspection{})

		// The review service uses custom Dapr routes (the subscription list
		// needs camelCase pubsubName, the ack body is {"status":0}, and two
		// topics share the /on-topic-event route), so it registers them via
		// ExtraRoutes rather than the generic dapr.Subscriber.
		eventHandler := events.NewHandler(st)

		return server.Config{
			ServiceName: "review",
			GraphQL:     gql,
			ExtraRoutes: func(mux *http.ServeMux) {
				eventHandler.Routes(mux)
			},
		}, nil
	})
}
