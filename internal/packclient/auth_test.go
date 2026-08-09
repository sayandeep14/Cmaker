package packclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveGitHubClientIDFromVar(t *testing.T) {
	old := GitHubClientID
	GitHubClientID = "from-var"
	defer func() { GitHubClientID = old }()

	got, err := resolveGitHubClientID()
	if err != nil {
		t.Fatalf("resolveGitHubClientID() error = %v", err)
	}
	if got != "from-var" {
		t.Errorf("resolveGitHubClientID() = %q, want from-var", got)
	}
}

func TestResolveGitHubClientIDFromEnv(t *testing.T) {
	old := GitHubClientID
	GitHubClientID = ""
	defer func() { GitHubClientID = old }()
	t.Setenv("CMAKER_PACKS_GITHUB_CLIENT_ID", "from-env")

	got, err := resolveGitHubClientID()
	if err != nil {
		t.Fatalf("resolveGitHubClientID() error = %v", err)
	}
	if got != "from-env" {
		t.Errorf("resolveGitHubClientID() = %q, want from-env", got)
	}
}

func TestResolveGitHubClientIDMissing(t *testing.T) {
	old := GitHubClientID
	GitHubClientID = ""
	defer func() { GitHubClientID = old }()
	t.Setenv("CMAKER_PACKS_GITHUB_CLIENT_ID", "")

	if _, err := resolveGitHubClientID(); err == nil {
		t.Error("resolveGitHubClientID() with nothing configured: expected an error, got nil")
	}
}

// withFakeGitHub points githubDeviceCodeURL/githubTokenURL at a local
// httptest server for the duration of the test, restoring the real
// github.com URLs afterward - never actually dials out to GitHub in
// tests.
func withFakeGitHub(t *testing.T, deviceCodeHandler, tokenHandler http.HandlerFunc) {
	t.Helper()
	noop := func(w http.ResponseWriter, r *http.Request) {}
	if deviceCodeHandler == nil {
		deviceCodeHandler = noop
	}
	if tokenHandler == nil {
		tokenHandler = noop
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/device", deviceCodeHandler)
	mux.HandleFunc("/token", tokenHandler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	oldDevice, oldToken := githubDeviceCodeURL, githubTokenURL
	githubDeviceCodeURL = srv.URL + "/device"
	githubTokenURL = srv.URL + "/token"
	t.Cleanup(func() {
		githubDeviceCodeURL, githubTokenURL = oldDevice, oldToken
	})
}

func TestRequestDeviceCode(t *testing.T) {
	withFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(deviceCodeResponse{
			DeviceCode: "dc123", UserCode: "ABCD-1234",
			VerificationURI: "https://github.com/login/device", ExpiresIn: 900, Interval: 5,
		})
	}, nil)

	got, err := requestDeviceCode(context.Background(), "client123")
	if err != nil {
		t.Fatalf("requestDeviceCode() error = %v", err)
	}
	if got.UserCode != "ABCD-1234" || got.DeviceCode != "dc123" {
		t.Errorf("requestDeviceCode() = %+v, unexpected", got)
	}
}

func TestRequestDeviceCodeEmptyResponse(t *testing.T) {
	withFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(deviceCodeResponse{})
	}, nil)

	if _, err := requestDeviceCode(context.Background(), "client123"); err == nil {
		t.Error("requestDeviceCode() with no device_code: expected an error, got nil")
	}
}

func TestPollForTokenPendingThenSuccess(t *testing.T) {
	attempts := 0
	withFakeGitHub(t, nil, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			json.NewEncoder(w).Encode(tokenPollResponse{Error: "authorization_pending"})
			return
		}
		json.NewEncoder(w).Encode(tokenPollResponse{AccessToken: "gho_realtoken"})
	})

	got, err := pollForToken(context.Background(), "client123", "dc123", 0, 60)
	if err != nil {
		t.Fatalf("pollForToken() error = %v", err)
	}
	if got != "gho_realtoken" {
		t.Errorf("pollForToken() = %q, want gho_realtoken", got)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestPollForTokenExpired(t *testing.T) {
	withFakeGitHub(t, nil, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tokenPollResponse{Error: "expired_token"})
	})

	if _, err := pollForToken(context.Background(), "client123", "dc123", 0, 60); err == nil {
		t.Error("pollForToken() with expired_token: expected an error, got nil")
	}
}

func TestPollForTokenAccessDenied(t *testing.T) {
	withFakeGitHub(t, nil, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tokenPollResponse{Error: "access_denied"})
	})

	if _, err := pollForToken(context.Background(), "client123", "dc123", 0, 60); err == nil {
		t.Error("pollForToken() with access_denied: expected an error, got nil")
	}
}

func TestPollForTokenTimesOut(t *testing.T) {
	withFakeGitHub(t, nil, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tokenPollResponse{Error: "authorization_pending"})
	})

	// expiresIn=0 with interval=0 means the deadline is already in the
	// past by the time the first wait elapses - should time out fast.
	_, err := pollForToken(context.Background(), "client123", "dc123", 0, 0)
	if err == nil {
		t.Error("pollForToken() with an already-past deadline: expected an error, got nil")
	}
}

func TestDeviceLoginEndToEnd(t *testing.T) {
	old := GitHubClientID
	GitHubClientID = "client123"
	defer func() { GitHubClientID = old }()

	mux := http.NewServeMux()
	mux.HandleFunc("/device", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(deviceCodeResponse{DeviceCode: "dc", UserCode: "AB-CD", VerificationURI: "https://github.com/login/device", ExpiresIn: 60, Interval: 0})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tokenPollResponse{AccessToken: "gho_test"})
	})
	githubSrv := httptest.NewServer(mux)
	defer githubSrv.Close()
	oldDevice, oldToken := githubDeviceCodeURL, githubTokenURL
	githubDeviceCodeURL, githubTokenURL = githubSrv.URL+"/device", githubSrv.URL+"/token"
	defer func() { githubDeviceCodeURL, githubTokenURL = oldDevice, oldToken }()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		json.NewDecoder(r.Body).Decode(&req)
		if req["github_token"] != "gho_test" {
			t.Errorf("github_token = %q, want gho_test", req["github_token"])
		}
		json.NewEncoder(w).Encode(map[string]any{
			"token": "cmpk_live_abc",
			"user":  map[string]any{"login": "octocat", "id": 42},
		})
	}))
	defer apiSrv.Close()

	c := NewClient(apiSrv.URL, "")
	var promptedCode, promptedURI string
	token, login, err := c.DeviceLogin(context.Background(), func(userCode, verificationURI string) {
		promptedCode, promptedURI = userCode, verificationURI
	})
	if err != nil {
		t.Fatalf("DeviceLogin() error = %v", err)
	}
	if token != "cmpk_live_abc" || login != "octocat" {
		t.Errorf("DeviceLogin() = (%q, %q), unexpected", token, login)
	}
	if promptedCode != "AB-CD" {
		t.Errorf("prompted code = %q, want AB-CD", promptedCode)
	}
	if promptedURI == "" {
		t.Error("expected a non-empty verification URI to be prompted")
	}
}
