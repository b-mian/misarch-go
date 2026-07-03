// Package otelinit configures OpenTelemetry metrics export over OTLP/HTTP,
// honoring the standard OTEL_* environment variables the compose files set
// (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_SERVICE_NAME, OTEL_METRIC_EXPORT_INTERVAL).
// Traces are disabled in the MiSArch deployment (OTEL_TRACES_EXPORTER=none),
// so only the metrics pipeline is set up.
package otelinit

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Setup initializes the global MeterProvider if OTEL_EXPORTER_OTLP_ENDPOINT is
// configured. It returns a shutdown function (never nil).
func Setup(ctx context.Context, serviceName string) func(context.Context) error {
	noop := func(context.Context) error { return nil }
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return noop
	}
	if env := os.Getenv("OTEL_SERVICE_NAME"); env != "" {
		serviceName = env
	}

	exporter, err := otlpmetrichttp.New(ctx) // endpoint/insecure taken from env
	if err != nil {
		slog.Warn("otel: metric exporter setup failed, metrics disabled", "error", err)
		return noop
	}

	interval := 60 * time.Second
	if raw := os.Getenv("OTEL_METRIC_EXPORT_INTERVAL"); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
			interval = time.Duration(ms) * time.Millisecond
		}
	}

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
	))
	if err != nil {
		res = resource.Default()
	}

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(interval))),
	)
	otel.SetMeterProvider(provider)
	return provider.Shutdown
}

// WrapHandler instruments an HTTP handler with OTel HTTP server metrics.
func WrapHandler(name string, h http.Handler) http.Handler {
	return otelhttp.NewHandler(h, name)
}
