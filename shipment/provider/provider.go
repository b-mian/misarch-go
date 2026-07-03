// Package provider talks to the external shipment provider: on every shipment
// creation the service POSTs a shipment definition to
// MISARCH_SHIPMENT_PROVIDER_ENDPOINT, with an ECS-tuned retry loop. Mirrors
// org.misarch.shipment.provider.* and ShipmentService.sendShipmentToExternalProvider*.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// RetryConfig supplies the live, ECS-tunable retry parameters. *ecs.Config
// satisfies it. providerRetries is the number of GUARDED attempts (not
// counting the final unguarded attempt); providerRetryDelay is the ms delay
// between guarded attempts.
type RetryConfig interface {
	ProviderRetries() int
	ProviderRetryDelay() int
}

// AddressDefinition is the address block of the provider payload (camelCase).
type AddressDefinition struct {
	Street1     string  `json:"street1"`
	Street2     string  `json:"street2"`
	City        string  `json:"city"`
	PostalCode  string  `json:"postalCode"`
	Country     string  `json:"country"`
	CompanyName *string `json:"companyName"`
}

// ShipmentDefinition is the provider payload (ShipmentProviderShipmentDefinition,
// camelCase JSON). weight is a JSON number (kg).
type ShipmentDefinition struct {
	ShipmentID uuid.UUID         `json:"shipmentId"`
	Ref        string            `json:"ref"`
	Quantity   int               `json:"quantity"`
	Weight     float64           `json:"weight"`
	Address    AddressDefinition `json:"address"`
}

// Client POSTs shipment definitions to the external provider.
type Client struct {
	endpoint string
	cfg      RetryConfig
	hc       *http.Client
}

// New builds a provider Client for the given endpoint (from
// MISARCH_SHIPMENT_PROVIDER_ENDPOINT) reading retry parameters live from cfg.
func New(endpoint string, cfg RetryConfig) *Client {
	return &Client{
		endpoint: endpoint,
		cfg:      cfg,
		hc:       &http.Client{Timeout: 30 * time.Second},
	}
}

// Send POSTs the shipment definition with retries. It performs
// providerRetries GUARDED attempts (each failure is logged and followed by a
// providerRetryDelay-ms sleep), then ONE final unguarded attempt whose error
// propagates. Total attempts = providerRetries + 1. Retry parameters are read
// live from the ECS config on each call, so providerRetries == 0 → a single
// (final) POST. A non-2xx response is treated as a failure.
func (c *Client) Send(ctx context.Context, def ShipmentDefinition) error {
	retries := c.cfg.ProviderRetries()
	for i := 0; i < retries; i++ {
		if err := c.post(ctx, def); err != nil {
			slog.Error("Failed to send shipment to external provider, retry", "error", err)
			delay := time.Duration(c.cfg.ProviderRetryDelay()) * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			continue
		}
		return nil
	}
	// Final unguarded attempt: its error propagates (triggers creation-failed).
	return c.post(ctx, def)
}

// post performs a single POST of the definition. Non-2xx → error.
func (c *Client) post(ctx context.Context, def ShipmentDefinition) error {
	body, err := json.Marshal(def)
	if err != nil {
		return fmt.Errorf("marshal shipment definition: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("post shipment to provider: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("provider returned status %d", resp.StatusCode)
	}
	return nil
}
