package events

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"misarch/order/store"
)

// EventStore is the persistence surface the Dapr event handlers (and the
// compensation saga) need.
type EventStore interface {
	// UUID creation events.
	InsertCoupon(ctx context.Context, id uuid.UUID) error
	InsertShipmentMethod(ctx context.Context, id uuid.UUID) error
	InsertUser(ctx context.Context, id uuid.UUID) error
	// Product variant version / update.
	GetProductVariant(ctx context.Context, id uuid.UUID) (store.ProductVariant, error)
	UpdateProductVariantVersion(ctx context.Context, pvID uuid.UUID, version store.ProductVariantVersion) error
	InsertProductVariant(ctx context.Context, pvID uuid.UUID, version store.ProductVariantVersion) error
	SetProductVariantVisibility(ctx context.Context, id uuid.UUID, visible any) error
	// Tax rate version.
	UpsertTaxRate(ctx context.Context, taxRateID uuid.UUID, version store.TaxRateVersion) error
	// User addresses.
	PushUserAddress(ctx context.Context, userID, addressID uuid.UUID) error
	PullUserAddress(ctx context.Context, userID, addressID uuid.UUID) error
	// Compensation saga.
	ValidateObjectOrder(ctx context.Context, id uuid.UUID) error
	CountUncompensatedProbe(ctx context.Context, orderItemIDs []uuid.UUID) (int64, error)
	InsertOrderCompensation(ctx context.Context, c store.OrderCompensation) error
	GetOrder(ctx context.Context, id uuid.UUID) (store.Order, error)
}

// Service wires the Dapr HTTP event surface: the subscription list and the
// delivery routes. It reproduces the original axum service's byte-level
// behavior (success body {"status":0}, topic assertion → 500) rather than using
// the shared dapr.Subscriber (which emits {"status":"SUCCESS"} and does not
// validate topics or advertise pubsubName).
type Service struct {
	store EventStore
	pub   *Publisher
}

// NewService builds the event Service over a store and publisher.
func NewService(s EventStore, pub *Publisher) *Service {
	return &Service{store: s, pub: pub}
}

// subscription serializes exactly like the original Rust Pubsub struct:
// {"pubsubName": ..., "topic": ..., "route": ...}.
type subscription struct {
	PubsubName string `json:"pubsubName"`
	Topic      string `json:"topic"`
	Route      string `json:"route"`
}

// subscriptions is the exact /dapr/subscribe response, verbatim from the
// original — the array is built with product-variant-updated FIRST (even though
// the source vars are declared in a different order), pubsubName casing, and the
// shared /on-id-creation-event route for coupon/shipment-method/user.
//
// NOTE: shipment/shipment/creation-failed is deliberately ABSENT here (the
// original does not advertise it programmatically); its route is still served
// and is delivered via a declarative Dapr subscription in the deployment.
var subscriptions = []subscription{
	{PubsubName: "pubsub", Topic: TopicProductVariantUpdated, Route: RouteProductVariantUpdated},
	{PubsubName: "pubsub", Topic: TopicProductVariantVersionCreated, Route: RouteProductVariantVersion},
	{PubsubName: "pubsub", Topic: TopicCouponCreated, Route: RouteIDCreation},
	{PubsubName: "pubsub", Topic: TopicTaxRateVersionCreated, Route: RouteTaxRateVersion},
	{PubsubName: "pubsub", Topic: TopicShipmentMethodCreated, Route: RouteIDCreation},
	{PubsubName: "pubsub", Topic: TopicUserCreated, Route: RouteIDCreation},
	{PubsubName: "pubsub", Topic: TopicUserAddressCreated, Route: RouteUserAddressCreated},
	{PubsubName: "pubsub", Topic: TopicUserAddressArchived, Route: RouteUserAddressArchived},
}

// Register mounts the Dapr subscribe endpoint and the delivery routes on mux.
// Passed as server.Config.ExtraRoutes.
func (svc *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /dapr/subscribe", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(subscriptions)
	})

	// /on-id-creation-event switches on the topic between coupon / shipment
	// method / user (each 500s on any other topic).
	mux.HandleFunc("POST "+RouteIDCreation, svc.onIDCreation)

	handle(mux, RouteProductVariantVersion, TopicProductVariantVersionCreated, svc.onProductVariantVersionCreated)
	handle(mux, RouteProductVariantUpdated, TopicProductVariantUpdated, svc.onProductVariantUpdated)
	handle(mux, RouteTaxRateVersion, TopicTaxRateVersionCreated, svc.onTaxRateVersionCreated)
	handle(mux, RouteUserAddressCreated, TopicUserAddressCreated, svc.onUserAddressCreated)
	handle(mux, RouteUserAddressArchived, TopicUserAddressArchived, svc.onUserAddressArchived)
	handle(mux, RouteShipmentCreationFailed, TopicShipmentCreationFailed, svc.compensateOrder)
}

// okBody is the exact success payload the original returns for every event
// route: the integer-status form {"status":0} (Dapr's SUCCESS), not the
// {"status":"SUCCESS"} string form.
var okBody = []byte(`{"status":0}`)

// onIDCreation handles /on-id-creation-event, which serves three topics
// (coupon/shipment-method/user creation) and switches on the envelope topic —
// an unrecognized topic 500s. Mirrors on_id_creation_event.
func (svc *Service) onIDCreation(w http.ResponseWriter, r *http.Request) {
	env, status, ok := decodeEnvelope[UUIDEventData](r)
	if !ok {
		w.WriteHeader(status)
		return
	}
	var err error
	switch env.Topic {
	case TopicCouponCreated:
		err = svc.store.InsertCoupon(r.Context(), env.Data.ID)
	case TopicShipmentMethodCreated:
		err = svc.store.InsertShipmentMethod(r.Context(), env.Data.ID)
	case TopicUserCreated:
		err = svc.store.InsertUser(r.Context(), env.Data.ID)
	default:
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeOK(w)
}

// onProductVariantVersionCreated handles catalog/product-variant-version/created:
// create-or-update the local product variant's current_version (insert new
// variants as publicly visible). Mirrors create_or_update_product_variant_in_mongodb.
func (svc *Service) onProductVariantVersionCreated(ctx context.Context, data ProductVariantVersionEventData) error {
	version := store.ProductVariantVersion{
		ID:        data.ID,
		Price:     data.RetailPrice,
		TaxRateID: data.TaxRateID,
	}
	if _, err := svc.store.GetProductVariant(ctx, data.ProductVariantID); err != nil {
		if err == store.ErrNoDocuments {
			return svc.store.InsertProductVariant(ctx, data.ProductVariantID, version)
		}
		// A non-"not found" error on the find takes the original's create path
		// too (it only inspects Ok vs Err), so mirror by inserting.
		return svc.store.InsertProductVariant(ctx, data.ProductVariantID, version)
	}
	return svc.store.UpdateProductVariantVersion(ctx, data.ProductVariantID, version)
}

// onProductVariantUpdated handles catalog/product-variant/updated: set
// is_publicly_visible to the event's STRING value (fidelity trap — the create
// path uses a bool). Mirrors update_product_variant_visibility_in_mongodb.
func (svc *Service) onProductVariantUpdated(ctx context.Context, data UpdateProductVariantEventData) error {
	return svc.store.SetProductVariantVisibility(ctx, data.ID, data.IsPubliclyVisible)
}

// onTaxRateVersionCreated handles tax/tax-rate-version/created: upsert the local
// tax rate's current_version. Mirrors create_or_update_tax_rate_in_mongodb
// (upsert = true).
func (svc *Service) onTaxRateVersionCreated(ctx context.Context, data TaxRateVersionEventData) error {
	version := store.TaxRateVersion{
		ID:      data.ID,
		Rate:    data.Rate,
		Version: data.Version,
	}
	return svc.store.UpsertTaxRate(ctx, data.TaxRateID, version)
}

// onUserAddressCreated handles address/user-address/created: $push the address
// id onto the user's user_address_ids (no upsert). Mirrors
// insert_user_address_in_mongodb.
func (svc *Service) onUserAddressCreated(ctx context.Context, data UserAddressEventData) error {
	return svc.store.PushUserAddress(ctx, data.UserID, data.ID)
}

// onUserAddressArchived handles address/user-address/archived: $pull the address
// id from the user's user_address_ids. Mirrors remove_user_address_in_mongodb.
func (svc *Service) onUserAddressArchived(ctx context.Context, data UserAddressEventData) error {
	return svc.store.PullUserAddress(ctx, data.UserID, data.ID)
}

// ---------------------------------------------------------------------------
// Envelope decoding / route helper (shared with the invoice service pattern).
// ---------------------------------------------------------------------------

// writeOK writes the {"status":0} success response.
func writeOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(okBody)
}

// handle registers a POST route that decodes the CloudEvent envelope into
// Envelope[T], asserts the topic (→ 500 on mismatch), runs fn (→ 500 on error),
// and writes {"status":0} on success.
func handle[T any](mux *http.ServeMux, route, expectedTopic string, fn func(ctx context.Context, data T) error) {
	mux.HandleFunc("POST "+route, func(w http.ResponseWriter, r *http.Request) {
		env, status, ok := decodeEnvelope[T](r)
		if !ok {
			w.WriteHeader(status)
			return
		}
		if env.Topic != expectedTopic {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if err := fn(r.Context(), env.Data); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeOK(w)
	})
}

// decodeEnvelope reproduces axum's Json<Event<T>> extractor rejections as
// closely as the standard library allows:
//   - wrong Content-Type            → 415
//   - malformed JSON (syntax error) → 400
//   - well-formed JSON, wrong shape → 422 (type mismatch, bad UUID)
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
		return env, http.StatusUnprocessableEntity, false
	}
	return env, http.StatusOK, true
}
