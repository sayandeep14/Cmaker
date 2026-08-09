package heal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSuggestUsesExtraFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "src", "main.cpp"), "int main() { return widget_value(); }\n")
	writeFile(t, filepath.Join(dir, "src", "widget.hpp"), "int widget_value() { return 0; }\n")
	logPath := filepath.Join(dir, "build.log")
	writeFile(t, logPath, "src/main.cpp:1:22: error: use of undeclared identifier 'widget_value'\n")

	fc := &fakeCompleter{response: "--- file: src/widget.hpp ---\nint widget_value() { return 1; }\n"}

	got, err := Suggest(context.Background(), fc, dir, logPath, []string{filepath.Join("src", "widget.hpp")})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if !strings.Contains(fc.gotUser, "widget_value() { return 0; }") {
		t.Errorf("Suggest() prompt = %q, missing the extra file's content", fc.gotUser)
	}
	if len(got.ReferencedFiles) != 2 {
		t.Errorf("Suggest() ReferencedFiles = %v, want both the log-referenced file and the extra one", got.ReferencedFiles)
	}
}

func TestSuggestExtraFilesDedupedAgainstReferenced(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "src", "main.cpp"), "int main() { foo(); }\n")
	logPath := filepath.Join(dir, "build.log")
	writeFile(t, logPath, "src/main.cpp:1:15: error: use of undeclared identifier 'foo'\n")

	fc := &fakeCompleter{response: "--- file: src/main.cpp ---\nint main() { return 0; }\n"}

	got, err := Suggest(context.Background(), fc, dir, logPath, []string{filepath.Join("src", "main.cpp")})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if len(got.ReferencedFiles) != 1 {
		t.Errorf("Suggest() ReferencedFiles = %v, want the duplicate deduped away", got.ReferencedFiles)
	}
}

func TestSuggestRelatedFiles(t *testing.T) {
	fc := &fakeCompleter{response: `["src/widget.hpp", "src/other.hpp"]`}
	got, err := SuggestRelatedFiles(context.Background(), fc, "some log", map[string]string{"src/main.cpp": "code"}, []string{"src/widget.hpp", "src/other.hpp", "src/unrelated.hpp"})
	if err != nil {
		t.Fatalf("SuggestRelatedFiles() error = %v", err)
	}
	if len(got) != 2 || got[0] != "src/widget.hpp" || got[1] != "src/other.hpp" {
		t.Errorf("SuggestRelatedFiles() = %v, unexpected", got)
	}
}

func TestSuggestRelatedFilesRejectsPathsNotInCandidates(t *testing.T) {
	// The model naming a path it wasn't offered must never be trusted -
	// Suggest would have no way to read it against the real project
	// anyway, and it's not something the model was told exists.
	fc := &fakeCompleter{response: `["src/widget.hpp", "src/invented-file-not-offered.hpp"]`}
	got, err := SuggestRelatedFiles(context.Background(), fc, "log", nil, []string{"src/widget.hpp"})
	if err != nil {
		t.Fatalf("SuggestRelatedFiles() error = %v", err)
	}
	if len(got) != 1 || got[0] != "src/widget.hpp" {
		t.Errorf("SuggestRelatedFiles() = %v, want only the offered candidate", got)
	}
}

func TestSuggestRelatedFilesCapsAtMax(t *testing.T) {
	candidates := make([]string, 0, maxRelatedFiles+5)
	for i := 0; i < maxRelatedFiles+5; i++ {
		candidates = append(candidates, filepath.Join("src", "f"+string(rune('a'+i))+".hpp"))
	}
	var picked strings.Builder
	picked.WriteString("[")
	for i, c := range candidates {
		if i > 0 {
			picked.WriteString(",")
		}
		picked.WriteString(`"` + c + `"`)
	}
	picked.WriteString("]")

	fc := &fakeCompleter{response: picked.String()}
	got, err := SuggestRelatedFiles(context.Background(), fc, "log", nil, candidates)
	if err != nil {
		t.Fatalf("SuggestRelatedFiles() error = %v", err)
	}
	if len(got) != maxRelatedFiles {
		t.Errorf("SuggestRelatedFiles() returned %d files, want capped at %d", len(got), maxRelatedFiles)
	}
}

func TestSuggestRelatedFilesEmptyCandidates(t *testing.T) {
	got, err := SuggestRelatedFiles(context.Background(), &fakeCompleter{}, "log", nil, nil)
	if err != nil {
		t.Fatalf("SuggestRelatedFiles() error = %v", err)
	}
	if got != nil {
		t.Errorf("SuggestRelatedFiles() = %v, want nil with no candidates (and no LLM call made)", got)
	}
}

func TestSuggestRelatedFilesStripsCodeFence(t *testing.T) {
	fc := &fakeCompleter{response: "```json\n[\"src/widget.hpp\"]\n```"}
	got, err := SuggestRelatedFiles(context.Background(), fc, "log", nil, []string{"src/widget.hpp"})
	if err != nil {
		t.Fatalf("SuggestRelatedFiles() error = %v", err)
	}
	if len(got) != 1 || got[0] != "src/widget.hpp" {
		t.Errorf("SuggestRelatedFiles() = %v, unexpected", got)
	}
}

func TestSuggestRelatedFilesNoneHelpful(t *testing.T) {
	fc := &fakeCompleter{response: "[]"}
	got, err := SuggestRelatedFiles(context.Background(), fc, "log", nil, []string{"src/widget.hpp"})
	if err != nil {
		t.Fatalf("SuggestRelatedFiles() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("SuggestRelatedFiles() = %v, want empty", got)
	}
}

func TestSuggestRelatedFilesMalformed(t *testing.T) {
	fc := &fakeCompleter{response: "not json"}
	if _, err := SuggestRelatedFiles(context.Background(), fc, "log", nil, []string{"src/widget.hpp"}); err == nil {
		t.Error("SuggestRelatedFiles() with malformed input: expected an error, got nil")
	}
}

func TestSuggestRelatedFilesPropagatesCompleterError(t *testing.T) {
	fc := &fakeCompleter{err: os.ErrClosed}
	if _, err := SuggestRelatedFiles(context.Background(), fc, "log", nil, []string{"src/widget.hpp"}); err == nil {
		t.Error("SuggestRelatedFiles() expected an error when the completer fails, got nil")
	}
}
