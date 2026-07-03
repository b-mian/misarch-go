// Command wishlist is the MiSArch wishlist subgraph: it owns customer wishlists
// (each tying one user to a deduplicated set of product variants) and serves a
// federated GraphQL API over MongoDB. It publishes NOTHING; it only subscribes
// to two Dapr events (user/user/created and catalog/product-variant/created) to
// maintain the local user / product_variant shadow collections used for
// referential-integrity validation.
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
	"misarch/wishlist/events"
	"misarch/wishlist/graph"
	"misarch/wishlist/store"
)

func main() {
	// server.Main handles the "healthcheck" self-probe argument BEFORE init
	// runs, so the database is only touched on a real start.
	server.Main(func(ctx context.Context) (server.Config, error) {
		mdb, err := db.ConnectMongo(ctx, "wishlist-database")
		if err != nil {
			return server.Config{}, err
		}

		st := store.New(mdb)
		resolver := &graph.Resolver{Store: st}

		gql := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))
		gql.AddTransport(transport.POST{})
		gql.AddTransport(transport.GET{})
		gql.SetQueryCache(lru.New[*ast.QueryDocument](1000))
		gql.Use(extension.Introspection{})

		// The wishlist service publishes nothing, so there is no Dapr client. Its
		// two subscriptions are served through the bespoke events package (spec
		// §4): a single /on-topic-event route that branches on the CloudEvent
		// topic, a /dapr/subscribe list using the `pubsubName` (camelCase) key,
		// and a `{"status":0}` success body — a byte-for-byte reproduction of the
		// original Rust contract that the shared per-route Subscriber cannot
		// express. They are wired via ExtraRoutes; Subscriber stays nil.
		return server.Config{
			ServiceName: "wishlist",
			GraphQL:     gql,
			ExtraRoutes: func(mux *http.ServeMux) {
				events.Register(mux, st)
			},
		}, nil
	})
}
