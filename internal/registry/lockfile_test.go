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

func TestRecordAndRemovePack(t *testing.T) {
	dir := t.TempDir()

	if err := RecordPack(dir, "mypack", "1.0.0", "abc123"); err != nil {
		t.Fatalf("RecordPack() error = %v", err)
	}
	lf, err := LoadLockfile(dir)
	if err != nil {
		t.Fatalf("LoadLockfile() error = %v", err)
	}
	got, ok := lf.Packs["mypack"]
	if !ok {
		t.Fatal("expected mypack to be recorded in Packs")
	}
	if got.Version != "1.0.0" || got.ChecksumSHA256 != "abc123" {
		t.Errorf("Packs[mypack] = %+v, unexpected", got)
	}
	if got.InstalledAt.IsZero() {
		t.Error("expected a non-zero InstalledAt")
	}

	if err := RemovePack(dir, "mypack"); err != nil {
		t.Fatalf("RemovePack() error = %v", err)
	}
	lf, err = LoadLockfile(dir)
	if err != nil {
		t.Fatalf("LoadLockfile() error = %v", err)
	}
	if _, ok := lf.Packs["mypack"]; ok {
		t.Error("expected mypack to be removed from Packs")
	}
}

func TestRemovePackWhenNotPresent(t *testing.T) {
	dir := t.TempDir()
	if err := RemovePack(dir, "doesnotexist"); err != nil {
		t.Errorf("RemovePack() for an absent pack: error = %v, want nil", err)
	}
}

// TestUpdateLockfilePreservesPacks is a regression test for a real bug:
// UpdateLockfile used to build a brand-new Lockfile{Dependencies: ...}
// from scratch on every call, silently discarding any Packs section a
// prior RecordPack had written the moment the next 'cmaker build'/
// 'install'/'uninstall' ran UpdateLockfile again.
func TestUpdateLockfilePreservesPacks(t *testing.T) {
	dir := t.TempDir()
	if err := RecordPack(dir, "mypack", "1.0.0", "abc123"); err != nil {
		t.Fatalf("RecordPack() error = %v", err)
	}

	// No Dependencies - UpdateLockfile should short-circuit as a no-op
	// (see TestUpdateLockfileNoDependencies), leaving Packs untouched.
	if err := UpdateLockfile(dir, filepath.Join(dir, "build"), config.Config{}); err != nil {
		t.Fatalf("UpdateLockfile() error = %v", err)
	}
	lf, err := LoadLockfile(dir)
	if err != nil {
		t.Fatalf("LoadLockfile() error = %v", err)
	}
	if _, ok := lf.Packs["mypack"]; !ok {
		t.Error("UpdateLockfile() with no dependencies should not touch Packs, but mypack is gone")
	}

	// With a real (unfetched, so best-effort-failing) dependency, Packs
	// must still survive the regeneration of Dependencies.
	cfg := config.Config{Dependencies: []config.Dependency{{Name: "fmt", Repo: "fmtlib/fmt", Tag: "11.0.2"}}}
	_ = UpdateLockfile(dir, filepath.Join(dir, "build"), cfg) // error expected (unfetched) - only Packs preservation is under test
	lf, err = LoadLockfile(dir)
	if err != nil {
		t.Fatalf("LoadLockfile() error = %v", err)
	}
	if _, ok := lf.Packs["mypack"]; !ok {
		t.Error("UpdateLockfile() wiped out the Packs section while regenerating Dependencies")
	}
}

func TestDepSourceDirLowercasesName(t *testing.T) {
	got := depSourceDir("/build", "Catch2")
	want := filepath.Join("/build", "_deps", "catch2-src")
	if got != want {
		t.Errorf("depSourceDir(Catch2) = %q, want %q", got, want)
	}
}
