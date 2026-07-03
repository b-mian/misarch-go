// Command shipment is the MiSArch shipment subgraph. It owns shipment methods
// (CRUD-ish over GraphQL mutations) and shipments (created reactively from
// payment/return Dapr events, priced against shipment methods, and pushed to an
// external shipment provider). It exposes:
//   - a federated GraphQL API (queries + two mutations),
//   - five Dapr event subscriptions (address/catalog/payment/return),
//   - a provider status callback (POST /shipment/{id}/status),
//   - the ECS experiment-config endpoints (GET/POST /ecs/*).
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

	"misarch/pkg/dapr"
	"misarch/pkg/db"
	"misarch/pkg/server"
	"misarch/shipment/ecs"
	"misarch/shipment/events"
	"misarch/shipment/graph"
	"misarch/shipment/handlers"
	"misarch/shipment/provider"
	"misarch/shipment/service"
	"misarch/shipment/store"
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
		daprClient := dapr.NewClient()

		// Experiment config (process-local, mutable via /ecs/variables) governs
		// the external-provider retry loop.
		ecsConfig := ecs.New()
		providerClient := provider.New(os.Getenv("MISARCH_SHIPMENT_PROVIDER_ENDPOINT"), ecsConfig)

		// Shared fee/weight domain logic used by the GraphQL resolvers.
		svc := service.NewService(st)
		// The create-shipment saga used by the Dapr event handlers.
		saga := service.NewShipmentSaga(st, daprClient, providerClient)

		resolver := &graph.Resolver{Store: st, Service: svc, Dapr: daprClient}

		gql := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))
		gql.AddTransport(transport.POST{})
		gql.AddTransport(transport.GET{})
		gql.SetQueryCache(lru.New[*ast.QueryDocument](1000))
		gql.Use(extension.Introspection{})

		h := handlers.New(st, saga, ecsConfig)

		// Subscribe to the five topics (routes embed the topic string exactly).
		sub := dapr.NewSubscriber()
		sub.Subscribe(events.TopicUserAddressCreated, events.RouteUserAddressCreated,
			func(ctx context.Context, data json.RawMessage) error { return h.OnUserAddressCreated(ctx, data) })
		sub.Subscribe(events.TopicVendorAddressCreated, events.RouteVendorAddressCreated,
			func(ctx context.Context, data json.RawMessage) error { return h.OnVendorAddressCreated(ctx, data) })
		sub.Subscribe(events.TopicProductVariantVersionCreated, events.RouteProductVariantVersionCreated,
			func(ctx context.Context, data json.RawMessage) error {
				return h.OnProductVariantVersionCreated(ctx, data)
			})
		sub.Subscribe(events.TopicPaymentEnabled, events.RoutePaymentEnabled,
			func(ctx context.Context, data json.RawMessage) error { return h.OnPaymentEnabled(ctx, data) })
		sub.Subscribe(events.TopicReturnCreated, events.RouteReturnCreated,
			func(ctx context.Context, data json.RawMessage) error { return h.OnReturnCreated(ctx, data) })

		return server.Config{
			ServiceName: "shipment",
			GraphQL:     gql,
			Subscriber:  sub,
			ExtraRoutes: h.RegisterRoutes,
		}, nil
	})
}
