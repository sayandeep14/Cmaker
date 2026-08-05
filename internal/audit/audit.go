package audit

import (
	"context"
	"sort"

	"cmaker/internal/registry"
)

// Report is one dependency's audit result: known vulnerabilities affecting
// its exact locked commit, plus its declared license where discoverable.
type Report struct {
	Name    string
	Repo    string
	Tag     string
	Commit  string
	Vulns   []Vulnerability
	License string // empty if undetected, or not a GitHub-hosted dependency
	Err     error  // set if the OSV.dev query itself failed (network, etc.) - distinct from "queried fine, found nothing"
}

// Run audits every dependency in lf: queries OSV.dev by its locked commit,
// and looks up its license (best-effort - silently left empty for
// non-GitHub dependencies or when GitHub's API doesn't report one).
// Reports are sorted by dependency name for stable, diffable output.
func Run(ctx context.Context, lf registry.Lockfile) []Report {
	names := make([]string, 0, len(lf.Dependencies))
	for name := range lf.Dependencies {
		names = append(names, name)
	}
	sort.Strings(names)

	reports := make([]Report, 0, len(names))
	for _, name := range names {
		entry := lf.Dependencies[name]
		r := Report{Name: name, Repo: entry.Repo, Tag: entry.Tag, Commit: entry.Commit}

		if vulns, err := QueryCommit(ctx, entry.Commit); err != nil {
			r.Err = err
		} else {
			r.Vulns = vulns
		}

		if license, err := GitHubLicense(ctx, entry.Repo); err == nil {
			r.License = license
		}

		reports = append(reports, r)
	}
	return reports
}
