package suggest

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

	gotUser string
}

func (f *fakeCompleter) Complete(ctx context.Context, system, user string) (string, error) {
	f.gotUser = user
	return f.response, f.err
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

func TestBuildPromptIncludesConfigAndSource(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "cmaker.yaml", "project_name: demo\n")
	writeFile(t, root, "src/main.cpp", "int main() { return 0; }\n")

	got, err := BuildPrompt(root)
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if !strings.Contains(got, "project_name: demo") {
		t.Errorf("BuildPrompt() = %q, missing cmaker.yaml content", got)
	}
	if !strings.Contains(got, "int main() { return 0; }") {
		t.Errorf("BuildPrompt() = %q, missing source content", got)
	}
}

func TestBuildPromptNoSourceFiles(t *testing.T) {
	root := t.TempDir()
	if _, err := BuildPrompt(root); err == nil {
		t.Error("BuildPrompt() with no source files: expected an error, got nil")
	}
}

func TestBuildPromptSkipsFilesTooBigForBudget(t *testing.T) {
	root := t.TempDir()
	// A file larger than the whole budget should be skipped entirely,
	// not truncated mid-file - a smaller file should still make it in.
	writeFile(t, root, "src/huge.cpp", strings.Repeat("x", maxProjectChars+1000))
	writeFile(t, root, "src/small.cpp", "int small() { return 1; }\n")

	got, err := BuildPrompt(root)
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if strings.Contains(got, strings.Repeat("x", 100)) {
		t.Error("BuildPrompt() included a file larger than the whole budget")
	}
	if !strings.Contains(got, "int small() { return 1; }") {
		t.Error("BuildPrompt() should still include a file that fits")
	}
}

func TestSuggestReturnsSuggestions(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.cpp", "int main() { return 0; }\n")

	fc := &fakeCompleter{response: `[{"title": "Add error handling", "detail": "main() ignores possible failures"}]`}
	got, err := Suggest(context.Background(), fc, root)
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if len(got) != 1 || got[0].Title != "Add error handling" {
		t.Errorf("Suggest() = %+v, unexpected", got)
	}
}

func TestParseSuggestionsStripsCodeFence(t *testing.T) {
	raw := "```json\n[{\"title\": \"x\", \"detail\": \"y\"}]\n```"
	got, err := ParseSuggestions(raw)
	if err != nil {
		t.Fatalf("ParseSuggestions() error = %v", err)
	}
	if len(got) != 1 || got[0].Title != "x" {
		t.Errorf("ParseSuggestions() = %+v, unexpected", got)
	}
}

func TestParseSuggestionsToleratesTrailingProse(t *testing.T) {
	raw := `[{"title": "x", "detail": "y"}]` + "\n\nHope this helps!"
	got, err := ParseSuggestions(raw)
	if err != nil {
		t.Fatalf("ParseSuggestions() error = %v", err)
	}
	if len(got) != 1 {
		t.Errorf("ParseSuggestions() = %+v, want 1", got)
	}
}

func TestParseSuggestionsEmpty(t *testing.T) {
	got, err := ParseSuggestions("[]")
	if err != nil {
		t.Fatalf("ParseSuggestions() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ParseSuggestions() = %+v, want empty", got)
	}
}

func TestParseSuggestionsMalformed(t *testing.T) {
	if _, err := ParseSuggestions("not json"); err == nil {
		t.Error("ParseSuggestions() with malformed input: expected an error, got nil")
	}
}

func TestRenderChecklist(t *testing.T) {
	got := RenderChecklist([]Suggestion{
		{Title: "Use RAII for the file handle", Detail: "src/io.cpp:42 leaks a FILE* on the error path"},
		{Title: "No detail item"},
	})
	if !strings.Contains(got, "- [ ] Use RAII for the file handle") {
		t.Errorf("RenderChecklist() = %q, missing checklist item", got)
	}
	if !strings.Contains(got, "src/io.cpp:42 leaks a FILE* on the error path") {
		t.Errorf("RenderChecklist() = %q, missing detail text", got)
	}
	if !strings.Contains(got, "- [ ] No detail item") {
		t.Errorf("RenderChecklist() = %q, missing item with no detail", got)
	}
}

func TestRenderChecklistEmpty(t *testing.T) {
	got := RenderChecklist(nil)
	if !strings.Contains(got, "# cmaker suggest") {
		t.Errorf("RenderChecklist() = %q, want at least a header", got)
	}
}
