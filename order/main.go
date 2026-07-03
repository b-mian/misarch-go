// Command order is the MiSArch order subgraph — the saga heart of the platform.
// It owns the order lifecycle (createOrder → PENDING, placeOrder → PLACED,
// lazy REJECTED on timeout), fans out synchronous cross-service GraphQL calls
// (shoppingcart, inventory, discount, shipment) to compose orders, publishes
// order/order/created on placement and order/order-compensation/created on the
// shipment-failure compensation saga, and maintains local foreign-type read
// models (users, product variants, tax rates, coupons, shipment methods, user
// addresses) from Dapr events.
//
// This is a faithful port of the original Rust service, reproducing its
// observable behavior — including its known idiosyncrasies (the never-written
// order_items collection, the string-vs-bool is_publicly_visible after an
// update, the apparently-inverted compensation verify query, the discarded
// shipment fee, the never-populated rejection_reason). See the service spec and
// the store/events packages for the specifics.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/ast"

	"misarch/order/events"
	"misarch/order/graph"
	"misarch/order/store"
	"misarch/pkg/db"
	"misarch/pkg/server"
)

// dbName is the MongoDB database, hardcoded in the original service. NOTE: it is
// "order-database", not "order" or the "order-db" container name.
const dbName = "order-database"

func main() {
	// --generate-schema: print the canonical SDL and exit, mirroring the Rust
	// CLI flag. gqlgen is schema-first, so the on-disk schema.graphql IS the
	// federation SDL input; emit it as-is.
	if len(os.Args) > 1 && os.Args[1] == "--generate-schema" {
		generateSchema()
		return
	}

	// server.Main handles the "healthcheck" self-probe argument BEFORE init
	// runs, so MongoDB is only touched on a real start.
	server.Main(func(ctx context.Context) (server.Config, error) {
		database, err := db.ConnectMongo(ctx, dbName)
		if err != nil {
			return server.Config{}, err
		}

		st := store.New(database)
		pub := events.NewPublisher()
		resolver := graph.NewResolver(st, pub)

		gql := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))
		gql.AddTransport(transport.POST{})
		gql.AddTransport(transport.GET{})
		gql.SetQueryCache(lru.New[*ast.QueryDocument](1000))
		gql.Use(extension.Introspection{})

		// The Dapr event surface (subscribe list + delivery routes) is served
		// via ExtraRoutes rather than the shared dapr.Subscriber: the original
		// returns {"status":0} (not {"status":"SUCCESS"}), asserts the topic
		// (→ 500 on mismatch), advertises pubsubName casing and a specific array
		// order, and serves the shipment-failed route WITHOUT advertising it in
		// /dapr/subscribe — behaviors the shared subscriber does not reproduce.
		evtService := events.NewService(st, pub)

		return server.Config{
			ServiceName: "order",
			GraphQL:     gql,
			ExtraRoutes: evtService.Register,
		}, nil
	})
}

// generateSchema writes the federation SDL to ./schemas/order.graphql (as the
// Rust --generate-schema flag does), copying the gqlgen schema input verbatim.
func generateSchema() {
	const src = "schema.graphql"
	const dstDir = "schemas"
	const dst = dstDir + "/order.graphql"

	data, err := os.ReadFile(src)
	if err != nil {
		slog.Error("generate-schema: read schema", "error", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		slog.Error("generate-schema: mkdir", "error", err)
		os.Exit(1)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		slog.Error("generate-schema: write schema", "error", err)
		os.Exit(1)
	}
	slog.Info("GraphQL schema written", "path", dst)
}
