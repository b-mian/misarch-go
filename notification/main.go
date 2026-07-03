// Command notification is the MiSArch notification subgraph: it stores
// per-user notifications, serves a federated GraphQL API, and materializes
// notifications from two Dapr pub/sub topics (user creation and notification
// creation). It is a pure sink + query service — it publishes no events and
// makes no outgoing calls to other services.
package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/ast"

	"misarch/notification/events"
	"misarch/notification/graph"
	"misarch/notification/store"
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
		resolver := &graph.Resolver{Store: st}

		gql := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))
		gql.AddTransport(transport.POST{})
		gql.AddTransport(transport.GET{})
		gql.SetQueryCache(lru.New[*ast.QueryDocument](1000))
		gql.Use(extension.Introspection{})

		// Two subscriptions, both on the "pubsub" component. Handlers return an
		// error on any failure → 500 → Dapr redelivers (this powers the
		// eventual-consistency retry loop for notification-before-user, and the
		// intentional poison-message behavior on duplicate user creation).
		sub := dapr.NewSubscriber()
		sub.Subscribe(events.TopicUserCreated, events.RouteUserCreated,
			func(ctx context.Context, data json.RawMessage) error {
				var payload events.UserCreated
				if err := json.Unmarshal(data, &payload); err != nil {
					return err
				}
				// INSERT INTO userentity (id) VALUES (:id) — not idempotent: a
				// duplicate id violates the PK and returns 500.
				return st.CreateUser(ctx, payload.ID)
			})
		sub.Subscribe(events.TopicNotificationCreate, events.RouteNotificationCreate,
			func(ctx context.Context, data json.RawMessage) error {
				var payload events.NotificationCreate
				if err := json.Unmarshal(data, &payload); err != nil {
					return err
				}
				// Same code path as createNotification with no auth: an unknown
				// user returns 500 so Dapr retries until user/user/created lands.
				_, err := st.CreateNotification(ctx, payload.Title, payload.Body, payload.UserID, time.Now())
				return err
			})

		return server.Config{ServiceName: "notification", GraphQL: gql, Subscriber: sub}, nil
	})
}
