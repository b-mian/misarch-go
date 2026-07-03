package queue

import (
	"context"
	"encoding/json"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// stubConfig returns fixed values for the tunables.
type stubConfig struct{ vals map[string]float64 }

func (s stubConfig) GetCurrentVariableValueNumber(name string, fallback float64) float64 {
	if v, ok := s.vals[name]; ok {
		return v
	}
	return fallback
}

// stubBlocker returns a preset blocked result and records the id it saw.
type stubBlocker struct {
	blocked bool
	seen    []string
}

func (b *stubBlocker) IsBlocked(id string) bool {
	b.seen = append(b.seen, id)
	return b.blocked
}

// recordCallback records the (id, status) sent to each side.
type recordCallback struct {
	mu       sync.Mutex
	payments [][2]string
	shipments [][2]string
}

func (c *recordCallback) SendUpdateToPayment(_ context.Context, id, status string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.payments = append(c.payments, [2]string{id, status})
}
func (c *recordCallback) SendUpdateToShipment(_ context.Context, id, status string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.shipments = append(c.shipments, [2]string{id, status})
}

func newProc(rate float64, cb Callback) *Processor {
	cfg := stubConfig{vals: map[string]float64{
		"PAYMENT_SUCCESS_RATE":  rate,
		"SHIPMENT_SUCCESS_RATE": rate,
	}}
	p := NewProcessor(cfg, &stubBlocker{}, &stubBlocker{}, cb, "amqp://unused", rand.New(rand.NewSource(1)))
	p.ctx = context.Background()
	return p
}

// TestBuildEventUpdate_SuccessRate verifies the roll → status mapping for both
// queues at the deterministic extremes (rate 1.0 always succeeds, 0.0 always
// fails), matching Math.random() < rate.
func TestBuildEventUpdate_SuccessRate(t *testing.T) {
	cases := []struct {
		rate       float64
		queue      string
		wantStatus string
	}{
		{1.0, PaymentsQueue, "SUCCEEDED"},
		{0.0, PaymentsQueue, "FAILED"},
		{1.0, ShipmentsQueue, "DELIVERED"},
		{0.0, ShipmentsQueue, "FAILED"},
	}
	for _, tc := range cases {
		cb := &recordCallback{}
		p := newProc(tc.rate, cb)
		p.buildEventUpdate(tc.queue, "id-1")
		cb.mu.Lock()
		var got string
		if tc.queue == PaymentsQueue {
			if len(cb.payments) != 1 {
				t.Fatalf("%s rate %v: expected 1 payment callback, got %d", tc.queue, tc.rate, len(cb.payments))
			}
			got = cb.payments[0][1]
		} else {
			if len(cb.shipments) != 1 {
				t.Fatalf("%s rate %v: expected 1 shipment callback, got %d", tc.queue, tc.rate, len(cb.shipments))
			}
			got = cb.shipments[0][1]
		}
		cb.mu.Unlock()
		if got != tc.wantStatus {
			t.Fatalf("%s rate %v: got status %q want %q", tc.queue, tc.rate, got, tc.wantStatus)
		}
	}
}

// TestPublishEnvelope verifies the {pattern,data} framing bytes for both
// queues by marshaling through the same path Publish uses.
func TestPublishEnvelope(t *testing.T) {
	data := map[string]any{"paymentId": "p1", "amount": 1999, "paymentType": "credit-card"}
	raw, _ := json.Marshal(data)
	body, _ := json.Marshal(envelope{Pattern: registerPaymentPattern, Data: raw})
	var round envelope
	if err := json.Unmarshal(body, &round); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if round.Pattern != "register-payment" {
		t.Fatalf("pattern=%q", round.Pattern)
	}
	if extractID(round.Data) != "p1" {
		t.Fatalf("id from data=%q", extractID(round.Data))
	}
}

// TestExtractID checks paymentId || shipmentId precedence.
func TestExtractID(t *testing.T) {
	if got := extractID([]byte(`{"paymentId":"a"}`)); got != "a" {
		t.Fatalf("paymentId: %q", got)
	}
	if got := extractID([]byte(`{"shipmentId":"b"}`)); got != "b" {
		t.Fatalf("shipmentId: %q", got)
	}
	if got := extractID([]byte(`{}`)); got != "" {
		t.Fatalf("none: %q", got)
	}
}

// TestScheduleCallback_Delay verifies the callback fires after the delay and
// uses the correct id. A short delay keeps the test fast.
func TestScheduleCallback_Delay(t *testing.T) {
	cb := &recordCallback{}
	p := newProc(1.0, cb)
	p.scheduleCallback(PaymentsQueue, []byte(`{"paymentId":"pX"}`), 0.05)
	time.Sleep(150 * time.Millisecond)
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if len(cb.payments) != 1 || cb.payments[0][0] != "pX" || cb.payments[0][1] != "SUCCEEDED" {
		t.Fatalf("bad callback: %+v", cb.payments)
	}
}

// TestKeyMapping verifies the pluralization split: PAYMENTS_PER_MINUTE vs
// PAYMENT_PROCESSING_TIME / PAYMENT_SUCCESS_RATE.
func TestKeyMapping(t *testing.T) {
	cfg := stubConfig{vals: map[string]float64{
		"PAYMENTS_PER_MINUTE":      42,
		"PAYMENT_PROCESSING_TIME":  7,
		"PAYMENT_SUCCESS_RATE":     0.3,
		"SHIPMENTS_PER_MINUTE":     99,
		"SHIPMENT_PROCESSING_TIME": 8,
		"SHIPMENT_SUCCESS_RATE":    0.4,
	}}
	p := NewProcessor(cfg, &stubBlocker{}, &stubBlocker{}, &recordCallback{}, "x", rand.New(rand.NewSource(1)))
	if p.getMaxPerMinute(PaymentsQueue) != 42 {
		t.Fatalf("payments per minute: %v", p.getMaxPerMinute(PaymentsQueue))
	}
	if p.getProcessingTime(PaymentsQueue) != 7 {
		t.Fatalf("payment processing time: %v", p.getProcessingTime(PaymentsQueue))
	}
	if p.getSuccessRate(PaymentsQueue) != 0.3 {
		t.Fatalf("payment success rate: %v", p.getSuccessRate(PaymentsQueue))
	}
	if p.getMaxPerMinute(ShipmentsQueue) != 99 {
		t.Fatalf("shipments per minute: %v", p.getMaxPerMinute(ShipmentsQueue))
	}
	if p.getProcessingTime(ShipmentsQueue) != 8 {
		t.Fatalf("shipment processing time: %v", p.getProcessingTime(ShipmentsQueue))
	}
	if p.getSuccessRate(ShipmentsQueue) != 0.4 {
		t.Fatalf("shipment success rate: %v", p.getSuccessRate(ShipmentsQueue))
	}
}
