package cmd

import (
	"path/filepath"
	"sort"
	"testing"
)

func TestSourceFilesCollectsTrackedExtensions(t *testing.T) {
	dir := t.TempDir()
	writeFileAt(t, filepath.Join(dir, "src", "main.cpp"), "")
	writeFileAt(t, filepath.Join(dir, "src", "helper.cxx"), "")
	writeFileAt(t, filepath.Join(dir, "src", "clib.c"), "")
	writeFileAt(t, filepath.Join(dir, "include", "mylib", "mylib.h"), "")
	writeFileAt(t, filepath.Join(dir, "include", "mylib", "extra.hpp"), "")
	writeFileAt(t, filepath.Join(dir, "cmaker.yaml"), "")        // not a source extension
	writeFileAt(t, filepath.Join(dir, "README.md"), "")          // not a source extension
	writeFileAt(t, filepath.Join(dir, "build", "stale.cpp"), "") // must be skipped
	writeFileAt(t, filepath.Join(dir, ".git", "HEAD"), "")

	files, err := sourceFiles(dir)
	if err != nil {
		t.Fatalf("sourceFiles() error = %v", err)
	}

	var got []string
	for _, f := range files {
		got = append(got, filepath.Base(f))
	}
	sort.Strings(got)

	want := []string{"clib.c", "extra.hpp", "helper.cxx", "main.cpp", "mylib.h"}
	if len(got) != len(want) {
		t.Fatalf("sourceFiles() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sourceFiles()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSourceFilesEmptyProject(t *testing.T) {
	files, err := sourceFiles(t.TempDir())
	if err != nil {
		t.Fatalf("sourceFiles() error = %v", err)
	}
	if len(files) != 0 {
		t.Errorf("sourceFiles() on an empty dir = %v, want empty", files)
	}
}
