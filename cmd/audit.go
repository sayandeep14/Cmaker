package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"cmaker/internal/audit"
	"cmaker/internal/registry"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Check locked dependencies for known vulnerabilities and surface their licenses",
	Long: "Queries OSV.dev for known vulnerabilities affecting each dependency's exact locked commit\n" +
		"(cmaker.lock, see §17 - not just its 'tag:', which can move), and looks up each\n" +
		"GitHub-hosted dependency's declared license. Requires cmaker.lock to exist\n" +
		"(see 'cmaker install'). Exits non-zero if any known vulnerability is found.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAudit()
	},
}

func runAudit() error {
	lf, err := registry.LoadLockfile(".")
	if err != nil {
		return fmt.Errorf("failed to read cmaker.lock: %w", err)
	}
	if len(lf.Dependencies) == 0 {
		infof("No locked dependencies to audit (see 'cmaker install').")
		return nil
	}

	infof("Auditing %d locked dependencies against OSV.dev...", len(lf.Dependencies))
	reports := audit.Run(context.Background(), lf)

	foundVuln := false
	for _, r := range reports {
		license := r.License
		if license == "" {
			license = "unknown"
		}
		fmt.Printf("%s (%s@%s) - license: %s\n", r.Name, r.Repo, shortCommit(r.Commit), license)

		switch {
		case r.Err != nil:
			warnf("  could not check %s for vulnerabilities: %v", r.Name, r.Err)
		case len(r.Vulns) == 0:
			fmt.Println(colorize(ansiGreen, "  no known vulnerabilities"))
		default:
			foundVuln = true
			for _, v := range r.Vulns {
				severity := v.DatabaseSpecific.Severity
				if severity == "" {
					severity = "unknown severity"
				}
				fmt.Println(colorize(ansiRed, fmt.Sprintf("  %s (%s): %s", v.ID, severity, v.Summary)))
			}
		}
	}

	if foundVuln {
		return fmt.Errorf("known vulnerabilities found in one or more locked dependencies (see above)")
	}
	okf("No known vulnerabilities found.")
	return nil
}

// shortCommit trims a full 40-char git SHA down to a readable 8-char prefix
// for display, matching the convention `git log --oneline`/GitHub UIs use.
func shortCommit(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}
