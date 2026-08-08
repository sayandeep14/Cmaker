package heal

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initGitRepo creates a fresh git repo at t.TempDir(), skipping the test if
// git isn't available - mirrors diff_test.go's own live-git verification
// approach (actually shelling out to git, not just eyeballing diff text).
func initGitRepo(t *testing.T) string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found on PATH, skipping")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(gitPath, append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	return dir
}

func TestWorkingTreeCleanOnFreshRepo(t *testing.T) {
	dir := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitPath, _ := exec.LookPath("git")
	exec.Command(gitPath, "-C", dir, "add", "f.txt").Run()
	exec.Command(gitPath, "-C", dir, "commit", "-q", "-m", "initial").Run()

	clean, err := WorkingTreeClean(dir)
	if err != nil {
		t.Fatalf("WorkingTreeClean() error = %v", err)
	}
	if !clean {
		t.Error("WorkingTreeClean() = false, want true for a freshly committed repo")
	}
}

func TestWorkingTreeCleanDetectsUncommittedChanges(t *testing.T) {
	dir := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitPath, _ := exec.LookPath("git")
	exec.Command(gitPath, "-C", dir, "add", "f.txt").Run()
	exec.Command(gitPath, "-C", dir, "commit", "-q", "-m", "initial").Run()

	// An untracked file makes the tree dirty.
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	clean, err := WorkingTreeClean(dir)
	if err != nil {
		t.Fatalf("WorkingTreeClean() error = %v", err)
	}
	if clean {
		t.Error("WorkingTreeClean() = true, want false with an untracked file present")
	}
}

func TestWorkingTreeCleanNotAGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found on PATH, skipping")
	}
	dir := t.TempDir()
	if _, err := WorkingTreeClean(dir); err == nil {
		t.Error("WorkingTreeClean() on a non-git directory: expected an error, got nil")
	}
}

func TestApplyAppliesADiffToTheWorkingTree(t *testing.T) {
	dir := initGitRepo(t)
	old := "int main() {\n    return 1;\n}\n"
	new_ := "int main() {\n    return 0;\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.cpp"), []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	gitPath, _ := exec.LookPath("git")
	exec.Command(gitPath, "-C", dir, "add", "main.cpp").Run()
	exec.Command(gitPath, "-C", dir, "commit", "-q", "-m", "initial").Run()

	diff := unifiedDiff("main.cpp", old, new_)
	if diff == "" {
		t.Fatal("unifiedDiff() returned empty for a real change")
	}

	if err := Apply(dir, diff); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "main.cpp"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != new_ {
		t.Errorf("after Apply(), file content = %q, want %q", got, new_)
	}
}

func TestApplyRejectsAnAlreadyAppliedDiff(t *testing.T) {
	dir := initGitRepo(t)
	old := "int main() {\n    return 1;\n}\n"
	new_ := "int main() {\n    return 0;\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.cpp"), []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	gitPath, _ := exec.LookPath("git")
	exec.Command(gitPath, "-C", dir, "add", "main.cpp").Run()
	exec.Command(gitPath, "-C", dir, "commit", "-q", "-m", "initial").Run()

	diff := unifiedDiff("main.cpp", old, new_)
	if err := Apply(dir, diff); err != nil {
		t.Fatalf("first Apply() error = %v", err)
	}
	if err := Apply(dir, diff); err == nil {
		t.Error("second Apply() of the same diff: expected an error (already applied), got nil")
	}
}
