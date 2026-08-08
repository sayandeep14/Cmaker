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

// Kind mirrors config.DependencyKind (§27) - kept as its own type rather
// than importing config.DependencyKind directly, matching Entry.ToDependency
// already being the one place this package talks to package config, so a
// registry.yaml author never needs to know config's Go types exist.
type Kind string

const (
	KindCPM             Kind = ""                 // the original, still-default shape - repo/default_tag/CPMAddPackage
	KindSystemPackage   Kind = "system_package"   // installed via the machine's package manager, wired in via find_package
	KindPrebuiltArchive Kind = "prebuilt_archive" // a platform-matched prebuilt binary release, downloaded+extracted at configure time
	KindPkgConfig       Kind = "pkg_config"       // installed via the machine's package manager, wired in via pkg-config (for libraries with no CMake package config at all - confirmed live: GTK/GTKmm)
)

// Entry describes one registry-listed library. Which fields apply depends
// on Kind - see config.DependencyKind's doc for what each shape actually
// does at configure time; this struct just carries the same data through
// registry.yaml/entries.yaml.
type Entry struct {
	Name       string   `yaml:"name"`
	Kind       Kind     `yaml:"kind,omitempty"`
	Repo       string   `yaml:"repo,omitempty"`        // cpm only
	DefaultTag string   `yaml:"default_tag,omitempty"` // cpm only
	Link       []string `yaml:"link"`
	Options    []string `yaml:"options,omitempty"` // cpm only
	Notes      string   `yaml:"notes"`

	// DownloadOnly (cpm only) - see config.Dependency.DownloadOnly. Used
	// together with PostFetchExtra when the library's own CMakeLists.txt
	// (if it even has one) isn't meant to be add_subdirectory'd directly
	// (e.g. eigen, imgui - see their template's own meta.yaml for the
	// same pattern this mirrors).
	DownloadOnly bool `yaml:"download_only,omitempty"`
	// PostFetchExtra (cpm only) - see config.Dependency.PostFetchExtra.
	PostFetchExtra string `yaml:"post_fetch_extra,omitempty"`
	// Requires names other registry entries that must be installed first
	// (in order, before this one) for this entry to actually work - e.g.
	// crow requires asio (Crow's own CMakeLists.txt calls
	// find_package(asio REQUIRED) with no fetch mechanism of its own),
	// imgui requires glfw (imgui's hand-built target links against it).
	// cmd/install.go resolves this transitively, skipping anything
	// already present in cmaker.yaml rather than erroring on it.
	Requires []string `yaml:"requires,omitempty"`

	// FindPackage (system_package only) is the CMake find_package() name.
	FindPackage string `yaml:"find_package,omitempty"`
	// PkgConfigModule (pkg_config only) is the .pc module name (e.g.
	// "gtkmm-4.0") - see config.Dependency.PkgConfigModule.
	PkgConfigModule string `yaml:"pkg_config_module,omitempty"`
	// PackageManagers (system_package and pkg_config only) maps a
	// package-manager id ("brew", "apt", "pacman") to the package name that
	// manager should install - cmaker install picks whichever manager is
	// actually found on PATH, checked in a fixed preference order (see
	// cmd/install.go). "pacman" is MSYS2's package manager on Windows
	// specifically (mingw-w64-ucrt-x86_64-* packages, matching the
	// MinGW-w64/UCRT compiler toolchain the Windows installer sets up) -
	// only ever attempted on GOOS=="windows", never on a native Linux
	// pacman (Arch etc.), since the mingw-w64-ucrt-x86_64- prefixed package
	// names are MSYS2-specific and wouldn't resolve there.
	PackageManagers map[string]string `yaml:"package_managers,omitempty"`

	// ArchiveURLTemplate (prebuilt_archive only) is a download URL
	// containing a "{platform}" placeholder, resolved against
	// PlatformNames (below) for the current GOOS/GOARCH before it's ever
	// written to cmaker.yaml.
	ArchiveURLTemplate string `yaml:"archive_url_template,omitempty"`
	// PlatformNames (prebuilt_archive only) maps "<GOOS>/<GOARCH>" (e.g.
	// "darwin/arm64") to whatever platform token that vendor's own release
	// asset naming uses (e.g. "osx-arm64") - every vendor names these
	// differently, so this has to be data, not a hardcoded Go mapping.
	PlatformNames     map[string]string `yaml:"platform_names,omitempty"`
	ArchiveIncludeDir string            `yaml:"archive_include_dir,omitempty"` // prebuilt_archive only
	ArchiveLibDir     string            `yaml:"archive_lib_dir,omitempty"`     // prebuilt_archive only

	Source Source `yaml:"-"` // set by the loader, never read from registry.yaml itself
}

// ResolveArchiveURL substitutes "{platform}" in ArchiveURLTemplate using
// PlatformNames for goos/goarch (normally runtime.GOOS/runtime.GOARCH -
// passed in rather than read directly so this stays unit-testable across
// platforms). Returns an error naming exactly which platform is
// unsupported, rather than silently producing a broken URL.
func (e Entry) ResolveArchiveURL(goos, goarch string) (string, error) {
	key := goos + "/" + goarch
	platform, ok := e.PlatformNames[key]
	if !ok {
		return "", fmt.Errorf("%q has no known prebuilt archive for %s - supported: %s", e.Name, key, strings.Join(sortedKeys(e.PlatformNames), ", "))
	}
	return strings.ReplaceAll(e.ArchiveURLTemplate, "{platform}", platform), nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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

// ToDependency converts a cpm or system_package registry entry into a
// config.Dependency, ready to append to cmaker.yaml's dependencies: list.
// Both kinds need no extra runtime info to convert - a system_package
// entry's actual package-manager install is expected to have already run
// (see cmd/install.go) before this is called, same as a cpm entry's actual
// fetch doesn't happen until the next `cmake` configure either way.
//
// A prebuilt_archive entry can't be converted this way - its ArchiveURL
// needs the current platform resolved first (ResolveArchiveURL), which
// this method deliberately doesn't do implicitly; use ToDependencyForArchive
// with the already-resolved URL instead. Calling this on a prebuilt_archive
// entry is a caller bug, not a runtime condition - it panics rather than
// silently producing a Dependency with no ArchiveURL.
func (e Entry) ToDependency() config.Dependency {
	switch e.Kind {
	case KindSystemPackage:
		return config.Dependency{
			Name:        e.Name,
			Kind:        config.DependencyKindSystemPackage,
			FindPackage: e.FindPackage,
			Link:        e.Link,
		}
	case KindPkgConfig:
		return config.Dependency{
			Name:            e.Name,
			Kind:            config.DependencyKindPkgConfig,
			PkgConfigModule: e.PkgConfigModule,
			Link:            e.Link,
		}
	case KindPrebuiltArchive:
		panic("registry: ToDependency called on a prebuilt_archive entry - use ToDependencyForArchive with a ResolveArchiveURL result instead")
	default:
		return config.Dependency{
			Name:           e.Name,
			Kind:           config.DependencyKindCPM,
			Repo:           e.Repo,
			Tag:            e.DefaultTag,
			Link:           e.Link,
			Options:        e.Options,
			DownloadOnly:   e.DownloadOnly,
			PostFetchExtra: e.PostFetchExtra,
		}
	}
}

// ToDependencyForArchive converts a prebuilt_archive registry entry into a
// config.Dependency using resolvedURL (see ResolveArchiveURL) - kept
// separate from ToDependency because resolving the URL requires the
// caller's own GOOS/GOARCH, which this package deliberately doesn't read
// directly (see ResolveArchiveURL's own doc).
func (e Entry) ToDependencyForArchive(resolvedURL string) config.Dependency {
	return config.Dependency{
		Name:              e.Name,
		Kind:              config.DependencyKindPrebuiltArchive,
		ArchiveURL:        resolvedURL,
		ArchiveIncludeDir: e.ArchiveIncludeDir,
		ArchiveLibDir:     e.ArchiveLibDir,
		Link:              e.Link,
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
