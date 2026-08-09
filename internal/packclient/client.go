// Package packclient is the CLI-side HTTP client for the cmaker packs API
// server (see ../../PACKS_PLAN.md and ../../server/) - login (GitHub
// device flow), publish, search, and install. Mirrors internal/llm's own
// "small net/http client, no framework dependency" shape.
package packclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// DefaultBaseURL is the production cmaker packs API.
const DefaultBaseURL = "https://cmaker-packs-api.fly.dev"

// Client talks to the cmaker packs API server.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// NewClient builds a Client. An empty baseURL resolves to the
// CMAKER_PACKS_URL environment variable if set (handy for pointing at a
// local dev server), otherwise DefaultBaseURL. token may be "" for the
// one endpoint that doesn't need it (login itself).
func NewClient(baseURL, token string) *Client {
	if baseURL == "" {
		baseURL = os.Getenv("CMAKER_PACKS_URL")
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL:    baseURL,
		Token:      token,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// apiError is the {"error": "..."} shape every handler in server/internal/api
// responds with on failure.
type apiError struct {
	Error string `json:"error"`
}

// doJSON does one JSON-in/JSON-out request against the API server -
// shared by every method in packs.go/auth.go. out may be nil for an
// endpoint with no meaningful response body.
func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to encode request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		var apiErr apiError
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Error != "" {
			return fmt.Errorf("%s (status %d)", apiErr.Error, resp.StatusCode)
		}
		return fmt.Errorf("request to %s failed with status %d", path, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to decode response from %s: %w", path, err)
	}
	return nil
}
