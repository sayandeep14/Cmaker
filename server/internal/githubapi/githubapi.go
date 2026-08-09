// Package githubapi verifies a GitHub user access token by asking GitHub
// who it belongs to - the server's only use of GitHub: it never stores
// the token, never needs a GitHub OAuth App client secret (the CLI's own
// device-flow polling happens entirely against GitHub directly, with no
// server involvement - see PACKS_PLAN.md §1), and this call is the one
// place a caller's claimed identity actually gets verified.
package githubapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// User is the subset of GitHub's "GET /user" response cmaker packs
// actually needs.
type User struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

// Verifier calls GitHub's API - an interface (not a concrete *Client)
// purely so handler tests can supply a fake, the same "external
// dependency behind a small interface" shape the root cmaker CLI already
// uses throughout for its own LLM Completer interfaces.
type Verifier interface {
	VerifyToken(ctx context.Context, githubToken string) (User, error)
}

// Client is the real Verifier, calling api.github.com.
type Client struct {
	HTTPClient *http.Client
}

// NewClient returns a Client with a sane request timeout - GitHub's API
// being briefly slow should never hang a login request indefinitely.
func NewClient() *Client {
	return &Client{HTTPClient: &http.Client{Timeout: 10 * time.Second}}
}

// VerifyToken asks GitHub who githubToken belongs to. A non-200 response
// (expired/invalid/revoked token, or GitHub itself unreachable) is
// reported as an error - the caller (the login handler) turns that into
// an HTTP 401, never a 500, since an invalid GitHub token is a client
// error, not a server fault.
func (c *Client) VerifyToken(ctx context.Context, githubToken string) (User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return User{}, fmt.Errorf("failed to build GitHub API request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+githubToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return User{}, fmt.Errorf("failed to reach GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return User{}, fmt.Errorf("GitHub API rejected the token (status %d)", resp.StatusCode)
	}

	var u User
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return User{}, fmt.Errorf("failed to parse GitHub API response: %w", err)
	}
	if u.Login == "" {
		return User{}, fmt.Errorf("GitHub API response had no login")
	}
	return u, nil
}
