package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func withUserRegistry(t *testing.T, path string) {
	t.Helper()
	old := userRegistryPath
	userRegistryPath = func() string { return path }
	t.Cleanup(func() { userRegistryPath = old })
}

func TestListIncludesUserOverlayEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.yaml")
	if err := os.WriteFile(path, []byte(`- name: my-internal-lib
  repo: myorg/my-internal-lib
  default_tag: v1.0.0
  link: [my-internal-lib::my-internal-lib]
  notes: our team's internal library
`), 0644); err != nil {
		t.Fatal(err)
	}
	withUserRegistry(t, path)

	entry, ok := Find("my-internal-lib")
	if !ok {
		t.Fatal("expected to find the user-overlay entry")
	}
	if entry.Source != SourceUser {
		t.Errorf("Find(my-internal-lib).Source = %q, want SourceUser", entry.Source)
	}
	if entry.Repo != "myorg/my-internal-lib" {
		t.Errorf("Find(my-internal-lib).Repo = %q, want myorg/my-internal-lib", entry.Repo)
	}

	// The built-in entries must still be present alongside the overlay.
	if _, ok := Find("fmt"); !ok {
		t.Error("expected the built-in \"fmt\" entry to still be findable")
	}
}

func TestUserOverlayOverridesBuiltIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.yaml")
	if err := os.WriteFile(path, []byte(`- name: fmt
  repo: myfork/fmt
  default_tag: custom-tag
  link: [fmt::fmt]
  notes: a pinned internal fork
`), 0644); err != nil {
		t.Fatal(err)
	}
	withUserRegistry(t, path)

	entry, ok := Find("fmt")
	if !ok {
		t.Fatal("expected to find fmt")
	}
	if entry.Source != SourceUser || entry.Repo != "myfork/fmt" {
		t.Errorf("Find(fmt) = %+v, want the user overlay's version to win", entry)
	}
}

func TestNoUserOverlayFileIsFine(t *testing.T) {
	withUserRegistry(t, filepath.Join(t.TempDir(), "does-not-exist.yaml"))

	if _, ok := Find("fmt"); !ok {
		t.Error("expected built-in entries to still work with no user overlay present")
	}
}

func TestMalformedUserOverlayIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.yaml")
	if err := os.WriteFile(path, []byte("not: [valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	withUserRegistry(t, path)

	if _, ok := Find("fmt"); !ok {
		t.Error("expected built-in entries to still work despite a malformed user overlay")
	}
}

func TestSearchLabelsUserEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.yaml")
	if err := os.WriteFile(path, []byte(`- name: internal-json-tool
  repo: myorg/internal-json-tool
  default_tag: v1.0.0
  link: [internal-json-tool::internal-json-tool]
  notes: internal JSON helper
`), 0644); err != nil {
		t.Fatal(err)
	}
	withUserRegistry(t, path)

	matches := Search("json")
	var sawUserEntry, sawBuiltInEntry bool
	for _, m := range matches {
		if m.Name == "internal-json-tool" && m.Source == SourceUser {
			sawUserEntry = true
		}
		if m.Name == "nlohmann-json" && m.Source == SourceBuiltIn {
			sawBuiltInEntry = true
		}
	}
	if !sawUserEntry {
		t.Errorf("Search(json) = %+v, expected to include the labeled user entry", matches)
	}
	if !sawBuiltInEntry {
		t.Errorf("Search(json) = %+v, expected to still include the labeled built-in entry", matches)
	}
}
