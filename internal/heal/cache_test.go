package heal

import "testing"

func TestSaveAndLoadSuggestionRoundTrip(t *testing.T) {
	root := t.TempDir()
	s := Suggestion{Diff: "some diff", ReferencedFiles: []string{"a.cpp", "b.cpp"}}

	if err := SaveSuggestion(root, "/logs/x-fail.log", s); err != nil {
		t.Fatalf("SaveSuggestion() error = %v", err)
	}

	got, ok := LoadSuggestionFor(root, "/logs/x-fail.log")
	if !ok {
		t.Fatal("LoadSuggestionFor() ok = false, want true")
	}
	if got.Diff != s.Diff {
		t.Errorf("LoadSuggestionFor().Diff = %q, want %q", got.Diff, s.Diff)
	}
	if len(got.ReferencedFiles) != 2 {
		t.Errorf("LoadSuggestionFor().ReferencedFiles = %v, want 2 entries", got.ReferencedFiles)
	}
}

func TestLoadSuggestionForDifferentLogPathMisses(t *testing.T) {
	root := t.TempDir()
	if err := SaveSuggestion(root, "/logs/x-fail.log", Suggestion{Diff: "d"}); err != nil {
		t.Fatalf("SaveSuggestion() error = %v", err)
	}

	if _, ok := LoadSuggestionFor(root, "/logs/y-fail.log"); ok {
		t.Error("LoadSuggestionFor() for a different log path: ok = true, want false")
	}
}

func TestLoadSuggestionForNoCacheIsFine(t *testing.T) {
	root := t.TempDir()
	if _, ok := LoadSuggestionFor(root, "/logs/x-fail.log"); ok {
		t.Error("LoadSuggestionFor() with no cache present: ok = true, want false")
	}
}

func TestClearSuggestionRemovesTheCache(t *testing.T) {
	root := t.TempDir()
	if err := SaveSuggestion(root, "/logs/x-fail.log", Suggestion{Diff: "d"}); err != nil {
		t.Fatalf("SaveSuggestion() error = %v", err)
	}
	ClearSuggestion(root)

	if _, ok := LoadSuggestionFor(root, "/logs/x-fail.log"); ok {
		t.Error("LoadSuggestionFor() after ClearSuggestion(): ok = true, want false")
	}
}

func TestClearSuggestionWithNoCacheIsFine(t *testing.T) {
	root := t.TempDir()
	ClearSuggestion(root) // must not panic or error despite nothing to clear
}
