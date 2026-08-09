package packclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServerClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "test-token")
}

func TestWhoami(t *testing.T) {
	c := newTestServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", got)
		}
		json.NewEncoder(w).Encode(WhoamiResponse{Login: "octocat", ID: 42})
	})

	got, err := c.Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami() error = %v", err)
	}
	if got.Login != "octocat" || got.ID != 42 {
		t.Errorf("Whoami() = %+v, unexpected", got)
	}
}

func TestWhoamiErrorResponse(t *testing.T) {
	c := newTestServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(apiError{Error: "invalid or revoked token"})
	})

	_, err := c.Whoami(context.Background())
	if err == nil {
		t.Fatal("Whoami() expected an error, got nil")
	}
	if err.Error() != "invalid or revoked token (status 401)" {
		t.Errorf("Whoami() error = %q, unexpected", err.Error())
	}
}

func TestSearchEncodesQuery(t *testing.T) {
	c := newTestServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "json helpers" {
			t.Errorf("q = %q, want %q", got, "json helpers")
		}
		json.NewEncoder(w).Encode([]SearchResult{{Name: "json-helpers", Description: "small utils"}})
	})

	results, err := c.Search(context.Background(), "json helpers")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].Name != "json-helpers" {
		t.Errorf("Search() = %+v, unexpected", results)
	}
}

func TestGetPack(t *testing.T) {
	c := newTestServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/packs/mypack" {
			t.Errorf("path = %q, want /v1/packs/mypack", r.URL.Path)
		}
		json.NewEncoder(w).Encode(PackInfo{Name: "mypack", Versions: []PackVersionSummary{{Version: "1.0.0", Status: "published"}}})
	})

	got, err := c.GetPack(context.Background(), "mypack")
	if err != nil {
		t.Fatalf("GetPack() error = %v", err)
	}
	if got.Name != "mypack" || len(got.Versions) != 1 {
		t.Errorf("GetPack() = %+v, unexpected", got)
	}
}

func TestCreateVersion(t *testing.T) {
	c := newTestServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		var req CreateVersionRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Version != "1.0.0" {
			t.Errorf("req.Version = %q, want 1.0.0", req.Version)
		}
		json.NewEncoder(w).Encode(CreateVersionResponse{UploadURL: "https://r2.example.com/put", ObjectKey: "packs/mypack/1.0.0.tar.gz"})
	})

	resp, err := c.CreateVersion(context.Background(), "mypack", CreateVersionRequest{Version: "1.0.0"})
	if err != nil {
		t.Fatalf("CreateVersion() error = %v", err)
	}
	if resp.UploadURL == "" {
		t.Error("expected a non-empty upload URL")
	}
}

func TestCompleteUpload(t *testing.T) {
	called := false
	c := newTestServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/v1/packs/mypack/versions/1.0.0/complete" {
			t.Errorf("path = %q, unexpected", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "published"})
	})

	if err := c.CompleteUpload(context.Background(), "mypack", "1.0.0"); err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	if !called {
		t.Error("expected the complete endpoint to be called")
	}
}

func TestUploadAndDownloadTarball(t *testing.T) {
	var uploaded []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			uploaded = data
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			w.Write(uploaded)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	data := []byte("fake tarball content")
	if err := c.UploadTarball(context.Background(), srv.URL, data); err != nil {
		t.Fatalf("UploadTarball() error = %v", err)
	}

	got, err := c.DownloadTarball(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("DownloadTarball() error = %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("DownloadTarball() = %q, want %q", got, data)
	}
}
