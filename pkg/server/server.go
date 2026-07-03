// Package server boots a MiSArch Go service: GraphQL endpoint (served on both
// /graphql and / to match either Dapr routing style), /health, Dapr
// subscription endpoints, auth-header parsing, OTel metrics, graceful
// shutdown, and a self-healthcheck mode for shell-less container images.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"misarch/pkg/auth"
	"misarch/pkg/dapr"
	"misarch/pkg/otelinit"
)

// Config describes one service.
type Config struct {
	// ServiceName is the Dapr app id / OTel service name (e.g. "catalog").
	ServiceName string
	// GraphQL is the gqlgen handler; may be nil for non-GraphQL services.
	GraphQL http.Handler
	// Subscriber holds the Dapr topic subscriptions; may be nil.
	Subscriber *dapr.Subscriber
	// ExtraRoutes registers additional service-specific routes; may be nil.
	ExtraRoutes func(mux *http.ServeMux)
}

const port = "8080"

// Main is the canonical service entrypoint. It handles the "healthcheck"
// self-probe argument BEFORE any dependencies are initialized (the probe must
// work even when the database is down), then builds the service via init and
// serves it.
func Main(init func(ctx context.Context) (Config, error)) {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(selfCheck())
	}
	ctx := context.Background()
	cfg, err := init(ctx)
	if err != nil {
		slog.Error("service init failed", "error", err)
		os.Exit(1)
	}
	Run(cfg)
}

// Run starts the service and blocks until SIGTERM/SIGINT. When the process is
// started with the single argument "healthcheck" it instead probes its own
// /health endpoint and exits 0/1 (used by the compose healthcheck, since the
// distroless runtime image has no shell or wget).
func Run(cfg Config) {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(selfCheck())
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	shutdownMetrics := otelinit.Setup(ctx, cfg.ServiceName)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"UP"}`))
	})
	if cfg.Subscriber != nil {
		cfg.Subscriber.Mount(mux)
	}
	if cfg.GraphQL != nil {
		gql := auth.Middleware(cfg.GraphQL)
		mux.Handle("POST /graphql", gql)
		mux.Handle("GET /graphql", gql)
		mux.Handle("POST /{$}", gql)
		mux.Handle("GET /{$}", gql)
	}
	if cfg.ExtraRoutes != nil {
		cfg.ExtraRoutes(mux)
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           otelinit.WrapHandler(cfg.ServiceName, mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("service listening", "service", cfg.ServiceName, "port", port)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutting down", "service", cfg.ServiceName)
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	_ = shutdownMetrics(shutdownCtx)
}

func selfCheck() int {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%s/health", port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck failed:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck failed: status", resp.StatusCode)
		return 1
	}
	return 0
}
