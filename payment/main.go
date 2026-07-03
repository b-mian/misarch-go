// Command payment is the MiSArch payment subgraph. It manages users' stored
// payment informations and plans the execution and compensation of payments as
// an order-saga participant: reacting to discount/user Dapr events, driving the
// per-method payment state machines, registering payments with the external
// simulation provider, and publishing saga events. It serves a federated
// GraphQL API, a REST callback for the simulation provider, two Dapr topic
// routes, and a health endpoint.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/ast"

	"misarch/payment/graph"
	"misarch/payment/service"
	"misarch/payment/store"
	"misarch/pkg/dapr"
	"misarch/pkg/db"
	"misarch/pkg/server"
)

func main() {
	// server.Main handles the "healthcheck" self-probe argument BEFORE init
	// runs, so the database is only touched on a real start.
	server.Main(func(ctx context.Context) (server.Config, error) {
		// The platform Mongo helper reads MONGODB_URI; the database name comes
		// from DATABASE_NAME (compose sets it to "misarch"), defaulting to
		// "misarch" to match the original DATABASE_NAME.
		dbName := os.Getenv("DATABASE_NAME")
		if dbName == "" {
			dbName = "misarch"
		}
		mdb, err := db.ConnectMongo(ctx, dbName)
		if err != nil {
			return server.Config{}, err
		}

		st, err := store.New(ctx, mdb)
		if err != nil {
			return server.Config{}, err
		}

		daprClient := dapr.NewClient()
		svc := service.New(st, daprClient, service.NewSimulation())

		resolver := &graph.Resolver{Store: st, Dapr: daprClient}

		gql := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))
		gql.AddTransport(transport.POST{})
		gql.AddTransport(transport.GET{})
		gql.SetQueryCache(lru.New[*ast.QueryDocument](1000))
		gql.Use(extension.Introspection{})

		// Dapr subscriptions. Routes carry a leading slash because the platform
		// Subscriber uses the same string as the net/http ServeMux pattern
		// (which requires a leading slash) and as the /dapr/subscribe route
		// field; Dapr normalizes both forms.
		sub := dapr.NewSubscriber()
		sub.Subscribe("discount/order/validation-succeeded", "/order-validation-succeeded",
			func(ctx context.Context, data json.RawMessage) error {
				return handleOrderValidationSucceeded(ctx, svc, data)
			})
		sub.Subscribe("user/user/created", "/user-created",
			func(ctx context.Context, data json.RawMessage) error {
				return handleUserCreated(ctx, svc, data)
			})

		// Start the two 15-minute overdue-payment cron jobs.
		svc.StartCron(ctx)

		return server.Config{
			ServiceName: "payment",
			GraphQL:     gql,
			Subscriber:  sub,
			ExtraRoutes: func(mux *http.ServeMux) {
				mux.HandleFunc("POST /payment/update-payment-status", updatePaymentStatusHandler(ctx, svc))
			},
		}, nil
	})
}
