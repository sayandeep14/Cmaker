// Package registry implements cmaker's package registry (§17): a small,
// curated index of well-behaved CPM/CMake-friendly libraries built into the
// binary (see entries.yaml), merged with an optional user-local overlay
// (~/.cmaker/registry.yaml, §23) so someone can `cmaker install` their own
// or their team's internal libraries without waiting on a cmaker release to
// add them to the built-in index. Also home to the lockfile logic
// (cmaker.lock) that pins the exact commit CPM resolved for each
// dependency. It has no CLI concerns - that wrapping lives in package cmd,
// mirroring internal/config/internal/cmake's split.
package registry

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"cmaker/internal/config"
)

//go:embed entries.yaml
var entriesYAML []byte

// Source records where an Entry was discovered from - purely informational
// (surfaced by `cmaker search`), not part of registry.yaml itself.
type Source string

const (
	SourceBuiltIn Source = "built-in"
	SourceUser    Source = "user (~/.cmaker/registry.yaml)"
)

// Entry describes one registry-listed library.
type Entry struct {
	Name       string   `yaml:"name"`
	Repo       string   `yaml:"repo"`
	DefaultTag string   `yaml:"default_tag"`
	Link       []string `yaml:"link"`
	Options    []string `yaml:"options,omitempty"`
	Notes      string   `yaml:"notes"`

	Source Source `yaml:"-"` // set by the loader, never read from registry.yaml itself
}

// builtInEntries is parsed once at package init - the embedded content
// never changes at runtime, unlike the user overlay.
var builtInEntries = mustParseBuiltInEntries()

func mustParseBuiltInEntries() []Entry {
	var e []Entry
	if err := yaml.Unmarshal(entriesYAML, &e); err != nil {
		panic(fmt.Sprintf("internal/registry: malformed entries.yaml: %v", err))
	}
	for i := range e {
		e[i].Source = SourceBuiltIn
	}
	return e
}

// userRegistryPath is a package var (not a const) so tests can point it at
// a temp file instead of a real $HOME/.cmaker/registry.yaml.
var userRegistryPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cmaker", "registry.yaml")
}

// loadUserEntries reads the user-local registry overlay, if present. A
// missing file (the common case - most users have no overlay at all) or a
// malformed one is not an error: registry lookups shouldn't hard-fail
// because of one bad entry in an optional personal file, so this is
// deliberately best-effort, mirroring internal/config.TryLoad's philosophy.
func loadUserEntries() []Entry {
	path := userRegistryPath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entries []Entry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil
	}
	for i := range entries {
		entries[i].Source = SourceUser
	}
	return entries
}

// mergedEntries re-reads the user overlay on every call (cheap for the
// tiny sizes involved here) rather than caching it once, so a change to
// ~/.cmaker/registry.yaml is picked up without needing anything to be
// re-initialized - a user entry overrides a built-in one with the same
// name.
func mergedEntries() []Entry {
	byName := make(map[string]Entry, len(builtInEntries))
	for _, e := range builtInEntries {
		byName[e.Name] = e
	}
	for _, e := range loadUserEntries() {
		byName[e.Name] = e
	}
	merged := make([]Entry, 0, len(byName))
	for _, e := range byName {
		merged = append(merged, e)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Name < merged[j].Name })
	return merged
}

// List returns every registry entry (built-in + user overlay), sorted by
// name.
func List() []Entry {
	return mergedEntries()
}

// Find looks up name (case-insensitive, exact match).
func Find(name string) (Entry, bool) {
	for _, e := range mergedEntries() {
		if strings.EqualFold(e.Name, name) {
			return e, true
		}
	}
	return Entry{}, false
}

// Search returns every entry whose name or notes contains term
// (case-insensitive substring match).
func Search(term string) []Entry {
	term = strings.ToLower(term)
	var matches []Entry
	for _, e := range mergedEntries() {
		if strings.Contains(strings.ToLower(e.Name), term) || strings.Contains(strings.ToLower(e.Notes), term) {
			matches = append(matches, e)
		}
	}
	return matches
}

// CloseMatches returns registry names that might be what the caller meant
// by name (substring match either direction, or a small edit distance) -
// used to make an "unknown package" error actionable instead of a dead end.
func CloseMatches(name string) []string {
	lower := strings.ToLower(name)
	var matches []string
	for _, e := range mergedEntries() {
		entryLower := strings.ToLower(e.Name)
		if strings.Contains(entryLower, lower) || strings.Contains(lower, entryLower) || levenshtein(lower, entryLower) <= 2 {
			matches = append(matches, e.Name)
		}
	}
	return matches
}

// ToDependency converts a registry entry into a config.Dependency, ready to
// append to cmaker.yaml's dependencies: list.
func (e Entry) ToDependency() config.Dependency {
	return config.Dependency{
		Name:    e.Name,
		Repo:    e.Repo,
		Tag:     e.DefaultTag,
		Link:    e.Link,
		Options: e.Options,
	}
}

// levenshtein computes the classic edit distance between a and b, used only
// for short package-name typo suggestions (CloseMatches) - not performance
// sensitive.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}
