# MiSArch — Go rewrite (energy-efficient fork)

This fork replaces the 17 MiSArch business services (originally Kotlin/Spring,
Rust, and TypeScript/NestJS) with **Go implementations vendored directly into
this repository**, built by **one common Dockerfile** with aggressive layer
caching and a distroless static runtime. The goal is to cut the energy and
memory footprint of the running cluster while preserving the system's external
behavior exactly:

- identical GraphQL federation subgraph schemas (Apollo Federation v2.5,
  composed by the unchanged gateway),
- identical Dapr pub/sub topics, routes, and event payloads,
- identical databases and compose topology (Postgres/MongoDB/MinIO/RabbitMQ
  per service, same `-db`/`-dapr`/`-ecs` sidecar triplets),
- identical authorization semantics (gateway-issued `Authorized-User` header).

The gateway, frontend, Keycloak, and the experiment tooling
(experiment-config*, executors, testdata) remain upstream submodules — they are
infrastructure, not business services.

## Why this reduces energy & memory

| | Before (JVM / Node / Rust mix) | After (Go) |
|---|---|---|
| Runtime image base | `eclipse-temurin:17` / `node:18` (200–400 MB) | distroless static (~2 MB) + single binary |
| Process model | JVM heap + JIT warmup; Node event loop + V8 heap | AOT-compiled static binary, no shared runtime |
| Typical idle RSS per service | 150–500 MB (JVM), 60–150 MB (Node) | 10–30 MB |
| Cold start | seconds (JVM) | milliseconds |
| Build caching | per-service dependency downloads | one shared `go mod download` layer for all 17 images |

17 services × the per-service saving is the point: the whole cluster fits in a
fraction of the memory, idles at a fraction of the CPU, and rebuilds share one
dependency layer.

## Repository layout

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

## Building & running

Prebuilt upstream images no longer apply to the business services — they build
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

### Local development

```sh
go build ./...                      # compile all services + platform
go vet ./...
cd <service> && go tool gqlgen generate   # re-run codegen after resolver config changes
```

## Fidelity notes

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
