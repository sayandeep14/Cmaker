package templates

import (
	"os"
	"path/filepath"
	"testing"
)

// withDirs points userTemplatesDir/projectTemplatesDir at temp directories
// for the duration of a test, restoring the real functions afterward.
func withDirs(t *testing.T, userDir, projectDir string) {
	t.Helper()
	oldUser, oldProject := userTemplatesDir, projectTemplatesDir
	userTemplatesDir = func() string { return userDir }
	projectTemplatesDir = func() string { return projectDir }
	t.Cleanup(func() {
		userTemplatesDir, projectTemplatesDir = oldUser, oldProject
	})
}

func writeTemplate(t *testing.T, dir, name, metaYAML string, files map[string]string) {
	t.Helper()
	templateDir := filepath.Join(dir, name)
	if err := os.MkdirAll(templateDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templateDir, "meta.yaml"), []byte(metaYAML), 0644); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		path := filepath.Join(templateDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListIncludesUserAndProjectTemplates(t *testing.T) {
	userDir, projectDir := t.TempDir(), t.TempDir()
	withDirs(t, userDir, projectDir)

	writeTemplate(t, userDir, "my-user-template", "name: my-user-template\ndescription: a user template\ncpp_version: 20\n", nil)
	writeTemplate(t, projectDir, "my-project-template", "name: my-project-template\ndescription: a project template\ncpp_version: 20\n", nil)

	metas, err := List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	byName := map[string]Meta{}
	for _, m := range metas {
		byName[m.Name] = m
	}

	if m, ok := byName["my-user-template"]; !ok || m.Source != SourceUser {
		t.Errorf("expected my-user-template with Source=SourceUser, got %+v (found=%v)", m, ok)
	}
	if m, ok := byName["my-project-template"]; !ok || m.Source != SourceProject {
		t.Errorf("expected my-project-template with Source=SourceProject, got %+v (found=%v)", m, ok)
	}
	// Built-in templates must still be present alongside the extensions.
	if _, ok := byName["default"]; !ok {
		t.Error("expected the built-in \"default\" template to still be listed")
	}
}

func TestListPrecedenceProjectOverUserOverBuiltIn(t *testing.T) {
	userDir, projectDir := t.TempDir(), t.TempDir()
	withDirs(t, userDir, projectDir)

	// Override the built-in "default" template's name at both the user and
	// project level - project-local must win.
	writeTemplate(t, userDir, "default", "name: default\ndescription: user override\ncpp_version: 20\n", nil)
	writeTemplate(t, projectDir, "default", "name: default\ndescription: project override\ncpp_version: 20\n", nil)

	metas, err := List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	var got Meta
	for _, m := range metas {
		if m.Name == "default" {
			got = m
		}
	}
	if got.Source != SourceProject || got.Description != "project override" {
		t.Errorf("List() \"default\" = %+v, want project-local override to win", got)
	}

	meta, err := LoadMeta("default")
	if err != nil {
		t.Fatalf("LoadMeta() error = %v", err)
	}
	if meta.Source != SourceProject || meta.Description != "project override" {
		t.Errorf("LoadMeta(default) = %+v, want project-local override to win", meta)
	}
}

func TestLoadMetaUserOverBuiltIn(t *testing.T) {
	userDir, projectDir := t.TempDir(), t.TempDir()
	withDirs(t, userDir, projectDir)

	writeTemplate(t, userDir, "default", "name: default\ndescription: user override, no project override present\ncpp_version: 20\n", nil)

	meta, err := LoadMeta("default")
	if err != nil {
		t.Fatalf("LoadMeta() error = %v", err)
	}
	if meta.Source != SourceUser {
		t.Errorf("LoadMeta(default).Source = %q, want SourceUser", meta.Source)
	}
}

func TestListSkipsMalformedDiskTemplates(t *testing.T) {
	userDir, projectDir := t.TempDir(), t.TempDir()
	withDirs(t, userDir, projectDir)

	writeTemplate(t, userDir, "broken", "not: [valid: yaml", nil)
	writeTemplate(t, userDir, "good", "name: good\ndescription: fine\ncpp_version: 20\n", nil)

	metas, err := List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	var sawGood, sawBroken bool
	for _, m := range metas {
		if m.Name == "good" {
			sawGood = true
		}
		if m.Name == "broken" {
			sawBroken = true
		}
	}
	if !sawGood {
		t.Error("expected the well-formed \"good\" user template to still be listed")
	}
	if sawBroken {
		t.Error("expected the malformed \"broken\" user template to be skipped, not listed")
	}
}

func TestListNoUserOrProjectDirsIsFine(t *testing.T) {
	withDirs(t, filepath.Join(t.TempDir(), "does-not-exist"), filepath.Join(t.TempDir(), "also-missing"))

	metas, err := List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(metas) == 0 {
		t.Error("expected at least the built-in templates when no user/project dirs exist")
	}
}

func TestWriteFilesFromDisk(t *testing.T) {
	userDir, projectDir := t.TempDir(), t.TempDir()
	withDirs(t, userDir, projectDir)

	writeTemplate(t, userDir, "my-template", "name: my-template\ndescription: d\ncpp_version: 20\n", map[string]string{
		"src/main.cpp":       "int main() { return 0; }\n",
		"include/helper.hpp": "#pragma once\n",
	})

	meta, err := LoadMeta("my-template")
	if err != nil {
		t.Fatalf("LoadMeta() error = %v", err)
	}

	dest := t.TempDir()
	if err := WriteFiles(meta, dest); err != nil {
		t.Fatalf("WriteFiles() error = %v", err)
	}

	for _, rel := range []string{"src/main.cpp", "include/helper.hpp"} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
			t.Errorf("expected %s to be copied: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "meta.yaml")); !os.IsNotExist(err) {
		t.Error("meta.yaml itself should not be copied into the destination")
	}
}

func TestWriteFilesFromEmbedded(t *testing.T) {
	// No user/project override for "default" - WriteFiles should fall back
	// to the embedded filesystem.
	withDirs(t, t.TempDir(), t.TempDir())

	meta, err := LoadMeta("default")
	if err != nil {
		t.Fatalf("LoadMeta() error = %v", err)
	}
	if meta.Source != SourceBuiltIn {
		t.Fatalf("expected the built-in default template, got Source=%q", meta.Source)
	}

	dest := t.TempDir()
	if err := WriteFiles(meta, dest); err != nil {
		t.Fatalf("WriteFiles() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "src", "main.cpp")); err != nil {
		t.Errorf("expected the embedded default template's src/main.cpp to be copied: %v", err)
	}
}
