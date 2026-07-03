// Command discount is the MiSArch discount subgraph: it owns discounts,
// coupons, coupon redemptions and discount usages, serving a federated GraphQL
// API, maintaining id-only replicas of foreign entities from Dapr events,
// participating in the order-validation saga, and publishing
// discount/coupon/order events over Dapr pub/sub.
package main

import (
	"context"
	"encoding/json"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/ast"

	"misarch/discount/events"
	"misarch/discount/graph"
	"misarch/discount/store"
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

		// Dapr subscriptions. Routes are /subscription/<full/topic/with/slashes>,
		// matching the original @PostMapping("/subscription/${TOPIC}") paths that
		// GET /dapr/subscribe advertises.
		sub := dapr.NewSubscriber()
		sub.Subscribe(events.TopicUserCreated, "/subscription/"+events.TopicUserCreated,
			func(ctx context.Context, data json.RawMessage) error {
				var e events.UserCreated
				if err := json.Unmarshal(data, &e); err != nil {
					return err
				}
				return st.RegisterUser(ctx, e.ID)
			})
		sub.Subscribe(events.TopicProductCreated, "/subscription/"+events.TopicProductCreated,
			func(ctx context.Context, data json.RawMessage) error {
				var e events.ProductCreated
				if err := json.Unmarshal(data, &e); err != nil {
					return err
				}
				return st.RegisterProduct(ctx, e.ID, e.CategoryIds)
			})
		sub.Subscribe(events.TopicCategoryCreated, "/subscription/"+events.TopicCategoryCreated,
			func(ctx context.Context, data json.RawMessage) error {
				var e events.CategoryCreated
				if err := json.Unmarshal(data, &e); err != nil {
					return err
				}
				return st.RegisterCategory(ctx, e.ID)
			})
		sub.Subscribe(events.TopicProductVariantCreated, "/subscription/"+events.TopicProductVariantCreated,
			func(ctx context.Context, data json.RawMessage) error {
				var e events.ProductVariantCreated
				if err := json.Unmarshal(data, &e); err != nil {
					return err
				}
				return st.RegisterProductVariant(ctx, e.ID, e.ProductID)
			})
		sub.Subscribe(events.TopicInventoryReservationSucceeded, "/subscription/"+events.TopicInventoryReservationSucceeded,
			resolver.HandleReservationSucceeded)

		return server.Config{ServiceName: "discount", GraphQL: gql, Subscriber: sub}, nil
	})
}
