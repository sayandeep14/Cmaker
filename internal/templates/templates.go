// Package templates discovers and materializes cmaker's embedded project
// templates (templates/<name>/meta.yaml + source files).
package templates

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"cmaker/internal/config"
)

//go:embed templates
var templateFS embed.FS

const templatesRoot = "templates"

// Meta describes a scaffoldable project template. It's read from
// templates/<name>/meta.yaml - adding a template is "drop a directory in
// templates/", not "edit Go source and recompile."
type Meta struct {
	Name          string              `yaml:"name"`
	Description   string              `yaml:"description"`
	CppVersion    int                 `yaml:"cpp_version"`
	Dependencies  []config.Dependency `yaml:"dependencies"`
	LinkLibraries []string            `yaml:"link_libraries"`
	CMakeExtra    string              `yaml:"cmake_extra"` // raw CMake, for deps needing more than CPMAddPackage+target_link_libraries (see the ml-eigen template)
	Testing       bool                `yaml:"testing"`     // if true, scaffolded projects get testing.enabled: true (ctest wiring) - see the catch2 template
}

// List returns every embedded template's metadata, sorted by name, with
// "default" always first since it's the natural starting point.
func List() ([]Meta, error) {
	entries, err := templateFS.ReadDir(templatesRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded templates: %w", err)
	}

	var metas []Meta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, err := LoadMeta(e.Name())
		if err != nil {
			return nil, err
		}
		metas = append(metas, meta)
	}

	sort.Slice(metas, func(i, j int) bool {
		if metas[i].Name == "default" {
			return true
		}
		if metas[j].Name == "default" {
			return false
		}
		return metas[i].Name < metas[j].Name
	})
	return metas, nil
}

// LoadMeta reads templates/<name>/meta.yaml.
func LoadMeta(name string) (Meta, error) {
	data, err := templateFS.ReadFile(filepath.Join(templatesRoot, name, "meta.yaml"))
	if err != nil {
		return Meta{}, fmt.Errorf("template %q has no meta.yaml: %w", name, err)
	}
	var meta Meta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return Meta{}, fmt.Errorf("template %q has malformed meta.yaml: %w", name, err)
	}
	if meta.Name == "" {
		meta.Name = name
	}
	return meta, nil
}

// WriteFiles copies every file in templates/<name>/ (except meta.yaml) into
// destRoot, preserving the relative directory structure.
func WriteFiles(name string, destRoot string) error {
	srcRoot := filepath.Join(templatesRoot, name)
	return fs.WalkDir(templateFS, srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		if rel == "." || rel == "meta.yaml" {
			return nil
		}

		destPath := filepath.Join(destRoot, rel)
		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		data, err := templateFS.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}
		return os.WriteFile(destPath, data, 0644)
	})
}
