package commitmsg

import (
	"context"
	"errors"
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

func TestGenerateReturnsMessage(t *testing.T) {
	fc := &fakeCompleter{response: "Add LinkedList sum() method"}
	got, err := Generate(context.Background(), fc, "diff --git a/x.cpp b/x.cpp\n+int x;")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got != "Add LinkedList sum() method" {
		t.Errorf("Generate() = %q, unexpected", got)
	}
	if !strings.Contains(fc.gotUser, "diff --git") {
		t.Error("expected the prompt to include the diff")
	}
}

func TestGenerateEmptyDiff(t *testing.T) {
	if _, err := Generate(context.Background(), &fakeCompleter{}, ""); err == nil {
		t.Error("Generate() with an empty diff: expected an error, got nil")
	}
	if _, err := Generate(context.Background(), &fakeCompleter{}, "   \n  "); err == nil {
		t.Error("Generate() with a whitespace-only diff: expected an error, got nil")
	}
}

func TestGenerateStripsCodeFence(t *testing.T) {
	fc := &fakeCompleter{response: "```\nFix the thing\n```"}
	got, err := Generate(context.Background(), fc, "some diff")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got != "Fix the thing" {
		t.Errorf("Generate() = %q, want fence stripped", got)
	}
}

func TestGenerateStripsSurroundingQuotes(t *testing.T) {
	fc := &fakeCompleter{response: `"Fix the thing"`}
	got, err := Generate(context.Background(), fc, "some diff")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got != "Fix the thing" {
		t.Errorf("Generate() = %q, want quotes stripped", got)
	}
}

func TestGenerateTruncatesLargeDiff(t *testing.T) {
	fc := &fakeCompleter{response: "Refactor large file"}
	huge := strings.Repeat("+line\n", maxDiffChars)
	if _, err := Generate(context.Background(), fc, huge); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(fc.gotUser) > maxDiffChars+200 {
		t.Errorf("Generate() sent %d chars, want it truncated near maxDiffChars", len(fc.gotUser))
	}
	if !strings.Contains(fc.gotUser, "truncated") {
		t.Error("expected a truncation marker in the prompt for an oversized diff")
	}
}

func TestGeneratePropagatesCompleterError(t *testing.T) {
	fc := &fakeCompleter{err: errors.New("network error")}
	if _, err := Generate(context.Background(), fc, "some diff"); err == nil {
		t.Error("Generate() expected an error when the completer fails, got nil")
	}
}

func TestGenerateEmptyResponse(t *testing.T) {
	fc := &fakeCompleter{response: "   "}
	if _, err := Generate(context.Background(), fc, "some diff"); err == nil {
		t.Error("Generate() with an empty model response: expected an error, got nil")
	}
}

func TestGenerateBranchSlugReturnsSlug(t *testing.T) {
	fc := &fakeCompleter{response: "fix-off-by-one-sum"}
	got, err := GenerateBranchSlug(context.Background(), fc, "diff --git a/x.cpp b/x.cpp\n+int x;")
	if err != nil {
		t.Fatalf("GenerateBranchSlug() error = %v", err)
	}
	if got != "fix-off-by-one-sum" {
		t.Errorf("GenerateBranchSlug() = %q, unexpected", got)
	}
}

func TestGenerateBranchSlugEmptyDiff(t *testing.T) {
	if _, err := GenerateBranchSlug(context.Background(), &fakeCompleter{}, ""); err == nil {
		t.Error("GenerateBranchSlug() with an empty diff: expected an error, got nil")
	}
}

func TestGenerateBranchSlugSanitizesMessyResponse(t *testing.T) {
	fc := &fakeCompleter{response: "  Fix The Thing!! (v2)  "}
	got, err := GenerateBranchSlug(context.Background(), fc, "some diff")
	if err != nil {
		t.Fatalf("GenerateBranchSlug() error = %v", err)
	}
	if got != "fix-the-thing-v2" {
		t.Errorf("GenerateBranchSlug() = %q, want sanitized slug", got)
	}
}

func TestGenerateBranchSlugEmptyAfterSanitize(t *testing.T) {
	fc := &fakeCompleter{response: "!!!"}
	if _, err := GenerateBranchSlug(context.Background(), fc, "some diff"); err == nil {
		t.Error("GenerateBranchSlug() expected an error when sanitizing leaves nothing, got nil")
	}
}

func TestSanitizeSlug(t *testing.T) {
	cases := map[string]string{
		"Fix Bug":          "fix-bug",
		"  leading  ":      "leading",
		"a--b":             "a-b",
		"-trim-":           "trim",
		"already-a-slug":   "already-a-slug",
		"Weird!!Chars??42": "weird-chars-42",
	}
	for in, want := range cases {
		if got := sanitizeSlug(in); got != want {
			t.Errorf("sanitizeSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
