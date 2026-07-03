// Package service holds the payment saga orchestration that lives outside the
// GraphQL layer: reacting to Dapr events, driving the payment-method state
// machines, calling the external simulation provider, handling the simulation
// REST callback, and running the overdue-payment cron jobs. It is wired into
// main.go's Dapr subscriptions, extra REST route, and background tickers.
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// Simulation is the connector to the external payment simulation provider,
// mirroring the original ConnectorService. It POSTs JSON to
// {SIMULATION_URL}/{endpoint}. When SIMULATION_URL is unset it logs and skips
// (returns nil) exactly as the original did; send errors are swallowed (logged)
// so a bad HTTP response never fails the payment flow.
type Simulation struct {
	baseURL string
	hc      *http.Client
}

// NewSimulation reads SIMULATION_URL from the environment.
func NewSimulation() *Simulation {
	return &Simulation{
		baseURL: os.Getenv("SIMULATION_URL"),
		hc:      &http.Client{Timeout: 30 * time.Second},
	}
}

// send POSTs data as JSON to {baseURL}/{endpoint}. Mirrors ConnectorService.send:
//   - if baseURL is empty → log "Simulation URL not set" and skip.
//   - non-2xx responses are logged but not treated as fatal.
//   - transport errors are logged and swallowed.
func (s *Simulation) send(ctx context.Context, endpoint string, data any) {
	if s.baseURL == "" {
		slog.Error("Simulation URL not set")
		return
	}
	body, err := json.Marshal(data)
	if err != nil {
		slog.Error("simulation: marshal request", "endpoint", endpoint, "error", err)
		return
	}
	url := fmt.Sprintf("%s/%s", s.baseURL, endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		slog.Error("simulation: build request", "endpoint", endpoint, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.hc.Do(req)
	if err != nil {
		slog.Error("simulation: request failed", "endpoint", endpoint, "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		slog.Error("simulation: request returned non-2xx", "endpoint", endpoint, "status", resp.StatusCode)
	}
}

// registerPayment is the body for the create path: POST payment/register with
// { paymentId, amount, paymentType } (paymentType is credit-card/prepayment/invoice).
type registerPayment struct {
	PaymentID   string `json:"paymentId"`
	Amount      int64  `json:"amount"`
	PaymentType string `json:"paymentType"`
}

// retryPayment is the body for the credit-card retry path: POST register with
// { paymentId, type }. [PRESERVE — quirk] the endpoint (`register`, not
// `payment/register`) and the key (`type`, not `paymentType`) differ from the
// create path.
type retryPayment struct {
	PaymentID string `json:"paymentId"`
	Type      string `json:"type"`
}

// Register posts to the create endpoint payment/register.
func (s *Simulation) Register(ctx context.Context, paymentID string, amount int64, paymentType string) {
	s.send(ctx, "payment/register", registerPayment{PaymentID: paymentID, Amount: amount, PaymentType: paymentType})
}

// Retry posts to the retry endpoint register with the `type` key.
func (s *Simulation) Retry(ctx context.Context, paymentID, paymentType string) {
	s.send(ctx, "register", retryPayment{PaymentID: paymentID, Type: paymentType})
}
