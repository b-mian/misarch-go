// Command inventory is the MiSArch inventory subgraph: it tracks individual
// physical product items (one document per unit) of product variants, serving a
// federated GraphQL API and participating in the order reservation saga over
// Dapr pub/sub. It owns the ProductItem entity and contributes productItems /
// inventoryCount to the foreign ProductVariant entity.
package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/ast"

	"misarch/inventory/events"
	"misarch/inventory/graph"
	"misarch/inventory/store"
	"misarch/pkg/dapr"
	"misarch/pkg/db"
	"misarch/pkg/server"
)

func main() {
	// server.Main handles the "healthcheck" self-probe argument BEFORE init
	// runs, so the database is only touched on a real start.
	server.Main(func(ctx context.Context) (server.Config, error) {
		// The original service reads DATABASE_URI / DATABASE_NAME (defaults
		// mongodb://localhost:27017 and "test"; the deployed compose sets only
		// DATABASE_URI). The platform db.ConnectMongo helper reads MONGODB_URI,
		// so bridge the original env var name into it if MONGODB_URI is unset.
		if os.Getenv("MONGODB_URI") == "" {
			uri := os.Getenv("DATABASE_URI")
			if uri == "" {
				uri = "mongodb://localhost:27017"
			}
			_ = os.Setenv("MONGODB_URI", uri)
		}
		dbName := os.Getenv("DATABASE_NAME")
		if dbName == "" {
			dbName = "test"
		}

		mdb, err := db.ConnectMongo(ctx, dbName)
		if err != nil {
			return server.Config{}, err
		}

		st := store.New(mdb)
		client := dapr.NewClient()
		resolver := &graph.Resolver{Store: st, Dapr: client}

		gql := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))
		gql.AddTransport(transport.POST{})
		gql.AddTransport(transport.GET{})
		gql.SetQueryCache(lru.New[*ast.QueryDocument](1000))
		gql.Use(extension.Introspection{})

		// Dapr event subscriptions (spec §4). The event handlers drive the
		// per-item state machine and the order reservation saga; publishing
		// (reservation-succeeded/failed) goes through the same Dapr client.
		h := events.NewHandlers(st, client)
		sub := dapr.NewSubscriber()
		subscribe := func(topic, route string, fn dapr.EventHandler) {
			sub.Subscribe(topic, route, fn)
		}
		subscribe(events.TopicProductVariantCreated, events.RouteProductVariantCreated,
			func(ctx context.Context, data json.RawMessage) error { return h.ProductVariantCreated(ctx, data) })
		subscribe(events.TopicOrderCreated, events.RouteOrderCreated,
			func(ctx context.Context, data json.RawMessage) error { return h.OrderCreated(ctx, data) })
		subscribe(events.TopicPaymentEnabled, events.RoutePaymentEnabled,
			func(ctx context.Context, data json.RawMessage) error { return h.PaymentEnabled(ctx, data) })
		subscribe(events.TopicPaymentFailed, events.RoutePaymentFailed,
			func(ctx context.Context, data json.RawMessage) error { return h.PaymentFailed(ctx, data) })
		subscribe(events.TopicShipmentStatusUpdated, events.RouteShipmentStatusUpdated,
			func(ctx context.Context, data json.RawMessage) error { return h.ShipmentStatusUpdated(ctx, data) })
		subscribe(events.TopicShipmentCreated, events.RouteShipmentCreated,
			func(ctx context.Context, data json.RawMessage) error { return h.ShipmentCreated(ctx, data) })
		subscribe(events.TopicDiscountValidationFailed, events.RouteDiscountValidationFailed,
			func(ctx context.Context, data json.RawMessage) error { return h.DiscountValidationFailed(ctx, data) })

		return server.Config{ServiceName: "inventory", GraphQL: gql, Subscriber: sub}, nil
	})
}
