package registry

import "testing"

func TestListSortedAndNonEmpty(t *testing.T) {
	entries := List()
	if len(entries) == 0 {
		t.Fatal("expected a non-empty built-in registry")
	}
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Name > entries[i].Name {
			t.Errorf("List() not sorted by name: %q comes before %q", entries[i-1].Name, entries[i].Name)
		}
	}
	for _, e := range entries {
		if e.Repo == "" || e.DefaultTag == "" || len(e.Link) == 0 {
			t.Errorf("entry %q missing required fields: %+v", e.Name, e)
		}
	}
}

func TestFind(t *testing.T) {
	e, ok := Find("fmt")
	if !ok {
		t.Fatal("expected to find 'fmt' in the registry")
	}
	if e.Repo != "fmtlib/fmt" {
		t.Errorf("Find(fmt).Repo = %q, want fmtlib/fmt", e.Repo)
	}

	if _, ok := Find("FMT"); !ok {
		t.Error("Find should be case-insensitive")
	}

	if _, ok := Find("definitely-not-a-real-package"); ok {
		t.Error("expected Find to report not-found for an unknown package")
	}
}

func TestSearch(t *testing.T) {
	matches := Search("json")
	found := false
	for _, m := range matches {
		if m.Name == "nlohmann-json" {
			found = true
		}
	}
	if !found {
		t.Errorf("Search(json) = %+v, expected to include nlohmann-json", matches)
	}
}

func TestCloseMatches(t *testing.T) {
	matches := CloseMatches("nlohman-json") // one letter dropped
	found := false
	for _, m := range matches {
		if m == "nlohmann-json" {
			found = true
		}
	}
	if !found {
		t.Errorf("CloseMatches(nlohman-json) = %v, expected to suggest nlohmann-json", matches)
	}
}

func TestToDependency(t *testing.T) {
	e, ok := Find("catch2")
	if !ok {
		t.Fatal("expected to find catch2")
	}
	dep := e.ToDependency()
	if dep.Name != "catch2" || dep.Repo != "catchorg/Catch2" || dep.Tag != e.DefaultTag {
		t.Errorf("ToDependency() = %+v, unexpected", dep)
	}
	if len(dep.Link) != 1 || dep.Link[0] != "Catch2::Catch2WithMain" {
		t.Errorf("ToDependency().Link = %v, want [Catch2::Catch2WithMain]", dep.Link)
	}
}
