package cmake

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// withPath points PATH at exactly dirs for the duration of a test, so
// DiscoverCompilers only sees fake binaries this test controls, not
// whatever's actually installed on the machine running the suite.
func withPath(t *testing.T, dirs ...string) {
	t.Helper()
	old := os.Getenv("PATH")
	os.Setenv("PATH", strings.Join(dirs, string(os.PathListSeparator)))
	t.Cleanup(func() { os.Setenv("PATH", old) })
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverCompilersFindsMatchingNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable-bit based detection doesn't apply on windows")
	}
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "clang++"))
	writeExecutable(t, filepath.Join(dir, "clang++-17"))
	writeExecutable(t, filepath.Join(dir, "g++-15"))
	writeExecutable(t, filepath.Join(dir, "gcc"))
	writeExecutable(t, filepath.Join(dir, "not-a-compiler"))
	writeExecutable(t, filepath.Join(dir, "clang++.txt")) // wrong name shape (matches only if literal)

	withPath(t, dir)

	got := DiscoverCompilers()
	want := map[string]bool{
		filepath.Join(dir, "clang++"):    true,
		filepath.Join(dir, "clang++-17"): true,
		filepath.Join(dir, "g++-15"):     true,
		filepath.Join(dir, "gcc"):        true,
	}
	if len(got) != len(want) {
		t.Fatalf("DiscoverCompilers() = %v, want exactly %v", got, want)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("DiscoverCompilers() unexpectedly included %q", g)
		}
	}
}

func TestDiscoverCompilersSkipsNonExecutableFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable-bit based detection doesn't apply on windows")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "clang++"), []byte("not executable"), 0644); err != nil {
		t.Fatal(err)
	}
	withPath(t, dir)

	if got := DiscoverCompilers(); len(got) != 0 {
		t.Errorf("DiscoverCompilers() = %v, want none (file isn't executable)", got)
	}
}

func TestDiscoverCompilersDeduplicatesAcrossPathEntries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable-bit based detection doesn't apply on windows")
	}
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "clang++"))
	// Same directory listed twice on PATH - must not produce a duplicate.
	withPath(t, dir, dir)

	got := DiscoverCompilers()
	if len(got) != 1 {
		t.Errorf("DiscoverCompilers() = %v, want exactly 1 entry (deduplicated)", got)
	}
}

func TestDiscoverCompilersNoneFound(t *testing.T) {
	dir := t.TempDir()
	withPath(t, dir)

	got := DiscoverCompilers()
	// The two hardcoded /opt/homebrew and /usr/local/opt llvm paths may or
	// may not exist on the machine running this test - only assert that an
	// empty PATH dir itself contributes nothing.
	for _, g := range got {
		if filepath.Dir(g) == dir {
			t.Errorf("DiscoverCompilers() found %q in an intentionally empty directory", g)
		}
	}
}
