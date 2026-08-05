package registry

import (
	"os"
	"path/filepath"
	"testing"

	"cmaker/internal/config"
)

func TestSaveAndLoadLockfile(t *testing.T) {
	dir := t.TempDir()
	lf := Lockfile{Dependencies: map[string]LockEntry{
		"fmt": {Repo: "fmtlib/fmt", Tag: "11.0.2", Commit: "abc123"},
	}}
	if err := SaveLockfile(dir, lf); err != nil {
		t.Fatalf("SaveLockfile() error = %v", err)
	}

	got, err := LoadLockfile(dir)
	if err != nil {
		t.Fatalf("LoadLockfile() error = %v", err)
	}
	if got.Dependencies["fmt"] != lf.Dependencies["fmt"] {
		t.Errorf("LoadLockfile() = %+v, want %+v", got, lf)
	}
}

func TestLoadLockfileMissing(t *testing.T) {
	lf, err := LoadLockfile(t.TempDir())
	if err != nil {
		t.Fatalf("LoadLockfile() on a missing file should not error, got %v", err)
	}
	if len(lf.Dependencies) != 0 {
		t.Errorf("expected an empty lockfile, got %+v", lf)
	}
}

func TestUpdateLockfileNoDependencies(t *testing.T) {
	dir := t.TempDir()
	if err := UpdateLockfile(dir, filepath.Join(dir, "build"), config.Config{}); err != nil {
		t.Fatalf("UpdateLockfile() with no dependencies should be a no-op, got error %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, lockfileName)); !os.IsNotExist(err) {
		t.Error("UpdateLockfile() with no dependencies should not write a lockfile")
	}
}

func TestUpdateLockfileUnfetchedDependencyIsBestEffort(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{Dependencies: []config.Dependency{
		{Name: "fmt", Repo: "fmtlib/fmt", Tag: "11.0.2"},
	}}

	err := UpdateLockfile(dir, filepath.Join(dir, "build"), cfg)
	if err == nil {
		t.Fatal("expected an error reporting the unfetched dependency")
	}

	// Even though resolving the commit failed, a (empty-for-this-dep)
	// lockfile should still have been written - best-effort, not all-
	// or-nothing.
	if _, statErr := os.Stat(filepath.Join(dir, lockfileName)); statErr != nil {
		t.Errorf("expected a lockfile to still be written on partial failure: %v", statErr)
	}
}

func TestDepSourceDirLowercasesName(t *testing.T) {
	got := depSourceDir("/build", "Catch2")
	want := filepath.Join("/build", "_deps", "catch2-src")
	if got != want {
		t.Errorf("depSourceDir(Catch2) = %q, want %q", got, want)
	}
}
