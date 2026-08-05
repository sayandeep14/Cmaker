package audit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubLicenseFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/fmtlib/fmt") {
			t.Errorf("request path = %q, want suffix /fmtlib/fmt", r.URL.Path)
		}
		w.Write([]byte(`{"license": {"spdx_id": "MIT", "name": "MIT License"}}`))
	}))
	defer srv.Close()

	old := githubAPIBase
	githubAPIBase = srv.URL + "/"
	defer func() { githubAPIBase = old }()

	license, err := GitHubLicense(context.Background(), "fmtlib/fmt")
	if err != nil {
		t.Fatalf("GitHubLicense() error = %v", err)
	}
	if license != "MIT" {
		t.Errorf("GitHubLicense() = %q, want %q", license, "MIT")
	}
}

func TestGitHubLicenseUndetected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"license": null}`))
	}))
	defer srv.Close()

	old := githubAPIBase
	githubAPIBase = srv.URL + "/"
	defer func() { githubAPIBase = old }()

	license, err := GitHubLicense(context.Background(), "someowner/somerepo")
	if err != nil {
		t.Fatalf("GitHubLicense() error = %v", err)
	}
	if license != "" {
		t.Errorf("GitHubLicense() = %q, want empty for an undetected license", license)
	}
}

func TestGitHubLicenseRejectsFullURL(t *testing.T) {
	if _, err := GitHubLicense(context.Background(), "https://gitlab.com/libeigen/eigen.git"); err == nil {
		t.Error("expected an error for a non-GitHub-shorthand repo")
	}
}

func TestGitHubLicenseNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	old := githubAPIBase
	githubAPIBase = srv.URL + "/"
	defer func() { githubAPIBase = old }()

	if _, err := GitHubLicense(context.Background(), "someowner/doesnotexist"); err == nil {
		t.Error("expected an error for a 404 from the GitHub API")
	}
}
