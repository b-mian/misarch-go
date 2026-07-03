# syntax=docker/dockerfile:1
# Common Dockerfile for every MiSArch Go service.
#
#   docker build --build-arg SERVICE=<service-dir> -t misarch/<service> .
#
# All 17 services share this file. Layer caching is structured so that:
#   1. `go mod download` runs against go.mod/go.sum only — this layer is
#      IDENTICAL for every service and every code change, so all images share
#      one cached dependency layer.
#   2. pkg/ (shared platform) is copied separately from the service source, so
#      editing one service invalidates only that service's build layer.
#   3. BuildKit cache mounts persist the module and build caches across
#      builds, making rebuilds incremental.
# The runtime image is distroless static (no shell, no libc, non-root,
# ~2 MB base): minimal memory/energy footprint and attack surface. The
# service binary doubles as its own healthcheck probe
# (`/service healthcheck`) since there is no wget in distroless.

ARG GO_VERSION=1.25

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS deps
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

FROM deps AS build
ARG SERVICE
ARG TARGETOS
ARG TARGETARCH
COPY pkg/ pkg/
COPY ${SERVICE}/ ${SERVICE}/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/service ./${SERVICE}

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/service /service
EXPOSE 8080
ENTRYPOINT ["/service"]
