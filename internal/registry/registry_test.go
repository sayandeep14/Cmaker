package registry

import (
	"testing"

	"cmaker/internal/config"
)

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
		if len(e.Link) == 0 {
			t.Errorf("entry %q missing required fields: %+v", e.Name, e)
		}
		switch e.Kind {
		case KindSystemPackage:
			if e.FindPackage == "" || len(e.PackageManagers) == 0 {
				t.Errorf("system_package entry %q missing required fields: %+v", e.Name, e)
			}
		case KindPrebuiltArchive:
			if e.ArchiveURLTemplate == "" || len(e.PlatformNames) == 0 {
				t.Errorf("prebuilt_archive entry %q missing required fields: %+v", e.Name, e)
			}
		case KindPkgConfig:
			if e.PkgConfigModule == "" || len(e.PackageManagers) == 0 {
				t.Errorf("pkg_config entry %q missing required fields: %+v", e.Name, e)
			}
		default: // KindCPM
			if e.Repo == "" || e.DefaultTag == "" {
				t.Errorf("cpm entry %q missing required fields: %+v", e.Name, e)
			}
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

func TestToDependencySystemPackage(t *testing.T) {
	e, ok := Find("opencv")
	if !ok {
		t.Fatal("expected to find opencv")
	}
	dep := e.ToDependency()
	if dep.Name != "opencv" || dep.Kind != config.DependencyKindSystemPackage || dep.FindPackage != "OpenCV" {
		t.Errorf("ToDependency() = %+v, unexpected", dep)
	}
}

func TestToDependencyPkgConfig(t *testing.T) {
	e, ok := Find("gtkmm")
	if !ok {
		t.Fatal("expected to find gtkmm")
	}
	dep := e.ToDependency()
	if dep.Name != "gtkmm" || dep.Kind != config.DependencyKindPkgConfig || dep.PkgConfigModule != "gtkmm-4.0" {
		t.Errorf("ToDependency() = %+v, unexpected", dep)
	}
	if len(dep.Link) != 1 || dep.Link[0] != "PkgConfig::GTKMM" {
		t.Errorf("ToDependency().Link = %v, want [PkgConfig::GTKMM]", dep.Link)
	}
}

func TestToDependencyPrebuiltArchivePanicsWithoutResolvedURL(t *testing.T) {
	e, ok := Find("onnxruntime")
	if !ok {
		t.Fatal("expected to find onnxruntime")
	}
	defer func() {
		if recover() == nil {
			t.Error("expected ToDependency() on a prebuilt_archive entry to panic")
		}
	}()
	e.ToDependency()
}

func TestToDependencyForArchive(t *testing.T) {
	e, ok := Find("onnxruntime")
	if !ok {
		t.Fatal("expected to find onnxruntime")
	}
	dep := e.ToDependencyForArchive("https://example.com/resolved.tgz")
	if dep.Name != "onnxruntime" || dep.Kind != config.DependencyKindPrebuiltArchive {
		t.Errorf("ToDependencyForArchive() = %+v, unexpected", dep)
	}
	if dep.ArchiveURL != "https://example.com/resolved.tgz" {
		t.Errorf("ToDependencyForArchive().ArchiveURL = %q, want the resolved URL", dep.ArchiveURL)
	}
	if dep.ArchiveIncludeDir != "include" || dep.ArchiveLibDir != "lib" {
		t.Errorf("ToDependencyForArchive() = %+v, expected include/lib dirs carried over from the entry", dep)
	}
}

func TestResolveArchiveURL(t *testing.T) {
	e, ok := Find("onnxruntime")
	if !ok {
		t.Fatal("expected to find onnxruntime")
	}
	got, err := e.ResolveArchiveURL("darwin", "arm64")
	if err != nil {
		t.Fatalf("ResolveArchiveURL() error = %v", err)
	}
	want := "https://github.com/microsoft/onnxruntime/releases/download/v1.28.0/onnxruntime-osx-arm64-1.28.0.tgz"
	if got != want {
		t.Errorf("ResolveArchiveURL(darwin, arm64) = %q, want %q", got, want)
	}
}

func TestResolveArchiveURLUnsupportedPlatform(t *testing.T) {
	e, ok := Find("onnxruntime")
	if !ok {
		t.Fatal("expected to find onnxruntime")
	}
	if _, err := e.ResolveArchiveURL("plan9", "mips"); err == nil {
		t.Error("ResolveArchiveURL() for an unsupported platform: expected an error, got nil")
	}
}
