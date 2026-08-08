package improvise

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

func TestClarifyProceeds(t *testing.T) {
	fc := &fakeCompleter{response: `{"proceed": true, "questions": []}`}
	proceed, questions, err := Clarify(context.Background(), fc, "a REST API backend with JSON support")
	if err != nil {
		t.Fatalf("Clarify() error = %v", err)
	}
	if !proceed {
		t.Error("Clarify() proceed = false, want true")
	}
	if len(questions) != 0 {
		t.Errorf("Clarify() questions = %v, want none when proceed=true", questions)
	}
}

func TestClarifyAsksQuestions(t *testing.T) {
	fc := &fakeCompleter{response: `{
		"proceed": false,
		"questions": [
			{"prompt": "Which web framework?", "type": "single_select", "options": ["crow", "oatpp", "drogon"]},
			{"prompt": "Any extra notes?", "type": "free_text"}
		]
	}`}
	proceed, questions, err := Clarify(context.Background(), fc, "a backend service")
	if err != nil {
		t.Fatalf("Clarify() error = %v", err)
	}
	if proceed {
		t.Error("Clarify() proceed = true, want false")
	}
	if len(questions) != 2 {
		t.Fatalf("Clarify() questions = %v, want 2", questions)
	}
	if questions[0].Type != QuestionSingleSelect || len(questions[0].Options) != 3 {
		t.Errorf("questions[0] = %+v, unexpected", questions[0])
	}
	if questions[1].Type != QuestionFreeText {
		t.Errorf("questions[1] = %+v, want free_text", questions[1])
	}
}

func TestClarifyCapsQuestionsAtMax(t *testing.T) {
	fc := &fakeCompleter{response: `{
		"proceed": false,
		"questions": [
			{"prompt": "q1", "type": "free_text"},
			{"prompt": "q2", "type": "free_text"},
			{"prompt": "q3", "type": "free_text"},
			{"prompt": "q4", "type": "free_text"},
			{"prompt": "q5", "type": "free_text"},
			{"prompt": "q6", "type": "free_text"}
		]
	}`}
	_, questions, err := Clarify(context.Background(), fc, "something vague")
	if err != nil {
		t.Fatalf("Clarify() error = %v", err)
	}
	if len(questions) != maxQuestions {
		t.Errorf("Clarify() returned %d questions, want capped at %d", len(questions), maxQuestions)
	}
}

func TestClarifyUnrecognizedTypeFallsBackToFreeText(t *testing.T) {
	fc := &fakeCompleter{response: `{
		"proceed": false,
		"questions": [{"prompt": "q", "type": "essay"}]
	}`}
	_, questions, err := Clarify(context.Background(), fc, "x")
	if err != nil {
		t.Fatalf("Clarify() error = %v", err)
	}
	if len(questions) != 1 || questions[0].Type != QuestionFreeText {
		t.Errorf("questions = %+v, want a single free_text fallback", questions)
	}
}

func TestClarifyStripsCodeFence(t *testing.T) {
	fc := &fakeCompleter{response: "```json\n{\"proceed\": true, \"questions\": []}\n```"}
	proceed, _, err := Clarify(context.Background(), fc, "x")
	if err != nil {
		t.Fatalf("Clarify() error = %v", err)
	}
	if !proceed {
		t.Error("Clarify() proceed = false, want true (code fence should have been stripped)")
	}
}

func TestClarifyStripsCodeFenceWithTrailingProse(t *testing.T) {
	// Real bug, caught by live testing against claude-haiku-4-5 (see
	// ROADMAP.md §28): the model wrapped its JSON in a ```json fence and
	// then appended several full sentences of unrequested explanation
	// after the closing fence. json.Unmarshal (the original
	// implementation) fails outright on trailing content after a valid
	// JSON value - decodeFirstJSONValue (json.Decoder-based) must parse
	// the JSON and simply stop, ignoring everything after it.
	fc := &fakeCompleter{response: "```json\n" +
		`{"proceed": true, "questions": []}` +
		"\n```\n\nThis description has enough information to scaffold confidently:\n- Language: C++\n- Framework: cpp-httplib\n\nI can create a working skeleton now."}
	proceed, questions, err := Clarify(context.Background(), fc, "x")
	if err != nil {
		t.Fatalf("Clarify() error = %v", err)
	}
	if !proceed {
		t.Error("Clarify() proceed = false, want true")
	}
	if len(questions) != 0 {
		t.Errorf("Clarify() questions = %v, want none", questions)
	}
}

func TestClarifyMalformedJSON(t *testing.T) {
	fc := &fakeCompleter{response: "not json at all"}
	if _, _, err := Clarify(context.Background(), fc, "x"); err == nil {
		t.Error("Clarify() with malformed JSON: expected an error, got nil")
	}
}

func writeScaffoldFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCollectScaffoldFilesFindsSrcAndInclude(t *testing.T) {
	root := t.TempDir()
	writeScaffoldFile(t, root, "src/main.cpp", "int main() {}\n")
	writeScaffoldFile(t, root, "include/lib.hpp", "#pragma once\n")
	writeScaffoldFile(t, root, "src/notes.txt", "not a source file\n")
	writeScaffoldFile(t, root, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.14)\n")

	got, err := CollectScaffoldFiles(root)
	if err != nil {
		t.Fatalf("CollectScaffoldFiles() error = %v", err)
	}
	want := map[string]bool{
		filepath.Join("src", "main.cpp"):    true,
		filepath.Join("include", "lib.hpp"): true,
	}
	if len(got) != len(want) {
		t.Fatalf("CollectScaffoldFiles() = %v, want exactly %v", got, want)
	}
	for _, f := range got {
		if !want[f] {
			t.Errorf("CollectScaffoldFiles() unexpectedly included %q", f)
		}
	}
}

func TestCollectScaffoldFilesNoSrcOrIncludeIsFine(t *testing.T) {
	root := t.TempDir()
	got, err := CollectScaffoldFiles(root)
	if err != nil {
		t.Fatalf("CollectScaffoldFiles() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("CollectScaffoldFiles() = %v, want none", got)
	}
}

func TestModifyScaffoldReturnsChangedContent(t *testing.T) {
	root := t.TempDir()
	writeScaffoldFile(t, root, "src/main.cpp", "int main() { return 0; }\n")

	fc := &fakeCompleter{response: "--- file: src/main.cpp ---\nint main() { return 42; }\n"}
	diff, changed, err := ModifyScaffold(context.Background(), fc, root, "exit with 42", "", []string{filepath.Join("src", "main.cpp")})
	if err != nil {
		t.Fatalf("ModifyScaffold() error = %v", err)
	}
	if diff == "" {
		t.Error("ModifyScaffold() diff is empty, want a real diff")
	}
	if !strings.Contains(diff, "+int main() { return 42; }") {
		t.Errorf("ModifyScaffold() diff = %q, missing the proposed change", diff)
	}
	// ParseFileBlocks trims a block's trailing newline(s) when extracting
	// it (same normalization heal's own diff computation already relies
	// on, including unifiedDiff's "\ No newline at end of file" handling
	// for exactly this case) - so the changed content has none here,
	// matching that established behavior rather than the original
	// (newline-terminated) source.
	want := filepath.Join("src", "main.cpp")
	if changed[want] != "int main() { return 42; }" {
		t.Errorf("ModifyScaffold() changed[%q] = %q, unexpected", want, changed[want])
	}
}

func TestModifyScaffoldNoChanges(t *testing.T) {
	root := t.TempDir()
	writeScaffoldFile(t, root, "src/main.cpp", "int main() { return 0; }\n")

	fc := &fakeCompleter{response: "NO_CHANGES"}
	diff, changed, err := ModifyScaffold(context.Background(), fc, root, "this is already fine", "", []string{filepath.Join("src", "main.cpp")})
	if err != nil {
		t.Fatalf("ModifyScaffold() error = %v", err)
	}
	if diff != "" || changed != nil {
		t.Errorf("ModifyScaffold() = (%q, %v), want (\"\", nil) for NO_CHANGES", diff, changed)
	}
}

func TestModifyScaffoldIdenticalContentIsNotAChange(t *testing.T) {
	root := t.TempDir()
	writeScaffoldFile(t, root, "src/main.cpp", "int main() { return 0; }\n")

	// The model "changes" a file back to its own original content - not a
	// real change, must not show up in the diff or the changed map.
	fc := &fakeCompleter{response: "--- file: src/main.cpp ---\nint main() { return 0; }\n"}
	diff, changed, err := ModifyScaffold(context.Background(), fc, root, "x", "", []string{filepath.Join("src", "main.cpp")})
	if err != nil {
		t.Fatalf("ModifyScaffold() error = %v", err)
	}
	if diff != "" || len(changed) != 0 {
		t.Errorf("ModifyScaffold() = (%q, %v), want no-op for identical content", diff, changed)
	}
}

func TestModifyScaffoldNoFilesFound(t *testing.T) {
	root := t.TempDir()
	fc := &fakeCompleter{response: "NO_CHANGES"}
	if _, _, err := ModifyScaffold(context.Background(), fc, root, "x", "", []string{filepath.Join("src", "missing.cpp")}); err == nil {
		t.Error("ModifyScaffold() with no existing files: expected an error, got nil")
	}
}

func TestModifyScaffoldPromptIncludesDescriptionAndReasoning(t *testing.T) {
	root := t.TempDir()
	writeScaffoldFile(t, root, "src/main.cpp", "int main() { return 0; }\n")

	fc := &fakeCompleter{response: "NO_CHANGES"}
	if _, _, err := ModifyScaffold(context.Background(), fc, root, "a REST API", "chose backend template", []string{filepath.Join("src", "main.cpp")}); err != nil {
		t.Fatalf("ModifyScaffold() error = %v", err)
	}
	if !strings.Contains(fc.gotUser, "a REST API") {
		t.Error("expected the prompt to include the description")
	}
	if !strings.Contains(fc.gotUser, "chose backend template") {
		t.Error("expected the prompt to include the plan reasoning")
	}
}
