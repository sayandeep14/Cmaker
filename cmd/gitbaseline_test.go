package cmd

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// runGitForTest runs a git command in the current directory, failing the
// test immediately on error - a small helper so the setup below reads as
// plain steps rather than repeated error-checking boilerplate.
func runGitForTest(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func initGitRepoForTest(t *testing.T, dir string) {
	t.Helper()
	t.Chdir(dir)
	runGitForTest(t, "init", "-q")
	runGitForTest(t, "config", "user.email", "test@example.com")
	runGitForTest(t, "config", "user.name", "Test")
}

func TestEnsureGitBaselineBootstrapsWhenNoRepo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("main.cpp", []byte("int main(){}"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	baseline, err := ensureGitBaseline()
	if err != nil {
		t.Fatalf("ensureGitBaseline() error = %v", err)
	}
	if !baseline.usedScratchRepo {
		t.Fatal("ensureGitBaseline() should bootstrap a scratch repo when none exists")
	}
	if _, err := os.Stat(".git"); err != nil {
		t.Fatalf(".git not created: %v", err)
	}

	// A failed run should remove the scratch repo again, leaving no trace.
	baseline.finish(false)
	if _, err := os.Stat(".git"); !os.IsNotExist(err) {
		t.Errorf(".git should have been removed after a failed run, stat err = %v", err)
	}
}

func TestEnsureGitBaselineKeepsScratchRepoOnSuccess(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	os.WriteFile("main.cpp", []byte("int main(){}"), 0644)

	baseline, err := ensureGitBaseline()
	if err != nil {
		t.Fatalf("ensureGitBaseline() error = %v", err)
	}
	baseline.finish(true)
	if _, err := os.Stat(".git"); err != nil {
		t.Errorf(".git should survive a successful run, stat err = %v", err)
	}
}

func TestEnsureGitBaselineNoOpOnCleanRepo(t *testing.T) {
	dir := t.TempDir()
	initGitRepoForTest(t, dir)
	os.WriteFile("main.cpp", []byte("int main(){}"), 0644)
	runGitForTest(t, "add", "-A")
	runGitForTest(t, "commit", "-q", "-m", "initial")

	baseline, err := ensureGitBaseline()
	if err != nil {
		t.Fatalf("ensureGitBaseline() error = %v", err)
	}
	if baseline.usedScratchRepo || baseline.usedSafetyCommit {
		t.Errorf("ensureGitBaseline() on an already-clean repo should be a no-op, got %+v", baseline)
	}
	baseline.finish(true)
}

func TestEnsureGitBaselineSafetyCommitsAndRestoresDirtyTree(t *testing.T) {
	dir := t.TempDir()
	initGitRepoForTest(t, dir)
	os.WriteFile("main.cpp", []byte("int main(){}"), 0644)
	runGitForTest(t, "add", "-A")
	runGitForTest(t, "commit", "-q", "-m", "initial")

	// Simulate a self-inflicted dirty tree, e.g. codegen's own
	// --intent-from breakdown step editing SUGGESTIONS.md before the
	// clean-tree check runs.
	if err := os.WriteFile("SUGGESTIONS.md", []byte("- [ ] task\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	baseline, err := ensureGitBaseline()
	if err != nil {
		t.Fatalf("ensureGitBaseline() error = %v", err)
	}
	if !baseline.usedSafetyCommit {
		t.Fatal("ensureGitBaseline() should make a temporary safety commit over a dirty tree instead of refusing")
	}

	// While "clean" (from the safety commit), simulate the agentic step
	// applying its own change and (in the non-safety-commit path this
	// would be a real checkpoint commit; here we just leave it dirty, as
	// runCodegenCore does when baseline.usedSafetyCommit is true).
	if err := os.WriteFile("main.cpp", []byte("int main(){return 0;}"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	baseline.finish(true)

	status, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	statusStr := string(status)
	if !strings.Contains(statusStr, "SUGGESTIONS.md") {
		t.Errorf("expected SUGGESTIONS.md to be back in the working tree after finish(), git status:\n%s", statusStr)
	}
	if !strings.Contains(statusStr, "main.cpp") {
		t.Errorf("expected main.cpp's change to be back in the working tree after finish(), git status:\n%s", statusStr)
	}
}

func TestCommitFileOnlyLeavesOtherDirtFilesUntouched(t *testing.T) {
	dir := t.TempDir()
	initGitRepoForTest(t, dir)
	os.WriteFile("main.cpp", []byte("int main(){}"), 0644)
	runGitForTest(t, "add", "-A")
	runGitForTest(t, "commit", "-q", "-m", "initial")

	os.WriteFile("SUGGESTIONS.md", []byte("- [ ] task\n"), 0644)
	os.WriteFile("unrelated.txt", []byte("dirty\n"), 0644)

	if err := commitFileOnly("SUGGESTIONS.md", "cmaker: break down suggestion"); err != nil {
		t.Fatalf("commitFileOnly() error = %v", err)
	}

	status, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	statusStr := string(status)
	if strings.Contains(statusStr, "SUGGESTIONS.md") {
		t.Errorf("SUGGESTIONS.md should have been committed, still shows dirty:\n%s", statusStr)
	}
	if !strings.Contains(statusStr, "unrelated.txt") {
		t.Errorf("unrelated.txt should still be dirty (untouched by commitFileOnly), git status:\n%s", statusStr)
	}
}
