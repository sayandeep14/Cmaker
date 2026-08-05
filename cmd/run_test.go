package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

func writeFileAt(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
