# MiSArch — Go rewrite (energy-efficient fork)

This fork replaces the 17 MiSArch business services (originally Kotlin/Spring,
Rust, and TypeScript/NestJS) with **Go implementations vendored directly into
this repository**, built by **one common Dockerfile** with aggressive layer
caching and a distroless static runtime. The goal is to cut the energy and
memory footprint of the running cluster while preserving the system's external
behavior exactly:

The gateway, frontend, Keycloak, and the experiment tooling
(experiment-config*, executors, testdata) remain consistent.

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

## Building & Running ON GKE (Cloud)

Unfortunately, one of the major limitations in this re-factor was lack of CPU/MEM 
and resource quota to run the services in GKE.

Also, many services broke or were not able to run in the cloud due to various issues, 
which may be common when applying/configuring terraform infra meant for a completely 
different runtime and architecture (JVM vs Golang). 

Too many dependencies and bugs to track/resolve with limited time for one person (EVEN WITH AN LLM!!) :(

## License

MisArch is [MIT licensed](LICENSE).
