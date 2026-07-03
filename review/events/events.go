// Package events implements the review service's Dapr subscription surface: the
// GET /dapr/subscribe registration and the two POST event-handler routes. The
// review service PUBLISHES nothing; it only consumes user/catalog create events
// to maintain its local shadow copies.
//
// The subscription list and response shapes are reproduced exactly from the
// spec: pubsubName is camelCase on the wire, the success ack is the JSON body
// {"status":0} (Dapr SUCCESS), and any failure (unknown topic on a route or a
// Mongo error) returns a bare HTTP 500 so Dapr retries.
package events

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"misarch/review/store"
)

// Topic constants, matching the original service's subscriptions exactly.
const (
	TopicUserCreated           = "user/user/created"
	TopicProductCreated        = "catalog/product/created"
	TopicProductVariantCreated = "catalog/product-variant/created"
)

// Route constants for the two event-handler endpoints.
const (
	RouteOnTopicEvent             = "/on-topic-event"
	RouteOnProductVariantCreation = "/on-product-variant-creation-event"
)

// PubsubName is the Dapr pub/sub component all subscriptions use.
const PubsubName = "pubsub"

// subscription is one entry of the /dapr/subscribe response. The JSON key for
// the component is pubsubName (camelCase), matching the Rust serde rename.
type subscription struct {
	PubsubName string `json:"pubsubName"`
	Topic      string `json:"topic"`
	Route      string `json:"route"`
}

// subscriptions is the exact 3-element registration array. Order is preserved.
// Per the spec §4 the product-variant subscription is routed to its dedicated
// handler /on-product-variant-creation-event.
var subscriptions = []subscription{
	{PubsubName: PubsubName, Topic: TopicUserCreated, Route: RouteOnTopicEvent},
	{PubsubName: PubsubName, Topic: TopicProductCreated, Route: RouteOnTopicEvent},
	{PubsubName: PubsubName, Topic: TopicProductVariantCreated, Route: RouteOnProductVariantCreation},
}

// cloudEvent is the subset of the Dapr CloudEvent envelope the handlers read:
// the topic and the (raw) data payload, both at the top level of the delivered
// JSON.
type cloudEvent struct {
	Topic string          `json:"topic"`
	Data  json.RawMessage `json:"data"`
}

// eventData is the data payload shape for /on-topic-event: only an id.
type eventData struct {
	ID uuid.UUID `json:"id"`
}

// productVariantEventData is the data payload for the product-variant event.
// productId is camelCase on the wire (Rust serde rename_all = camelCase).
type productVariantEventData struct {
	ID        uuid.UUID `json:"id"`
	ProductID uuid.UUID `json:"productId"`
}

// Handler owns the store and serves the Dapr endpoints.
type Handler struct {
	store *store.Store
}

// NewHandler builds an event Handler over the given store.
func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s}
}

// Routes registers GET /dapr/subscribe and the two POST event routes on mux.
// Wire this via server.Config.ExtraRoutes.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /dapr/subscribe", h.listSubscriptions)
	mux.HandleFunc("POST "+RouteOnTopicEvent, h.onTopicEvent)
	mux.HandleFunc("POST "+RouteOnProductVariantCreation, h.onProductVariantCreationEvent)
}

// listSubscriptions returns the fixed subscription array as JSON.
func (h *Handler) listSubscriptions(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(subscriptions)
}

// onTopicEvent handles user/user/created and catalog/product/created. Unknown
// topics and Mongo errors yield HTTP 500; success yields {"status":0}.
func (h *Handler) onTopicEvent(w http.ResponseWriter, r *http.Request) {
	ce, ok := decodeEnvelope(w, r)
	if !ok {
		return
	}
	var data eventData
	if err := json.Unmarshal(ce.Data, &data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var err error
	switch ce.Topic {
	case TopicUserCreated:
		err = h.store.UpsertUser(r.Context(), data.ID)
	case TopicProductCreated:
		err = h.store.UpsertProduct(r.Context(), data.ID)
	default:
		http.Error(w, "unknown topic", http.StatusInternalServerError)
		return
	}
	if err != nil {
		slog.Error("review event: insert failed", "route", RouteOnTopicEvent, "topic", ce.Topic, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeSuccess(w)
}

// onProductVariantCreationEvent handles catalog/product-variant/created.
func (h *Handler) onProductVariantCreationEvent(w http.ResponseWriter, r *http.Request) {
	ce, ok := decodeEnvelope(w, r)
	if !ok {
		return
	}
	var data productVariantEventData
	if err := json.Unmarshal(ce.Data, &data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	switch ce.Topic {
	case TopicProductVariantCreated:
		if err := h.store.UpsertProductVariant(r.Context(), data.ID, data.ProductID); err != nil {
			slog.Error("review event: insert failed", "route", RouteOnProductVariantCreation, "topic", ce.Topic, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "unknown topic", http.StatusInternalServerError)
		return
	}
	writeSuccess(w)
}

// decodeEnvelope reads the CloudEvent envelope. On a parse failure it writes a
// 500 (Dapr will retry; a genuinely malformed body will keep failing, matching
// the original which had no special malformed-event handling).
func decodeEnvelope(w http.ResponseWriter, r *http.Request) (cloudEvent, bool) {
	var ce cloudEvent
	if err := json.NewDecoder(r.Body).Decode(&ce); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return cloudEvent{}, false
	}
	return ce, true
}

// writeSuccess writes the Dapr success ack: HTTP 200 with body {"status":0}.
func writeSuccess(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":0}`))
}
