# MiSArch — Go rewrite (energy-efficient fork)

This fork replaces the 17 MiSArch business services (originally Kotlin/Spring,
Rust, and TypeScript/NestJS) with **Go implementations vendored directly into
this repository**, built by **one common Dockerfile** with aggressive layer
caching and a distroless static runtime. The goal is to cut the energy and
memory footprint of the running cluster while preserving the system's external
behavior exactly:

- identical GraphQL subgraph schemas (Apollo Federation v2.5,
  composed by the unchanged gateway),
- identical Dapr pub/sub topics, routes, and event payloads,
- identical databases and compose topology (Postgres/MongoDB/MinIO/RabbitMQ
  per service, same `-db`/`-dapr`/`-ecs` sidecar triplets),
- identical authorization semantics (gateway-issued `Authorized-User` header).

The gateway, frontend, Keycloak, and the experiment tooling
(experiment-config*, executors, testdata) remain consistent.

## Repository Layout

```
Dockerfile                  ← THE common Dockerfile (all 17 services)
.dockerignore               ← allowlist keeping the build context minimal
go.mod / go.sum             ← single Go module ("misarch") for all services
pkg/                        ← shared platform: server bootstrap, auth header,
                              Dapr pub/sub + invocation, Postgres/Mongo helpers,
                              OTel metrics, GraphQL scalars
tools/sdlprep/              ← canonical SDL → gqlgen input schema converter
<service>/                  ← one dir per business service:
  main.go                     wiring (server.Main pattern)
  schema.graphql              gqlgen input derived from MiSArch/schemas SDL
  gqlgen.yml                  codegen config
  graph/                      generated federation code + hand-written resolvers
  store/ (+ store/migrations) persistence layer (embedded SQL migrations)
  events/                     Dapr topic constants + payload structs
  docker-compose-base.yaml    service + db + dapr sidecar definition
```

## Building & Running

Prebuilt upstream images no longer apply to the business services, they build
locally from this repo (BuildKit required, default in modern Docker):

```sh
# build everything and start the full system
docker compose up --build

# or the dev variant (no ghcr images at all)
docker compose -f docker-compose-dev.yaml up --build

# build one service image by hand
docker build --build-arg SERVICE=catalog -t misarch-go/catalog:latest .
```

The first build downloads Go dependencies once; every further service build
reuses that cached layer. Code changes to one service invalidate only that
service's final build stage.

### Local Development

```sh
go build ./...                      # compile all services + platform
go vet ./...
cd <service> && go tool gqlgen generate   # re-run codegen after resolver config changes
```

## Notes

- Subgraph SDLs are derived mechanically from the canonical
  [MiSArch/schemas](https://github.com/MiSArch/schemas) files by
  `tools/sdlprep` (it strips only the federation machinery gqlgen regenerates
  itself). The gateway's committed supergraph inputs therefore stay valid.
- Go services serve GraphQL on both `/` and `/graphql`, so both Dapr
  routing-URL styles in the gateway's `supergraph.yaml` keep working.
- Compose healthchecks use the service binary's built-in self-probe
  (`/service healthcheck`) because distroless images contain no shell/wget.
- Postgres services replace Flyway with embedded SQL migrations applied at
  startup (`schema_migrations` table); Mongo services create their indexes at
  startup.

## License

MisArch is [MIT licensed](LICENSE).
