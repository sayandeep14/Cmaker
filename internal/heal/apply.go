package heal

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// WorkingTreeClean reports whether root's git working tree has no
// uncommitted changes (tracked or staged) - Apply refuses to run
// otherwise. This is a deliberate, non-negotiable gate for §24 v2: an
// LLM-proposed patch should never land on top of already-dirty state,
// where a failed/partial apply or a later revert becomes ambiguous about
// what came from the user vs. what came from the patch.
func WorkingTreeClean(root string) (bool, error) {
	out, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		return false, fmt.Errorf("failed to check git status (is %s a git repository?): %w", root, err)
	}
	return len(strings.TrimSpace(string(out))) == 0, nil
}

// Apply runs `git apply` against diff, rooted at root - the same mechanism
// v1's own printed instructions already told a human to use by hand, just
// automated. diff.go's unifiedDiff always emits standard a/-b/ prefixed
// headers, so this relies on git apply's default -p1 stripping.
func Apply(root, diff string) error {
	cmd := exec.Command("git", "-C", root, "apply")
	cmd.Stdin = strings.NewReader(diff + "\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git apply failed: %w\n%s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
