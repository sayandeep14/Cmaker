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
	Language         string            `yaml:"language,omitempty"` // cpp | c | hybrid (default cpp)
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
	CMakeExtra       string            `yaml:"cmake_extra,omitempty"` // raw CMake appended verbatim, for cases the generator doesn't model
	Configs          map[string]string `yaml:"configs,omitempty"`     // named shortcuts, e.g. {test: "run --only=test1.cpp"}, run via `cmaker <name>`
	Rust             *RustConfig       `yaml:"rust,omitempty"`        // opt-in Rust crate wired into the build
	Zig              *ZigConfig        `yaml:"zig,omitempty"`         // opt-in Zig library wired into the build
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

// Dependency describes a third-party library fetched automatically at
// configure time via CPM.cmake, instead of assuming it's already installed
// system-wide.
type Dependency struct {
	Name         string   `yaml:"name"`
	Repo         string   `yaml:"repo"` // "owner/repo" GitHub shorthand, or a full git URL (e.g. gitlab) for GIT_REPOSITORY
	Tag          string   `yaml:"tag"`
	Link         []string `yaml:"link"`                    // targets to pass to target_link_libraries
	Options      []string `yaml:"options"`                 // optional CPMAddPackage OPTIONS lines
	DownloadOnly bool     `yaml:"download_only,omitempty"` // CPM DOWNLOAD_ONLY YES - fetch source but don't add_subdirectory it; used when the dep's own CMakeLists.txt isn't meant to be consumed directly (e.g. Eigen), pairing with a cmake_extra block that wires up the include dir manually
}

// ValidLanguages is the set of values `language:` may take (including the
// empty string, meaning "unset, defaults to cpp").
var ValidLanguages = map[string]bool{
	"": true, "cpp": true, "c": true, "hybrid": true,
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

// Validate checks a Config for internal consistency (supported language,
// matching version fields, known sanitizer names, a schema version this
// build understands).
func Validate(c Config) error {
	if c.SchemaVersion > CurrentSchemaVersion {
		return fmt.Errorf("cmaker.yaml has schema_version %d, but this build of cmaker only understands up to %d - please upgrade cmaker", c.SchemaVersion, CurrentSchemaVersion)
	}
	if c.Executable == "" {
		return fmt.Errorf("'executable' must not be empty")
	}
	if !ValidLanguages[c.Language] {
		return fmt.Errorf("'language: %s' is not one of cpp, c, hybrid", c.Language)
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
