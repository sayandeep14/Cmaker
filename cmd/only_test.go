package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func writeOnlyTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("int main() { return 0; }\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveOnlyFilesLiteralPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeOnlyTestFile(t, "scratch.cpp")

	got, err := resolveOnlyFiles("scratch.cpp")
	if err != nil {
		t.Fatalf("resolveOnlyFiles() error = %v", err)
	}
	if len(got) != 1 || got[0] != "scratch.cpp" {
		t.Errorf("resolveOnlyFiles(\"scratch.cpp\") = %v, want [scratch.cpp]", got)
	}
}

func TestResolveOnlyFilesLiteralPathMissing(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if _, err := resolveOnlyFiles("does-not-exist.cpp"); err == nil {
		t.Error("resolveOnlyFiles() for a missing literal path: expected an error, got nil")
	}
}

func TestResolveOnlyFilesGlobMatchesSortedInOrder(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeOnlyTestFile(t, filepath.Join("tests", "b_test.cpp"))
	writeOnlyTestFile(t, filepath.Join("tests", "a_test.cpp"))
	writeOnlyTestFile(t, filepath.Join("tests", "c_test.cpp"))
	// Must not be matched by the tests/*.cpp glob below.
	writeOnlyTestFile(t, filepath.Join("tests", "readme.txt"))

	got, err := resolveOnlyFiles(filepath.Join("tests", "*.cpp"))
	if err != nil {
		t.Fatalf("resolveOnlyFiles() error = %v", err)
	}
	want := []string{
		filepath.Join("tests", "a_test.cpp"),
		filepath.Join("tests", "b_test.cpp"),
		filepath.Join("tests", "c_test.cpp"),
	}
	if len(got) != len(want) {
		t.Fatalf("resolveOnlyFiles(tests/*.cpp) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("resolveOnlyFiles(tests/*.cpp)[%d] = %q, want %q (sorted order)", i, got[i], want[i])
		}
	}
}

func TestResolveOnlyFilesGlobNoMatches(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll("tests", 0755); err != nil {
		t.Fatal(err)
	}

	if _, err := resolveOnlyFiles(filepath.Join("tests", "*.cpp")); err == nil {
		t.Error("resolveOnlyFiles() for a glob with no matches: expected an error, got nil")
	}
}

func TestReportOnlyGlobResultAllPassed(t *testing.T) {
	if err := reportOnlyGlobResult(3, nil); err != nil {
		t.Errorf("reportOnlyGlobResult(3, nil) error = %v, want nil", err)
	}
}
