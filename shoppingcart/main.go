// Command shoppingcart is the MiSArch shoppingcart subgraph: it owns each
// user's shopping cart (embedded in the user document, keyed by the user's
// UUID) and serves a federated GraphQL API over MongoDB. It publishes nothing;
// it only subscribes to Dapr events to maintain its local read-model (users and
// product_variants) and to empty carts on order checkout.
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
	"misarch/shoppingcart/events"
	"misarch/shoppingcart/graph"
	"misarch/shoppingcart/store"
)

func main() {
	// server.Main handles the "healthcheck" self-probe argument BEFORE init
	// runs, so the database is only touched on a real start.
	server.Main(func(ctx context.Context) (server.Config, error) {
		mdb, err := db.ConnectMongo(ctx, "shoppingcart-database")
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

		// Dapr event subscriptions (spec §4). The original Rust service served
		// user-created and product-variant-created on a single /on-topic-event
		// route and dispatched on the CloudEvent topic; the Go subscriber routes
		// by URL and hands the handler only the event data, so each topic gets a
		// distinct route. The /dapr/subscribe list is self-describing, so Dapr
		// behavior is unchanged. The service publishes nothing (no Dapr client).
		h := events.NewHandlers(st)
		sub := dapr.NewSubscriber()
		sub.Subscribe(events.TopicUserCreated, events.RouteUserCreated, h.UserCreated)
		sub.Subscribe(events.TopicProductVariantCreated, events.RouteProductVariantCreated, h.ProductVariantCreated)
		sub.Subscribe(events.TopicOrderCreated, events.RouteOrderCreated, h.OrderCreated)

		return server.Config{ServiceName: "shoppingcart", GraphQL: gql, Subscriber: sub}, nil
	})
}
