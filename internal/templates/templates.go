// Package templates discovers and materializes cmaker's project templates:
// the built-in ones embedded in the binary (templates/<name>/meta.yaml +
// source files), plus two on-disk extension points (§23) checked in
// addition to them - a project-local .cmaker/templates/ (for a team to
// commit shared templates into a repo) and a user-global
// ~/.cmaker/templates/ - so adding a template doesn't require a cmaker
// release. Same meta.yaml format either way; a name collision is resolved
// project-local > user-global > built-in, so a project or user can
// deliberately override a built-in template's name.
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

// Source records where a Meta was discovered from - purely informational
// (surfaced by `cmaker templates`), not part of meta.yaml itself.
type Source string

const (
	SourceBuiltIn Source = "built-in"
	SourceUser    Source = "user (~/.cmaker/templates)"
	SourceProject Source = "project (.cmaker/templates)"
)

// Meta describes a scaffoldable project template. It's read from a
// <name>/meta.yaml - adding a template is "drop a directory with a
// meta.yaml," not "edit Go source and recompile," whether that directory
// lives in the embedded templates/, ~/.cmaker/templates/, or
// ./.cmaker/templates/.
type Meta struct {
	Name          string              `yaml:"name"`
	Description   string              `yaml:"description"`
	CppVersion    int                 `yaml:"cpp_version"`
	Dependencies  []config.Dependency `yaml:"dependencies"`
	LinkLibraries []string            `yaml:"link_libraries"`
	CMakeExtra    string              `yaml:"cmake_extra"` // raw CMake, for deps needing more than CPMAddPackage+target_link_libraries (see the ml-eigen template)
	Testing       bool                `yaml:"testing"`     // if true, scaffolded projects get testing.enabled: true (ctest wiring) - see the catch2 template

	Source   Source `yaml:"-"` // set by the loader, never read from meta.yaml itself
	diskPath string // "" for a built-in template; the directory to copy files from otherwise
}

// userTemplatesDir/projectTemplatesDir are package vars (not consts) so
// tests can point them at a temp directory instead of a real
// $HOME/./.cmaker/templates.
var (
	userTemplatesDir = func() string {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, ".cmaker", "templates")
	}
	projectTemplatesDir = func() string {
		return filepath.Join(".cmaker", "templates")
	}
)

// List returns every available template's metadata - built-in plus any
// user-global/project-local ones - sorted by name, with "default" always
// first since it's the natural starting point. A name present in more than
// one source resolves project-local > user-global > built-in; malformed or
// unreadable user/project templates are skipped rather than failing the
// whole list (a typo in someone's personal template shouldn't break
// `cmaker templates` for the built-in ones).
func List() ([]Meta, error) {
	builtIn, err := listEmbedded()
	if err != nil {
		return nil, err
	}

	byName := make(map[string]Meta, len(builtIn))
	for _, m := range builtIn {
		byName[m.Name] = m
	}
	for _, m := range listDisk(userTemplatesDir(), SourceUser) {
		byName[m.Name] = m
	}
	for _, m := range listDisk(projectTemplatesDir(), SourceProject) {
		byName[m.Name] = m
	}

	metas := make([]Meta, 0, len(byName))
	for _, m := range byName {
		metas = append(metas, m)
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

// LoadMeta resolves name to a Meta, checking project-local, then
// user-global, then built-in - the same precedence List() merges by.
func LoadMeta(name string) (Meta, error) {
	if m, ok := loadDiskMeta(projectTemplatesDir(), name, SourceProject); ok {
		return m, nil
	}
	if m, ok := loadDiskMeta(userTemplatesDir(), name, SourceUser); ok {
		return m, nil
	}
	return loadEmbeddedMeta(name)
}

// WriteFiles copies every file in meta's template directory (except
// meta.yaml) into destRoot, preserving the relative directory structure -
// from disk if meta came from a user/project template dir, from the
// embedded filesystem otherwise.
func WriteFiles(meta Meta, destRoot string) error {
	if meta.diskPath != "" {
		return writeFilesFromDisk(meta.diskPath, destRoot)
	}
	return writeFilesFromEmbedded(meta.Name, destRoot)
}

func listEmbedded() ([]Meta, error) {
	entries, err := templateFS.ReadDir(templatesRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded templates: %w", err)
	}

	var metas []Meta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, err := loadEmbeddedMeta(e.Name())
		if err != nil {
			return nil, err
		}
		metas = append(metas, meta)
	}
	return metas, nil
}

func loadEmbeddedMeta(name string) (Meta, error) {
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
	meta.Source = SourceBuiltIn
	return meta, nil
}

func writeFilesFromEmbedded(name string, destRoot string) error {
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

// listDisk returns every template found directly under dir (one
// subdirectory per template, same shape as the embedded templates/) -
// silently empty if dir doesn't exist, since a project/user having no
// extension templates at all is the common case, not an error.
func listDisk(dir string, source Source) []Meta {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var metas []Meta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if m, ok := loadDiskMeta(dir, e.Name(), source); ok {
			metas = append(metas, m)
		}
	}
	return metas
}

func loadDiskMeta(dir, name string, source Source) (Meta, bool) {
	if dir == "" {
		return Meta{}, false
	}
	templateDir := filepath.Join(dir, name)
	data, err := os.ReadFile(filepath.Join(templateDir, "meta.yaml"))
	if err != nil {
		return Meta{}, false
	}
	var meta Meta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return Meta{}, false
	}
	if meta.Name == "" {
		meta.Name = name
	}
	meta.Source = source
	meta.diskPath = templateDir
	return meta, true
}

func writeFilesFromDisk(srcRoot, destRoot string) error {
	return filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, err error) error {
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

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}
		return os.WriteFile(destPath, data, 0644)
	})
}
