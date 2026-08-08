package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"cmaker/internal/config"
	"cmaker/internal/registry"
)

func TestResolveInstallChainNoRequires(t *testing.T) {
	entry, ok := registry.Find("fmt")
	if !ok {
		t.Fatal("expected to find fmt in the registry")
	}
	chain, err := resolveInstallChain(entry, config.Config{})
	if err != nil {
		t.Fatalf("resolveInstallChain() error = %v", err)
	}
	if len(chain) != 1 || chain[0].Name != "fmt" {
		t.Errorf("resolveInstallChain() = %+v, want just [fmt]", chain)
	}
}

func TestResolveInstallChainWithRequires(t *testing.T) {
	entry, ok := registry.Find("crow")
	if !ok {
		t.Fatal("expected to find crow in the registry")
	}
	chain, err := resolveInstallChain(entry, config.Config{})
	if err != nil {
		t.Fatalf("resolveInstallChain() error = %v", err)
	}
	if len(chain) != 2 {
		t.Fatalf("resolveInstallChain(crow) = %+v, want 2 entries (asio, crow)", chain)
	}
	if chain[0].Name != "asio" {
		t.Errorf("resolveInstallChain(crow)[0].Name = %q, want %q (prerequisite first)", chain[0].Name, "asio")
	}
	if chain[1].Name != "crow" {
		t.Errorf("resolveInstallChain(crow)[1].Name = %q, want %q", chain[1].Name, "crow")
	}
}

func TestResolveInstallChainSkipsAlreadyInstalledPrerequisite(t *testing.T) {
	entry, ok := registry.Find("imgui")
	if !ok {
		t.Fatal("expected to find imgui in the registry")
	}
	cfg := config.Config{Dependencies: []config.Dependency{{Name: "glfw"}}}
	chain, err := resolveInstallChain(entry, cfg)
	if err != nil {
		t.Fatalf("resolveInstallChain() error = %v", err)
	}
	if len(chain) != 1 || chain[0].Name != "imgui" {
		t.Errorf("resolveInstallChain(imgui) with glfw already installed = %+v, want just [imgui]", chain)
	}
}

func withFakeRegistry(t *testing.T, entries map[string]registry.Entry) {
	t.Helper()
	old := registryFind
	registryFind = func(name string) (registry.Entry, bool) {
		e, ok := entries[name]
		return e, ok
	}
	t.Cleanup(func() { registryFind = old })
}

func TestResolveInstallChainDetectsCycle(t *testing.T) {
	a := registry.Entry{Name: "a", Requires: []string{"b"}}
	b := registry.Entry{Name: "b", Requires: []string{"a"}}
	withFakeRegistry(t, map[string]registry.Entry{"a": a, "b": b})

	if _, err := resolveInstallChain(a, config.Config{}); err == nil {
		t.Error("resolveInstallChain() with a circular 'requires': expected an error, got nil")
	}
}

func TestResolveInstallChainDetectsSelfCycle(t *testing.T) {
	self := registry.Entry{Name: "self-cycle-test", Requires: []string{"self-cycle-test"}}
	withFakeRegistry(t, map[string]registry.Entry{"self-cycle-test": self})

	if _, err := resolveInstallChain(self, config.Config{}); err == nil {
		t.Error("resolveInstallChain() with a self-referencing 'requires': expected an error, got nil")
	}
}

func TestResolveInstallChainMissingRequiredEntry(t *testing.T) {
	orphan := registry.Entry{Name: "orphan", Requires: []string{"does-not-exist"}}
	withFakeRegistry(t, map[string]registry.Entry{"orphan": orphan})

	if _, err := resolveInstallChain(orphan, config.Config{}); err == nil {
		t.Error("resolveInstallChain() with a 'requires' entry missing from the registry: expected an error, got nil")
	}
}

// TestInstallSystemPackagePacmanNeverAttemptedOffWindows is the one part of
// installSystemPackage's pacman branch that's actually testable from
// macOS/Linux CI (runtime.GOOS is a compile-time constant, so the "windows"
// success path itself can't be exercised here - see ROADMAP.md's Windows-
// support entry). It stubs a fake "pacman" on PATH that would create a
// sentinel file if actually invoked, and confirms installSystemPackage
// never calls it on this platform even though it's resolvable via
// exec.LookPath - proving the windowsOnly gate isn't just "hope GOOS
// matches", it actually skips the manager entirely.
func TestInstallSystemPackagePacmanNeverAttemptedOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this test specifically verifies pacman is skipped OFF windows")
	}

	fakeBin := t.TempDir()
	sentinel := filepath.Join(fakeBin, "pacman-was-invoked")
	script := "#!/bin/sh\ntouch " + sentinel + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "pacman"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := installSystemPackage("testpkg", map[string]string{"pacman": "mingw-w64-ucrt-x86_64-testpkg"})
	if err == nil {
		t.Error("installSystemPackage() with only a pacman entry, off Windows: expected an error (no manager should be attempted), got nil")
	}
	if _, statErr := os.Stat(sentinel); statErr == nil {
		t.Error("installSystemPackage() invoked the stub pacman despite not running on Windows")
	}
}

func TestInstallFetchMessageKinds(t *testing.T) {
	tests := []struct {
		name string
		dep  config.Dependency
		want string
	}{
		{"cpm", config.Dependency{Name: "fmt", Repo: "fmtlib/fmt", Tag: "11.2.0"}, "Fetching fmt (fmtlib/fmt@11.2.0)..."},
		{"system_package", config.Dependency{Name: "opencv", Kind: config.DependencyKindSystemPackage, FindPackage: "OpenCV"}, "Wiring in opencv via find_package(OpenCV)..."},
		{"prebuilt_archive", config.Dependency{Name: "onnxruntime", Kind: config.DependencyKindPrebuiltArchive, ArchiveURL: "https://example.com/x.tgz"}, "Downloading onnxruntime from https://example.com/x.tgz..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := installFetchMessage(tt.dep); got != tt.want {
				t.Errorf("installFetchMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}
