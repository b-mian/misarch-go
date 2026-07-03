// Command invoice is the MiSArch invoice subgraph. It is event-driven: it has
// no GraphQL mutations. It materializes local read models (users, user
// addresses, the vendor address) from Dapr events, generates a Markdown invoice
// on discount/order/validation-succeeded, persists it in MongoDB, and (nominally)
// publishes invoice/invoice/created. It serves a federation subgraph exposing
// Invoice, Query.invoice, and the Order.invoice federated field.
//
// This is a bug-for-bug port of the original Rust service: the mismatched
// subscribe route, the field-name bugs in the read models, and the broken Dapr
// publish URL are all preserved (see events/ and store/ for the specifics).
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

	"misarch/invoice/events"
	"misarch/invoice/graph"
	"misarch/invoice/store"
	"misarch/pkg/db"
	"misarch/pkg/server"
)

// dbName is the MongoDB database, hardcoded in the original service.
const dbName = "invoice-database"

func main() {
	// --generate-schema: print the canonical SDL and exit, mirroring the Rust
	// CLI flag used by the update-schema CI workflow. gqlgen is schema-first, so
	// the on-disk schema.graphql IS the federation SDL input; emit it as-is.
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
		resolver := &graph.Resolver{Store: st}

		gql := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))
		gql.AddTransport(transport.POST{})
		gql.AddTransport(transport.GET{})
		gql.SetQueryCache(lru.New[*ast.QueryDocument](1000))
		gql.Use(extension.Introspection{})

		// The Dapr event surface (subscribe list + five delivery routes) is
		// served via ExtraRoutes rather than the shared dapr.Subscriber: the
		// original returns {"status":0} (not {"status":"SUCCESS"}), asserts the
		// topic (→ 500 on mismatch), and advertises a mismatched subscribe route
		// — behaviors the shared subscriber does not reproduce.
		evtService := events.NewService(st, events.NewPublisher())

		return server.Config{
			ServiceName: "invoice",
			GraphQL:     gql,
			ExtraRoutes: evtService.Register,
		}, nil
	})
}

// generateSchema writes the federation SDL to ./schemas/invoice.graphql (as the
// Rust --generate-schema flag does), copying the gqlgen schema input verbatim.
func generateSchema() {
	const src = "schema.graphql"
	const dstDir = "schemas"
	const dst = dstDir + "/invoice.graphql"

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
