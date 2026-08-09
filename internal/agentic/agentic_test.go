package agentic

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeCompleter struct {
	response string
	err      error

	gotSystem string
	gotUser   string
}

func (f *fakeCompleter) Complete(ctx context.Context, system, user string) (string, error) {
	f.gotSystem = system
	f.gotUser = user
	return f.response, f.err
}

func TestSuggestRelevantFilesFiltersToCandidates(t *testing.T) {
	fc := &fakeCompleter{response: `["src/main.cpp", "src/nope.cpp", "include/util.h"]`}
	got, err := SuggestRelevantFiles(context.Background(), fc, "add logging", []string{"src/main.cpp", "include/util.h"})
	if err != nil {
		t.Fatalf("SuggestRelevantFiles() error = %v", err)
	}
	want := []string{"src/main.cpp", "include/util.h"}
	if len(got) != len(want) {
		t.Fatalf("SuggestRelevantFiles() = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("SuggestRelevantFiles()[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestSuggestRelevantFilesEmptyCandidates(t *testing.T) {
	fc := &fakeCompleter{response: `[]`}
	got, err := SuggestRelevantFiles(context.Background(), fc, "add logging", nil)
	if err != nil {
		t.Fatalf("SuggestRelevantFiles() error = %v", err)
	}
	if got != nil {
		t.Errorf("SuggestRelevantFiles() with no candidates = %v, want nil (and no LLM call)", got)
	}
	if fc.gotUser != "" {
		t.Error("expected no LLM call when there are no candidates")
	}
}

func TestSuggestRelevantFilesToleratesSmartQuotes(t *testing.T) {
	// Observed live from claude-opus-5: a response using curly/smart
	// quotes instead of straight ASCII ones, which plain JSON rejects.
	fc := &fakeCompleter{response: "[“src/main.cpp”]"}
	got, err := SuggestRelevantFiles(context.Background(), fc, "add logging", []string{"src/main.cpp"})
	if err != nil {
		t.Fatalf("SuggestRelevantFiles() error = %v", err)
	}
	if len(got) != 1 || got[0] != "src/main.cpp" {
		t.Errorf("SuggestRelevantFiles() = %v, want [src/main.cpp]", got)
	}
}

func TestSuggestRelevantFilesCapsAtMax(t *testing.T) {
	var candidates []string
	var respPaths []string
	for i := range maxRelevantFiles + 5 {
		p := string(rune('a'+i)) + ".cpp"
		candidates = append(candidates, p)
		respPaths = append(respPaths, `"`+p+`"`)
	}
	fc := &fakeCompleter{response: "[" + strings.Join(respPaths, ",") + "]"}
	got, err := SuggestRelevantFiles(context.Background(), fc, "intent", candidates)
	if err != nil {
		t.Fatalf("SuggestRelevantFiles() error = %v", err)
	}
	if len(got) != maxRelevantFiles {
		t.Errorf("SuggestRelevantFiles() returned %d files, want capped at %d", len(got), maxRelevantFiles)
	}
}

func TestAskClarifyingQuestionsNoQuestions(t *testing.T) {
	fc := &fakeCompleter{response: "NO_QUESTIONS"}
	got, err := AskClarifyingQuestions(context.Background(), fc, "add a flag", []string{"a.cpp"})
	if err != nil {
		t.Fatalf("AskClarifyingQuestions() error = %v", err)
	}
	if got != nil {
		t.Errorf("AskClarifyingQuestions() = %v, want nil", got)
	}
}

func TestAskClarifyingQuestionsReturnsQuestions(t *testing.T) {
	fc := &fakeCompleter{response: `["Which flag name?", "Should it be global or per-command?"]`}
	got, err := AskClarifyingQuestions(context.Background(), fc, "add a flag", []string{"a.cpp"})
	if err != nil {
		t.Fatalf("AskClarifyingQuestions() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("AskClarifyingQuestions() = %v, want 2 questions", got)
	}
}

func TestAssessBreakdownNoBreakdown(t *testing.T) {
	fc := &fakeCompleter{response: "NO_BREAKDOWN"}
	got, err := AssessBreakdown(context.Background(), fc, "fix a typo", []string{"a.cpp"})
	if err != nil {
		t.Fatalf("AssessBreakdown() error = %v", err)
	}
	if got != nil {
		t.Errorf("AssessBreakdown() = %v, want nil", got)
	}
}

func TestAssessBreakdownReturnsSubtasks(t *testing.T) {
	fc := &fakeCompleter{response: "```json\n[\"Add config struct\", \"Wire it into main\", \"Add tests\"]\n```"}
	got, err := AssessBreakdown(context.Background(), fc, "add a whole config system", []string{"a.cpp"})
	if err != nil {
		t.Fatalf("AssessBreakdown() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("AssessBreakdown() = %v, want 3 subtasks", got)
	}
}

func TestProposeChangesRequiresIntent(t *testing.T) {
	fc := &fakeCompleter{}
	if _, err := ProposeChanges(context.Background(), fc, "", map[string]string{"a.cpp": "int main(){}"}); err == nil {
		t.Error("ProposeChanges() with empty intent: expected an error, got nil")
	}
}

func TestProposeChangesRequiresFiles(t *testing.T) {
	fc := &fakeCompleter{}
	if _, err := ProposeChanges(context.Background(), fc, "do something", nil); err == nil {
		t.Error("ProposeChanges() with no files: expected an error, got nil")
	}
}

func TestProposeChangesNoChange(t *testing.T) {
	fc := &fakeCompleter{response: "NO_CHANGE"}
	got, err := ProposeChanges(context.Background(), fc, "do something", map[string]string{"a.cpp": "int main(){}"})
	if err != nil {
		t.Fatalf("ProposeChanges() error = %v", err)
	}
	if got != nil {
		t.Errorf("ProposeChanges() = %v, want nil", got)
	}
}

func TestProposeChangesParsesFileBlocks(t *testing.T) {
	fc := &fakeCompleter{response: "--- file: a.cpp ---\nint main(){ return 1; }\n"}
	got, err := ProposeChanges(context.Background(), fc, "return 1", map[string]string{"a.cpp": "int main(){ return 0; }"})
	if err != nil {
		t.Fatalf("ProposeChanges() error = %v", err)
	}
	if got["a.cpp"] != "int main(){ return 1; }" {
		t.Errorf("ProposeChanges()[a.cpp] = %q, unexpected", got["a.cpp"])
	}
	if !strings.Contains(fc.gotUser, "return 1") {
		t.Error("expected the intent text in the prompt sent to the model")
	}
}

func TestProposeChangesUnparsableResponseErrors(t *testing.T) {
	fc := &fakeCompleter{response: "some unrelated prose with no file blocks"}
	if _, err := ProposeChanges(context.Background(), fc, "do something", map[string]string{"a.cpp": "x"}); err == nil {
		t.Error("ProposeChanges() with an unparsable response: expected an error, got nil")
	}
}

func TestProposeChangesPropagatesCompleterError(t *testing.T) {
	fc := &fakeCompleter{err: errors.New("network error")}
	if _, err := ProposeChanges(context.Background(), fc, "do something", map[string]string{"a.cpp": "x"}); err == nil {
		t.Error("ProposeChanges() expected an error when the completer fails, got nil")
	}
}
