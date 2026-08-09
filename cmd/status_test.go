package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHumanSize(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{1024 * 1024 * 1024, "1.0 GiB"},
	}
	for _, tt := range tests {
		if got := humanSize(tt.in); got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDirSizeOKMissingPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, ok := dirSizeOK("does-not-exist"); ok {
		t.Error("dirSizeOK() on a missing path: expected ok=false")
	}
	if got := dirSize("does-not-exist"); got != 0 {
		t.Errorf("dirSize() on a missing path = %d, want 0", got)
	}
}

func TestDirSizeOKSumsFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("12345"), 0644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("1234567890"), 0644)

	size, ok := dirSizeOK(dir)
	if !ok {
		t.Fatal("dirSizeOK() ok = false, want true")
	}
	if size != 15 {
		t.Errorf("dirSizeOK() = %d, want 15", size)
	}
}

func TestGitStatusCountsCleanRepo(t *testing.T) {
	dir := t.TempDir()
	initGitRepoForTest(t, dir)
	os.WriteFile("tracked.txt", []byte("x"), 0644)
	runGitForTest(t, "add", "-A")
	runGitForTest(t, "commit", "-q", "-m", "initial")

	staged, modified, untracked := gitStatusCounts()
	if staged != 0 || modified != 0 || untracked != 0 {
		t.Errorf("gitStatusCounts() on a clean repo = (%d, %d, %d), want (0, 0, 0)", staged, modified, untracked)
	}
}

func TestGitStatusCountsMixedState(t *testing.T) {
	dir := t.TempDir()
	initGitRepoForTest(t, dir)
	os.WriteFile("tracked.txt", []byte("x"), 0644)
	os.WriteFile("staged.txt", []byte("x"), 0644)
	runGitForTest(t, "add", "-A")
	runGitForTest(t, "commit", "-q", "-m", "initial")

	os.WriteFile("tracked.txt", []byte("changed"), 0644) // modified, not staged
	os.WriteFile("staged.txt", []byte("changed"), 0644)
	runGitForTest(t, "add", "staged.txt")      // staged
	os.WriteFile("new.txt", []byte("x"), 0644) // untracked

	staged, modified, untracked := gitStatusCounts()
	if staged != 1 {
		t.Errorf("gitStatusCounts() staged = %d, want 1", staged)
	}
	if modified != 1 {
		t.Errorf("gitStatusCounts() modified = %d, want 1", modified)
	}
	if untracked != 1 {
		t.Errorf("gitStatusCounts() untracked = %d, want 1", untracked)
	}
}

func TestGitLastCommit(t *testing.T) {
	dir := t.TempDir()
	initGitRepoForTest(t, dir)
	os.WriteFile("a.txt", []byte("x"), 0644)
	runGitForTest(t, "add", "-A")
	runGitForTest(t, "commit", "-q", "-m", "a real commit message")

	subject, when, ok := gitLastCommit()
	if !ok {
		t.Fatal("gitLastCommit() ok = false, want true")
	}
	if subject != "a real commit message" {
		t.Errorf("gitLastCommit() subject = %q", subject)
	}
	if when == "" {
		t.Error("gitLastCommit() when is empty")
	}
}

func TestGitLastCommitNoCommitsYet(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	runGitForTest(t, "init", "-q")

	if _, _, ok := gitLastCommit(); ok {
		t.Error("gitLastCommit() on an empty repo: expected ok=false")
	}
}

func TestGitAheadBehindNoUpstream(t *testing.T) {
	dir := t.TempDir()
	initGitRepoForTest(t, dir)
	os.WriteFile("a.txt", []byte("x"), 0644)
	runGitForTest(t, "add", "-A")
	runGitForTest(t, "commit", "-q", "-m", "initial")

	if _, _, ok := gitAheadBehind(); ok {
		t.Error("gitAheadBehind() with no upstream configured: expected ok=false")
	}
}

func TestParseCoveragePercentFromHTML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.html")
	html := `<html><body><table><tr><td>Lines:</td><td class="right">87.5%</td></tr></table></body></html>`
	os.WriteFile(path, []byte(html), 0644)

	pct, ok := parseCoveragePercentFromHTML(path)
	if !ok {
		t.Fatal("parseCoveragePercentFromHTML() ok = false, want true")
	}
	if pct != 87.5 {
		t.Errorf("parseCoveragePercentFromHTML() = %v, want 87.5", pct)
	}
}

func TestParseCoveragePercentFromHTMLNoMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.html")
	os.WriteFile(path, []byte("<html><body>no coverage info here</body></html>"), 0644)

	if _, ok := parseCoveragePercentFromHTML(path); ok {
		t.Error("parseCoveragePercentFromHTML() on unrelated HTML: expected ok=false")
	}
}
