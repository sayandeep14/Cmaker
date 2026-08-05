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
