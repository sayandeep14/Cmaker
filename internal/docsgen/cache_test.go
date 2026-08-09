package docsgen

import "testing"

func TestHashPromptStableAndDistinct(t *testing.T) {
	a := HashPrompt("some source content")
	b := HashPrompt("some source content")
	if a != b {
		t.Errorf("HashPrompt() not stable: %q != %q", a, b)
	}
	if c := HashPrompt("different content"); c == a {
		t.Error("HashPrompt() produced the same hash for different content")
	}
}

func TestSaveAndLoadQualityRoundTrip(t *testing.T) {
	root := t.TempDir()
	q := Quality{Score: 8, Summary: "Well documented overall."}
	if err := SaveQuality(root, "hash1", "claude-haiku-4-5", q); err != nil {
		t.Fatalf("SaveQuality() error = %v", err)
	}

	got, ok := LoadQuality(root, "hash1", "claude-haiku-4-5")
	if !ok {
		t.Fatal("LoadQuality() ok = false, want true")
	}
	if got != q {
		t.Errorf("LoadQuality() = %+v, want %+v", got, q)
	}
}

func TestLoadQualityMissesOnHashChange(t *testing.T) {
	root := t.TempDir()
	SaveQuality(root, "hash1", "claude-haiku-4-5", Quality{Score: 8, Summary: "x"})

	if _, ok := LoadQuality(root, "hash2", "claude-haiku-4-5"); ok {
		t.Error("LoadQuality() with a different prompt hash: expected ok=false")
	}
}

func TestLoadQualityMissesOnModelChange(t *testing.T) {
	root := t.TempDir()
	SaveQuality(root, "hash1", "claude-haiku-4-5", Quality{Score: 8, Summary: "x"})

	if _, ok := LoadQuality(root, "hash1", "claude-opus-5"); ok {
		t.Error("LoadQuality() with a different model: expected ok=false")
	}
}

func TestLoadQualityNoCacheYet(t *testing.T) {
	root := t.TempDir()
	if _, ok := LoadQuality(root, "hash1", "claude-haiku-4-5"); ok {
		t.Error("LoadQuality() with no cache file: expected ok=false")
	}
}
