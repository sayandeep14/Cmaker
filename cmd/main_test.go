package cmd

import (
	"os"
	"testing"
)

// TestMain gives git a fixed commit identity so tests that make commits
// (e.g. ensureGitBaseline's scratch repo) don't depend on the machine's
// global git config - CI runners have no user.name/user.email set.
func TestMain(m *testing.M) {
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME":     "cmaker test",
		"GIT_AUTHOR_EMAIL":    "test@example.com",
		"GIT_COMMITTER_NAME":  "cmaker test",
		"GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		os.Setenv(k, v)
	}
	os.Exit(m.Run())
}
