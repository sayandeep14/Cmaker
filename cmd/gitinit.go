package cmd

import (
	"os/exec"
	"strings"
)

// maybeInitGit runs `git init` and an initial commit of the freshly
// scaffolded project at root, unless skip is true (--nogit), git isn't
// installed, or root is already inside a git repository (nesting a new
// repo inside an existing one - e.g. `cmaker init` run inside an
// already-tracked parent directory - is never what's wanted).
//
// The initial commit specifically matters beyond convention (matching
// tools like `cargo new`/create-react-app): 'cmaker heal --apply' already
// requires a clean git working tree, which a bare `git init` (nothing
// committed) would immediately fail, since untracked files still show up
// in `git status --porcelain`. Committing here means a freshly scaffolded
// project can use `cmaker heal --apply` right away.
//
// A failure here is a soft warning, not a fatal error - matching this
// whole scaffold flow's existing posture for pre-flight steps (CMake
// configure, package manager installs) that can legitimately fail in some
// environments (git not configured with a user identity, etc.) without
// that being cmaker's fault or invalidating the scaffold itself.
func maybeInitGit(root string, skip bool) {
	if skip {
		return
	}
	if _, err := exec.LookPath("git"); err != nil {
		return
	}
	if isInsideGitRepo(root) {
		return
	}

	if err := exec.Command("git", "-C", root, "init", "-q").Run(); err != nil {
		warnf("git init failed: %v (use --nogit to skip this)", err)
		return
	}
	if err := exec.Command("git", "-C", root, "add", "-A").Run(); err != nil {
		warnf("git add failed after 'git init': %v", err)
		return
	}
	if err := exec.Command("git", "-C", root, "commit", "-q", "-m", "Initial commit from cmaker").Run(); err != nil {
		warnf("initial git commit failed: %v (configure git's user.name/user.email and commit by hand, or re-run with --nogit next time)", err)
		return
	}
	okf("Initialized a git repository with an initial commit (use --nogit to skip this).")
}

// isInsideGitRepo reports whether root is inside a git working tree -
// checked from root's own perspective, so a parent directory that's
// already a repo is correctly detected too, not just root itself.
func isInsideGitRepo(root string) bool {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// currentBranch returns root's currently checked-out branch name.
func currentBranch(root string) (string, error) {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
