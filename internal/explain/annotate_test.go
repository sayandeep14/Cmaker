package explain

import (
	"strings"
	"testing"
)

func TestBuildAnnotatePromptNumbersLines(t *testing.T) {
	got := BuildAnnotatePrompt("int main() {\n    return 0;\n}")
	if !strings.Contains(got, "1: int main() {") {
		t.Errorf("BuildAnnotatePrompt() = %q, missing numbered line 1", got)
	}
	if !strings.Contains(got, "2:     return 0;") {
		t.Errorf("BuildAnnotatePrompt() = %q, missing numbered line 2", got)
	}
}

func TestParseAnnotations(t *testing.T) {
	got, err := ParseAnnotations(`[{"line": 2, "comment": "returns success"}]`)
	if err != nil {
		t.Fatalf("ParseAnnotations() error = %v", err)
	}
	if len(got) != 1 || got[0].Line != 2 || got[0].Comment != "returns success" {
		t.Errorf("ParseAnnotations() = %+v, unexpected", got)
	}
}

func TestParseAnnotationsStripsCodeFence(t *testing.T) {
	raw := "```json\n[{\"line\": 1, \"comment\": \"entry point\"}]\n```"
	got, err := ParseAnnotations(raw)
	if err != nil {
		t.Fatalf("ParseAnnotations() error = %v", err)
	}
	if len(got) != 1 || got[0].Comment != "entry point" {
		t.Errorf("ParseAnnotations() = %+v, unexpected", got)
	}
}

func TestParseAnnotationsToleratesTrailingProse(t *testing.T) {
	raw := `[{"line": 1, "comment": "x"}]` + "\n\nThis code looks straightforward overall."
	got, err := ParseAnnotations(raw)
	if err != nil {
		t.Fatalf("ParseAnnotations() error = %v", err)
	}
	if len(got) != 1 {
		t.Errorf("ParseAnnotations() = %+v, want 1 annotation", got)
	}
}

func TestParseAnnotationsEmpty(t *testing.T) {
	got, err := ParseAnnotations("[]")
	if err != nil {
		t.Fatalf("ParseAnnotations() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ParseAnnotations() = %+v, want empty", got)
	}
}

func TestParseAnnotationsMalformed(t *testing.T) {
	if _, err := ParseAnnotations("not json"); err == nil {
		t.Error("ParseAnnotations() with malformed input: expected an error, got nil")
	}
}

func TestInsertAnnotationsCodeUnchanged(t *testing.T) {
	code := "int main() {\n    return 0;\n}"
	annotations := []Annotation{{Line: 2, Comment: "return success"}}
	got := InsertAnnotations(code, annotations)

	// The exact original code lines must all still be present, verbatim,
	// unmodified - this is the whole safety property InsertAnnotations
	// exists to guarantee.
	for line := range strings.SplitSeq(code, "\n") {
		if !strings.Contains(got, line) {
			t.Errorf("InsertAnnotations() = %q, missing original line %q", got, line)
		}
	}
	if !strings.Contains(got, "// return success") {
		t.Errorf("InsertAnnotations() = %q, missing the inserted comment", got)
	}
}

func TestInsertAnnotationsMatchesIndentation(t *testing.T) {
	code := "int main() {\n    return 0;\n}"
	got := InsertAnnotations(code, []Annotation{{Line: 2, Comment: "return success"}})
	if !strings.Contains(got, "    // return success\n    return 0;") {
		t.Errorf("InsertAnnotations() = %q, comment should be indented to match line 2", got)
	}
}

func TestInsertAnnotationsMultipleOnSameLine(t *testing.T) {
	code := "x();"
	got := InsertAnnotations(code, []Annotation{
		{Line: 1, Comment: "first"},
		{Line: 1, Comment: "second"},
	})
	if !strings.Contains(got, "// first") || !strings.Contains(got, "// second") {
		t.Errorf("InsertAnnotations() = %q, expected both comments", got)
	}
}

func TestInsertAnnotationsOutOfRangeIgnored(t *testing.T) {
	code := "int main() { return 0; }"
	got := InsertAnnotations(code, []Annotation{{Line: 99, Comment: "unreachable"}})
	if got != code {
		t.Errorf("InsertAnnotations() = %q, want unchanged for an out-of-range line", got)
	}
}

func TestInsertAnnotationsEmptyCommentIgnored(t *testing.T) {
	code := "int main() { return 0; }"
	got := InsertAnnotations(code, []Annotation{{Line: 1, Comment: "   "}})
	if got != code {
		t.Errorf("InsertAnnotations() = %q, want unchanged for a blank comment", got)
	}
}

func TestInsertAnnotationsNoAnnotations(t *testing.T) {
	code := "int main() { return 0; }"
	if got := InsertAnnotations(code, nil); got != code {
		t.Errorf("InsertAnnotations() = %q, want unchanged for no annotations", got)
	}
}
