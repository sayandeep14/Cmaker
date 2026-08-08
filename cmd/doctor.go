package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"cmaker/internal/cmake"
	"cmaker/internal/config"
	"cmaker/internal/llm"
	"cmaker/internal/registry"
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
		checkAI, _ := cmd.Flags().GetBool("ai")
		return runDoctor(checkAI)
	},
}

func init() {
	doctorCmd.Flags().Bool("ai", false, "also test the configured Anthropic API key with a real request")
}

func runDoctor(checkAI bool) error {
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

	if compilers := cmake.DiscoverCompilers(); len(compilers) > 0 {
		fmt.Println(colorize(ansiBold, "Detected toolchains (use with 'cmaker build --compiler=<path>'):"))
		for _, c := range compilers {
			fmt.Printf("  -- %s\n", c)
		}
	}

	reportSystemPackages()

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

	if checkAI {
		reportAIStatus()
	}

	if missingRequired {
		return fmt.Errorf("one or more required tools are missing")
	}
	okf("All required tools are ready.")
	return nil
}

// reportAIStatus checks whether cmaker's AI-assisted features (heal,
// describe/improvise, explain, generate accessors) are actually usable -
// opt-in via --ai since, unlike every other doctor check, this makes a
// real network call to Anthropic (and spends a trivial amount of real API
// usage - see Client.TestConnection). Deliberately never contributes to
// missingRequired/doctor's exit code: AI features are meant to degrade
// gracefully when unconfigured, not block "cmaker is installed and
// usable for its core job."
func reportAIStatus() {
	fmt.Println(colorize(ansiBold, "AI-assisted features (heal/describe/explain/generate accessors):"))
	client, err := llm.NewClientFromEnv("")
	if err != nil {
		fmt.Printf("  -- ANTHROPIC_API_KEY: %s\n", colorize(ansiYellow, "Not configured"))
		fmt.Printf("       %v\n", err)
		return
	}
	if err := client.TestConnection(context.Background()); err != nil {
		fmt.Printf("  -- ANTHROPIC_API_KEY: %s\n", colorize(ansiYellow, "Configured but not working"))
		fmt.Printf("       %v\n", err)
		return
	}
	fmt.Printf("  -- ANTHROPIC_API_KEY: %s\n", colorize(ansiGreen, "Ready"))
}

// reportSystemPackages extends 'cmaker doctor' to cover §27's
// system_package registry entries (opencv today) - mirroring
// discoverCompilers' "report what's already usable on this machine"
// model, not just something `cmaker install` alone knows about. A no-op
// (prints nothing) if the registry has no system_package entries at all,
// so a build with no §27 entries yet still gets a clean 'doctor' output.
func reportSystemPackages() {
	var entries []registry.Entry
	for _, e := range registry.List() {
		if e.Kind == registry.KindSystemPackage || e.Kind == registry.KindPkgConfig {
			entries = append(entries, e)
		}
	}
	if len(entries) == 0 {
		return
	}
	fmt.Println(colorize(ansiBold, "Extended packages (see 'cmaker search'):"))
	for _, e := range entries {
		if manager, ok := systemPackageInstalledVia(e); ok {
			fmt.Printf("  -- %s: %s (via %s)\n", e.Name, colorize(ansiGreen, "Ready"), manager)
		} else {
			fmt.Printf("  -- %s: %s (see 'cmaker install %s')\n", e.Name, colorize(ansiYellow, "Not installed"), e.Name)
		}
	}
}

// systemPackageInstalledVia checks whether e is already installed via
// whichever of its PackageManagers is actually present on this machine -
// brew (macOS) and apt/dpkg (Debian/Ubuntu) only, matching
// installSystemPackage's own supported set (see cmd/install.go).
func systemPackageInstalledVia(e registry.Entry) (manager string, installed bool) {
	if pkg, ok := e.PackageManagers["brew"]; ok {
		if _, err := exec.LookPath("brew"); err == nil {
			if exec.Command("brew", "list", "--formula", pkg).Run() == nil {
				return "brew", true
			}
		}
	}
	if pkg, ok := e.PackageManagers["apt"]; ok {
		if _, err := exec.LookPath("dpkg"); err == nil {
			if exec.Command("dpkg", "-s", pkg).Run() == nil {
				return "apt", true
			}
		}
	}
	return "", false
}
