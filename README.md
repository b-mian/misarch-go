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

## Building & Running Locally

Prebuilt upstream images no longer apply to the business services, they build
locally from this repo (BuildKit required, default in modern Docker):

```sh
git clone -b vanilla https://github.com/b-mian/misarch-go.git
cd misarch-go
git submodule update --init
for s in address catalog discount inventory invoice media notification order payment return review shipment shoppingcart simulation tax user wishlist; do docker compose build "$s"; done
docker compose pull --ignore-buildable
docker compose up -d --no-build
```

## Notes

- Subgraph SDLs are derived from
  [MiSArch/schemas](https://github.com/MiSArch/schemas) files by
  `tools/sdlprep` (it strips only the infra gqlgen regenerates
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
