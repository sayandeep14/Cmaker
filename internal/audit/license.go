package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var githubAPIBase = "https://api.github.com/repos/"

// GitHubLicense looks up repo's (an "owner/repo" GitHub shorthand - the same
// format config.Dependency.Repo uses for GitHub-hosted deps, see
// internal/cmake's GITHUB_REPOSITORY/GIT_REPOSITORY split) declared license
// via GitHub's public repos API, which reports whatever license GitHub's own
// detector (licensee) found in the repo. Only meaningful for GitHub-hosted
// dependencies; a full git URL (e.g. GitLab) isn't supported here and
// returns an error callers are expected to treat as "unknown, skip."
func GitHubLicense(ctx context.Context, repo string) (string, error) {
	if strings.Contains(repo, "://") {
		return "", fmt.Errorf("%q is not a GitHub owner/repo shorthand", repo)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPIBase+repo, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build GitHub API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to reach GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d for %s", resp.StatusCode, repo)
	}

	var parsed struct {
		License *struct {
			SPDXID string `json:"spdx_id"`
			Name   string `json:"name"`
		} `json:"license"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("failed to parse GitHub API response: %w", err)
	}
	if parsed.License == nil || parsed.License.SPDXID == "" || parsed.License.SPDXID == "NOASSERTION" {
		if parsed.License != nil && parsed.License.Name != "" {
			return parsed.License.Name, nil
		}
		return "", nil
	}
	return parsed.License.SPDXID, nil
}
