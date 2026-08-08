// Package llm is a deliberately minimal Anthropic Messages API client - just
// enough to send a single system+user prompt and get back the text
// response, for cmaker's one LLM-backed feature (`cmaker generate
// accessors`, see internal/codegen). A raw net/http call rather than the
// full anthropic-sdk-go module, to keep this single-purpose feature from
// pulling in a new dependency for a project that has otherwise stayed
// deliberately dependency-light (see e.g. the heuristic-parser-over-libclang
// call in ROADMAP.md §19).
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultModel is used when no --model override is given: a fast, cheap
// model is the right default for a small structured-extraction task like
// identifying a class's data members, not a large generation task.
const DefaultModel = "claude-haiku-4-5-20251001"

// DefaultImproviseModel is the default model for `cmaker new --describe
// --improvise` (§28) specifically - unlike DefaultModel's single-shot
// structured-extraction tasks (identify a class's members, pick a
// template), --improvise does multi-turn clarification plus real code
// authorship, a heavier reasoning job that benefits from a stronger model
// by default. Still overridable via --model like every other AI-assisted
// command.
const DefaultImproviseModel = "claude-sonnet-5"

const (
	apiURL     = "https://api.anthropic.com/v1/messages"
	apiVersion = "2023-06-01"
)

// Client is a minimal Anthropic Messages API client.
type Client struct {
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

// NewClientFromEnv builds a Client from the ANTHROPIC_API_KEY environment
// variable (the same variable name the official Anthropic SDKs use). model
// overrides DefaultModel when non-empty.
func NewClientFromEnv(model string) (*Client, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		// The env var always wins when set; this is only a fallback for
		// anyone who saved a key via the installer (scripts/install.sh)
		// instead of exporting it themselves - see SaveAPIKey.
		stored, err := resolveStoredAPIKey()
		if err != nil {
			return nil, err
		}
		key = stored
	}
	if key == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY is not set - this command needs an Anthropic API key (get one at https://console.anthropic.com/; re-run the cmaker installer to save one, or set the environment variable yourself)")
	}
	if model == "" {
		model = DefaultModel
	}
	return &Client{APIKey: key, Model: model, HTTPClient: &http.Client{Timeout: 60 * time.Second}}, nil
}

type messagesRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends a single-turn system+user prompt and returns the
// concatenated text of the response's text content blocks. Satisfies
// internal/codegen.Completer.
func (c *Client) Complete(ctx context.Context, system, user string) (string, error) {
	reqBody, err := json.Marshal(messagesRequest{
		Model:     c.Model,
		MaxTokens: 2048,
		System:    system,
		Messages:  []message{{Role: "user", Content: user}},
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal Anthropic API request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to build Anthropic API request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to reach the Anthropic API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read Anthropic API response: %w", err)
	}

	var parsed messagesResponse
	if jsonErr := json.Unmarshal(body, &parsed); jsonErr != nil {
		return "", fmt.Errorf("Anthropic API returned an unparseable response (status %d): %s", resp.StatusCode, string(body))
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("Anthropic API error: %s", parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Anthropic API returned status %d: %s", resp.StatusCode, string(body))
	}

	var text strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	return text.String(), nil
}

// TestConnection sends a minimal request to confirm the API key is valid
// and the Anthropic API is reachable - used by 'cmaker doctor --ai' and
// the installer's post-install verification step. Complete's own
// MaxTokens cap doesn't need lowering for this to stay cheap: Anthropic
// bills the actual output produced, not the cap, and a "reply with one
// word" prompt naturally stops after a few tokens.
func (c *Client) TestConnection(ctx context.Context) error {
	_, err := c.Complete(ctx, "", "Reply with just the word OK.")
	return err
}
