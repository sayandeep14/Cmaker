package packclient

import (
	"testing"
)

func TestManifestValidateOK(t *testing.T) {
	m := Manifest{Name: "mypack", Version: "1.0.0"}
	if err := m.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestManifestValidateRequiresName(t *testing.T) {
	m := Manifest{Version: "1.0.0"}
	if err := m.Validate(); err == nil {
		t.Error("Validate() with no name: expected an error, got nil")
	}
}

func TestManifestValidateRejectsBadNameChars(t *testing.T) {
	m := Manifest{Name: "my pack!", Version: "1.0.0"}
	if err := m.Validate(); err == nil {
		t.Error("Validate() with an invalid name: expected an error, got nil")
	}
}

func TestManifestValidateRequiresVersion(t *testing.T) {
	m := Manifest{Name: "mypack"}
	if err := m.Validate(); err == nil {
		t.Error("Validate() with no version: expected an error, got nil")
	}
}

func TestManifestValidateDependencyNeedsSource(t *testing.T) {
	m := Manifest{Name: "mypack", Version: "1.0.0", Dependencies: []ManifestDependency{
		{Name: "fmt", Source: "somewhere-else"},
	}}
	if err := m.Validate(); err == nil {
		t.Error("Validate() with an invalid dependency source: expected an error, got nil")
	}
}

func TestManifestValidatePackDependencyNeedsVersion(t *testing.T) {
	m := Manifest{Name: "mypack", Version: "1.0.0", Dependencies: []ManifestDependency{
		{Name: "other-pack", Source: "pack"},
	}}
	if err := m.Validate(); err == nil {
		t.Error("Validate() with a pack dependency missing a version: expected an error, got nil")
	}
}

func TestManifestValidateRegistryDependencyOK(t *testing.T) {
	m := Manifest{Name: "mypack", Version: "1.0.0", Dependencies: []ManifestDependency{
		{Name: "fmt", Source: "registry"},
	}}
	if err := m.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestSaveLoadManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := Manifest{
		Name: "mypack", Version: "1.2.3", Description: "a test pack", License: "MIT",
		Kind: "directory", Authors: []string{"octocat"},
		Dependencies: []ManifestDependency{{Name: "fmt", Source: "registry"}},
		Placement:    []ManifestPlacement{{Src: "include/a.hpp", Dest: "include/a.hpp"}},
	}
	if err := SaveManifest(dir, m); err != nil {
		t.Fatalf("SaveManifest() error = %v", err)
	}

	got, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if got.Name != m.Name || got.Version != m.Version || got.Description != m.Description {
		t.Errorf("LoadManifest() = %+v, want %+v", got, m)
	}
	if len(got.Dependencies) != 1 || got.Dependencies[0].Name != "fmt" {
		t.Errorf("LoadManifest() Dependencies = %+v", got.Dependencies)
	}
	if len(got.Placement) != 1 || got.Placement[0].Dest != "include/a.hpp" {
		t.Errorf("LoadManifest() Placement = %+v", got.Placement)
	}
}

func TestLoadManifestMissingFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadManifest(dir); err == nil {
		t.Error("LoadManifest() with no manifest.yaml: expected an error, got nil")
	}
}

func TestLoadManifestValidatesContent(t *testing.T) {
	dir := t.TempDir()
	if err := SaveManifest(dir, Manifest{Name: "", Version: ""}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(dir); err == nil {
		t.Error("LoadManifest() with an invalid manifest: expected an error, got nil")
	}
}
