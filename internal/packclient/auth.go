package packclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// GitHubClientID is cmaker's own public GitHub OAuth App client ID for
// the device flow - safe to embed in the binary (the whole point of
// device flow is that it needs no client secret). Injected at release-
// build time via -ldflags "-X cmaker/internal/packclient.GitHubClientID=..."
// (mirrors main.go's own version var); falls back to the
// CMAKER_PACKS_GITHUB_CLIENT_ID environment variable for local dev/
// testing before a real OAuth App's ID is baked into a release build.
var GitHubClientID = ""

// githubDeviceCodeURL/githubTokenURL are package-level vars (not
// consts) purely so tests can point them at a local httptest server
// instead of the real github.com - never reassigned outside tests.
var (
	githubDeviceCodeURL = "https://github.com/login/device/code"
	githubTokenURL      = "https://github.com/login/oauth/access_token"
)

func resolveGitHubClientID() (string, error) {
	if GitHubClientID != "" {
		return GitHubClientID, nil
	}
	if v := os.Getenv("CMAKER_PACKS_GITHUB_CLIENT_ID"); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("this build of cmaker has no GitHub OAuth client ID configured - 'cmaker login' isn't available")
}

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// requestDeviceCode starts GitHub's device flow (talking to GitHub
// directly - no cmaker packs server involvement at this step).
func requestDeviceCode(ctx context.Context, clientID string) (deviceCodeResponse, error) {
	form := url.Values{"client_id": {clientID}, "scope": {"read:user"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubDeviceCodeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return deviceCodeResponse{}, fmt.Errorf("failed to build device code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return deviceCodeResponse{}, fmt.Errorf("failed to reach GitHub: %w", err)
	}
	defer resp.Body.Close()

	var out deviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return deviceCodeResponse{}, fmt.Errorf("failed to parse GitHub's device code response: %w", err)
	}
	if out.DeviceCode == "" {
		return deviceCodeResponse{}, fmt.Errorf("GitHub didn't return a device code - check the OAuth App has Device Flow enabled")
	}
	if out.Interval == 0 {
		out.Interval = 5
	}
	return out, nil
}

type tokenPollResponse struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
}

// pollForToken polls GitHub, per requestDeviceCode's own Interval, until
// the user finishes authorizing in their browser, the device code
// expires, or they deny it.
func pollForToken(ctx context.Context, clientID, deviceCode string, interval, expiresIn int) (string, error) {
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	wait := time.Duration(interval) * time.Second

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(wait):
		}

		form := url.Values{
			"client_id":   {clientID},
			"device_code": {deviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubTokenURL, strings.NewReader(form.Encode()))
		if err != nil {
			return "", fmt.Errorf("failed to build token poll request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to reach GitHub: %w", err)
		}
		var out tokenPollResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if decodeErr != nil {
			return "", fmt.Errorf("failed to parse GitHub's token response: %w", decodeErr)
		}

		switch out.Error {
		case "":
			if out.AccessToken != "" {
				return out.AccessToken, nil
			}
		case "authorization_pending":
			// Keep polling at the same interval - the normal case while
			// the user hasn't finished authorizing in their browser yet.
		case "slow_down":
			wait += 5 * time.Second
		case "expired_token":
			return "", fmt.Errorf("the login code expired - run 'cmaker login' again")
		case "access_denied":
			return "", fmt.Errorf("login was denied")
		default:
			return "", fmt.Errorf("GitHub device flow error: %s", out.Error)
		}
	}
	return "", fmt.Errorf("timed out waiting for authorization")
}

// DeviceLoginPrompt is called once, with the code the user needs to enter
// and the URL to enter it at - the caller (cmd/login.go) owns how that's
// actually displayed/opened in a browser, keeping this package free of
// presentation concerns.
type DeviceLoginPrompt func(userCode, verificationURI string)

// DeviceLogin runs GitHub's device flow end to end, then immediately
// exchanges the resulting GitHub token for cmaker's own opaque API token
// via ExchangeToken - the GitHub token itself is never persisted or used
// again after this call.
func (c *Client) DeviceLogin(ctx context.Context, prompt DeviceLoginPrompt) (token, login string, err error) {
	clientID, err := resolveGitHubClientID()
	if err != nil {
		return "", "", err
	}

	dc, err := requestDeviceCode(ctx, clientID)
	if err != nil {
		return "", "", err
	}

	prompt(dc.UserCode, dc.VerificationURI)

	githubToken, err := pollForToken(ctx, clientID, dc.DeviceCode, dc.Interval, dc.ExpiresIn)
	if err != nil {
		return "", "", err
	}

	return c.ExchangeToken(ctx, githubToken)
}

// ExchangeToken calls POST /v1/auth/login with a verified GitHub token,
// returning cmaker's own opaque API token and the caller's GitHub login.
func (c *Client) ExchangeToken(ctx context.Context, githubToken string) (token, login string, err error) {
	var resp struct {
		Token string `json:"token"`
		User  struct {
			Login string `json:"login"`
			ID    int64  `json:"id"`
		} `json:"user"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v1/auth/login", map[string]string{"github_token": githubToken}, &resp); err != nil {
		return "", "", err
	}
	return resp.Token, resp.User.Login, nil
}
