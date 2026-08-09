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
	cmd := exec.Command("git", "-C", root, "status", "--porcelain")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// 'cmaker new' never runs git init on a scaffolded project, so
		// hitting this on a brand new project is the common case, not an
		// edge case - worth a distinct, actionable message (not just the
		// raw "exit status 128" a plain %w would surface) rather than
		// leaving the user to guess that "git init" is the fix.
		if strings.Contains(stderr.String(), "not a git repository") {
			return false, fmt.Errorf("%s isn't a git repository - 'cmaker heal --apply' requires one, to safely confirm your working tree has no uncommitted changes before applying a patch. Run 'git init' (and make an initial commit) in this project, then retry - or use plain 'cmaker heal' (no --apply) to just see the suggested diff without needing git at all", root)
		}
		return false, fmt.Errorf("failed to check git status in %s: %w\n%s", root, err, strings.TrimSpace(stderr.String()))
	}
	return len(strings.TrimSpace(string(out))) == 0, nil
}

// Apply runs `git apply` against diff, rooted at root - the same mechanism
// v1's own printed instructions already told a human to use by hand, just
// automated. diff.go's UnifiedDiff always emits standard a/-b/ prefixed
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
