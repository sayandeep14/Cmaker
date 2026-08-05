package audit

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueryCommitParsesVulns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req osvQueryRequest
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request body: %v", err)
		}
		if req.Commit != "deadbeef" {
			t.Errorf("request commit = %q, want %q", req.Commit, "deadbeef")
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"vulns": [{"id": "GHSA-test-1234", "summary": "a bad thing", "database_specific": {"severity": "HIGH"}}]}`))
	}))
	defer srv.Close()

	old := osvQueryURL
	osvQueryURL = srv.URL
	defer func() { osvQueryURL = old }()

	vulns, err := QueryCommit(context.Background(), "deadbeef")
	if err != nil {
		t.Fatalf("QueryCommit() error = %v", err)
	}
	if len(vulns) != 1 || vulns[0].ID != "GHSA-test-1234" || vulns[0].DatabaseSpecific.Severity != "HIGH" {
		t.Errorf("QueryCommit() = %+v, unexpected", vulns)
	}
}

func TestQueryCommitEmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	old := osvQueryURL
	osvQueryURL = srv.URL
	defer func() { osvQueryURL = old }()

	vulns, err := QueryCommit(context.Background(), "somecommit")
	if err != nil {
		t.Fatalf("QueryCommit() error = %v", err)
	}
	if len(vulns) != 0 {
		t.Errorf("QueryCommit() = %+v, want empty", vulns)
	}
}

func TestQueryCommitServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	}))
	defer srv.Close()

	old := osvQueryURL
	osvQueryURL = srv.URL
	defer func() { osvQueryURL = old }()

	if _, err := QueryCommit(context.Background(), "somecommit"); err == nil {
		t.Error("expected an error for a non-200 OSV.dev response")
	}
}
