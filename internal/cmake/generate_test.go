package cmake

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmaker/internal/config"
)

func mustGenerate(t *testing.T, c config.Config) string {
	t.Helper()
	dir := t.TempDir()
	if err := Generate(dir, c); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "CMakeLists.txt"))
	if err != nil {
		t.Fatalf("failed to read generated CMakeLists.txt: %v", err)
	}
	return string(data)
}

func TestGenerateDefaultCpp(t *testing.T) {
	content := mustGenerate(t, config.Config{
		ProjectName: "myproj",
		CppVersion:  20,
		Executable:  "main",
	})

	for _, want := range []string{
		"cmake_minimum_required(VERSION 3.14)",
		"project(myproj)",
		"set(CMAKE_CXX_STANDARD 20)",
		"set(CMAKE_CXX_STANDARD_REQUIRED ON)",
		`file(GLOB_RECURSE SOURCES "src/*.cpp")`,
		"add_executable(main ${SOURCES})",
		"target_include_directories(main PRIVATE include)",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("generated CMakeLists.txt missing %q\n--- full output ---\n%s", want, content)
		}
	}
	if strings.Contains(content, "CMAKE_C_STANDARD") {
		t.Errorf("plain cpp project should not set CMAKE_C_STANDARD:\n%s", content)
	}
}

func TestGenerateCLanguage(t *testing.T) {
	content := mustGenerate(t, config.Config{
		ProjectName: "cproj",
		Language:    "c",
		CVersion:    17,
		Executable:  "main",
	})

	if !strings.Contains(content, "set(CMAKE_C_STANDARD 17)") {
		t.Errorf("expected CMAKE_C_STANDARD 17:\n%s", content)
	}
	if !strings.Contains(content, `file(GLOB_RECURSE SOURCES "src/*.c")`) {
		t.Errorf("expected a src/*.c glob:\n%s", content)
	}
	if strings.Contains(content, "CMAKE_CXX_STANDARD") {
		t.Errorf("plain c project should not set CMAKE_CXX_STANDARD:\n%s", content)
	}
}

func TestGenerateHybrid(t *testing.T) {
	content := mustGenerate(t, config.Config{
		ProjectName: "hybridproj",
		Language:    "hybrid",
		CppVersion:  17,
		CVersion:    11,
		Executable:  "main",
	})

	for _, want := range []string{
		"set(CMAKE_CXX_STANDARD 17)",
		"set(CMAKE_C_STANDARD 11)",
		`file(GLOB_RECURSE SOURCES "src/*.c" "src/*.cpp" "src/*.cxx")`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("hybrid project missing %q:\n%s", want, content)
		}
	}
}

func TestGenerateDependencies(t *testing.T) {
	content := mustGenerate(t, config.Config{
		ProjectName: "depsproj",
		CppVersion:  17,
		Executable:  "main",
		Dependencies: []config.Dependency{
			{Name: "raylib", Repo: "raysan5/raylib", Tag: "5.0", Link: []string{"raylib"}, Options: []string{"BUILD_EXAMPLES OFF"}},
		},
	})

	for _, want := range []string{
		"include(${CPM_DOWNLOAD_LOCATION})",
		"CPMAddPackage(",
		"NAME raylib",
		"GITHUB_REPOSITORY raysan5/raylib",
		"GIT_TAG 5.0",
		"GIT_SHALLOW TRUE",
		`"BUILD_EXAMPLES OFF"`,
		"target_link_libraries(main PRIVATE raylib)",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("dependency wiring missing %q:\n%s", want, content)
		}
	}
}

func TestGenerateDependencyGitURLAndDownloadOnly(t *testing.T) {
	content := mustGenerate(t, config.Config{
		ProjectName: "eigenproj",
		CppVersion:  17,
		Executable:  "main",
		Dependencies: []config.Dependency{
			{Name: "eigen", Repo: "https://gitlab.com/libeigen/eigen.git", Tag: "3.4.0", DownloadOnly: true},
		},
		CMakeExtra: "add_library(Eigen INTERFACE IMPORTED)\n",
	})

	if !strings.Contains(content, "GIT_REPOSITORY https://gitlab.com/libeigen/eigen.git") {
		t.Errorf("expected GIT_REPOSITORY for a full URL, not GITHUB_REPOSITORY:\n%s", content)
	}
	if strings.Contains(content, "GITHUB_REPOSITORY https://gitlab.com") {
		t.Errorf("should not use GITHUB_REPOSITORY for a full git URL:\n%s", content)
	}
	if !strings.Contains(content, "DOWNLOAD_ONLY YES") {
		t.Errorf("expected DOWNLOAD_ONLY YES:\n%s", content)
	}

	// cmake_extra must appear before add_executable, since it may define
	// custom targets (like the Eigen INTERFACE library) that
	// target_link_libraries needs to already exist.
	extraIdx := strings.Index(content, "add_library(Eigen INTERFACE IMPORTED)")
	execIdx := strings.Index(content, "add_executable(")
	if extraIdx == -1 || execIdx == -1 || extraIdx > execIdx {
		t.Errorf("expected cmake_extra content before add_executable(), got extraIdx=%d execIdx=%d:\n%s", extraIdx, execIdx, content)
	}
}

func TestGenerateSanitizersAndWarnings(t *testing.T) {
	content := mustGenerate(t, config.Config{
		ProjectName:      "sanproj",
		CppVersion:       20,
		Executable:       "main",
		WarningsAsErrors: true,
		Sanitizers:       []string{"address", "undefined"},
	})

	for _, want := range []string{
		"-Wall -Wextra -Werror",
		"-fsanitize=address,undefined",
		"target_link_options(main PRIVATE -fsanitize=address,undefined)",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q:\n%s", want, content)
		}
	}
}

func TestGenerateRustAndZigOptIn(t *testing.T) {
	plain := mustGenerate(t, config.Config{ProjectName: "plain", CppVersion: 17, Executable: "main"})
	if strings.Contains(plain, "cargo") || strings.Contains(plain, "zig build-lib") {
		t.Errorf("a project with Rust/Zig unset must not generate any Rust/Zig CMake (opt-in must be zero-cost):\n%s", plain)
	}

	withRust := mustGenerate(t, config.Config{
		ProjectName: "rustproj", CppVersion: 17, Executable: "main",
		Rust: &config.RustConfig{Enabled: true, CrateDir: "rust"},
	})
	for _, want := range []string{
		"cargo build --release",
		"add_dependencies(main cmaker_rust_crate)",
		"target_link_libraries(main PRIVATE ${CMAKER_RUST_LIB})",
	} {
		if !strings.Contains(withRust, want) {
			t.Errorf("rust-enabled project missing %q:\n%s", want, withRust)
		}
	}

	withZig := mustGenerate(t, config.Config{
		ProjectName: "zigproj", CppVersion: 17, Executable: "main",
		Zig: &config.ZigConfig{Enabled: true, SrcDir: "zig"},
	})
	for _, want := range []string{
		"zig build-lib",
		"add_dependencies(main cmaker_zig_lib)",
		"target_link_libraries(main PRIVATE ${CMAKER_ZIG_LIB})",
	} {
		if !strings.Contains(withZig, want) {
			t.Errorf("zig-enabled project missing %q:\n%s", want, withZig)
		}
	}
}

func TestGenerateTesting(t *testing.T) {
	withTesting := mustGenerate(t, config.Config{
		ProjectName: "myproj",
		CppVersion:  20,
		Executable:  "main",
		Testing:     &config.TestingConfig{Enabled: true},
	})
	for _, want := range []string{
		"enable_testing()",
		"add_test(NAME main COMMAND main)",
	} {
		if !strings.Contains(withTesting, want) {
			t.Errorf("testing-enabled project missing %q:\n%s", want, withTesting)
		}
	}

	withoutTesting := mustGenerate(t, config.Config{
		ProjectName: "myproj",
		CppVersion:  20,
		Executable:  "main",
	})
	if strings.Contains(withoutTesting, "enable_testing") {
		t.Errorf("project with no 'testing:' config should not get ctest wiring:\n%s", withoutTesting)
	}

	withDisabledTesting := mustGenerate(t, config.Config{
		ProjectName: "myproj",
		CppVersion:  20,
		Executable:  "main",
		Testing:     &config.TestingConfig{Enabled: false},
	})
	if strings.Contains(withDisabledTesting, "enable_testing") {
		t.Errorf("project with testing.enabled: false should not get ctest wiring:\n%s", withDisabledTesting)
	}
}

func TestCompilerArgs(t *testing.T) {
	if got := CompilerArgs("", "cpp"); got != nil {
		t.Errorf("empty compiler should produce no args, got %v", got)
	}

	got := CompilerArgs("clang++-17", "cpp")
	want := []string{"-DCMAKE_CXX_COMPILER=clang++-17"}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("CompilerArgs(clang++-17, cpp) = %v, want %v", got, want)
	}

	got = CompilerArgs("gcc-13", "c")
	want = []string{"-DCMAKE_C_COMPILER=gcc-13"}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("CompilerArgs(gcc-13, c) = %v, want %v", got, want)
	}

	// zig is special-cased: it should set both C and CXX compilers for a
	// hybrid project, via CMAKE_<LANG>_COMPILER_ARG1, not a bare "zig cc".
	got = CompilerArgs("zig", "hybrid")
	wantSet := map[string]bool{
		"-DCMAKE_C_COMPILER=zig": true, "-DCMAKE_C_COMPILER_ARG1=cc": true,
		"-DCMAKE_CXX_COMPILER=zig": true, "-DCMAKE_CXX_COMPILER_ARG1=c++": true,
	}
	if len(got) != len(wantSet) {
		t.Fatalf("CompilerArgs(zig, hybrid) = %v, want 4 args covering both C and C++", got)
	}
	for _, a := range got {
		if !wantSet[a] {
			t.Errorf("unexpected zig compiler arg %q", a)
		}
	}
}

func TestGenerateExecutableTargetTypeUnchanged(t *testing.T) {
	content := mustGenerate(t, config.Config{
		ProjectName: "appproj",
		CppVersion:  20,
		Executable:  "main",
	})
	for _, want := range []string{
		"add_executable(main ${SOURCES})",
		"target_include_directories(main PRIVATE include)",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("plain executable project missing %q:\n%s", want, content)
		}
	}
	for _, unwanted := range []string{"add_library(", "install(", "GNUInstallDirs"} {
		if strings.Contains(content, unwanted) {
			t.Errorf("plain executable project should not contain %q:\n%s", unwanted, content)
		}
	}
}

func TestGenerateStaticLibraryTargetType(t *testing.T) {
	dir := t.TempDir()
	content := mustGenerateInDir(t, dir, config.Config{
		ProjectName: "mylib",
		CppVersion:  20,
		Executable:  "mylib",
		TargetType:  "static_library",
	})

	for _, want := range []string{
		"add_library(mylib STATIC ${SOURCES})",
		"target_include_directories(mylib PUBLIC $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include> $<INSTALL_INTERFACE:include>)",
		"include(GNUInstallDirs)",
		"install(TARGETS mylib",
		"EXPORT mylibTargets",
		"install(DIRECTORY include/ DESTINATION ${CMAKE_INSTALL_INCLUDEDIR})",
		"NAMESPACE mylib::",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("static_library project missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "add_executable(") {
		t.Errorf("static_library project should not declare an add_executable when no examples/ demo exists:\n%s", content)
	}
}

func TestGenerateSharedLibraryTargetType(t *testing.T) {
	content := mustGenerate(t, config.Config{
		ProjectName: "mylib",
		CppVersion:  20,
		Executable:  "mylib",
		TargetType:  "shared_library",
	})
	if !strings.Contains(content, "add_library(mylib SHARED ${SOURCES})") {
		t.Errorf("shared_library project missing SHARED add_library:\n%s", content)
	}
}

func TestGenerateLibraryWithDemoExecutable(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "examples"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "examples", "demo.cpp"), []byte("int main(){}"), 0644); err != nil {
		t.Fatal(err)
	}

	content := mustGenerateInDir(t, dir, config.Config{
		ProjectName: "mylib",
		CppVersion:  20,
		Executable:  "mylib",
		TargetType:  "static_library",
	})

	for _, want := range []string{
		`file(GLOB_RECURSE mylib_SOURCES "examples/*.cpp")`,
		"add_executable(mylib_demo ${mylib_SOURCES})",
		"target_link_libraries(mylib_demo PRIVATE mylib)",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("library-with-demo project missing %q:\n%s", want, content)
		}
	}
}

func TestDetectCompilerLauncher(t *testing.T) {
	t.Run("none on PATH", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if _, _, found := DetectCompilerLauncher(); found {
			t.Error("expected no compiler launcher to be found on an empty PATH")
		}
	})

	t.Run("ccache found", func(t *testing.T) {
		dir := t.TempDir()
		fakeTool(t, dir, "ccache")
		t.Setenv("PATH", dir)

		tool, path, found := DetectCompilerLauncher()
		if !found || tool != "ccache" || !strings.Contains(path, "ccache") {
			t.Errorf("DetectCompilerLauncher() = (%q, %q, %v), want ccache found", tool, path, found)
		}
	})

	t.Run("ccache preferred over sccache", func(t *testing.T) {
		dir := t.TempDir()
		fakeTool(t, dir, "ccache")
		fakeTool(t, dir, "sccache")
		t.Setenv("PATH", dir)

		tool, _, found := DetectCompilerLauncher()
		if !found || tool != "ccache" {
			t.Errorf("DetectCompilerLauncher() = (%q, _, %v), want ccache preferred when both are present", tool, found)
		}
	})
}

func TestStandardConfigureFlags(t *testing.T) {
	t.Run("no launcher on PATH", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		flags := StandardConfigureFlags(config.Config{})
		if len(flags) != 2 {
			t.Errorf("StandardConfigureFlags() = %v, want just the two unconditional flags", flags)
		}
	})

	t.Run("launcher found and wired in", func(t *testing.T) {
		dir := t.TempDir()
		fakeTool(t, dir, "ccache")
		t.Setenv("PATH", dir)

		flags := StandardConfigureFlags(config.Config{})
		joined := strings.Join(flags, " ")
		for _, want := range []string{"CMAKE_C_COMPILER_LAUNCHER", "CMAKE_CXX_COMPILER_LAUNCHER"} {
			if !strings.Contains(joined, want) {
				t.Errorf("StandardConfigureFlags() = %v, missing %q", flags, want)
			}
		}
	})

	t.Run("disable_ccache opts out even when found", func(t *testing.T) {
		dir := t.TempDir()
		fakeTool(t, dir, "ccache")
		t.Setenv("PATH", dir)

		flags := StandardConfigureFlags(config.Config{DisableCcache: true})
		if strings.Contains(strings.Join(flags, " "), "COMPILER_LAUNCHER") {
			t.Errorf("StandardConfigureFlags() with DisableCcache = %v, should not wire in a launcher", flags)
		}
	})
}

// fakeTool creates an executable file named name in dir, so exec.LookPath
// can find it via a PATH containing dir - used to test PATH-based tool
// detection without depending on what's actually installed on the test
// runner.
func fakeTool(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
}

// mustGenerateInDir is like mustGenerate but writes into a caller-supplied
// directory instead of a fresh t.TempDir(), so a test can seed files (like
// examples/demo.cpp) before calling Generate.
func mustGenerateInDir(t *testing.T, dir string, c config.Config) string {
	t.Helper()
	if err := Generate(dir, c); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "CMakeLists.txt"))
	if err != nil {
		t.Fatalf("failed to read generated CMakeLists.txt: %v", err)
	}
	return string(data)
}
