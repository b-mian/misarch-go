// Package dapr provides the minimal Dapr HTTP API surface the MiSArch
// services use: pub/sub publishing, subscription registration, CloudEvent
// handling, and service invocation (including cross-service GraphQL queries).
package dapr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

// PubsubName is the Dapr pub/sub component used by all business events.
const PubsubName = "pubsub"

// BaseURL returns the Dapr sidecar HTTP endpoint, honoring DAPR_HTTP_PORT.
func BaseURL() string {
	port := os.Getenv("DAPR_HTTP_PORT")
	if port == "" {
		port = "3500"
	}
	return "http://localhost:" + port
}

// Client talks to the Dapr sidecar HTTP API.
type Client struct {
	base string
	hc   *http.Client
}

// NewClient creates a client for the local Dapr sidecar.
func NewClient() *Client {
	return &Client{base: BaseURL(), hc: &http.Client{Timeout: 30 * time.Second}}
}

// Publish publishes payload as JSON on the given topic of the "pubsub"
// component.
func (c *Client) Publish(ctx context.Context, topic string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("dapr publish %s: marshal: %w", topic, err)
	}
	url := fmt.Sprintf("%s/v1.0/publish/%s/%s", c.base, PubsubName, topic)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("dapr publish %s: %w", topic, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("dapr publish %s: status %d: %s", topic, resp.StatusCode, msg)
	}
	return nil
}

// Invoke calls another Dapr app's method with a JSON body and decodes the JSON
// response into out (which may be nil).
func (c *Client) Invoke(ctx context.Context, appID, method string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	url := fmt.Sprintf("%s/v1.0/invoke/%s/method/%s", c.base, appID, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("dapr invoke %s/%s: %w", appID, method, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("dapr invoke %s/%s: status %d: %s", appID, method, resp.StatusCode, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("dapr invoke %s/%s: decode: %w", appID, method, err)
		}
	}
	return nil
}

// graphQLRequest is the wire format of a GraphQL POST request.
type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// graphQLResponse is the wire format of a GraphQL response.
type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// QueryGraphQL executes a GraphQL query against another service through Dapr
// service invocation and decodes the "data" object into out. path is the
// GraphQL endpoint path of the target ("graphql" for all Go services).
func (c *Client) QueryGraphQL(ctx context.Context, appID, path, query string, vars map[string]any, out any) error {
	var resp graphQLResponse
	if err := c.Invoke(ctx, appID, path, graphQLRequest{Query: query, Variables: vars}, &resp); err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		return fmt.Errorf("graphql query to %s failed: %s", appID, resp.Errors[0].Message)
	}
	if out != nil {
		if err := json.Unmarshal(resp.Data, out); err != nil {
			return fmt.Errorf("graphql query to %s: decode data: %w", appID, err)
		}
	}
	return nil
}

// Subscription is one entry of the /dapr/subscribe registration response.
type Subscription struct {
	PubsubName string `json:"pubsubname"`
	Topic      string `json:"topic"`
	Route      string `json:"route"`
}

// EventHandler processes the "data" payload of an incoming CloudEvent.
// Returning an error causes a 500 response so Dapr redelivers the event.
type EventHandler func(ctx context.Context, data json.RawMessage) error

// cloudEvent is the subset of the CloudEvents envelope Dapr delivers.
type cloudEvent struct {
	ID    string          `json:"id"`
	Topic string          `json:"topic"`
	Data  json.RawMessage `json:"data"`
}

// Subscriber accumulates topic subscriptions and serves the Dapr endpoints.
type Subscriber struct {
	mu       sync.Mutex
	subs     []Subscription
	handlers map[string]EventHandler // route -> handler
}

// NewSubscriber returns an empty subscription registry.
func NewSubscriber() *Subscriber {
	return &Subscriber{handlers: map[string]EventHandler{}}
}

// Subscribe registers handler for topic, served on route (e.g. "/subscription/catalog/product/created").
func (s *Subscriber) Subscribe(topic, route string, handler EventHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs = append(s.subs, Subscription{PubsubName: PubsubName, Topic: topic, Route: route})
	s.handlers[route] = handler
}

// Mount registers GET /dapr/subscribe and the event POST routes on mux.
func (s *Subscriber) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /dapr/subscribe", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.subs)
	})
	for route, handler := range s.handlers {
		h := handler
		route := route
		mux.HandleFunc("POST "+route, func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var ce cloudEvent
			if err := json.Unmarshal(body, &ce); err != nil {
				slog.Error("dapr event: invalid CloudEvent", "route", route, "error", err)
				// Malformed events can never succeed; drop them.
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"DROP"}`))
				return
			}
			if err := h(r.Context(), ce.Data); err != nil {
				slog.Error("dapr event: handler failed", "route", route, "topic", ce.Topic, "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"SUCCESS"}`))
		})
	}
}

// Subscriptions returns the registered subscriptions (for logging/tests).
func (s *Subscriber) Subscriptions() []Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Subscription(nil), s.subs...)
}
