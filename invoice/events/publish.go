package events

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"misarch/pkg/dapr"
)

// Publisher publishes the invoice-created event. It reproduces the original's
// broken publish behavior faithfully (see PublishInvoiceCreated).
type Publisher struct {
	hc  *http.Client
	url string
}

// NewPublisher builds a Publisher targeting the local Dapr sidecar with the
// EXACT (buggy) publish URL the original uses.
func NewPublisher() *Publisher {
	return &Publisher{
		hc: &http.Client{Timeout: 30 * time.Second},
		// BUG (preserved): the Dapr publish route is
		// /v1.0/publish/{pubsubname}/{topic}. The original omits the pubsub
		// component segment, so this addresses component "invoice" with topic
		// "invoice/created" — neither of which exists. Dapr answers 404
		// ERR_PUBSUB_NOT_FOUND; the original ignores non-transport errors, so
		// the event is silently lost. Intended URL would append the "pubsub"
		// component before the topic.
		url: dapr.BaseURL() + "/v1.0/publish/invoice/invoice/created",
	}
}

// PublishInvoiceCreated POSTs the invoice-created DTO as JSON to the (broken)
// publish URL. Matching the original send_invoice_created_event: only a
// transport error (the HTTP request itself failing) is returned; any HTTP
// status — including Dapr's 404 — is treated as success and ignored.
func (p *Publisher) PublishInvoiceCreated(ctx context.Context, dto InvoiceCreatedDTO) error {
	body, err := json.Marshal(dto)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.hc.Do(req)
	if err != nil {
		return err
	}
	// Response status is intentionally ignored (bug-for-bug): only the
	// transport error above fails. Drain and close the body.
	_ = resp.Body.Close()
	return nil
}
