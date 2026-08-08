package explain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"cmaker/internal/logs"
)

type fakeCompleter struct {
	response string
	err      error

	gotSystem, gotUser string
}

func (f *fakeCompleter) Complete(ctx context.Context, system, user string) (string, error) {
	f.gotSystem, f.gotUser = system, user
	return f.response, f.err
}

func TestAsk(t *testing.T) {
	fc := &fakeCompleter{response: "  this class represents a widget  \n"}
	got, err := Ask(context.Background(), fc, "explain Widget")
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if got != "this class represents a widget" {
		t.Errorf("Ask() = %q, want trimmed response", got)
	}
	if fc.gotUser != "explain Widget" {
		t.Errorf("Ask() sent user = %q, want %q", fc.gotUser, "explain Widget")
	}
	if fc.gotSystem == "" {
		t.Error("Ask() sent an empty system prompt")
	}
}

func TestAskPropagatesError(t *testing.T) {
	fc := &fakeCompleter{err: os.ErrClosed}
	if _, err := Ask(context.Background(), fc, "x"); err == nil {
		t.Error("Ask() expected an error, got nil")
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestWalkSourceFilesSkipsBuildAndGit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.cpp", "int main() {}\n")
	writeFile(t, root, "include/lib.hpp", "#pragma once\n")
	writeFile(t, root, "src/notes.txt", "not source\n")
	writeFile(t, root, "build/generated.cpp", "// should be skipped\n")
	writeFile(t, root, ".git/hooks/dummy.c", "// should be skipped\n")

	got, err := WalkSourceFiles(root)
	if err != nil {
		t.Fatalf("WalkSourceFiles() error = %v", err)
	}
	want := map[string]bool{
		filepath.Join("src", "main.cpp"):    true,
		filepath.Join("include", "lib.hpp"): true,
	}
	if len(got) != len(want) {
		t.Fatalf("WalkSourceFiles() = %v, want exactly %v", got, want)
	}
	for _, f := range got {
		if !want[f] {
			t.Errorf("WalkSourceFiles() unexpectedly included %q", f)
		}
	}
}

func TestFindClassSingleMatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/widget.hpp", "class Widget {\npublic:\n    void draw();\n};\n")

	matches, err := FindClass(root, "Widget")
	if err != nil {
		t.Fatalf("FindClass() error = %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("FindClass() = %d matches, want 1", len(matches))
	}
	if matches[0].File != filepath.Join("src", "widget.hpp") {
		t.Errorf("FindClass() file = %q, unexpected", matches[0].File)
	}
	if !strings.Contains(matches[0].Body, "void draw();") {
		t.Errorf("FindClass() body = %q, missing expected content", matches[0].Body)
	}
}

func TestFindClassAmbiguousMultipleFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/a/widget.hpp", "class Widget { int a; };\n")
	writeFile(t, root, "src/b/widget.hpp", "class Widget { int b; };\n")

	matches, err := FindClass(root, "Widget")
	if err != nil {
		t.Fatalf("FindClass() error = %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("FindClass() = %d matches, want 2 (ambiguous)", len(matches))
	}
}

func TestFindClassNotFound(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.cpp", "int main() { return 0; }\n")

	matches, err := FindClass(root, "Nonexistent")
	if err != nil {
		t.Fatalf("FindClass() error = %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("FindClass() = %v, want none", matches)
	}
}

func TestFindFunctionSingleMatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/math.cpp", "int add(int a, int b) {\n    return a + b;\n}\n")

	matches, err := FindFunction(root, "add")
	if err != nil {
		t.Fatalf("FindFunction() error = %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("FindFunction() = %d matches, want 1", len(matches))
	}
	if !strings.Contains(matches[0].Body, "return a + b;") {
		t.Errorf("FindFunction() body = %q, missing expected content", matches[0].Body)
	}
}

func TestFindFunctionAmbiguousMultipleFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/a.cpp", "void run() { }\n")
	writeFile(t, root, "src/b.cpp", "void run() { }\n")

	matches, err := FindFunction(root, "run")
	if err != nil {
		t.Fatalf("FindFunction() error = %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("FindFunction() = %d matches, want 2 (ambiguous)", len(matches))
	}
}

func TestBuildFilePrompt(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.cpp", "int main() { return 0; }\n")

	got, err := BuildFilePrompt(root, filepath.Join("src", "main.cpp"))
	if err != nil {
		t.Fatalf("BuildFilePrompt() error = %v", err)
	}
	if !strings.Contains(got, "int main() { return 0; }") {
		t.Errorf("BuildFilePrompt() = %q, missing file content", got)
	}
}

func TestBuildFilePromptMissingFile(t *testing.T) {
	root := t.TempDir()
	if _, err := BuildFilePrompt(root, "missing.cpp"); err == nil {
		t.Error("BuildFilePrompt() expected an error for a missing file, got nil")
	}
}

func TestBuildConfigPrompt(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "cmaker.yaml", "project_name: demo\nexecutable: demo\n")
	writeFile(t, root, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.14)\n")

	got, err := BuildConfigPrompt(root)
	if err != nil {
		t.Fatalf("BuildConfigPrompt() error = %v", err)
	}
	if !strings.Contains(got, "project_name: demo") || !strings.Contains(got, "cmake_minimum_required") {
		t.Errorf("BuildConfigPrompt() = %q, missing expected content", got)
	}
}

func TestBuildConfigPromptMissingYaml(t *testing.T) {
	root := t.TempDir()
	if _, err := BuildConfigPrompt(root); err == nil {
		t.Error("BuildConfigPrompt() expected an error when cmaker.yaml is missing, got nil")
	}
}

func TestBuildConfigPromptToleratesMissingCMakeLists(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "cmaker.yaml", "project_name: demo\n")

	got, err := BuildConfigPrompt(root)
	if err != nil {
		t.Fatalf("BuildConfigPrompt() error = %v", err)
	}
	if !strings.Contains(got, "project_name: demo") {
		t.Errorf("BuildConfigPrompt() = %q, missing yaml content", got)
	}
}

func TestBuildDependencyPromptNotInRegistry(t *testing.T) {
	root := t.TempDir()
	if _, err := BuildDependencyPrompt(root, "definitely-not-a-real-registry-entry"); err == nil {
		t.Error("BuildDependencyPrompt() expected an error for an unregistered dependency, got nil")
	}
}

func TestBuildDependencyPromptFindsUsage(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.cpp", "#include <fmt/core.h>\nint main() { return 0; }\n")

	got, err := BuildDependencyPrompt(root, "fmt")
	if err != nil {
		t.Fatalf("BuildDependencyPrompt() error = %v", err)
	}
	if !strings.Contains(got, "fmt/core.h") {
		t.Errorf("BuildDependencyPrompt() = %q, missing found usage", got)
	}
}

func TestBuildDiffPromptNoChanges(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--allow-empty", "-m", "init")

	got, err := BuildDiffPrompt(root)
	if err != nil {
		t.Fatalf("BuildDiffPrompt() error = %v", err)
	}
	if got != "" {
		t.Errorf("BuildDiffPrompt() = %q, want empty for a clean working tree", got)
	}
}

func TestBuildDiffPromptWithChanges(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	writeFile(t, root, "main.cpp", "int main() { return 0; }\n")
	runGit(t, root, "add", "main.cpp")
	runGit(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "-m", "init")

	writeFile(t, root, "main.cpp", "int main() { return 1; }\n")

	got, err := BuildDiffPrompt(root)
	if err != nil {
		t.Fatalf("BuildDiffPrompt() error = %v", err)
	}
	if !strings.Contains(got, "return 1;") {
		t.Errorf("BuildDiffPrompt() = %q, missing the actual diff content", got)
	}
}

func TestBuildLastErrorPromptNoFailures(t *testing.T) {
	root := t.TempDir()
	if _, err := BuildLastErrorPrompt(root); err == nil {
		t.Error("BuildLastErrorPrompt() expected an error when no failing logs exist, got nil")
	}
}

func TestBuildLastErrorPromptIncludesReferencedFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.cpp", "int main() { return undeclared; }\n")

	s, err := logs.Start(root, "build", 5)
	if err != nil {
		t.Fatalf("logs.Start() error = %v", err)
	}
	w := s.Tee(os.Stdout)
	w.Write([]byte("src/main.cpp:1:23: error: use of undeclared identifier 'undeclared'\n"))
	s.Finish(os.ErrClosed) // any non-nil error marks the log as a failure

	got, err := BuildLastErrorPrompt(root)
	if err != nil {
		t.Fatalf("BuildLastErrorPrompt() error = %v", err)
	}
	if !strings.Contains(got, "undeclared identifier") {
		t.Errorf("BuildLastErrorPrompt() = %q, missing the log content", got)
	}
	if !strings.Contains(got, "int main() { return undeclared; }") {
		t.Errorf("BuildLastErrorPrompt() = %q, missing the referenced file's content", got)
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
