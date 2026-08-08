// Package config defines the cmaker.yaml schema and pure load/save/validate
// logic. It has no CLI concerns (no os.Exit, no colored output) - that
// wrapping lives in package cmd, so this package stays usable from tests and
// from other packages (internal/cmake, internal/templates, internal/tui)
// without dragging along CLI-exit behavior.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// CurrentSchemaVersion is the highest cmaker.yaml schema version this build
// understands. Configs omit schema_version entirely today (treated as 1);
// the field exists so a future breaking change to the config shape can
// detect and reject "this project needs a newer cmaker" instead of silently
// misinterpreting old/new fields.
const CurrentSchemaVersion = 1

// Config structure for cmaker.yaml
type Config struct {
	ProjectName      string            `yaml:"project_name"`
	SchemaVersion    int               `yaml:"schema_version,omitempty"`
	Language         string            `yaml:"language,omitempty"`    // cpp | c | hybrid (default cpp)
	TargetType       string            `yaml:"target_type,omitempty"` // executable | static_library | shared_library (default executable)
	CppVersion       int               `yaml:"cpp_version,omitempty"`
	CVersion         int               `yaml:"c_version,omitempty"`
	Executable       string            `yaml:"executable"`
	IncludeDirs      []string          `yaml:"include_dirs"`
	LinkLibraries    []string          `yaml:"libraries"`
	Dependencies     []Dependency      `yaml:"dependencies"`
	Compiler         string            `yaml:"compiler,omitempty"`   // e.g. clang++-17, /opt/homebrew/opt/llvm/bin/clang++
	Runner           string            `yaml:"runner,omitempty"`     // custom program invoked as `<runner> <file> [args...]` for 'run --only', replacing the default compile-then-run flow entirely (e.g. crun, which compiles and runs in one step); takes priority over 'compiler' when set
	Sanitizers       []string          `yaml:"sanitizers,omitempty"` // e.g. [address, undefined]
	WarningsAsErrors bool              `yaml:"warnings_as_errors,omitempty"`
	CMakeExtra       string            `yaml:"cmake_extra,omitempty"`    // raw CMake appended verbatim, for cases the generator doesn't model
	Configs          map[string]string `yaml:"configs,omitempty"`        // named shortcuts, e.g. {test: "run --only=test1.cpp"}, run via `cmaker <name>`
	Rust             *RustConfig       `yaml:"rust,omitempty"`           // opt-in Rust crate wired into the build
	Zig              *ZigConfig        `yaml:"zig,omitempty"`            // opt-in Zig library wired into the build
	Testing          *TestingConfig    `yaml:"testing,omitempty"`        // opt-in ctest wiring for the main executable
	DisableCcache    bool              `yaml:"disable_ccache,omitempty"` // opt out of the automatic ccache/sccache CMAKE_<LANG>_COMPILER_LAUNCHER wiring (on by default when either is found on PATH)
	LogsKeep         int               `yaml:"logs_keep,omitempty"`      // how many .cmaker/logs/ build/run logs to retain before pruning (default 5, see internal/logs.DefaultKeep)
	Coverage         bool              `yaml:"coverage,omitempty"`       // opt-in --coverage instrumentation (gcov-compatible, works with gcc and clang alike), consumed by 'cmaker coverage'
	Workspace        *WorkspaceConfig  `yaml:"workspace,omitempty"`      // opt-in workspace/monorepo root (§21) - a workspace root cmaker.yaml declares no target of its own, only member project directories
}

// WorkspaceConfig turns a cmaker.yaml into a workspace root: it declares no
// target of its own (no Executable/TargetType), only a list of member
// project directories, each with its own ordinary cmaker.yaml. Members are
// wired together via CMake's own add_subdirectory, not a second CPM-style
// fetch of something already sitting in the repo - so a member that depends
// on a sibling library member just lists that sibling's target name in its
// own 'libraries:', the same way it would link any other CMake target.
//
// Member order matters: CMake requires a target to already exist by the
// time target_link_libraries references it, so a library member must be
// listed before any member that links against it.
type WorkspaceConfig struct {
	Members []string `yaml:"members"`
}

// RustConfig describes an optional Rust crate compiled via cargo and linked
// into the main executable as a static library, exposed through a plain C
// ABI (extern "C") - this is strictly opt-in: a project with Rust == nil
// pays zero cost (no extra generated CMake, no doctor checks).
type RustConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CrateDir string `yaml:"crate_dir,omitempty"` // default "rust"
}

// ZigConfig describes an optional Zig source file compiled via
// `zig build-lib` and linked into the main executable as a static library,
// exposed through a plain C ABI (export fn) - strictly opt-in, same as Rust.
type ZigConfig struct {
	Enabled bool   `yaml:"enabled"`
	SrcDir  string `yaml:"src_dir,omitempty"` // default "zig"
}

// TestingConfig opts a project into ctest wiring: `cmake.Generate` emits
// `enable_testing()` plus an `add_test()` registering the main executable
// itself as a test run (the model every current test-oriented template,
// e.g. catch2, actually uses - the executable *is* the test binary). This
// is strictly opt-in, same as Rust/Zig: Testing == nil costs nothing.
type TestingConfig struct {
	Enabled bool `yaml:"enabled"`
}

// DependencyKind selects how a Dependency is acquired and wired into the
// generated CMakeLists.txt (§27). "" (the zero value) means DependencyKindCPM
// - every dependency before §27 was implicitly this shape, so an existing
// cmaker.yaml with no 'kind:' field keeps working unchanged.
type DependencyKind string

const (
	// DependencyKindCPM fetches Repo@Tag via CPMAddPackage - the original,
	// still-default shape (fmt, nlohmann-json, raylib, ...).
	DependencyKindCPM DependencyKind = "cpm"
	// DependencyKindSystemPackage shells out to the machine's own package
	// manager (Homebrew on macOS, apt on Debian/Ubuntu) to install a
	// system package, then wires it in via find_package(FindPackage
	// REQUIRED) instead of fetching source - for libraries realistically
	// installed system-wide rather than built from source most days
	// (OpenCV is the first real example; Boost is the same shape).
	DependencyKindSystemPackage DependencyKind = "system_package"
	// DependencyKindPrebuiltArchive downloads and extracts a
	// platform-matched prebuilt binary release archive at configure time
	// (via CMake's own file(DOWNLOAD)/file(ARCHIVE_EXTRACT), no external
	// tooling needed) and wires it in as a manually-constructed imported
	// target from the extracted include/lib directories - for SDKs
	// distributed as prebuilt binaries with no buildable-from-source CMake
	// project at all (ONNX Runtime's official releases are the first real
	// example; libtorch/TensorFlow's C API are the same shape).
	DependencyKindPrebuiltArchive DependencyKind = "prebuilt_archive"
	// DependencyKindPkgConfig shells out to the machine's own package
	// manager first, same as DependencyKindSystemPackage, but wires the
	// result in via pkg-config (find_package(PkgConfig) +
	// pkg_check_modules(... IMPORTED_TARGET ...)) instead of CMake's own
	// find_package(<name>) - for libraries that only ship a pkg-config
	// .pc file and no CMake package config at all (confirmed live: GTK/
	// GTKmm's real Homebrew formula ships gtkmm-4.0.pc and nothing
	// CMake-shaped whatsoever). The resulting CMake target is always
	// PkgConfig::<PKGCONFIGVAR>(see PkgConfigVar's own doc) - never
	// find_package's more familiar Namespace::target shape, since
	// pkg_check_modules doesn't know the upstream project's own naming
	// conventions the way a real CMake config package would.
	DependencyKindPkgConfig DependencyKind = "pkg_config"
)

// KindOrDefault returns kind with the DependencyKindCPM default applied,
// since the field is omitted (empty string) on every dependency written
// before §27 - this keeps old cmaker.yaml files working unchanged.
func (d Dependency) KindOrDefault() DependencyKind {
	if d.Kind == "" {
		return DependencyKindCPM
	}
	return d.Kind
}

// ResolveArchiveURLTemplate resolves d's ArchiveURLTemplate/PlatformNames
// (see their own doc) against goos/goarch (normally runtime.GOOS/
// runtime.GOARCH, passed in rather than read directly so this stays
// unit-testable across platforms) into a concrete ArchiveURL, clearing the
// template fields - mirrors registry.Entry.ResolveArchiveURL for a
// template's own prebuilt_archive dependency (see cmd/new.go). A no-op,
// returning d unchanged, if d has no ArchiveURLTemplate at all.
func (d Dependency) ResolveArchiveURLTemplate(goos, goarch string) (Dependency, error) {
	if d.ArchiveURLTemplate == "" {
		return d, nil
	}
	key := goos + "/" + goarch
	platform, ok := d.PlatformNames[key]
	if !ok {
		keys := make([]string, 0, len(d.PlatformNames))
		for k := range d.PlatformNames {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return Dependency{}, fmt.Errorf("%q has no known prebuilt archive for %s - supported: %s", d.Name, key, strings.Join(keys, ", "))
	}
	d.ArchiveURL = strings.ReplaceAll(d.ArchiveURLTemplate, "{platform}", platform)
	d.ArchiveURLTemplate = ""
	d.PlatformNames = nil
	return d, nil
}

// Dependency describes a third-party library wired into the generated
// CMakeLists.txt - by default fetched automatically at configure time via
// CPM.cmake (Kind == DependencyKindCPM, instead of assuming it's already
// installed system-wide), or (§27) acquired a different way entirely per
// Kind.
type Dependency struct {
	Name         string         `yaml:"name"`
	Kind         DependencyKind `yaml:"kind,omitempty"`          // "" == DependencyKindCPM; see DependencyKind's own doc for the other shapes
	Repo         string         `yaml:"repo,omitempty"`          // cpm only: "owner/repo" GitHub shorthand, or a full git URL (e.g. gitlab) for GIT_REPOSITORY
	Tag          string         `yaml:"tag,omitempty"`           // cpm only
	Link         []string       `yaml:"link"`                    // targets to pass to target_link_libraries - used by every kind
	Options      []string       `yaml:"options,omitempty"`       // cpm only: optional CPMAddPackage OPTIONS lines
	DownloadOnly bool           `yaml:"download_only,omitempty"` // cpm only: CPM DOWNLOAD_ONLY YES - fetch source but don't add_subdirectory it; used when the dep's own CMakeLists.txt isn't meant to be consumed directly (e.g. Eigen), pairing with a cmake_extra block that wires up the include dir manually
	// PostFetchExtra is raw CMake emitted immediately after this
	// dependency's CPMAddPackage() call, before the next dependency's -
	// unlike cmake_extra (which only runs once, after every dependency has
	// already been fetched), this exists for the rarer case where a later
	// dependency's own CMakeLists.txt does a find_package() during ITS
	// fetch that needs a variable set from an earlier dependency's fetch
	// result first (see the crow template's asio/ASIO_INCLUDE_DIR).
	PostFetchExtra string `yaml:"post_fetch_extra,omitempty"`

	// FindPackage is the system_package kind's CMake package name -
	// emitted as find_package(<FindPackage> REQUIRED) instead of a
	// CPMAddPackage call, after the package manager install step below has
	// actually run.
	FindPackage string `yaml:"find_package,omitempty"`
	// PkgConfigModule is the pkg_config kind's .pc module name (e.g.
	// "gtkmm-4.0") - emitted as pkg_check_modules(<VAR> REQUIRED
	// IMPORTED_TARGET <PkgConfigModule>), for libraries that only ship a
	// pkg-config file and no CMake package config at all.
	PkgConfigModule string `yaml:"pkg_config_module,omitempty"`
	// PackageManagers (system_package and pkg_config only) maps a
	// package-manager id ("brew", "apt") to the package name that manager
	// should install - both `cmaker install <name>` (a registry entry)
	// and `cmaker new --template=<name>` (a template's own meta.yaml
	// declaring this shape directly) run this exact install step before
	// anything that needs the result (a configure, or writing
	// cmaker.yaml) - see installSystemPackage in cmd/, shared by both
	// paths.
	PackageManagers map[string]string `yaml:"package_managers,omitempty"`

	// ArchiveURL (prebuilt_archive only) is the exact download URL for
	// this machine's platform/variant, already resolved by the caller
	// (registry.Entry.ResolveArchiveURL, or ResolveArchiveURLTemplate for
	// a template's own dependency - see cmd/new.go) before this
	// Dependency is ever written to cmaker.yaml - Generate itself does no
	// platform detection, it just downloads and extracts whatever URL
	// it's given.
	ArchiveURL string `yaml:"archive_url,omitempty"`
	// ArchiveURLTemplate/PlatformNames (prebuilt_archive only, template
	// authoring time only) mirror registry.Entry's own fields of the same
	// name - a template's meta.yaml declares a dependency this shape when
	// it wants a platform-matched prebuilt archive (e.g. the onnxruntime
	// template), and cmd/new.go resolves it to a concrete ArchiveURL via
	// ResolveArchiveURLTemplate before ever writing cmaker.yaml, the same
	// way cmd/install.go resolves a registry entry's own template. Once
	// resolved, these two fields are cleared - only ArchiveURL is ever
	// actually written to a real cmaker.yaml.
	ArchiveURLTemplate string            `yaml:"archive_url_template,omitempty"`
	PlatformNames      map[string]string `yaml:"platform_names,omitempty"`
	// ArchiveIncludeDir/ArchiveLibDir (prebuilt_archive only) are paths
	// relative to the extracted archive's own top-level directory (whose
	// name varies per release, so Generate discovers it at configure time
	// via a glob rather than assuming it matches the archive filename).
	ArchiveIncludeDir string `yaml:"archive_include_dir,omitempty"`
	ArchiveLibDir     string `yaml:"archive_lib_dir,omitempty"`
}

// ValidLanguages is the set of values `language:` may take (including the
// empty string, meaning "unset, defaults to cpp").
var ValidLanguages = map[string]bool{
	"": true, "cpp": true, "c": true, "hybrid": true,
}

// ValidTargetTypes is the set of values `target_type:` may take (including
// the empty string, meaning "unset, defaults to executable").
var ValidTargetTypes = map[string]bool{
	"": true, "executable": true, "static_library": true, "shared_library": true,
}

var validCppVersions = map[int]bool{
	98: true, 3: true, 11: true, 14: true, 17: true, 20: true, 23: true, 26: true,
}

var validCVersions = map[int]bool{
	89: true, 99: true, 11: true, 17: true, 23: true,
}

var validSanitizers = map[string]bool{
	"address": true, "undefined": true, "thread": true, "memory": true, "leak": true,
}

// LanguageOrDefault returns language with the "cpp" default applied, since
// the field is omitted (empty string) on every project scaffolded before
// language selection existed - this keeps old cmaker.yaml files working
// unchanged.
func LanguageOrDefault(language string) string {
	if language == "" {
		return "cpp"
	}
	return language
}

// TargetTypeOrDefault returns targetType with the "executable" default
// applied, since the field is omitted (empty string) on every project
// scaffolded before library targets existed - this keeps old cmaker.yaml
// files working unchanged.
func TargetTypeOrDefault(targetType string) string {
	if targetType == "" {
		return "executable"
	}
	return targetType
}

// Validate checks a Config for internal consistency (supported language,
// matching version fields, known sanitizer names, a schema version this
// build understands).
func Validate(c Config) error {
	if c.SchemaVersion > CurrentSchemaVersion {
		return fmt.Errorf("cmaker.yaml has schema_version %d, but this build of cmaker only understands up to %d - please upgrade cmaker", c.SchemaVersion, CurrentSchemaVersion)
	}
	if c.Workspace != nil {
		if c.Executable != "" {
			return fmt.Errorf("a workspace root cmaker.yaml must not also set 'executable' - a workspace root defines no target of its own, only 'workspace.members'")
		}
		if len(c.Workspace.Members) == 0 {
			return fmt.Errorf("'workspace.members' must not be empty")
		}
		return nil
	}
	if c.Executable == "" {
		return fmt.Errorf("'executable' must not be empty")
	}
	if !ValidLanguages[c.Language] {
		return fmt.Errorf("'language: %s' is not one of cpp, c, hybrid", c.Language)
	}
	if !ValidTargetTypes[c.TargetType] {
		return fmt.Errorf("'target_type: %s' is not one of executable, static_library, shared_library", c.TargetType)
	}
	lang := LanguageOrDefault(c.Language)
	if lang == "cpp" || lang == "hybrid" {
		if !validCppVersions[c.CppVersion] {
			return fmt.Errorf("'cpp_version: %d' is not a supported C++ standard (expected one of 98, 03, 11, 14, 17, 20, 23, 26)", c.CppVersion)
		}
	}
	if lang == "c" || lang == "hybrid" {
		if !validCVersions[c.CVersion] {
			return fmt.Errorf("'c_version: %d' is not a supported C standard (expected one of 89, 99, 11, 17, 23)", c.CVersion)
		}
	}
	for _, s := range c.Sanitizers {
		if !validSanitizers[s] {
			return fmt.Errorf("'sanitizers' contains %q, expected one of address, undefined, thread, memory, leak", s)
		}
	}
	return nil
}

// Load reads and validates a cmaker.yaml file at path. It never exits the
// process and never prints anything - callers that want the CLI's
// exit-on-failure behavior wrap this themselves (see cmd/config_helpers.go).
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("%s not found. Run 'cmaker new' first", path)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("%s is malformed: %w", path, err)
	}

	if err := Validate(cfg); err != nil {
		return Config{}, fmt.Errorf("invalid %s: %w", path, err)
	}
	return cfg, nil
}

// Save marshals c and writes it to path (overwriting any existing file).
func Save(path string, c Config) error {
	data, err := yaml.Marshal(&c)
	if err != nil {
		return fmt.Errorf("failed to marshal %s: %w", path, err)
	}
	return os.WriteFile(path, data, 0644)
}

// TryLoad reads and parses cmaker.yaml from dir. Unlike Load, it swallows
// all errors into an ok=false - callers (TUI sidebar, doctor's opt-in
// Rust/Zig checks) just want a best-effort peek at what's there, not a hard
// failure or a validated Config.
func TryLoad(dir string) (Config, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "cmaker.yaml"))
	if err != nil {
		return Config{}, false
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, false
	}
	return cfg, true
}

// TryLoadConfigs reads cmaker.yaml's configs: map from dir, if present.
func TryLoadConfigs(dir string) map[string]string {
	cfg, ok := TryLoad(dir)
	if !ok {
		return nil
	}
	return cfg.Configs
}
