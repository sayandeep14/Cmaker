package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"

	"github.com/spf13/cobra"

	"cmaker/internal/cmake"
	"cmaker/internal/config"
)

type tool struct {
	name     string
	required bool
	installs map[string]string // GOOS -> install hint
}

var doctorTools = []tool{
	{name: "cmake", required: true, installs: map[string]string{
		"darwin": "brew install cmake", "linux": "sudo apt install cmake  (or your distro's package manager)", "windows": "choco install cmake  (or winget install Kitware.CMake)",
	}},
	{name: "make", required: false, installs: map[string]string{
		"darwin": "xcode-select --install", "linux": "sudo apt install build-essential", "windows": "install via MSYS2 or Visual Studio Build Tools",
	}},
	{name: "ninja", required: false, installs: map[string]string{
		"darwin": "brew install ninja", "linux": "sudo apt install ninja-build", "windows": "choco install ninja",
	}},
	{name: "clang++", required: false, installs: map[string]string{
		"darwin": "xcode-select --install", "linux": "sudo apt install clang", "windows": "install via LLVM releases",
	}},
	{name: "g++", required: false, installs: map[string]string{
		"darwin": "brew install gcc", "linux": "sudo apt install g++", "windows": "install via MSYS2",
	}},
	{name: "vcpkg", required: false, installs: map[string]string{
		"darwin": "see https://github.com/microsoft/vcpkg", "linux": "see https://github.com/microsoft/vcpkg", "windows": "see https://github.com/microsoft/vcpkg",
	}},
	{name: "conan", required: false, installs: map[string]string{
		"darwin": "pip install conan", "linux": "pip install conan", "windows": "pip install conan",
	}},
	{name: "ccache", required: false, installs: map[string]string{
		"darwin": "brew install ccache", "linux": "sudo apt install ccache", "windows": "choco install ccache",
	}},
	{name: "sccache", required: false, installs: map[string]string{
		"darwin": "brew install sccache", "linux": "see https://github.com/mozilla/sccache", "windows": "choco install sccache",
	}},
	{name: "clang-format", required: false, installs: map[string]string{
		"darwin": "brew install clang-format", "linux": "sudo apt install clang-format", "windows": "install via LLVM releases",
	}},
	{name: "clang-tidy", required: false, installs: map[string]string{
		"darwin": "brew install llvm  (clang-tidy ships with it)", "linux": "sudo apt install clang-tidy", "windows": "install via LLVM releases",
	}},
	{name: "gcovr", required: false, installs: map[string]string{
		"darwin": "pip install gcovr", "linux": "pip install gcovr  (or sudo apt install gcovr)", "windows": "pip install gcovr",
	}},
	{name: "doxygen", required: false, installs: map[string]string{
		"darwin": "brew install doxygen", "linux": "sudo apt install doxygen", "windows": "choco install doxygen.install",
	}},
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check the local toolchain for cmake/compiler/build-system availability",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDoctor()
	},
}

func runDoctor() error {
	fmt.Printf("🩺 OS: %s\n", runtime.GOOS)

	missingRequired := false
	haveCompiler := false

	for _, t := range doctorTools {
		_, err := exec.LookPath(t.name)
		ready := err == nil
		if t.name == "clang++" || t.name == "g++" {
			if ready {
				haveCompiler = true
			}
		}
		if ready {
			fmt.Printf("  -- %s: %s\n", t.name, colorize(ansiGreen, "Ready"))
			continue
		}
		fmt.Printf("  -- %s: %s\n", t.name, colorize(ansiYellow, "Missing"))
		if hint, ok := t.installs[runtime.GOOS]; ok {
			fmt.Printf("       install: %s\n", hint)
		}
		if t.required {
			missingRequired = true
		}
	}

	if !haveCompiler {
		warnf("no C++ compiler found (need clang++ or g++)")
		missingRequired = true
	}

	if compilers := discoverCompilers(); len(compilers) > 0 {
		fmt.Println(colorize(ansiBold, "Detected toolchains (use with 'cmaker build --compiler=<path>'):"))
		for _, c := range compilers {
			fmt.Printf("  -- %s\n", c)
		}
	}

	if launcherTool, launcherPath, found := cmake.DetectCompilerLauncher(); found {
		disabled := false
		if cfg, ok := config.TryLoad("."); ok {
			disabled = cfg.DisableCcache
		}
		if disabled {
			fmt.Printf("Compiler cache: %s found (%s) but disabled via cmaker.yaml's disable_ccache\n", launcherTool, launcherPath)
		} else {
			fmt.Printf("Compiler cache: %s - %s\n", launcherTool, colorize(ansiGreen, "wired into every configure automatically"))
		}
	}

	// Rust/Zig toolchains are only checked when the project in the current
	// directory actually opts into them (rust.enabled: true in cmaker.yaml)
	// - a plain C/C++ project should never see (or pay the cost of) a check
	// for a toolchain it doesn't use.
	if cfg, ok := config.TryLoad("."); ok {
		if cfg.Rust != nil && cfg.Rust.Enabled {
			fmt.Println(colorize(ansiBold, "Rust toolchain (required by this project's cmaker.yaml):"))
			for _, name := range []string{"cargo", "rustc"} {
				if _, err := exec.LookPath(name); err == nil {
					fmt.Printf("  -- %s: %s\n", name, colorize(ansiGreen, "Ready"))
				} else {
					fmt.Printf("  -- %s: %s\n", name, colorize(ansiYellow, "Missing"))
					fmt.Printf("       install: see https://rustup.rs\n")
					missingRequired = true
				}
			}
		}
		if cfg.Zig != nil && cfg.Zig.Enabled {
			fmt.Println(colorize(ansiBold, "Zig toolchain (required by this project's cmaker.yaml):"))
			if _, err := exec.LookPath("zig"); err == nil {
				fmt.Printf("  -- zig: %s\n", colorize(ansiGreen, "Ready"))
			} else {
				fmt.Printf("  -- zig: %s\n", colorize(ansiYellow, "Missing"))
				fmt.Printf("       install: see https://ziglang.org/download/\n")
				missingRequired = true
			}
		}
	}

	if missingRequired {
		return fmt.Errorf("one or more required tools are missing")
	}
	okf("All required tools are ready.")
	return nil
}

var compilerNameRe = regexp.MustCompile(`^(clang\+\+|clang|g\+\+|gcc)(-[0-9]+(\.[0-9]+)?)?$`)

// discoverCompilers enumerates every C/C++ compiler found on PATH plus a
// handful of common non-PATH install locations, so users with multiple
// toolchains installed (several Homebrew/apt clang or gcc versions, a
// cross-compiler) can see and pick between them, instead of doctor only
// reporting a single ready/missing clang++/g++.
func discoverCompilers() []string {
	dirs := filepath.SplitList(os.Getenv("PATH"))
	dirs = append(dirs, "/opt/homebrew/opt/llvm/bin", "/usr/local/opt/llvm/bin")

	seen := map[string]bool{}
	var found []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !compilerNameRe.MatchString(e.Name()) {
				continue
			}
			full := filepath.Join(dir, e.Name())
			if seen[full] {
				continue
			}
			if info, err := os.Stat(full); err != nil || info.Mode()&0111 == 0 {
				continue
			}
			seen[full] = true
			found = append(found, full)
		}
	}
	sort.Strings(found)
	return found
}
