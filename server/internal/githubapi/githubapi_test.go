package githubapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestClient points a Client at a local httptest server instead of the
// real api.github.com - VerifyToken itself always dials a hardcoded URL,
// so these tests exercise the request/response handling logic via a
// small local server hook instead (a real network call would make this
// suite flaky/non-hermetic, and isn't needed to test parsing/error
// handling).
func newTestClient(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return srv.Client().Transport.RoundTrip(req)
		}),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestVerifyTokenSuccess(t *testing.T) {
	httpClient := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer gh_test_token" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer gh_test_token")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 42, "login": "octocat", "avatar_url": "https://example.com/a.png"}`))
	})
	c := &Client{HTTPClient: httpClient}

	u, err := c.VerifyToken(context.Background(), "gh_test_token")
	if err != nil {
		t.Fatalf("VerifyToken() error = %v", err)
	}
	if u.ID != 42 || u.Login != "octocat" {
		t.Errorf("VerifyToken() = %+v, unexpected", u)
	}
}

func TestVerifyTokenRejectsNon200(t *testing.T) {
	httpClient := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	c := &Client{HTTPClient: httpClient}

	if _, err := c.VerifyToken(context.Background(), "bad_token"); err == nil {
		t.Error("VerifyToken() with a 401 response: expected an error, got nil")
	}
}

func TestVerifyTokenRejectsMalformedJSON(t *testing.T) {
	httpClient := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	})
	c := &Client{HTTPClient: httpClient}

	if _, err := c.VerifyToken(context.Background(), "tok"); err == nil {
		t.Error("VerifyToken() with malformed JSON: expected an error, got nil")
	}
}

func TestVerifyTokenRejectsEmptyLogin(t *testing.T) {
	httpClient := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id": 1, "login": ""}`))
	})
	c := &Client{HTTPClient: httpClient}

	if _, err := c.VerifyToken(context.Background(), "tok"); err == nil {
		t.Error("VerifyToken() with an empty login: expected an error, got nil")
	}
}
