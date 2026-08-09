package packclient

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// ManifestFileName is what cmaker publish looks for at the root of the
// directory being published, and what `cmaker new <name> --pack` writes
// (see PACKS_PLAN.md §4).
const ManifestFileName = "manifest.yaml"

// namePattern mirrors the server's own validation
// (server/internal/api/packs.go's namePattern) - a name/version that
// would be rejected by the server is caught here first, before any
// network round trip.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ManifestDependency is one entry in Manifest.Dependencies - either a
// built-in registry entry (resolved via cmaker's existing
// internal/registry, source: "registry") or another pack (source:
// "pack", exact version pin only - no range resolution in the POC).
type ManifestDependency struct {
	Name    string `yaml:"name" json:"name"`
	Source  string `yaml:"source" json:"source"`
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
}

// ManifestPlacement is one src->dest file placement rule, applied when
// the pack is installed into a project - explicit rather than inferred,
// mirroring internal/registry.Entry.Link's own "author states it
// directly" convention. If Manifest.Placement is empty, the whole
// tarball is extracted preserving relative paths instead.
type ManifestPlacement struct {
	Src  string `yaml:"src" json:"src"`
	Dest string `yaml:"dest" json:"dest"`
}

// Manifest is manifest.yaml's shape - see PACKS_PLAN.md §4. Carries both
// yaml tags (the on-disk manifest.yaml format) and matching json tags
// (cmaker publish sends this same struct as the server's `manifest`
// field, and cmaker install will eventually parse it back out of the
// server's JSON response - without explicit json tags here,
// encoding/json silently falls back to Go's capitalized field names
// instead of the documented lowercase shape, a real mismatch caught live
// against the deployed server).
type Manifest struct {
	Name         string               `yaml:"name" json:"name"`
	Version      string               `yaml:"version" json:"version"`
	Description  string               `yaml:"description,omitempty" json:"description,omitempty"`
	License      string               `yaml:"license,omitempty" json:"license,omitempty"`
	Kind         string               `yaml:"kind,omitempty" json:"kind,omitempty"` // function | class | directory
	Authors      []string             `yaml:"authors,omitempty" json:"authors,omitempty"`
	Dependencies []ManifestDependency `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	Placement    []ManifestPlacement  `yaml:"placement,omitempty" json:"placement,omitempty"`
	Notes        string               `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// LoadManifest reads and validates dir/manifest.yaml.
func LoadManifest(dir string) (Manifest, error) {
	path := filepath.Join(dir, ManifestFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("failed to read %s: %w", path, err)
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// SaveManifest writes m to dir/manifest.yaml.
func SaveManifest(dir string, m Manifest) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to encode manifest: %w", err)
	}
	path := filepath.Join(dir, ManifestFileName)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

// Validate checks the fields cmaker publish and the server both actually
// depend on - not a full schema validator, just enough to fail locally
// with a clear message instead of a confusing 400 from the server.
func (m Manifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("manifest: name is required")
	}
	if !namePattern.MatchString(m.Name) {
		return fmt.Errorf("manifest: name %q must match ^[A-Za-z0-9._-]+$", m.Name)
	}
	if m.Version == "" {
		return fmt.Errorf("manifest: version is required")
	}
	if !namePattern.MatchString(m.Version) {
		return fmt.Errorf("manifest: version %q must match ^[A-Za-z0-9._-]+$", m.Version)
	}
	for _, d := range m.Dependencies {
		if d.Name == "" {
			return fmt.Errorf("manifest: a dependency is missing a name")
		}
		if d.Source != "registry" && d.Source != "pack" {
			return fmt.Errorf("manifest: dependency %q has source %q, want \"registry\" or \"pack\"", d.Name, d.Source)
		}
		if d.Source == "pack" && d.Version == "" {
			return fmt.Errorf("manifest: pack dependency %q needs a version pin (source: pack requires an exact version)", d.Name)
		}
	}
	return nil
}
