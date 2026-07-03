package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"misarch/pkg/dapr"
)

// gqlClient issues GraphQL queries to other MiSArch subgraphs through the local
// Dapr sidecar's service-invocation API. It mirrors the original's raw reqwest
// calls to http://localhost:3500/v1.0/invoke/{app}/method/graphql, including the
// ability to forward the Authorized-User header to the shoppingcart call (and
// only that call).
//
// A GraphQL "errors" array in the response is surfaced as a Go error; an empty
// "data" object is likewise an error, matching the original's `data.ok_or(...)`
// checks. Callers decode the "data" object into their own typed struct.
type gqlClient struct {
	hc   *http.Client
	base string
}

// newGQLClient builds a client for the local Dapr sidecar.
func newGQLClient() *gqlClient {
	return &gqlClient{
		hc:   &http.Client{Timeout: 30 * time.Second},
		base: dapr.BaseURL(),
	}
}

// gqlRequest is the GraphQL POST body.
type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// gqlResponse is the GraphQL response envelope. Data is captured raw so callers
// decode it into their own shapes; a null/absent data with the caller's "empty"
// check reproduces the original's behavior.
type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// query invokes appID's GraphQL endpoint (method path "graphql") with the given
// query and variables and decodes the "data" object into out. authorizedUser,
// when non-empty, is forwarded as the Authorized-User header (used only for the
// shoppingcart call). emptyDataErr is returned when the response "data" is null
// or absent, matching the original's `response_body.data.ok_or(...)`.
func (c *gqlClient) query(ctx context.Context, appID string, query string, vars map[string]any, authorizedUser string, out any, emptyDataErr error) error {
	body, err := json.Marshal(gqlRequest{Query: query, Variables: vars})
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/v1.0/invoke/%s/method/graphql", c.base, appID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if authorizedUser != "" {
		req.Header.Set("Authorized-User", authorizedUser)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var parsed gqlResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("graphql query to %s: decode: %w", appID, err)
	}
	if len(parsed.Errors) > 0 {
		return fmt.Errorf("graphql query to %s failed: %s", appID, parsed.Errors[0].Message)
	}
	if len(parsed.Data) == 0 || string(parsed.Data) == "null" {
		return emptyDataErr
	}
	if out != nil {
		if err := json.Unmarshal(parsed.Data, out); err != nil {
			return fmt.Errorf("graphql query to %s: decode data: %w", appID, err)
		}
	}
	return nil
}
