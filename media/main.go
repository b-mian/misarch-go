// Command media is the MiSArch media subgraph: it stores uploaded files in a
// MinIO/S3 bucket ("media-data") and resolves federated Media entities to
// pre-signed, reverse-proxied download paths. It publishes a media-created
// event over Dapr pub/sub on upload and subscribes to nothing.
//
// Unlike the other MiSArch services it has NO relational or document database:
// the object store is the single source of truth (despite the compose file
// setting a dead MONGODB_URI, which this service ignores — see the media spec
// §5). It is therefore publish-only, so no Subscriber is wired.
package main

import (
	"context"
	"math"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/ast"

	"misarch/media/config"
	"misarch/media/graph"
	"misarch/media/store"
	"misarch/pkg/dapr"
	"misarch/pkg/server"
)

func main() {
	// server.Main handles the "healthcheck" self-probe argument BEFORE init
	// runs, so MinIO / the bucket bootstrap is only touched on a real start.
	server.Main(func(ctx context.Context) (server.Config, error) {
		// MINIO_ENDPOINT is required: the Rust service unwrap()s it (panicking
		// if unset), so fail fast here with a clear message. PATH_EXPIRATION_TIME
		// and PROXY_PATH fall back to their defaults.
		cfg, err := config.Load()
		if err != nil {
			return server.Config{}, err
		}

		// Connect to MinIO and bootstrap the "media-data" bucket (create, or
		// fall back to the existing bucket — matching the Rust create-then-open).
		st, err := store.New(ctx, cfg)
		if err != nil {
			return server.Config{}, err
		}

		resolver := &graph.Resolver{Store: st, Dapr: dapr.NewClient()}

		gql := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))
		gql.AddTransport(transport.POST{})
		gql.AddTransport(transport.GET{})
		// media serves GraphQL file uploads via the multipart request spec
		// (the Upload! scalar); without this transport uploads 400.
		//
		// The Upload size MUST be effectively unlimited to mirror the Rust
		// service (which read_to_end's the whole file with no cap) fronted by
		// Nginx `client_max_body_size 0` (media spec §7, §10 step 2). gqlgen's
		// MultipartForm treats a zero MaxUploadSize/MaxMemory as "use the 32 MiB
		// default" (transport/http_form_multipart.go maxUploadSize/maxMemory),
		// so a bare MultipartForm{} would reject any body over 32 MiB with
		// "request body too large" before the resolver runs and truncate
		// chunked uploads via http.MaxBytesReader. Use math.MaxInt64 so the
		// ContentLength gate never trips and MaxBytesReader never truncates;
		// MaxMemory is set the same so the part is buffered fully in memory,
		// matching the Rust read_to_end (no temp-file spill).
		gql.AddTransport(transport.MultipartForm{
			MaxUploadSize: math.MaxInt64,
			MaxMemory:     math.MaxInt64,
		})
		gql.SetQueryCache(lru.New[*ast.QueryDocument](1000))
		gql.Use(extension.Introspection{})

		// Media is publish-only: it subscribes to no Dapr topics, so the
		// Subscriber is left nil (server.Run guards it).
		return server.Config{ServiceName: "media", GraphQL: gql}, nil
	})
}
