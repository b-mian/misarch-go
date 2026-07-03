package events

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"misarch/invoice/store"
)

// stubStore implements EventStore with no-op writes so the HTTP surface can be
// exercised without MongoDB.
type stubStore struct{ fakeStore }

func (stubStore) UpsertVendorAddress(context.Context, uuid.UUID) error                { return nil }
func (stubStore) InsertUser(context.Context, store.User) error                        { return nil }
func (stubStore) PushUserAddress(context.Context, uuid.UUID, store.UserAddress) error { return nil }
func (stubStore) PullUserAddress(context.Context, uuid.UUID, uuid.UUID) error         { return nil }

// newMux mirrors how server.Run mounts things: GraphQL on /{$} and /graphql,
// GET /health, plus our ExtraRoutes. This proves Register doesn't collide with
// the shared routes (a duplicate pattern would panic here).
func newMux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {})
	gql := func(w http.ResponseWriter, _ *http.Request) {}
	mux.HandleFunc("POST /graphql", gql)
	mux.HandleFunc("GET /graphql", gql)
	mux.HandleFunc("POST /{$}", gql)
	mux.HandleFunc("GET /{$}", gql)

	svc := NewService(stubStore{}, NewPublisher())
	svc.Register(mux) // must not panic
	return mux
}

func TestEventRouteSuccessBody(t *testing.T) {
	mux := newMux(t)

	// A vendor-address-created event with the correct topic → 200 {"status":0}.
	body := []byte(`{"topic":"address/vendor-address/created","data":{"id":"11111111-1111-1111-1111-111111111111","street1":"a","street2":"b","city":"c","postal_code":"d","country":"e","company_name":"f"}}`)
	req := httptest.NewRequest(http.MethodPost, "/on-vendor-address-creation-event", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != `{"status":0}` {
		t.Errorf("body = %q, want %q", got, `{"status":0}`)
	}
}

func TestEventRouteTopicMismatch(t *testing.T) {
	mux := newMux(t)

	// Right route, wrong topic → 500 (the handler asserts the topic).
	body := []byte(`{"topic":"wrong/topic","data":{"id":"11111111-1111-1111-1111-111111111111","street1":"a","street2":"b","city":"c","postal_code":"d","country":"e","company_name":"f"}}`)
	req := httptest.NewRequest(http.MethodPost, "/on-vendor-address-creation-event", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestEventRouteMalformedJSON(t *testing.T) {
	mux := newMux(t)

	req := httptest.NewRequest(http.MethodPost, "/on-vendor-address-creation-event", bytes.NewReader([]byte(`{not json`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("malformed JSON status = %d, want 400", rec.Code)
	}
}

func TestEventRouteWrongShape(t *testing.T) {
	mux := newMux(t)

	// Well-formed JSON but id is not a valid UUID → 422.
	body := []byte(`{"topic":"address/vendor-address/created","data":{"id":"not-a-uuid","street1":"a","street2":"b","city":"c","postal_code":"d","country":"e","company_name":"f"}}`)
	req := httptest.NewRequest(http.MethodPost, "/on-vendor-address-creation-event", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("wrong-shape status = %d, want 422", rec.Code)
	}
}

func TestUserCreatedRegisteredButNotSubscribed(t *testing.T) {
	mux := newMux(t)

	// The handler IS registered at /on-user-creation-event (reachable when
	// hit directly) ...
	body := []byte(`{"topic":"user/user/created","data":{"id":"11111111-1111-1111-1111-111111111111","first_name":"J","last_name":"D"}}`)
	req := httptest.NewRequest(http.MethodPost, "/on-user-creation-event", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != `{"status":0}` {
		t.Errorf("registered user route: status=%d body=%q", rec.Code, rec.Body.String())
	}

	// ... but the subscribe list advertises /on-id-creation-event, which is NOT
	// registered → 404 (Dapr would drop the delivery).
	req2 := httptest.NewRequest(http.MethodPost, "/on-id-creation-event", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Errorf("mismatched subscribe route: status=%d, want 404", rec2.Code)
	}
}
