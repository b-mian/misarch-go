// Command simulation is the MiSArch fake external provider: it stands in for a
// payment processor and a shipment carrier. It is NOT a GraphQL federation
// subgraph — it exposes a small HTTP/REST surface plus a RabbitMQ producer and
// consumer, and no database (state is durable RabbitMQ queues + two in-memory
// repositories lost on restart).
//
// Flow: a MiSArch service POSTs a registration to /payment/register or
// /shipment/register; the simulation enqueues it on a durable queue and records
// it in memory; a background consumer waits a configurable delay, rolls a
// success/failure probability, and calls the originating service back over HTTP.
// Operators can force a status via /{payment,shipment}/update, which blocks the
// entity so the automatic queue result is discarded. All tunables (processing
// time, success rate, per-minute rate limit) are runtime-adjustable through the
// ECS contract: GET /ecs/defined-variables + POST /ecs/variables.
//
// This is a faithful port of the reference NestJS service, preserving its
// observable behavior and quirks (see the service spec): void handlers return
// 200 with an empty body, the manual-update not-found message says "Shipment
// not found" for payments too, amount is dropped from the in-memory payment
// record but still emitted on the queue, unknown ECS keys return 500, the
// rate-limit gate leaves messages unacked and a minute-boundary reconnect
// redelivers them, and the in-memory record is deleted at consume time.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"misarch/pkg/server"
	"misarch/simulation/config"
	"misarch/simulation/connector"
	"misarch/simulation/queue"
	"misarch/simulation/store"
)

func main() {
	// server.Main handles the "healthcheck" self-probe argument BEFORE init
	// runs, so RabbitMQ is only touched on a real start.
	server.Main(func(ctx context.Context) (server.Config, error) {
		// Seed the ECS variable store (defaults for the six tunables). This is
		// the reference's ConfigurationService.onModuleInit.
		cfg := config.New()

		// Resolve RABBITMQ_URL through the ECS resolution order (override map →
		// env → fallback). It is not an ECS-defined variable, so this comes
		// from env. Fatal if still "NOT_SET", mirroring the reference's
		// EventProcessorService.onModuleInit throwing 'RABBITMQ_URL is not set'.
		rabbitURL := cfg.GetCurrentVariableValueString("RABBITMQ_URL", "NOT_SET")
		if rabbitURL == "NOT_SET" {
			slog.Error("RABBITMQ_URL is not set")
			os.Exit(1)
		}

		payments := store.NewPaymentRepository()
		shipments := store.NewShipmentRepository()

		conn := connector.New(cfg, &http.Client{Timeout: 30 * time.Second})

		proc := queue.NewProcessor(cfg, payments, shipments, conn, rabbitURL, nil)

		// Tie the queue lifetime (consumers, minute-reset ticker, AMQP
		// connection) to SIGTERM/SIGINT so the connection closes cleanly on
		// shutdown. server.Run installs its own signal handler for the HTTP
		// server; both firing on the same signals is harmless.
		procCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)

		// Start connects (retrying every 5s until reachable) and launches the
		// consumers + reset loop. It blocks only until the initial connection
		// succeeds.
		if err := proc.Start(procCtx); err != nil {
			stop()
			return server.Config{}, err
		}

		// Close the AMQP resources once the shutdown signal fires.
		go func() {
			<-procCtx.Done()
			proc.Close()
			stop()
		}()

		h := &handlers{
			cfg:       cfg,
			payments:  payments,
			shipments: shipments,
			connector: conn,
			queue:     proc,
			ctx:       context.Background(),
		}

		// No GraphQL, no Dapr subscriptions: the app talks to RabbitMQ directly
		// and exposes only the REST + ECS routes via ExtraRoutes.
		return server.Config{
			ServiceName: "simulation",
			ExtraRoutes: h.register,
		}, nil
	})
}
