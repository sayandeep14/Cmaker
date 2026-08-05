// Package audit implements cmaker's supply-chain auditing (§22): checking
// each locked dependency's exact resolved commit (see internal/registry's
// cmaker.lock) against OSV.dev for known vulnerabilities, and surfacing its
// declared license where discoverable. Pure logic, no CLI concerns - the
// wrapping lives in cmd/audit.go, mirroring internal/config/internal/cmake's
// split.
package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var osvQueryURL = "https://api.osv.dev/v1/query"

// Vulnerability is OSV.dev's own record shape, trimmed to the fields cmaker
// actually displays.
type Vulnerability struct {
	ID               string   `json:"id"`
	Summary          string   `json:"summary"`
	Aliases          []string `json:"aliases"` // e.g. ["CVE-2019-10906", "PYSEC-2019-217"]
	DatabaseSpecific struct {
		Severity string `json:"severity"` // e.g. "HIGH" - a plain string when present, unlike the CVSS-vector "severity" array OSV also sometimes includes
	} `json:"database_specific"`
}

type osvQueryRequest struct {
	Commit string `json:"commit"`
}

type osvQueryResponse struct {
	Vulns []Vulnerability `json:"vulns"`
}

// QueryCommit asks OSV.dev whether any known vulnerability affects the exact
// commit resolved for a dependency (see internal/registry.Lockfile) - a much
// more precise question than "does this tag have a CVE," since a tag is a
// mutable pointer and a locked commit is not. An empty result means OSV.dev
// has no record affecting this exact commit, not necessarily that the
// dependency has never had any vulnerability - coverage depends on OSV's own
// indexing for the project in question, the same caveat any vulnerability
// scanner has.
func QueryCommit(ctx context.Context, commit string) ([]Vulnerability, error) {
	body, err := json.Marshal(osvQueryRequest{Commit: commit})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal OSV.dev request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, osvQueryURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build OSV.dev request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach OSV.dev: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read OSV.dev response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OSV.dev returned status %d: %s", resp.StatusCode, string(data))
	}

	var parsed osvQueryResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse OSV.dev response: %w", err)
	}
	return parsed.Vulns, nil
}
