// Package deps anchors module dependencies that only a subset of the services
// import (kept here so `go mod tidy` run at the platform level never drops
// them while individual services are being ported).
package deps

import (
	_ "github.com/99designs/gqlgen/graphql/handler"
	_ "github.com/99designs/gqlgen/plugin/federation/fedruntime"
	_ "github.com/minio/minio-go/v7"
	_ "github.com/rabbitmq/amqp091-go"
	_ "github.com/shopspring/decimal"
)
