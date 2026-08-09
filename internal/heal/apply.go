package heal

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// HasGitRepo reports whether root is inside a git working tree at all -
// unlike WorkingTreeClean, this doesn't care whether it's dirty, only
// whether git has anything to check status against in the first place.
func HasGitRepo(root string) bool {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// InitScratchRepo bootstraps a throwaway git repo at root so 'git apply'
// has a clean baseline to work against, for projects that were never
// git-initialized at all - 'heal --apply' has no other way to confirm a
// clean working tree without git. Callers are expected to have already
// checked !HasGitRepo(root); the os.RemoveAll here is defensive (clearing
// any stale/partial .git left over from an interrupted previous run), not
// a way to nuke a real repo's history - RemoveScratchRepo is the intended
// teardown once the caller (heal) is done with it.
func InitScratchRepo(root string) error {
	if err := os.RemoveAll(filepath.Join(root, ".git")); err != nil {
		return fmt.Errorf("failed to clear stale .git before scratch init: %w", err)
	}
	for _, args := range [][]string{
		{"-C", root, "init", "-q"},
		{"-C", root, "add", "-A"},
		{"-C", root, "commit", "-q", "-m", "cmaker: temporary scratch commit for heal --apply"},
	} {
		if err := runGitStep(args); err != nil {
			return fmt.Errorf("scratch git init failed: %w", err)
		}
	}
	return nil
}

// RemoveScratchRepo tears down a repo created by InitScratchRepo,
// restoring root to its original git-free state once heal --apply is
// done with it (applied and verified, or given up).
func RemoveScratchRepo(root string) error {
	return os.RemoveAll(filepath.Join(root, ".git"))
}

// SafetyCommit commits everything currently in root's working tree
// (already a real git repo, just dirty) as a temporary baseline so
// 'heal --apply' has a clean tree to work against, without discarding the
// user's uncommitted work - UndoSafetyCommit reverses it afterward via a
// soft reset, landing the original changes (plus whatever heal itself
// applied on top) back in the working tree instead of erasing them.
func SafetyCommit(root string) error {
	for _, args := range [][]string{
		{"-C", root, "add", "-A"},
		{"-C", root, "commit", "-q", "-m", "cmaker: temporary WIP commit before heal --apply"},
	} {
		if err := runGitStep(args); err != nil {
			return fmt.Errorf("safety commit failed: %w", err)
		}
	}
	return nil
}

// UndoSafetyCommit soft-resets the commit SafetyCommit made - "soft" so
// its changes land back in the working tree (staged) rather than being
// discarded, restoring the pre-commit dirty state instead of erasing it.
func UndoSafetyCommit(root string) error {
	if err := runGitStep([]string{"-C", root, "reset", "--soft", "HEAD~1"}); err != nil {
		return fmt.Errorf("failed to undo safety commit: %w", err)
	}
	return nil
}

// runGitStep runs one git invocation, folding stderr into the returned
// error - a small shared helper for the multi-step sequences above.
func runGitStep(args []string) error {
	cmd := exec.Command("git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w\n%s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
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
