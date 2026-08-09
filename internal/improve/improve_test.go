package improve

import (
	"context"
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

func TestSuggestReturnsReplacement(t *testing.T) {
	fc := &fakeCompleter{response: "--- replacement ---\nint add(int a, int b) {\n    return a + b; // fast path\n}\n"}
	got, err := Suggest(context.Background(), fc, "function", "add", "int add(int a, int b) {\n    return a + b;\n}", "add a comment")
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if !strings.Contains(got, "fast path") {
		t.Errorf("Suggest() = %q, missing the proposed change", got)
	}
	if !strings.Contains(fc.gotUser, "add a comment") {
		t.Error("expected the prompt to include the intent")
	}
	if !strings.Contains(fc.gotUser, "int add(int a, int b)") {
		t.Error("expected the prompt to include the original code")
	}
}

func TestSuggestRequiresIntent(t *testing.T) {
	fc := &fakeCompleter{response: "NO_CHANGE"}
	if _, err := Suggest(context.Background(), fc, "function", "add", "int add() {}", ""); err == nil {
		t.Error("Suggest() with an empty intent: expected an error, got nil")
	}
}

func TestSuggestNoChange(t *testing.T) {
	fc := &fakeCompleter{response: "NO_CHANGE"}
	got, err := Suggest(context.Background(), fc, "function", "add", "int add() { return 0; }", "improve it")
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if got != "" {
		t.Errorf("Suggest() = %q, want empty for NO_CHANGE", got)
	}
}

func TestSuggestIdenticalContentIsNoChange(t *testing.T) {
	original := "int add() { return 0; }"
	fc := &fakeCompleter{response: "--- replacement ---\n" + original}
	got, err := Suggest(context.Background(), fc, "function", "add", original, "improve it")
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if got != "" {
		t.Errorf("Suggest() = %q, want empty when the model proposed the same content back", got)
	}
}

func TestSuggestStripsCodeFence(t *testing.T) {
	fc := &fakeCompleter{response: "--- replacement ---\n```cpp\nint add() { return 1; }\n```"}
	got, err := Suggest(context.Background(), fc, "function", "add", "int add() { return 0; }", "fix it")
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if got != "int add() { return 1; }" {
		t.Errorf("Suggest() = %q, want fence stripped", got)
	}
}

func TestSuggestTruncatesAtStrayDuplicateBlock(t *testing.T) {
	// Real bug, caught by live testing against claude-haiku-4-5: asked to
	// improve a function using an unordered_set, the model wanted to
	// mention a needed #include and - despite the prompt saying "never
	// more than one such block" - emitted a second, malformed
	// "--- replacement ---"-shaped block after the real one. Without
	// truncating at the repeat, that stray block's text got spliced
	// straight into the file mid-function.
	fc := &fakeCompleter{response: "--- replacement ---\nbool contains(int x) { return true; }\n--- replacement ---\n\nsome trailing garbage"}
	got, err := Suggest(context.Background(), fc, "function", "contains", "bool contains(int x) { return false; }", "improve it")
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if strings.Contains(got, "replacement") || strings.Contains(got, "trailing garbage") {
		t.Errorf("Suggest() = %q, want the stray second block truncated away", got)
	}
	if !strings.Contains(got, "return true") {
		t.Errorf("Suggest() = %q, missing the real proposed change", got)
	}
}

func TestSuggestMissingReplacementBlock(t *testing.T) {
	fc := &fakeCompleter{response: "I think this looks fine already."}
	if _, err := Suggest(context.Background(), fc, "function", "add", "int add() {}", "improve it"); err == nil {
		t.Error("Suggest() with no replacement block: expected an error, got nil")
	}
}

func TestSuggestPropagatesCompleterError(t *testing.T) {
	fc := &fakeCompleter{err: context.DeadlineExceeded}
	if _, err := Suggest(context.Background(), fc, "function", "add", "int add() {}", "improve it"); err == nil {
		t.Error("Suggest() expected an error when the completer fails, got nil")
	}
}

func TestSuggestFileReturnsChangedTargetFile(t *testing.T) {
	fc := &fakeCompleter{response: "--- file: src/main.cpp ---\n#include <unordered_set>\n\nbool contains() { return true; }\n"}
	got, err := SuggestFile(context.Background(), fc, "function", "contains", "src/main.cpp", "bool contains() { return false; }", "use a hash set")
	if err != nil {
		t.Fatalf("SuggestFile() error = %v", err)
	}
	if !strings.Contains(got["src/main.cpp"], "unordered_set") {
		t.Errorf("SuggestFile() = %+v, missing the proposed change", got)
	}
	if !strings.Contains(fc.gotUser, "use a hash set") {
		t.Error("expected the prompt to include the intent")
	}
	if !strings.Contains(fc.gotUser, "--- file: src/main.cpp ---") {
		t.Error("expected the prompt to include the target file in heal's own block format")
	}
}

func TestSuggestFileCanProposeNewFile(t *testing.T) {
	fc := &fakeCompleter{response: "--- file: src/main.cpp ---\n#include \"helpers.hpp\"\nbool contains() { return helper(); }\n" +
		"--- file: src/helpers.hpp ---\n#pragma once\nbool helper();\n"}
	got, err := SuggestFile(context.Background(), fc, "function", "contains", "src/main.cpp", "bool contains() { return false; }", "extract a helper")
	if err != nil {
		t.Fatalf("SuggestFile() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("SuggestFile() = %+v, want 2 files (target + new helper)", got)
	}
	if !strings.Contains(got["src/helpers.hpp"], "bool helper();") {
		t.Errorf("SuggestFile() = %+v, missing the new helper file", got)
	}
}

func TestSuggestFileNoChange(t *testing.T) {
	fc := &fakeCompleter{response: "NO_CHANGE"}
	got, err := SuggestFile(context.Background(), fc, "function", "contains", "src/main.cpp", "bool contains() { return false; }", "improve it")
	if err != nil {
		t.Fatalf("SuggestFile() error = %v", err)
	}
	if got != nil {
		t.Errorf("SuggestFile() = %+v, want nil for NO_CHANGE", got)
	}
}

func TestSuggestFileRequiresIntent(t *testing.T) {
	fc := &fakeCompleter{response: "NO_CHANGE"}
	if _, err := SuggestFile(context.Background(), fc, "function", "contains", "src/main.cpp", "code", ""); err == nil {
		t.Error("SuggestFile() with an empty intent: expected an error, got nil")
	}
}

func TestSuggestFileNoRecognizableBlocks(t *testing.T) {
	fc := &fakeCompleter{response: "I think this is fine as-is."}
	if _, err := SuggestFile(context.Background(), fc, "function", "contains", "src/main.cpp", "code", "improve it"); err == nil {
		t.Error("SuggestFile() with no file blocks: expected an error, got nil")
	}
}
