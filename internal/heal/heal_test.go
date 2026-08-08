package heal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSuggestReturnsComputedDiff(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "src", "main.cpp"), "int main() { foo(); }\n")
	logPath := filepath.Join(dir, "build.log")
	writeFile(t, logPath, "src/main.cpp:1:15: error: use of undeclared identifier 'foo'\n")

	fc := &fakeCompleter{response: "--- file: src/main.cpp ---\nint main() { return 0; }\n"}

	got, err := Suggest(context.Background(), fc, dir, logPath)
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if !strings.Contains(got.Diff, "-int main() { foo(); }") || !strings.Contains(got.Diff, "+int main() { return 0; }") {
		t.Errorf("Suggest().Diff = %q, missing expected +/- lines", got.Diff)
	}
	if !strings.HasPrefix(got.Diff, "--- a/src/main.cpp\n+++ b/src/main.cpp\n@@") {
		t.Errorf("Suggest().Diff = %q, doesn't look like a unified diff header", got.Diff)
	}
	if len(got.ReferencedFiles) != 1 || got.ReferencedFiles[0] != "src/main.cpp" {
		t.Errorf("Suggest().ReferencedFiles = %v, want [src/main.cpp]", got.ReferencedFiles)
	}
	if !strings.Contains(fc.gotUser, "foo();") {
		t.Errorf("expected the referenced file's content in the prompt, got: %q", fc.gotUser)
	}
	if !strings.Contains(fc.gotUser, "error: use of undeclared identifier") {
		t.Errorf("expected the log content in the prompt, got: %q", fc.gotUser)
	}
}

func TestSuggestHandlesAbsolutePathInLog(t *testing.T) {
	// Compilers often report absolute paths (e.g. CMake invoking them with
	// an absolute source dir) - filepath.Join(root, absolutePath) silently
	// mangles that into a bogus relative path, which previously made
	// Suggest() report "none of them exist" even though the file was
	// right there. Regression test for that real bug, caught via a live
	// end-to-end run against an actual failing build.
	dir := t.TempDir()
	absSrc := filepath.Join(dir, "src", "main.cpp")
	writeFile(t, absSrc, "int main() { foo(); }\n")
	logPath := filepath.Join(dir, "build.log")
	writeFile(t, logPath, absSrc+":1:15: error: use of undeclared identifier 'foo'\n")

	// The diff header/response contract uses a path relative to root, not
	// the raw absolute path the compiler reported - git apply rejects an
	// absolute path in a diff header outright ("invalid path"), caught via
	// a live end-to-end test against a real failing build.
	fc := &fakeCompleter{response: "--- file: src/main.cpp ---\nint main() { return 0; }\n"}
	got, err := Suggest(context.Background(), fc, dir, logPath)
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if len(got.ReferencedFiles) != 1 || got.ReferencedFiles[0] != "src/main.cpp" {
		t.Errorf("Suggest().ReferencedFiles = %v, want [src/main.cpp] (relative to root)", got.ReferencedFiles)
	}
	if !strings.Contains(fc.gotUser, "--- file: src/main.cpp ---") {
		t.Errorf("expected the file block label to use a root-relative path, not the raw absolute one, got: %q", fc.gotUser)
	}
	if !strings.Contains(fc.gotUser, "foo();") {
		t.Errorf("expected the absolute-path file's content in the prompt, got: %q", fc.gotUser)
	}
	if got.Diff == "" {
		t.Error("expected a non-empty diff")
	}
	if !strings.HasPrefix(got.Diff, "--- a/src/main.cpp") {
		t.Errorf("Suggest().Diff = %q, want a relative-path diff header (git apply rejects absolute ones)", got.Diff)
	}
}

func TestSuggestStripsStrayCodeFenceFromFileBlock(t *testing.T) {
	// Real, observed model behavior: wrapping a file block's content in a
	// markdown fence despite the prompt saying not to (the same lesson
	// already learned for the JSON-returning generate-accessors feature
	// and the original diff-returning heal prompt).
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "src", "main.cpp"), "int main() { foo(); }\n")
	logPath := filepath.Join(dir, "build.log")
	writeFile(t, logPath, "src/main.cpp:1:15: error: use of undeclared identifier 'foo'\n")

	fc := &fakeCompleter{response: "--- file: src/main.cpp ---\n```cpp\nint main() { return 0; }\n```\n"}
	got, err := Suggest(context.Background(), fc, dir, logPath)
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if strings.Contains(got.Diff, "```") {
		t.Errorf("Suggest().Diff still contains a code fence: %q", got.Diff)
	}
	if !strings.Contains(got.Diff, "+int main() { return 0; }") {
		t.Errorf("Suggest().Diff = %q, missing the fence-stripped content", got.Diff)
	}
}

func TestSuggestStripsStrayTrailingSeparator(t *testing.T) {
	// Real, observed model behavior from a live end-to-end run: the model's
	// corrected file content ended in a stray bare "---" line (echoing the
	// prompt's own "--- file: <path> ---" delimiter style out of habit) -
	// since it's the last (and only) block, nothing bounds where its
	// content should end, so this line would otherwise silently become
	// part of the "fixed" file. That patch applied to git perfectly
	// cleanly and still broke compilation, which is exactly why this needs
	// its own defense beyond "does git apply accept it."
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "src", "main.cpp"), "int main() { foo(); }\n")
	logPath := filepath.Join(dir, "build.log")
	writeFile(t, logPath, "src/main.cpp:1:15: error: use of undeclared identifier 'foo'\n")

	fc := &fakeCompleter{response: "--- file: src/main.cpp ---\nint main() { return 0; }\n---"}
	got, err := Suggest(context.Background(), fc, dir, logPath)
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if strings.Contains(got.Diff, "+---") {
		t.Errorf("Suggest().Diff still contains the stray trailing separator: %q", got.Diff)
	}
	if !strings.Contains(got.Diff, "+int main() { return 0; }") {
		t.Errorf("Suggest().Diff = %q, missing the real fix", got.Diff)
	}
}

func TestSuggestStripsStrayFileBlockMarker(t *testing.T) {
	// Real, observed model behavior from a live end-to-end 'cmaker heal
	// --apply' run: the model closed its response with "--- file: end ---",
	// imitating its own required "--- file: <path> ---" delimiter syntax as
	// an ad hoc (and never requested) "no more files" marker. Since it has
	// no trailing newline in the raw response, fileBlockRe (anchored on
	// "\n") never matches it as a genuine new block header, so without this
	// defense it silently becomes trailing content of the last real block -
	// corrupting the applied fix with a bogus extra line.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "src", "main.cpp"), "int main() { foo(); }\n")
	logPath := filepath.Join(dir, "build.log")
	writeFile(t, logPath, "src/main.cpp:1:15: error: use of undeclared identifier 'foo'\n")

	fc := &fakeCompleter{response: "--- file: src/main.cpp ---\nint main() { return 0; }\n--- file: end ---"}
	got, err := Suggest(context.Background(), fc, dir, logPath)
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if strings.Contains(got.Diff, "file: end") {
		t.Errorf("Suggest().Diff still contains the stray trailing file-block marker: %q", got.Diff)
	}
	if !strings.Contains(got.Diff, "+int main() { return 0; }") {
		t.Errorf("Suggest().Diff = %q, missing the real fix", got.Diff)
	}
}

func TestSuggestNoFixFound(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "src", "main.cpp"), "int main() {}\n")
	logPath := filepath.Join(dir, "build.log")
	writeFile(t, logPath, "src/main.cpp:1:1: error: something inscrutable\n")

	fc := &fakeCompleter{response: "NO_FIX_FOUND"}
	got, err := Suggest(context.Background(), fc, dir, logPath)
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if got.Diff != "" {
		t.Errorf("Suggest().Diff = %q, want empty for NO_FIX_FOUND", got.Diff)
	}
}

func TestSuggestUnrecognizedResponseFormat(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "src", "main.cpp"), "int main() {}\n")
	logPath := filepath.Join(dir, "build.log")
	writeFile(t, logPath, "src/main.cpp:1:1: error: whatever\n")

	fc := &fakeCompleter{response: "I think the fix is to add a semicolon."}
	if _, err := Suggest(context.Background(), fc, dir, logPath); err == nil {
		t.Error("expected an error when the response has no recognizable file blocks")
	}
}

func TestSuggestProposedContentIdenticalToOriginal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "src", "main.cpp"), "int main() {}\n")
	logPath := filepath.Join(dir, "build.log")
	writeFile(t, logPath, "src/main.cpp:1:1: error: whatever\n")

	fc := &fakeCompleter{response: "--- file: src/main.cpp ---\nint main() {}\n"}
	got, err := Suggest(context.Background(), fc, dir, logPath)
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if got.Diff != "" {
		t.Errorf("Suggest().Diff = %q, want empty when proposed content == original", got.Diff)
	}
}

func TestSuggestNoFileReferencesInLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "build.log")
	writeFile(t, logPath, "some generic failure with no file:line references\n")

	fc := &fakeCompleter{response: "should not be called"}
	if _, err := Suggest(context.Background(), fc, dir, logPath); err == nil {
		t.Error("expected an error when the log has no file:line references")
	}
}

func TestSuggestReferencedFileMissingOnDisk(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "build.log")
	writeFile(t, logPath, "src/gone.cpp:1:1: error: whatever\n")

	fc := &fakeCompleter{response: "--- file: src/gone.cpp ---\nsomething\n"}
	if _, err := Suggest(context.Background(), fc, dir, logPath); err == nil {
		t.Error("expected an error when none of the referenced files exist on disk")
	}
}

func TestSuggestMissingLogFile(t *testing.T) {
	fc := &fakeCompleter{}
	if _, err := Suggest(context.Background(), fc, t.TempDir(), "/nonexistent/log.log"); err == nil {
		t.Error("expected an error for a missing log file")
	}
}
