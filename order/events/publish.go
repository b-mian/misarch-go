package events

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"misarch/pkg/dapr"
)

// Publisher publishes the order-created and order-compensation-created events to
// the local Dapr sidecar. It reproduces the original's fire-and-forget-ish
// behavior: the publish is awaited but the HTTP response status is NOT checked —
// only a transport error (the request itself failing) propagates; a non-2xx
// from Dapr is ignored.
type Publisher struct {
	hc                   *http.Client
	orderCreatedURL      string
	orderCompensationURL string
}

// NewPublisher builds a Publisher targeting the local Dapr sidecar with the
// exact publish URLs the original uses (pubsub component "pubsub").
func NewPublisher() *Publisher {
	base := dapr.BaseURL()
	return &Publisher{
		hc:                   &http.Client{Timeout: 30 * time.Second},
		orderCreatedURL:      base + "/v1.0/publish/pubsub/" + TopicOrderCreated,
		orderCompensationURL: base + "/v1.0/publish/pubsub/" + TopicOrderCompensationCreated,
	}
}

// post marshals payload as JSON and POSTs it to url. Matching the original: only
// a transport error is returned; any HTTP status (including Dapr errors) is
// treated as success and ignored.
func (p *Publisher) post(ctx context.Context, url string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.hc.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// PublishOrderCreated publishes the order/order/created event. Mirrors
// send_order_created_event.
func (p *Publisher) PublishOrderCreated(ctx context.Context, dto OrderDTO) error {
	return p.post(ctx, p.orderCreatedURL, dto)
}

// PublishOrderCompensationCreated publishes the order/order-compensation/created
// event. Mirrors send_order_compensation_event.
func (p *Publisher) PublishOrderCompensationCreated(ctx context.Context, dto OrderCompensationDTO) error {
	return p.post(ctx, p.orderCompensationURL, dto)
}
