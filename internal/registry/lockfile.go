package registry

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"cmaker/internal/config"
)

// LockEntry pins the exact commit CPM resolved for one dependency, since a
// mutable tag/branch (or a tag someone force-moved) can otherwise silently
// change what gets built between machines or CI runs without cmaker.yaml
// itself changing at all.
type LockEntry struct {
	Repo   string `yaml:"repo"`
	Tag    string `yaml:"tag"`
	Commit string `yaml:"commit"`
}

// PackLock pins the exact version+checksum installed for a pack (see
// PACKS_PLAN.md) - a different shape from LockEntry deliberately:
// installing a pack means extracting files per its manifest, not
// resolving a CPM-fetched commit, so there's no "repo"/"commit" to
// record, only what was actually installed and when.
type PackLock struct {
	Version        string    `yaml:"version"`
	ChecksumSHA256 string    `yaml:"checksum_sha256"`
	InstalledAt    time.Time `yaml:"installed_at"`
}

// Lockfile is cmaker.lock's shape: modeled after Cargo.lock/
// package-lock.json - human-diffable, meant to be checked into git and
// regenerated, not hand-edited. Packs is a sibling section alongside
// Dependencies, not a second lockfile - one file records everything
// pinned for the project, even though a pack entry is a structurally
// different shape from a CPM dependency entry.
type Lockfile struct {
	Dependencies map[string]LockEntry `yaml:"dependencies"`
	Packs        map[string]PackLock  `yaml:"packs,omitempty"`
}

const lockfileName = "cmaker.lock"

// LoadLockfile reads root/cmaker.lock. A missing file is not an error - it
// just means nothing's been locked yet (e.g. before the first build).
func LoadLockfile(root string) (Lockfile, error) {
	data, err := os.ReadFile(filepath.Join(root, lockfileName))
	if os.IsNotExist(err) {
		return Lockfile{Dependencies: map[string]LockEntry{}}, nil
	}
	if err != nil {
		return Lockfile{}, err
	}
	var lf Lockfile
	if err := yaml.Unmarshal(data, &lf); err != nil {
		return Lockfile{}, fmt.Errorf("%s is malformed: %w", lockfileName, err)
	}
	if lf.Dependencies == nil {
		lf.Dependencies = map[string]LockEntry{}
	}
	return lf, nil
}

// SaveLockfile writes lf to root/cmaker.lock.
func SaveLockfile(root string, lf Lockfile) error {
	data, err := yaml.Marshal(&lf)
	if err != nil {
		return fmt.Errorf("failed to marshal %s: %w", lockfileName, err)
	}
	return os.WriteFile(filepath.Join(root, lockfileName), data, 0644)
}

// depSourceDir returns CMake FetchContent's populated source directory for
// a CPMAddPackage'd dependency: FetchContent lowercases the NAME it was
// declared with for the on-disk directory, regardless of the case used in
// cmaker.yaml or the CMake target name itself (e.g. NAME Catch2 populates
// _deps/catch2-src, not _deps/Catch2-src).
func depSourceDir(buildDir, depName string) string {
	return filepath.Join(buildDir, "_deps", strings.ToLower(depName)+"-src")
}

// resolvedCommit reads the exact commit CPM checked out for depName by
// asking git directly inside its populated source directory - the tag in
// cmaker.yaml only says what was requested, this says what was actually
// fetched.
func resolvedCommit(buildDir, depName string) (string, error) {
	dir := depSourceDir(buildDir, depName)
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("dependency %q hasn't been fetched yet (expected %s) - run 'cmaker build' first", depName, dir)
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("failed to resolve the commit git checked out for %q: %w", depName, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// UpdateLockfile regenerates root/cmaker.lock from cfg.Dependencies, using
// buildDir (already configured, so each dependency's source has actually
// been fetched) to resolve each one's exact commit. Dependencies whose
// source isn't populated yet (e.g. a configure that failed partway) are
// left out of the lock rather than failing the whole update - a partial
// lock is still useful, and this runs best-effort after every configure
// (see cmd/build.go, cmd/install.go), not as a hard gate.
func UpdateLockfile(root string, buildDir string, cfg config.Config) error {
	if len(cfg.Dependencies) == 0 {
		return nil
	}

	// Loaded first (rather than starting from a blank Lockfile) purely to
	// carry Packs forward - this function only ever regenerates the
	// Dependencies section from cfg, and previously discarded Packs
	// entirely on every call, silently wiping out anything RecordPack had
	// written the moment the next 'cmaker build'/'install'/'uninstall' ran.
	existing, err := LoadLockfile(root)
	if err != nil {
		existing = Lockfile{}
	}
	lf := Lockfile{Dependencies: map[string]LockEntry{}, Packs: existing.Packs}
	var firstErr error
	for _, dep := range cfg.Dependencies {
		commit, err := resolvedCommit(buildDir, dep.Name)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		lf.Dependencies[dep.Name] = LockEntry{Repo: dep.Repo, Tag: dep.Tag, Commit: commit}
	}

	if err := SaveLockfile(root, lf); err != nil {
		return err
	}
	return firstErr
}

// RecordPack pins name@version (and its checksum) into root/cmaker.lock's
// Packs section - called once a pack install has actually finished
// extracting successfully (see cmd/install.go). Loads and re-saves the
// whole lockfile rather than a targeted in-place edit, same as every
// other lockfile writer here.
func RecordPack(root, name, version, checksumSHA256 string) error {
	lf, err := LoadLockfile(root)
	if err != nil {
		return err
	}
	if lf.Packs == nil {
		lf.Packs = map[string]PackLock{}
	}
	lf.Packs[name] = PackLock{Version: version, ChecksumSHA256: checksumSHA256, InstalledAt: time.Now().UTC()}
	return SaveLockfile(root, lf)
}

// RemovePack deletes name from root/cmaker.lock's Packs section - a
// no-op, not an error, if it wasn't there.
func RemovePack(root, name string) error {
	lf, err := LoadLockfile(root)
	if err != nil {
		return err
	}
	if _, ok := lf.Packs[name]; !ok {
		return nil
	}
	delete(lf.Packs, name)
	return SaveLockfile(root, lf)
}
