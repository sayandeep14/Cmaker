// Package registry implements cmaker's built-in package registry (§17): a
// small, curated index of well-behaved CPM/CMake-friendly libraries (see
// entries.yaml), plus the lockfile logic (cmaker.lock) that pins the exact
// commit CPM resolved for each dependency. It has no CLI concerns - that
// wrapping lives in package cmd, mirroring internal/config/internal/cmake's
// split.
package registry

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"cmaker/internal/config"
)

//go:embed entries.yaml
var entriesYAML []byte

// Entry describes one registry-listed library.
type Entry struct {
	Name       string   `yaml:"name"`
	Repo       string   `yaml:"repo"`
	DefaultTag string   `yaml:"default_tag"`
	Link       []string `yaml:"link"`
	Options    []string `yaml:"options,omitempty"`
	Notes      string   `yaml:"notes"`
}

// entries is parsed once at package init - the registry is a fixed,
// embedded list, not something loaded per call.
var entries = mustParseEntries()

func mustParseEntries() []Entry {
	var e []Entry
	if err := yaml.Unmarshal(entriesYAML, &e); err != nil {
		panic(fmt.Sprintf("internal/registry: malformed entries.yaml: %v", err))
	}
	sort.Slice(e, func(i, j int) bool { return e[i].Name < e[j].Name })
	return e
}

// List returns every registry entry, sorted by name.
func List() []Entry {
	return entries
}

// Find looks up name (case-insensitive, exact match).
func Find(name string) (Entry, bool) {
	for _, e := range entries {
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
	for _, e := range entries {
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
	for _, e := range entries {
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
