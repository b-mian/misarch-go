// Command return is the MiSArch return subgraph. It owns the Return bounded
// context: it materializes local read models (orders, order items, shipments,
// product-variant-version return windows) from Dapr events, exposes a federated
// GraphQL API (the Return entity, return/returns queries, and federated User /
// Order / OrderItem fields), and publishes return/return/created when a return
// is created via the createReturn mutation.
//
// BUG-1 (faithfully reproduced): the catalog/product-variant-version/created
// topic is intentionally NOT subscribed — the original handler lacked its
// annotations, so the pvv table stays empty and createReturn cannot succeed for
// real order items. See specs/return.md §9.
package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/ast"

	"misarch/pkg/dapr"
	"misarch/pkg/db"
	"misarch/pkg/server"
	"misarch/return/events"
	"misarch/return/graph"
	"misarch/return/store"
)

func main() {
	// server.Main handles the "healthcheck" self-probe argument BEFORE init
	// runs, so the database is only touched on a real start.
	server.Main(func(ctx context.Context) (server.Config, error) {
		pool, err := db.ConnectPostgres(ctx)
		if err != nil {
			return server.Config{}, err
		}
		if err := db.Migrate(ctx, pool, store.Migrations); err != nil {
			return server.Config{}, err
		}

		st := store.New(pool)
		resolver := &graph.Resolver{Store: st, Dapr: dapr.NewClient()}

		gql := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))
		gql.AddTransport(transport.POST{})
		gql.AddTransport(transport.GET{})
		gql.SetQueryCache(lru.New[*ast.QueryDocument](1000))
		gql.Use(extension.Introspection{})

		sub := dapr.NewSubscriber()
		// Subscription declaration order matches the original EventController's
		// /dapr/subscribe output: shipment-created, shipment-status-updated,
		// order-created. catalog/product-variant-version/created is NOT
		// subscribed (BUG-1).
		sub.Subscribe(events.TopicShipmentCreated, events.RouteShipmentCreated, handleShipmentCreated(st))
		sub.Subscribe(events.TopicShipmentStatusUpdated, events.RouteShipmentStatusUpdated, handleShipmentStatusUpdated(st))
		sub.Subscribe(events.TopicOrderCreated, events.RouteOrderCreated, handleOrderCreated(st))

		return server.Config{ServiceName: "return", GraphQL: gql, Subscriber: sub}, nil
	})
}

// handleShipmentCreated mirrors ShipmentService.registerShipment: it ignores
// shipments with a null orderId (they belong to a return), inserts the shipment
// (deliveredAt set only when the status is DELIVERED), then upserts each order
// item's sentWithId. Any error → 500 → Dapr retries.
func handleShipmentCreated(st *store.Store) dapr.EventHandler {
	return func(ctx context.Context, data json.RawMessage) error {
		var s events.ShipmentCreated
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		if s.OrderID == nil {
			return nil
		}
		var deliveredAt *time.Time
		if s.Status == events.ShipmentStatusDelivered {
			now := time.Now().UTC()
			deliveredAt = &now
		}
		if err := st.CreateShipment(ctx, s.ID, deliveredAt); err != nil {
			return err
		}
		for _, itemID := range s.OrderItemIds {
			if err := st.UpsertOrderItemFromShipment(ctx, itemID, s.ID, *s.OrderID); err != nil {
				return err
			}
		}
		return nil
	}
}

// handleShipmentStatusUpdated mirrors ShipmentService.updateDeliveredAt: it acts
// only on a DELIVERED status, setting the shipment's deliveredAt to now (loading
// it first, so an unknown shipment errors → 500 → retry).
func handleShipmentStatusUpdated(st *store.Store) dapr.EventHandler {
	return func(ctx context.Context, data json.RawMessage) error {
		var s events.ShipmentStatusUpdated
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		if s.Status != events.ShipmentStatusDelivered {
			return nil
		}
		return st.SetShipmentDelivered(ctx, s.ID, time.Now().UTC())
	}
}

// handleOrderCreated mirrors OrderService.registerOrder: it inserts the order
// (raw, non-idempotent) then upserts each order item's compensatableAmount and
// productVariantVersionId.
func handleOrderCreated(st *store.Store) dapr.EventHandler {
	return func(ctx context.Context, data json.RawMessage) error {
		var o events.OrderCreated
		if err := json.Unmarshal(data, &o); err != nil {
			return err
		}
		if err := st.CreateOrder(ctx, o.ID, o.UserID); err != nil {
			return err
		}
		for _, item := range o.OrderItems {
			if err := st.UpsertOrderItemFromOrder(ctx, item.ID, item.CompensatableAmount, o.ID, item.ProductVariantVersionID); err != nil {
				return err
			}
		}
		return nil
	}
}
