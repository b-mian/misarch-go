package events

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"misarch/invoice/store"
)

// EventStore is the persistence surface the HTTP event handlers need. It is a
// superset of the invoice-builder Store.
type EventStore interface {
	Store // GetUserByAddressID, GetVendorAddress, GetUser, InsertInvoice
	UpsertVendorAddress(ctx context.Context, id uuid.UUID) error
	InsertUser(ctx context.Context, u store.User) error
	PushUserAddress(ctx context.Context, userID uuid.UUID, addr store.UserAddress) error
	PullUserAddress(ctx context.Context, userID, addressID uuid.UUID) error
}

// Service wires the Dapr HTTP event surface: the subscription list and the five
// delivery routes. It reproduces the original axum service's byte-level
// behavior (success body {"status":0}, topic assertion → 500, JSON shape →
// 4xx) rather than using the shared dapr.Subscriber (which emits
// {"status":"SUCCESS"} and does not validate topics/shape).
type Service struct {
	store EventStore
	pub   *Publisher
}

// NewService builds the event Service over a store and publisher.
func NewService(s EventStore, pub *Publisher) *Service {
	return &Service{store: s, pub: pub}
}

// subscriptions is the exact /dapr/subscribe response, verbatim from the
// original — array order, pubsubName casing, the route typo
// (/on-discount-validation-succeded) and the deliberate mismatch
// (user/user/created → /on-id-creation-event) all preserved.
var subscriptions = []dumbSubscription{
	{PubsubName: "pubsub", Topic: TopicDiscountValidationSucceeded, Route: RouteDiscountValidationSucceeded},
	{PubsubName: "pubsub", Topic: TopicVendorAddressCreated, Route: RouteVendorAddressCreated},
	{PubsubName: "pubsub", Topic: TopicUserCreated, Route: RouteUserCreatedSubscribed},
	{PubsubName: "pubsub", Topic: TopicUserAddressCreated, Route: RouteUserAddressCreated},
	{PubsubName: "pubsub", Topic: TopicUserAddressArchived, Route: RouteUserAddressArchived},
}

// dumbSubscription serializes exactly like the original Rust Pubsub struct:
// {"pubsubName": ..., "topic": ..., "route": ...}.
type dumbSubscription struct {
	PubsubName string `json:"pubsubName"`
	Topic      string `json:"topic"`
	Route      string `json:"route"`
}

// Register mounts the Dapr subscribe endpoint and the delivery routes on mux.
// Passed as server.Config.ExtraRoutes. NOTE: the user-created handler is
// registered at RouteUserCreated (/on-user-creation-event) while the subscribe
// list advertises RouteUserCreatedSubscribed (/on-id-creation-event) — so Dapr
// deliveries 404 and are dropped, exactly as the original.
func (svc *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /dapr/subscribe", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(subscriptions)
	})

	handle(mux, RouteDiscountValidationSucceeded, TopicDiscountValidationSucceeded, svc.onDiscountValidationSucceeded)
	handle(mux, RouteVendorAddressCreated, TopicVendorAddressCreated, svc.onVendorAddressCreated)
	handle(mux, RouteUserCreated, TopicUserCreated, svc.onUserCreated)
	handle(mux, RouteUserAddressCreated, TopicUserAddressCreated, svc.onUserAddressCreated)
	handle(mux, RouteUserAddressArchived, TopicUserAddressArchived, svc.onUserAddressArchived)
}

// okBody is the exact success payload the original returns for every event
// route: the integer-status form {"status":0} (not Dapr's documented
// {"status":"SUCCESS"} string form — Dapr logs a warning and treats the 200 as
// success). Reproduced byte-for-byte.
var okBody = []byte(`{"status":0}`)

// handle registers a POST route that decodes the CloudEvent envelope into
// Envelope[T], asserts the topic, runs fn, and writes {"status":0} on success.
// It maps decode failures to axum-like 4xx and topic mismatch / handler error
// to 500.
func handle[T any](mux *http.ServeMux, route, expectedTopic string, fn func(ctx context.Context, data T) error) {
	mux.HandleFunc("POST "+route, func(w http.ResponseWriter, r *http.Request) {
		env, status, ok := decodeEnvelope[T](r)
		if !ok {
			w.WriteHeader(status)
			return
		}
		// Topic assertion: the original 500s if the envelope topic is not the
		// one expected for this route.
		if env.Topic != expectedTopic {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if err := fn(r.Context(), env.Data); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(okBody)
	})
}

// decodeEnvelope reproduces axum's Json<Event<T>> extractor rejections as
// closely as the standard library allows:
//   - wrong Content-Type            → 415
//   - malformed JSON (syntax error) → 400
//   - well-formed JSON, wrong shape → 422 (e.g. type mismatch, bad UUID)
//
// On success it returns the decoded envelope; on failure it returns the status
// to write with an empty body (matching axum's empty error bodies here, since
// the handlers convert everything below into bare status codes anyway).
func decodeEnvelope[T any](r *http.Request) (Envelope[T], int, bool) {
	var env Envelope[T]

	if ct := r.Header.Get("Content-Type"); ct != "" {
		mediaType := strings.TrimSpace(strings.SplitN(ct, ";", 2)[0])
		if !strings.EqualFold(mediaType, "application/json") {
			return env, http.StatusUnsupportedMediaType, false
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return env, http.StatusBadRequest, false
	}
	if !json.Valid(body) {
		return env, http.StatusBadRequest, false
	}
	if err := json.Unmarshal(body, &env); err != nil {
		// Well-formed JSON that does not fit the target shape (type mismatch,
		// invalid UUID, etc.) → 422, matching serde/axum's JsonDataError.
		return env, http.StatusUnprocessableEntity, false
	}
	return env, http.StatusOK, true
}
