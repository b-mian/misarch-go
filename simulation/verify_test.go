package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"misarch/simulation/config"
	"misarch/simulation/store"
)

// mockQueue captures published messages instead of touching RabbitMQ.
type mockQueue struct {
	mu   sync.Mutex
	msgs []captured
}

type captured struct {
	queue string
	data  any
}

func (m *mockQueue) Publish(_ context.Context, queue string, data any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = append(m.msgs, captured{queue, data})
	return nil
}

// nopCallback satisfies callbackSender without doing anything.
type nopCallback struct{}

func (nopCallback) SendUpdateToPayment(context.Context, string, string)  {}
func (nopCallback) SendUpdateToShipment(context.Context, string, string) {}

// newTestServer builds handlers with a mock queue and real config/store/conn.
func newTestServer(t *testing.T) (*httptest.Server, *mockQueue, *store.PaymentRepository, *store.ShipmentRepository) {
	t.Helper()
	cfg := config.New()
	payments := store.NewPaymentRepository()
	shipments := store.NewShipmentRepository()
	mq := &mockQueue{}
	h := &handlers{
		cfg:       cfg,
		payments:  payments,
		shipments: shipments,
		connector: nopCallback{},
		queue:     mq,
		ctx:       context.Background(),
	}
	mux := http.NewServeMux()
	h.register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, mq, payments, shipments
}

func post(t *testing.T, url, body string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

func getReq(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

const uuidA = "3fa85f64-5717-4562-b3fc-2c963f66afa6"

func TestPaymentRegister_OK(t *testing.T) {
	srv, mq, payments, _ := newTestServer(t)
	resp, body := post(t, srv.URL+"/payment/register",
		`{"paymentId":"`+uuidA+`","amount":1999,"paymentType":"credit-card"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if body != "" {
		t.Fatalf("expected empty body, got %q", body)
	}
	// stored record drops amount
	all := payments.FindAll()
	if len(all) != 1 || all[0].ID != uuidA || all[0].PaymentType != "credit-card" || all[0].Blocked {
		t.Fatalf("bad stored record: %+v", all)
	}
	// queue got the full DTO incl amount
	if len(mq.msgs) != 1 || mq.msgs[0].queue != "payments-queue" {
		t.Fatalf("bad queue msgs: %+v", mq.msgs)
	}
	m := mq.msgs[0].data.(map[string]any)
	if _, ok := m["amount"]; !ok {
		t.Fatalf("amount not emitted to queue: %+v", m)
	}
}

func TestPaymentRegister_ValidationError(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	// missing amount + bad uuid
	resp, body := post(t, srv.URL+"/payment/register",
		`{"paymentId":"not-a-uuid","paymentType":"credit-card"}`)
	if resp.StatusCode != 400 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("bad envelope json: %v (%s)", err, body)
	}
	if env["statusCode"].(float64) != 400 || env["error"].(string) != "Bad Request" {
		t.Fatalf("bad envelope: %s", body)
	}
	msgs := env["message"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 constraint msgs, got %v", msgs)
	}
}

func TestPaymentUpdate_NotFound(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	resp, body := post(t, srv.URL+"/payment/update",
		`{"paymentId":"`+uuidA+`","status":"SUCCEEDED"}`)
	if resp.StatusCode != 404 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	// [QUIRK] message says "Shipment not found" for a payment
	want := `{"statusCode":404,"message":"Shipment not found","error":"Not Found"}`
	if strings.TrimSpace(body) != want {
		t.Fatalf("got %q want %q", strings.TrimSpace(body), want)
	}
}

func TestPaymentUpdate_Blocks(t *testing.T) {
	srv, _, payments, _ := newTestServer(t)
	// register first
	post(t, srv.URL+"/payment/register",
		`{"paymentId":"`+uuidA+`","amount":10,"paymentType":"invoice"}`)
	// manual update with a garbage status (no enum check → accepted)
	resp, body := post(t, srv.URL+"/payment/update",
		`{"paymentId":"`+uuidA+`","status":"WHATEVER"}`)
	if resp.StatusCode != 200 || body != "" {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
	all := payments.FindAll()
	if len(all) != 1 || !all[0].Blocked {
		t.Fatalf("record not blocked: %+v", all)
	}
}

func TestShipmentRegister_IgnoresExtraFields(t *testing.T) {
	srv, mq, _, shipments := newTestServer(t)
	body := `{"shipmentId":"` + uuidA + `","ref":"DHL","quantity":2,"weight":1.5,` +
		`"address":{"street1":"s","city":"c","postalCode":"p","country":"co","companyName":null}}`
	resp, rbody := post(t, srv.URL+"/shipment/register", body)
	if resp.StatusCode != 200 || rbody != "" {
		t.Fatalf("status=%d body=%q", resp.StatusCode, rbody)
	}
	all := shipments.FindAll()
	if len(all) != 1 || all[0].ID != uuidA || all[0].Blocked {
		t.Fatalf("bad stored shipment: %+v", all)
	}
	// full body emitted to queue
	if len(mq.msgs) != 1 || mq.msgs[0].queue != "shipments-queue" {
		t.Fatalf("bad queue: %+v", mq.msgs)
	}
}

func TestShipmentRegister_BadUUID(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	resp, body := post(t, srv.URL+"/shipment/register", `{"shipmentId":123}`)
	if resp.StatusCode != 400 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "shipmentId must be a UUID") {
		t.Fatalf("bad msg: %s", body)
	}
}

func TestDefinedVariables_ExactBytes(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	resp, body := getReq(t, srv.URL+"/ecs/defined-variables")
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	want := `{"PAYMENTS_PER_MINUTE":{"type":{"$schema":"http://json-schema.org/draft-07/schema#","type":"integer"},"defaultValue":1000000},` +
		`"SHIPMENTS_PER_MINUTE":{"type":{"$schema":"http://json-schema.org/draft-07/schema#","type":"integer"},"defaultValue":1000000},` +
		`"PAYMENT_PROCESSING_TIME":{"type":{"$schema":"http://json-schema.org/draft-07/schema#","type":"integer"},"defaultValue":5},` +
		`"SHIPMENT_PROCESSING_TIME":{"type":{"$schema":"http://json-schema.org/draft-07/schema#","type":"integer"},"defaultValue":5},` +
		`"PAYMENT_SUCCESS_RATE":{"type":{"$schema":"http://json-schema.org/draft-07/schema#","type":"number"},"defaultValue":0.95},` +
		`"SHIPMENT_SUCCESS_RATE":{"type":{"$schema":"http://json-schema.org/draft-07/schema#","type":"number"},"defaultValue":0.95}}`
	if body != want {
		t.Fatalf("defined-variables mismatch:\n got: %s\nwant: %s", body, want)
	}
}

func TestSetVariables_OK(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	resp, body := post(t, srv.URL+"/ecs/variables",
		`{"PAYMENT_SUCCESS_RATE":"0.5","PAYMENT_PROCESSING_TIME":"10"}`)
	if resp.StatusCode != 200 || body != "" {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
}

func TestSetVariables_UnknownKey500(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	resp, body := post(t, srv.URL+"/ecs/variables", `{"NOPE":"1"}`)
	if resp.StatusCode != 500 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	want := `{"message":"Internal server error","statusCode":500}`
	// order-insensitive check
	var got map[string]any
	json.Unmarshal([]byte(body), &got)
	if got["statusCode"].(float64) != 500 || got["message"].(string) != "Internal server error" {
		t.Fatalf("got %s want ~ %s", body, want)
	}
}

func TestFindAll_Empty(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	resp, body := post(t, srv.URL+"/payment/findAll", ``)
	if resp.StatusCode != 200 || strings.TrimSpace(body) != "[]" {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
	resp2, body2 := post(t, srv.URL+"/shipment/findAll", ``)
	if resp2.StatusCode != 200 || strings.TrimSpace(body2) != "[]" {
		t.Fatalf("status=%d body=%q", resp2.StatusCode, body2)
	}
}

// TestConfigCasting checks the JS-equivalent casting semantics.
func TestConfigCasting(t *testing.T) {
	cfg := config.New()
	// integer parseInt semantics
	_ = cfg.SetVariables(map[string]any{"PAYMENTS_PER_MINUTE": "10.9"})
	if got := cfg.GetCurrentVariableValueNumber("PAYMENTS_PER_MINUTE", -1); got != 10 {
		t.Fatalf("parseInt(10.9)=%v want 10", got)
	}
	// number semantics
	_ = cfg.SetVariables(map[string]any{"PAYMENT_SUCCESS_RATE": "0.5"})
	if got := cfg.GetCurrentVariableValueNumber("PAYMENT_SUCCESS_RATE", -1); got != 0.5 {
		t.Fatalf("Number(0.5)=%v want 0.5", got)
	}
}

// TestStoreReadAndDelete verifies isBlocked atomicity: absent→false,
// present→delete+blocked.
func TestStoreReadAndDelete(t *testing.T) {
	r := store.NewPaymentRepository()
	if r.IsBlocked("x") {
		t.Fatal("absent should be false")
	}
	r.Create(store.Payment{ID: "x", Blocked: true})
	if !r.IsBlocked("x") {
		t.Fatal("present blocked should be true")
	}
	if r.FindByID("x") {
		t.Fatal("record should be deleted after isBlocked")
	}
}
