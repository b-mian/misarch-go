// Command user is the MiSArch user subgraph: it owns the User bounded context,
// serving a federated GraphQL API for reading and updating users. Users are not
// created via a mutation — they are created reactively in response to the
// inbound Dapr event user/user/create (published after a Keycloak account is
// provisioned), whereupon the service emits user/user/created so downstream
// services can materialize their own copy.
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

	"misarch/pkg/dapr"
	"misarch/pkg/db"
	"misarch/pkg/server"
	"misarch/user/events"
	"misarch/user/graph"
	"misarch/user/store"
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
		dc := dapr.NewClient()
		resolver := &graph.Resolver{Store: st, Dapr: dc}

		gql := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))
		gql.AddTransport(transport.POST{})
		gql.AddTransport(transport.GET{})
		gql.SetQueryCache(lru.New[*ast.QueryDocument](1000))
		gql.Use(extension.Introspection{})

		// User is created reactively from the inbound create event: insert
		// (id from the event, dateJoined = now, birthday/gender NULL), re-read,
		// then publish the created event with the persisted dateJoined. There
		// is no idempotency and no transaction across insert+publish, matching
		// the original (a duplicate id is a PK violation -> error -> redeliver).
		sub := dapr.NewSubscriber()
		sub.Subscribe(events.TopicUserCreate, "/subscription/user/user/create",
			func(ctx context.Context, data json.RawMessage) error {
				var in events.CreateUser
				if err := json.Unmarshal(data, &in); err != nil {
					return err
				}
				user, err := st.CreateUser(ctx, in.ID, in.Username, in.FirstName, in.LastName, time.Now().UTC())
				if err != nil {
					return err
				}
				return events.PublishUserCreated(ctx, dc, events.UserCreated{
					ID:         user.ID,
					Username:   user.Username,
					FirstName:  user.FirstName,
					LastName:   user.LastName,
					DateJoined: events.FormatDateJoined(user.DateJoined),
				})
			})

		return server.Config{ServiceName: "user", GraphQL: gql, Subscriber: sub}, nil
	})
}
