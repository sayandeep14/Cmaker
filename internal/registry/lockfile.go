package registry

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

// Lockfile is cmaker.lock's shape: modeled after Cargo.lock/
// package-lock.json - human-diffable, meant to be checked into git and
// regenerated, not hand-edited.
type Lockfile struct {
	Dependencies map[string]LockEntry `yaml:"dependencies"`
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

	lf := Lockfile{Dependencies: map[string]LockEntry{}}
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
