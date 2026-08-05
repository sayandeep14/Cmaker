package audit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cmaker/internal/registry"
)

func TestRunSortedAndPopulated(t *testing.T) {
	osvSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer osvSrv.Close()
	oldOSV := osvQueryURL
	osvQueryURL = osvSrv.URL
	defer func() { osvQueryURL = oldOSV }()

	ghSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"license": {"spdx_id": "MIT"}}`))
	}))
	defer ghSrv.Close()
	oldGH := githubAPIBase
	githubAPIBase = ghSrv.URL + "/"
	defer func() { githubAPIBase = oldGH }()

	lf := registry.Lockfile{Dependencies: map[string]registry.LockEntry{
		"zzz": {Repo: "owner/zzz", Tag: "v1.0.0", Commit: "commit-zzz"},
		"aaa": {Repo: "owner/aaa", Tag: "v2.0.0", Commit: "commit-aaa"},
	}}

	reports := Run(context.Background(), lf)
	if len(reports) != 2 {
		t.Fatalf("Run() returned %d reports, want 2", len(reports))
	}
	if reports[0].Name != "aaa" || reports[1].Name != "zzz" {
		t.Errorf("Run() not sorted by name: got %q then %q", reports[0].Name, reports[1].Name)
	}
	for _, r := range reports {
		if r.Err != nil {
			t.Errorf("report %q has unexpected error: %v", r.Name, r.Err)
		}
		if r.License != "MIT" {
			t.Errorf("report %q License = %q, want MIT", r.Name, r.License)
		}
		if len(r.Vulns) != 0 {
			t.Errorf("report %q Vulns = %v, want empty", r.Name, r.Vulns)
		}
	}
}

func TestRunEmptyLockfile(t *testing.T) {
	reports := Run(context.Background(), registry.Lockfile{Dependencies: map[string]registry.LockEntry{}})
	if len(reports) != 0 {
		t.Errorf("Run() on an empty lockfile = %v, want empty", reports)
	}
}
