// Package cmake generates CMakeLists.txt content from a config.Config, and
// provides the small set of cmake-invocation helpers (policy flags,
// compiler-override args, compiler smoke-testing) shared by every command
// that shells out to `cmake`.
package cmake

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"cmaker/internal/config"
)

// PolicyVersionMinFlag works around a real, common compatibility break:
// CMake >= 4.0 refuses to configure any project (including CPM-fetched
// third-party ones) whose own cmake_minimum_required is below 3.5. Plenty
// of still-widely-used libraries (raylib 5.0 among them) haven't bumped
// that floor yet. Passed unconditionally to every cmaker-issued `cmake -S`
// invocation - it's a no-op for projects that don't need it.
const PolicyVersionMinFlag = "-DCMAKE_POLICY_VERSION_MINIMUM=3.5"

// ExportCompileCommandsFlag makes CMake emit build/compile_commands.json on
// every configure - needed by `cmaker lint` (clang-tidy wants a compilation
// database) and incidentally useful to any IDE/clangd already pointed at
// the project. Unconditional and effectively free, like PolicyVersionMinFlag.
const ExportCompileCommandsFlag = "-DCMAKE_EXPORT_COMPILE_COMMANDS=ON"

// DetectCompilerLauncher looks for ccache, then sccache, on PATH - whichever
// is found first is what StandardConfigureFlags wires in as
// CMAKE_<LANG>_COMPILER_LAUNCHER. Also used by `cmaker doctor` to report
// compiler-cache availability.
func DetectCompilerLauncher() (tool string, path string, found bool) {
	for _, name := range []string{"ccache", "sccache"} {
		if p, err := exec.LookPath(name); err == nil {
			return name, p, true
		}
	}
	return "", "", false
}

// StandardConfigureFlags returns the flags every cmaker-issued `cmake -S`
// invocation should pass, regardless of caller (cmd/build.go, cmd/new.go's
// pre-flight configure, cmd/install.go's post-install reconfigure): the
// policy-version workaround, compile_commands.json export, and - unless
// c.DisableCcache opts out - a compiler-launcher override for whichever of
// ccache/sccache is found on PATH. A build-speed win that costs nothing
// when neither tool is installed (DetectCompilerLauncher just reports
// found=false and this contributes no extra flags).
func StandardConfigureFlags(c config.Config) []string {
	flags := []string{PolicyVersionMinFlag, ExportCompileCommandsFlag}
	if c.DisableCcache {
		return flags
	}
	_, path, found := DetectCompilerLauncher()
	if !found {
		return flags
	}
	return append(flags,
		"-DCMAKE_C_COMPILER_LAUNCHER="+path,
		"-DCMAKE_CXX_COMPILER_LAUNCHER="+path,
	)
}

// cpmBootstrap downloads and includes CPM.cmake (the standard CMake package
// manager for fetching dependencies from git at configure time) so
// dependency-bearing templates don't require the library to already be
// installed system-wide.
const cpmBootstrap = `set(CPM_DOWNLOAD_VERSION 0.40.2)
set(CPM_DOWNLOAD_LOCATION "${CMAKE_BINARY_DIR}/cmake/CPM_${CPM_DOWNLOAD_VERSION}.cmake")
if(NOT EXISTS ${CPM_DOWNLOAD_LOCATION})
  file(DOWNLOAD https://github.com/cpm-cmake/CPM.cmake/releases/download/v${CPM_DOWNLOAD_VERSION}/CPM.cmake ${CPM_DOWNLOAD_LOCATION})
endif()
include(${CPM_DOWNLOAD_LOCATION})
`

// Generate writes CMakeLists.txt into root, derived from c.
func Generate(root string, c config.Config) error {
	lang := config.LanguageOrDefault(c.Language)
	targetType := config.TargetTypeOrDefault(c.TargetType)

	var libsBuilder strings.Builder
	for _, l := range c.LinkLibraries {
		fmt.Fprintf(&libsBuilder, "target_link_libraries(%s PRIVATE %s)\n", c.Executable, l)
	}
	for _, dep := range c.Dependencies {
		for _, l := range dep.Link {
			fmt.Fprintf(&libsBuilder, "target_link_libraries(%s PRIVATE %s)\n", c.Executable, l)
		}
	}
	libs := libsBuilder.String()

	var depsBuilder strings.Builder
	if len(c.Dependencies) > 0 {
		depsBuilder.WriteString(cpmBootstrap)
		depsBuilder.WriteString("\n")
		for _, dep := range c.Dependencies {
			// Most deps use the "owner/repo" GitHub shorthand, but some
			// (e.g. Eigen, hosted on GitLab) need a full git URL - CPM
			// supports both GITHUB_REPOSITORY and the generic GIT_REPOSITORY.
			repoKeyword := "GITHUB_REPOSITORY"
			if strings.Contains(dep.Repo, "://") {
				repoKeyword = "GIT_REPOSITORY"
			}
			// GIT_SHALLOW avoids a full-history clone of the dependency's
			// repo - for a dependency like raylib (500+ MB of git history
			// vs. ~90 MB at a single tag), a full clone can be slow enough
			// to look like a hung/failed fetch on anything but a fast
			// connection. Safe as long as 'tag:' names an actual tag or
			// branch (true for every dependency in cmaker's own templates);
			// an arbitrary commit SHA can fail a shallow fetch on git hosts
			// that don't support fetching arbitrary commits, GitHub does.
			fmt.Fprintf(&depsBuilder, "CPMAddPackage(\n  NAME %s\n  %s %s\n  GIT_TAG %s\n  GIT_SHALLOW TRUE\n", dep.Name, repoKeyword, dep.Repo, dep.Tag)
			if dep.DownloadOnly {
				depsBuilder.WriteString("  DOWNLOAD_ONLY YES\n")
			}
			if len(dep.Options) > 0 {
				depsBuilder.WriteString("  OPTIONS\n")
				for _, opt := range dep.Options {
					fmt.Fprintf(&depsBuilder, "    %q\n", opt)
				}
			}
			depsBuilder.WriteString(")\n\n")
		}
	}

	var stdBuilder strings.Builder
	if lang == "cpp" || lang == "hybrid" {
		fmt.Fprintf(&stdBuilder, "set(CMAKE_CXX_STANDARD %d)\nset(CMAKE_CXX_STANDARD_REQUIRED ON)\n", c.CppVersion)
	}
	if lang == "c" || lang == "hybrid" {
		fmt.Fprintf(&stdBuilder, "set(CMAKE_C_STANDARD %d)\nset(CMAKE_C_STANDARD_REQUIRED ON)\n", c.CVersion)
	}

	var globLine string
	switch lang {
	case "c":
		globLine = `file(GLOB_RECURSE SOURCES "src/*.c")` + "\n"
	case "hybrid":
		globLine = `file(GLOB_RECURSE SOURCES "src/*.c" "src/*.cpp" "src/*.cxx")` + "\n"
	default:
		globLine = `file(GLOB_RECURSE SOURCES "src/*.cpp")` + "\n"
	}

	var optsBuilder strings.Builder
	var flags []string
	if c.WarningsAsErrors {
		flags = append(flags, "-Wall", "-Wextra", "-Werror")
	}
	if len(c.Sanitizers) > 0 {
		flags = append(flags, "-fsanitize="+strings.Join(c.Sanitizers, ","), "-fno-omit-frame-pointer")
	}
	if len(flags) > 0 {
		fmt.Fprintf(&optsBuilder, "target_compile_options(%s PRIVATE %s)\n", c.Executable, strings.Join(flags, " "))
	}
	if len(c.Sanitizers) > 0 {
		fmt.Fprintf(&optsBuilder, "target_link_options(%s PRIVATE -fsanitize=%s)\n", c.Executable, strings.Join(c.Sanitizers, ","))
	}

	var extraBuilder strings.Builder
	if c.CMakeExtra != "" {
		extraBuilder.WriteString("\n# --- custom cmake_extra from cmaker.yaml ---\n")
		extraBuilder.WriteString(c.CMakeExtra)
		extraBuilder.WriteString("\n")
	}

	var rustPreamble, rustLink strings.Builder
	if c.Rust != nil && c.Rust.Enabled {
		crateDir := c.Rust.CrateDir
		if crateDir == "" {
			crateDir = "rust"
		}
		fmt.Fprintf(&rustPreamble, "\n# --- Rust crate wired in via cmaker (rust.enabled) ---\n"+
			"set(CMAKER_RUST_CRATE_DIR \"${CMAKE_SOURCE_DIR}/%s\")\n"+
			"set(CMAKER_RUST_LIB \"${CMAKER_RUST_CRATE_DIR}/target/release/librustlib.a\")\n"+
			"add_custom_command(\n"+
			"  OUTPUT ${CMAKER_RUST_LIB}\n"+
			"  COMMAND cargo build --release --manifest-path ${CMAKER_RUST_CRATE_DIR}/Cargo.toml\n"+
			"  WORKING_DIRECTORY ${CMAKER_RUST_CRATE_DIR}\n"+
			"  COMMENT \"Building Rust crate (rustlib) via cargo\"\n"+
			"  VERBATIM\n"+
			")\n"+
			"add_custom_target(cmaker_rust_crate DEPENDS ${CMAKER_RUST_LIB})\n",
			crateDir)
		fmt.Fprintf(&rustLink, "add_dependencies(%s cmaker_rust_crate)\n", c.Executable)
		fmt.Fprintf(&rustLink, "target_link_libraries(%s PRIVATE ${CMAKER_RUST_LIB})\n", c.Executable)
		fmt.Fprintf(&rustLink, "if(NOT WIN32)\n  target_link_libraries(%s PRIVATE dl pthread m)\nendif()\n", c.Executable)
	}

	var zigPreamble, zigLink strings.Builder
	if c.Zig != nil && c.Zig.Enabled {
		srcDir := c.Zig.SrcDir
		if srcDir == "" {
			srcDir = "zig"
		}
		fmt.Fprintf(&zigPreamble, "\n# --- Zig library wired in via cmaker (zig.enabled) ---\n"+
			"set(CMAKER_ZIG_SRC_DIR \"${CMAKE_SOURCE_DIR}/%s\")\n"+
			"set(CMAKER_ZIG_LIB \"${CMAKER_ZIG_SRC_DIR}/libziglib.a\")\n"+
			"add_custom_command(\n"+
			"  OUTPUT ${CMAKER_ZIG_LIB}\n"+
			"  COMMAND zig build-lib -O ReleaseFast -femit-bin=${CMAKER_ZIG_LIB} ${CMAKER_ZIG_SRC_DIR}/src/lib.zig\n"+
			"  WORKING_DIRECTORY ${CMAKER_ZIG_SRC_DIR}\n"+
			"  COMMENT \"Building Zig library (ziglib) via zig build-lib\"\n"+
			"  VERBATIM\n"+
			")\n"+
			"add_custom_target(cmaker_zig_lib DEPENDS ${CMAKER_ZIG_LIB})\n",
			srcDir)
		fmt.Fprintf(&zigLink, "add_dependencies(%s cmaker_zig_lib)\n", c.Executable)
		fmt.Fprintf(&zigLink, "target_link_libraries(%s PRIVATE ${CMAKER_ZIG_LIB})\n", c.Executable)
	}

	var testingBuilder strings.Builder
	if c.Testing != nil && c.Testing.Enabled {
		fmt.Fprintf(&testingBuilder, "\nenable_testing()\nadd_test(NAME %s COMMAND %s)\n", c.Executable, c.Executable)
	}

	// targetDeclLine/includeDirsLine branch on target_type (§16): a library
	// target is declared with add_library(... STATIC|SHARED ...) instead of
	// add_executable, and its headers need PUBLIC visibility (via the
	// BUILD_INTERFACE/INSTALL_INTERFACE generator-expression pair, so a
	// consumer sees a plain "include" path whether it's building against
	// this project's source tree or an installed copy) so that anything
	// linking against it can actually see its headers - an executable's
	// headers stay PRIVATE, unchanged from before target types existed.
	var targetDeclLine string
	var includeDirsLine string
	switch targetType {
	case "static_library":
		targetDeclLine = fmt.Sprintf("add_library(%s STATIC ${SOURCES})\n", c.Executable)
	case "shared_library":
		targetDeclLine = fmt.Sprintf("add_library(%s SHARED ${SOURCES})\n", c.Executable)
	default:
		targetDeclLine = fmt.Sprintf("add_executable(%s ${SOURCES})\n", c.Executable)
	}
	if targetType == "executable" {
		includeDirsLine = fmt.Sprintf("target_include_directories(%s PRIVATE include)\n", c.Executable)
	} else {
		includeDirsLine = fmt.Sprintf("target_include_directories(%s PUBLIC $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include> $<INSTALL_INTERFACE:include>)\n", c.Executable)
	}

	// A library target with an examples/*.cpp demo (see cmd/new.go's
	// writeLibrarySources) gets a second, always-linked executable target so
	// "does my library actually work" stays a one-command `cmaker run`
	// answer instead of requiring a separate consumer project.
	var demoBuilder strings.Builder
	if targetType != "executable" {
		if matches, _ := filepath.Glob(filepath.Join(root, "examples", "*.cpp")); len(matches) > 0 {
			demoName := c.Executable + "_demo"
			fmt.Fprintf(&demoBuilder, "\nfile(GLOB_RECURSE %s_SOURCES \"examples/*.cpp\")\nadd_executable(%s ${%s_SOURCES})\ntarget_link_libraries(%s PRIVATE %s)\n",
				c.Executable, demoName, c.Executable, demoName, c.Executable)
		}
	}

	// install() rules (CMake's own install, not a package manager) so
	// `cmake --install build` and downstream find_package(<name>) both
	// work for a library target - not emitted for a plain executable, which
	// has no consumer story to support.
	var installBuilder strings.Builder
	if targetType != "executable" {
		fmt.Fprintf(&installBuilder, `
include(GNUInstallDirs)
install(TARGETS %s
  EXPORT %sTargets
  LIBRARY DESTINATION ${CMAKE_INSTALL_LIBDIR}
  ARCHIVE DESTINATION ${CMAKE_INSTALL_LIBDIR}
  RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR}
)
install(DIRECTORY include/ DESTINATION ${CMAKE_INSTALL_INCLUDEDIR})
install(EXPORT %sTargets
  FILE %sConfig.cmake
  NAMESPACE %s::
  DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/%s
)
`, c.Executable, c.Executable, c.Executable, c.Executable, c.Executable, c.Executable)
	}

	// extraBuilder (cmake_extra) is injected here, before the target
	// declaration - custom targets it defines (e.g. an Eigen INTERFACE
	// library wrapping a DOWNLOAD_ONLY dependency) must exist before
	// target_link_libraries references them further down.
	content := fmt.Sprintf(`cmake_minimum_required(VERSION 3.14)
project(%s)
%s
%s%s%s%s%s%s%s%s%s%s%s%s`, c.ProjectName, stdBuilder.String(), depsBuilder.String(), rustPreamble.String(), zigPreamble.String(), extraBuilder.String(), globLine,
		targetDeclLine, includeDirsLine, libs, rustLink.String(), zigLink.String(), optsBuilder.String(), testingBuilder.String())
	content += demoBuilder.String() + installBuilder.String()

	return os.WriteFile(filepath.Join(root, "CMakeLists.txt"), []byte(content), 0644)
}

// CompilerArgs translates a cmaker.yaml `compiler` field (or a --compiler
// flag override) into the -D flags cmake needs *at configure time* -
// CMAKE_CXX_COMPILER/CMAKE_C_COMPILER can't be set via a `set()` in
// CMakeLists.txt after the fact, since CMake locks in the compiler the
// moment `project()` runs.
//
// Known limitation: a hybrid (C+C++) project only gets its C++ compiler
// overridden this way, since `compiler` is a single field. Overriding the C
// compiler independently for a hybrid project isn't supported yet - except
// for the `compiler: zig` special case below, which covers both.
func CompilerArgs(compiler string, language string) []string {
	if compiler == "" {
		return nil
	}
	lang := config.LanguageOrDefault(language)

	// "zig as the C/C++ compiler" (§12): zig doubles as a full C/C++
	// toolchain via `zig cc`/`zig c++`, but CMAKE_<LANG>_COMPILER wants a
	// single executable path, not "zig cc" as one string. CMake's
	// documented way to pass the subcommand is CMAKE_<LANG>_COMPILER_ARG1.
	// This also sidesteps the hybrid limitation above, since zig can serve
	// as both the C and C++ compiler at once.
	if compiler == "zig" {
		var args []string
		if lang == "c" || lang == "hybrid" {
			args = append(args, "-DCMAKE_C_COMPILER=zig", "-DCMAKE_C_COMPILER_ARG1=cc")
		}
		if lang == "cpp" || lang == "hybrid" {
			args = append(args, "-DCMAKE_CXX_COMPILER=zig", "-DCMAKE_CXX_COMPILER_ARG1=c++")
		}
		return args
	}

	if lang == "c" {
		return []string{"-DCMAKE_C_COMPILER=" + compiler}
	}
	return []string{"-DCMAKE_CXX_COMPILER=" + compiler}
}

// ValidateCompilerSupportsStandard does a cheap preprocessor-only smoke test
// (no object file emitted) to catch "this compiler doesn't support
// -std=c++23" before handing an opaque CMake/compiler error to the user.
// A no-op when no compiler override is configured.
func ValidateCompilerSupportsStandard(compiler string, language string, cppVersion, cVersion int) error {
	if compiler == "" {
		return nil
	}
	lang := config.LanguageOrDefault(language)
	var std, xLang string
	if lang == "c" {
		std, xLang = fmt.Sprintf("-std=c%d", cVersion), "c"
	} else {
		std, xLang = fmt.Sprintf("-std=c++%d", cppVersion), "c++"
	}

	// "zig" isn't invoked directly as a compiler binary - it's `zig cc`/
	// `zig c++` (see CompilerArgs's CMAKE_<LANG>_COMPILER_ARG1 handling
	// above), so the smoke test needs the matching subcommand prepended.
	var name string
	var args []string
	if compiler == "zig" {
		name = "zig"
		if xLang == "c" {
			args = []string{"cc", std, "-x", xLang, "-E", "-"}
		} else {
			args = []string{"c++", std, "-x", xLang, "-E", "-"}
		}
	} else {
		name = compiler
		args = []string{std, "-x", xLang, "-E", "-"}
	}

	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader("")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("compiler %q does not appear to support %s: %s", compiler, std, strings.TrimSpace(stderr.String()))
	}
	return nil
}
