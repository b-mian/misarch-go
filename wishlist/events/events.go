// Package events implements the wishlist service's Dapr pub/sub *subscribe*
// surface. The service publishes NOTHING; it only consumes user and
// product-variant creation events into local shadow collections.
//
// The shared pkg/dapr.Subscriber is not used here because this service must
// reproduce the original Rust contract byte-for-byte: the subscription list
// uses the JSON key `pubsubName` (camelCase), BOTH topics share the single
// route `/on-topic-event` and the handler branches on the CloudEvent `topic`
// field, the success body is exactly `{"status":0}`, and any failure/unknown
// topic returns a bare HTTP 500 so Dapr redelivers. These routes are wired via
// server.Config.ExtraRoutes.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
)

// Topic strings (identical to the original service and the emitting services).
const (
	// TopicUserCreated is emitted by the user service on user creation.
	TopicUserCreated = "user/user/created"
	// TopicProductVariantCreated is emitted by the catalog service on product
	// variant creation.
	TopicProductVariantCreated = "catalog/product-variant/created"
)

// route is the single Dapr event route both subscriptions POST to.
const route = "/on-topic-event"

// pubsubName is the Dapr pub/sub component name.
const pubsubName = "pubsub"

// Shadows is the subset of the store the event handlers need: idempotent
// inserts of the user / product-variant shadow copies.
type Shadows interface {
	InsertUser(ctx context.Context, id uuid.UUID) error
	InsertProductVariant(ctx context.Context, id uuid.UUID) error
}

// subscription is one entry of the /dapr/subscribe response. The JSON key is
// `pubsubName` (camelCase) to match the original serde rename.
type subscription struct {
	PubsubName string `json:"pubsubName"`
	Topic      string `json:"topic"`
	Route      string `json:"route"`
}

// event is the relevant part of the CloudEvent Dapr delivers: the topic and the
// data payload (which carries only an id). All other envelope fields are
// ignored.
type event struct {
	Topic string    `json:"topic"`
	Data  eventData `json:"data"`
}

// eventData is the payload shared by both topics: { "id": "<UUID>" }.
type eventData struct {
	ID uuid.UUID `json:"id"`
}

// subscriptions is the exact array (order preserved) returned by
// GET /dapr/subscribe.
func subscriptions() []subscription {
	return []subscription{
		{PubsubName: pubsubName, Topic: TopicUserCreated, Route: route},
		{PubsubName: pubsubName, Topic: TopicProductVariantCreated, Route: route},
	}
}

// Register wires the Dapr subscribe + topic-event routes onto mux. Pass it as
// server.Config.ExtraRoutes.
func Register(mux *http.ServeMux, shadows Shadows) {
	mux.HandleFunc("GET /dapr/subscribe", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(subscriptions())
	})

	mux.HandleFunc("POST "+route, func(w http.ResponseWriter, r *http.Request) {
		var ev event
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			// A malformed body can never succeed; 500 tells Dapr to retry,
			// matching the original (it has no dedicated drop path).
			slog.Error("wishlist event: invalid CloudEvent", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		var err error
		switch ev.Topic {
		case TopicProductVariantCreated:
			err = shadows.InsertProductVariant(r.Context(), ev.Data.ID)
		case TopicUserCreated:
			err = shadows.InsertUser(r.Context(), ev.Data.ID)
		default:
			// Unknown topic -> 500 (Dapr retry). Shouldn't happen since only
			// the two subscribed topics route here.
			slog.Error("wishlist event: unknown topic", "topic", ev.Topic)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if err != nil {
			slog.Error("wishlist event: insert failed", "topic", ev.Topic, "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Dapr SUCCESS: HTTP 200 with body {"status":0}.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":0}`))
	})
}
