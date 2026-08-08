package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"cmaker/internal/config"
)

func TestIsBuildRequiredNoBinary(t *testing.T) {
	t.Chdir(t.TempDir())
	if !isBuildRequired(filepath.Join("build", "main")) {
		t.Error("expected true when the binary doesn't exist yet")
	}
}

func TestIsBuildRequiredSourceNewerThanBinary(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	binaryPath := filepath.Join("build", "main")
	writeFileAt(t, binaryPath, "binary")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(binaryPath, old, old); err != nil {
		t.Fatal(err)
	}

	writeFileAt(t, filepath.Join("src", "main.cpp"), "int main(){}")

	if !isBuildRequired(binaryPath) {
		t.Error("expected true when a source file is newer than the binary")
	}
}

func TestIsBuildRequiredBinaryUpToDate(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeFileAt(t, filepath.Join("src", "main.cpp"), "int main(){}")
	old := time.Now().Add(-time.Hour)
	srcPath := filepath.Join(dir, "src", "main.cpp")
	if err := os.Chtimes(srcPath, old, old); err != nil {
		t.Fatal(err)
	}

	binaryPath := filepath.Join("build", "main")
	writeFileAt(t, binaryPath, "binary") // written after the source, so it's newer

	if isBuildRequired(binaryPath) {
		t.Error("expected false when the binary is newer than every tracked source")
	}
}

func TestIsBuildRequiredIgnoresBuildAndGitDirs(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	binaryPath := filepath.Join("build", "main")
	writeFileAt(t, binaryPath, "binary")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(binaryPath, old, old); err != nil {
		t.Fatal(err)
	}

	// Files inside build/ and .git/ must not count as "newer sources",
	// even though they postdate the binary - otherwise every build/.git
	// artifact (including the binary's own siblings) would force an
	// unnecessary rebuild on every single invocation.
	writeFileAt(t, filepath.Join("build", "CMakeFiles", "stamp.txt"), "stamp")
	writeFileAt(t, filepath.Join(".git", "HEAD"), "ref: refs/heads/main")

	if isBuildRequired(binaryPath) {
		t.Error("expected false - only build/ and .git/ have newer files, both should be skipped")
	}
}

func TestRunnableBinaryNameExecutable(t *testing.T) {
	got, err := runnableBinaryName(config.Config{Executable: "main"}, "executable")
	if err != nil {
		t.Fatalf("runnableBinaryName() error = %v", err)
	}
	if got != "main" {
		t.Errorf("runnableBinaryName() = %q, want %q", got, "main")
	}
}

func TestRunnableBinaryNameLibraryWithoutDemo(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := runnableBinaryName(config.Config{Executable: "mylib"}, "static_library"); err == nil {
		t.Error("expected an error when no examples/demo.cpp exists for a library project")
	}
}

func TestRunnableBinaryNameLibraryWithDemo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFileAt(t, filepath.Join("examples", "demo.cpp"), "int main(){}")

	got, err := runnableBinaryName(config.Config{Executable: "mylib"}, "static_library")
	if err != nil {
		t.Fatalf("runnableBinaryName() error = %v", err)
	}
	if got != "mylib_demo" {
		t.Errorf("runnableBinaryName() = %q, want %q", got, "mylib_demo")
	}
}

func writeFileAt(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestShellQuote only exercises this platform's own branch of shellQuote -
// runtime.GOOS is a compile-time constant, so the Windows branch can't be
// reached by a test run on macOS/Linux CI. It's covered by code review
// instead (see ROADMAP.md's Windows-support entry for the honesty note on
// what could and couldn't be live-verified for this platform).
func TestShellQuote(t *testing.T) {
	got := shellQuote("it's a test")
	want := "'it'\\''s a test'"
	if runtime.GOOS == "windows" {
		want = "'it''s a test'"
	}
	if got != want {
		t.Errorf("shellQuote(%q) = %q, want %q", "it's a test", got, want)
	}
}

func TestShellQuoteNoEmbeddedQuote(t *testing.T) {
	if got := shellQuote("plain"); got != "'plain'" {
		t.Errorf("shellQuote(%q) = %q, want %q", "plain", got, "'plain'")
	}
}

func TestLoginShellCommandUsesShellEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this platform's branch is exercised by TestLoginShellCommandWindows-equivalent logic on Windows CI, not here")
	}
	t.Setenv("SHELL", "/bin/zsh")
	cmd := loginShellCommand("echo hi")
	if len(cmd.Args) != 4 || cmd.Args[1] != "-i" || cmd.Args[2] != "-c" || cmd.Args[3] != "echo hi" {
		t.Errorf("loginShellCommand() args = %v, want [.../zsh -i -c \"echo hi\"]", cmd.Args)
	}
}

func TestLoginShellCommandDefaultsWhenShellUnset(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("only relevant to the $SHELL-based POSIX branch")
	}
	t.Setenv("SHELL", "")
	cmd := loginShellCommand("echo hi")
	if cmd.Args[0] != "/bin/sh" {
		t.Errorf("loginShellCommand() with no $SHELL = %v, want it to default to /bin/sh", cmd.Args)
	}
}
