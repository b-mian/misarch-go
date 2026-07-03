// Package connector performs the simulation's outbound callbacks to the
// originating services (payment and shipment). It mirrors the reference
// ConnectorService: base URLs come from ECS/env (PAYMENT_URL / SHIPMENT_URL,
// default "NOT_SET" with an error log), each send retries RETRY_COUNT+1 total
// attempts with a fixed 5s backoff, success is HTTP status in [200,299], and a
// final failure is swallowed (logged, never surfaced) — callbacks are
// best-effort.
package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"misarch/simulation/config"
)

// retryBackoff is the fixed delay between attempts (reference: sleep(5000)).
const retryBackoff = 5 * time.Second

// Config resolves the tunables the connector reads per call (URLs and retry
// count) through the ECS resolution order.
type Config interface {
	GetCurrentVariableValueString(name, fallback string) string
	GetCurrentVariableValueNumber(name string, fallback float64) float64
}

// Service sends status callbacks. The endpoints are resolved once in New (as
// the reference reads them in its constructor), while RETRY_COUNT is read fresh
// per send.
type Service struct {
	cfg              Config
	hc               *http.Client
	paymentEndpoint  string
	shipmentEndpoint string
}

// New builds the connector, resolving PAYMENT_URL and SHIPMENT_URL up front and
// logging an error for each still set to "NOT_SET" (without crashing), exactly
// as the reference constructor does.
func New(cfg *config.Service, hc *http.Client) *Service {
	paymentEndpoint := cfg.GetCurrentVariableValueString("PAYMENT_URL", "NOT_SET")
	shipmentEndpoint := cfg.GetCurrentVariableValueString("SHIPMENT_URL", "NOT_SET")
	if paymentEndpoint == "NOT_SET" {
		slog.Error("Payment URL not set")
	}
	if shipmentEndpoint == "NOT_SET" {
		slog.Error("Shipment URL not set")
	}
	return &Service{
		cfg:              cfg,
		hc:               hc,
		paymentEndpoint:  paymentEndpoint,
		shipmentEndpoint: shipmentEndpoint,
	}
}

// SendUpdateToShipment POSTs { status } to
// {SHIPMENT_URL}/shipment/{shipmentId}/status. The shipmentId is in the path,
// not the body.
func (s *Service) SendUpdateToShipment(ctx context.Context, shipmentID, status string) {
	endpoint := fmt.Sprintf("%s/shipment/%s/status", s.shipmentEndpoint, shipmentID)
	s.send(ctx, endpoint, map[string]string{"status": status})
}

// SendUpdateToPayment POSTs { paymentId, status } to
// {PAYMENT_URL}/payment/update-payment-status (both fields in the body).
func (s *Service) SendUpdateToPayment(ctx context.Context, paymentID, status string) {
	endpoint := fmt.Sprintf("%s/payment/update-payment-status", s.paymentEndpoint)
	s.send(ctx, endpoint, updatePaymentStatus{PaymentID: paymentID, Status: status})
}

// updatePaymentStatus is the payment callback body; field order/casing matches
// the receiver's UpdatePaymentStatusDto.
type updatePaymentStatus struct {
	PaymentID string `json:"paymentId"`
	Status    string `json:"status"`
}

// send POSTs data as JSON to endpoint with the reference's retry loop:
//
//	retryCount = RETRY_COUNT (env/default 3)
//	do { try post; ok if 2xx → return
//	     catch: log; attempts++; if attempts<=retryCount { log retry; sleep 5s } }
//	while attempts <= retryCount        // total attempts = retryCount + 1
//
// On exhaustion it returns having logged the failures (the reference returns
// undefined). A non-2xx status or a transport error both count as a failure.
func (s *Service) send(ctx context.Context, endpoint string, data any) {
	// RETRY_COUNT is not an ECS-defined variable; it comes from env or the
	// numeric default 3. Read via the string resolver so a set env var keeps
	// its raw string (as the reference's getCurrentVariableValue returns), then
	// coerce for the numeric comparison the way JS would (Number()). This
	// preserves the quirk that a non-numeric RETRY_COUNT yields NaN, making the
	// `attempts <= retryCount` comparison false → exactly one attempt.
	retryStr := s.cfg.GetCurrentVariableValueString("RETRY_COUNT", "3")
	retryCount := jsNumber(retryStr)

	body, err := json.Marshal(data)
	if err != nil {
		slog.Error(fmt.Sprintf("Error sending request to %s: %v", endpoint, err))
		return
	}

	attempts := 0
	for {
		ok := s.post(ctx, endpoint, body)
		if ok {
			return
		}
		attempts++
		// <= since the first attempt is not a retry. NaN comparisons are false
		// (matching JS), so a NaN retryCount stops after this single attempt.
		if float64(attempts) <= retryCount {
			slog.Info(fmt.Sprintf("Retrying request to %s [%d/%s]", endpoint, attempts, retryStr))
			select {
			case <-time.After(retryBackoff):
			case <-ctx.Done():
				return
			}
		}
		if !(float64(attempts) <= retryCount) {
			return
		}
	}
}

// jsNumber coerces a string to a float64 the way JavaScript's Number() does:
// "" → 0, a trimmed numeric string → its value, otherwise NaN. Used only for
// the RETRY_COUNT comparison.
func jsNumber(s string) float64 {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return math.NaN()
	}
	return f
}

// post performs a single POST and returns whether it succeeded (2xx). Any
// transport error or non-2xx status is logged and reported as failure,
// mirroring the reference's catch(error) → logger.error path.
func (s *Service) post(ctx context.Context, endpoint string, body []byte) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		slog.Error(fmt.Sprintf("Error sending request to %s: %v", endpoint, err))
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.hc.Do(req)
	if err != nil {
		slog.Error(fmt.Sprintf("Error sending request to %s: %v", endpoint, err))
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		slog.Error(fmt.Sprintf("Error sending request to %s: Request to %s failed with status %d", endpoint, endpoint, resp.StatusCode))
		return false
	}
	return true
}
