//go:build integration

// Integration tests actually invoke cmake and a real compiler, so they're
// gated behind the "integration" build tag (`go test -tags=integration ./...`)
// rather than running as part of the default `go test ./...` - CI
// environments without a C++ toolchain can still run the fast unit tests.
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"cmaker/internal/cmake"
	"cmaker/internal/config"
	"cmaker/internal/logs"
	"cmaker/internal/registry"
)

// TestIntegrationScaffoldBuildRun scaffolds a real project into a temp dir,
// configures/builds it with the real cmake + compiler on PATH, runs the
// resulting binary, and checks its output - exercising the actual
// scaffold -> cmake.Generate -> cmake -> compiler -> binary pipeline, not
// just the Go-level string generation unit tests already cover.
func TestIntegrationScaffoldBuildRun(t *testing.T) {
	requireTool(t, "cmake")

	dir := t.TempDir()
	if err := scaffoldProject(dir, "integrationtest", "default", "cpp", "", false, false, "", "executable"); err != nil {
		t.Fatalf("scaffoldProject() error = %v", err)
	}

	buildDir := filepath.Join(dir, "build")
	configCmd := exec.Command("cmake", "-S", dir, "-B", buildDir, cmake.PolicyVersionMinFlag)
	if out, err := configCmd.CombinedOutput(); err != nil {
		t.Fatalf("cmake configure failed: %v\n%s", err, out)
	}

	buildCmd := exec.Command("cmake", "--build", buildDir)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("cmake build failed: %v\n%s", err, out)
	}

	binary := filepath.Join(buildDir, "main")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("expected binary at %s: %v", binary, err)
	}

	out, err := exec.Command(binary).CombinedOutput()
	if err != nil {
		t.Fatalf("running scaffolded binary failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Hello from Cmaker") {
		t.Errorf("unexpected binary output: %s", out)
	}
}

// TestIntegrationLibraryScaffoldBuildRunInstall scaffolds a static library
// project (§16), builds it with a real cmake + compiler, runs its
// examples/demo.cpp demo executable, and installs it via `cmake --install`
// - exercising the whole add_library/target_link_libraries/install() path,
// not just the Go-level string generation unit tests already cover.
func TestIntegrationLibraryScaffoldBuildRunInstall(t *testing.T) {
	requireTool(t, "cmake")

	dir := t.TempDir()
	if err := scaffoldProject(dir, "libtest", "default", "cpp", "", false, false, "", "static_library"); err != nil {
		t.Fatalf("scaffoldProject() error = %v", err)
	}

	buildDir := filepath.Join(dir, "build")
	configCmd := exec.Command("cmake", "-S", dir, "-B", buildDir, cmake.PolicyVersionMinFlag)
	if out, err := configCmd.CombinedOutput(); err != nil {
		t.Fatalf("cmake configure failed: %v\n%s", err, out)
	}

	buildCmd := exec.Command("cmake", "--build", buildDir)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("cmake build failed: %v\n%s", err, out)
	}

	demo := filepath.Join(buildDir, "libtest_demo")
	if runtime.GOOS == "windows" {
		demo += ".exe"
	}
	out, err := exec.Command(demo).CombinedOutput()
	if err != nil {
		t.Fatalf("running demo binary failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "libtest::add(2, 3) = 5") {
		t.Errorf("unexpected demo output: %s", out)
	}

	installDir := t.TempDir()
	installCmd := exec.Command("cmake", "--install", buildDir, "--prefix", installDir)
	if out, err := installCmd.CombinedOutput(); err != nil {
		t.Fatalf("cmake --install failed: %v\n%s", out, err)
	}
	if _, err := os.Stat(filepath.Join(installDir, "include", "libtest", "libtest.h")); err != nil {
		t.Errorf("expected installed public header: %v", err)
	}
	if _, err := os.Stat(filepath.Join(installDir, "lib", "cmake", "libtest", "libtestConfig.cmake")); err != nil {
		t.Errorf("expected installed libtestConfig.cmake for find_package(): %v", err)
	}
}

// TestIntegrationInstallBuildRun exercises §17's real install pipeline end
// to end: scaffold a plain project, `cmaker install fmt` (registry lookup ->
// cmaker.yaml update -> real CPM fetch -> cmaker.lock with a real resolved
// commit), then actually build and run code that calls into fmt - proving
// the installed dependency is genuinely usable, not just present in YAML.
func TestIntegrationInstallBuildRun(t *testing.T) {
	requireTool(t, "cmake")

	dir := t.TempDir()
	if err := scaffoldProject(dir, "installtest", "default", "cpp", "", false, false, "", "executable"); err != nil {
		t.Fatalf("scaffoldProject() error = %v", err)
	}
	t.Chdir(dir)

	if err := runInstall("fmt", "", "", nil, nil, false); err != nil {
		t.Fatalf("runInstall(fmt) error = %v", err)
	}

	lf, err := registry.LoadLockfile(".")
	if err != nil {
		t.Fatalf("LoadLockfile() error = %v", err)
	}
	entry, ok := lf.Dependencies["fmt"]
	if !ok || entry.Commit == "" {
		t.Fatalf("expected cmaker.lock to have a resolved commit for fmt, got %+v", lf)
	}

	src := `#include <fmt/core.h>
int main() { fmt::print("install-test-ok: {}\n", 2 + 3); return 0; }
`
	if err := os.WriteFile(filepath.Join(dir, "src", "main.cpp"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	buildCmd := exec.Command("cmake", "--build", "build")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("cmake build failed: %v\n%s", err, out)
	}

	binary := filepath.Join("build", "main")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	out, err := exec.Command(binary).CombinedOutput()
	if err != nil {
		t.Fatalf("running built binary failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "install-test-ok: 5") {
		t.Errorf("unexpected output: %s", out)
	}
}

// TestIntegrationDomainTemplateWithRustCompose exercises §18's composition
// fix end to end: scaffold the 'backend' domain template together with
// --with-rust, and confirm both (a) the template's own real main.cpp
// content survived untouched and (b) the linked Rust crate actually builds
// and links into the final binary - not just that cmaker.yaml says so.
func TestIntegrationDomainTemplateWithRustCompose(t *testing.T) {
	requireTool(t, "cmake")
	requireTool(t, "cargo")

	dir := t.TempDir()
	if err := scaffoldProject(dir, "composetest", "backend", "cpp", "", true, false, "", "executable"); err != nil {
		t.Fatalf("scaffoldProject() error = %v", err)
	}

	mainSrc, err := os.ReadFile(filepath.Join(dir, "src", "main.cpp"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`svr.Get("/health"`, `#include "rustlib.h"`, "rust_add(2, 3)"} {
		if !strings.Contains(string(mainSrc), want) {
			t.Errorf("expected main.cpp to contain %q (template code preserved + Rust hint added):\n%s", want, mainSrc)
		}
	}

	buildDir := filepath.Join(dir, "build")
	configCmd := exec.Command("cmake", "-S", dir, "-B", buildDir, cmake.PolicyVersionMinFlag)
	if out, err := configCmd.CombinedOutput(); err != nil {
		t.Fatalf("cmake configure failed: %v\n%s", err, out)
	}
	buildCmd := exec.Command("cmake", "--build", buildDir)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("cmake build failed: %v\n%s", err, out)
	}

	binary := filepath.Join(buildDir, "main")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("expected binary at %s: %v", binary, err)
	}
}

// TestIntegrationBuildLogCapture exercises §24's log-capture foundation end
// to end: a real failing `cmaker build` (a genuine compiler error, not a
// synthetic one) must be captured as a *-fail.log findable via
// logs.LatestFailure, and a subsequent successful build must be captured
// as *-ok.log and not shadow the failure when a caller asks specifically
// for one. No LLM/API key involved - this is the AI-independent half of
// §24 the roadmap calls out as shipping and being verified on its own.
func TestIntegrationBuildLogCapture(t *testing.T) {
	requireTool(t, "cmake")

	dir := t.TempDir()
	if err := scaffoldProject(dir, "logtest", "default", "cpp", "", false, false, "", "executable"); err != nil {
		t.Fatalf("scaffoldProject() error = %v", err)
	}
	t.Chdir(dir)

	brokenSrc := "int main() { this_identifier_does_not_exist(); return 0; }\n"
	if err := os.WriteFile(filepath.Join(dir, "src", "main.cpp"), []byte(brokenSrc), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runBuild(false, "", 0); err == nil {
		t.Fatal("expected runBuild() to fail on genuinely broken source")
	}

	failLog, err := logs.LatestFailure(dir, "build")
	if err != nil {
		t.Fatalf("LatestFailure() error = %v", err)
	}
	data, err := os.ReadFile(failLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "this_identifier_does_not_exist") {
		t.Errorf("captured fail log doesn't contain the real compiler error:\n%s", data)
	}

	fixedSrc := "int main() { return 0; }\n"
	if err := os.WriteFile(filepath.Join(dir, "src", "main.cpp"), []byte(fixedSrc), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runBuild(false, "", 0); err != nil {
		t.Fatalf("runBuild() on fixed source failed: %v", err)
	}

	names, err := logs.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	var sawOK, sawFail bool
	for _, n := range names {
		if strings.HasSuffix(n, "-ok.log") {
			sawOK = true
		}
		if strings.HasSuffix(n, "-fail.log") {
			sawFail = true
		}
	}
	if !sawOK || !sawFail {
		t.Errorf("logs.List() = %v, want both a -fail.log and a -ok.log entry", names)
	}
}

// TestIntegrationDescribeScaffoldBuildRun exercises §25's natural-language
// scaffolding end to end against the real Anthropic API and a real build:
// `--describe` should pick a real template/package combination, and the
// resulting project should actually build. Skipped (not failed) when no
// ANTHROPIC_API_KEY is available, same policy as requireTool for a missing
// binary - this is a real network+API dependency, not something CI should
// be expected to have by default.
func TestIntegrationDescribeScaffoldBuildRun(t *testing.T) {
	requireTool(t, "cmake")
	requireEnv(t, "ANTHROPIC_API_KEY")

	dir := t.TempDir()
	root := filepath.Join(dir, "described")
	if err := runDescribeAndScaffold(root, "described", "a REST API backend that returns JSON, written in C++", "", ""); err != nil {
		t.Fatalf("runDescribeAndScaffold() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "cmaker.yaml")); err != nil {
		t.Fatalf("expected a scaffolded cmaker.yaml: %v", err)
	}

	buildDir := filepath.Join(root, "build")
	configCmd := exec.Command("cmake", "-S", root, "-B", buildDir, cmake.PolicyVersionMinFlag)
	if out, err := configCmd.CombinedOutput(); err != nil {
		t.Fatalf("cmake configure failed: %v\n%s", err, out)
	}
	buildCmd := exec.Command("cmake", "--build", buildDir)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("cmake build failed: %v\n%s", err, out)
	}

	binary := filepath.Join(buildDir, "main")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("expected binary at %s: %v", binary, err)
	}
}

// TestIntegrationOnlyCompileRun exercises the `--only` ad hoc single-file
// compile path (only.go) against a real compiler, without going through
// cmake at all.
func TestIntegrationOnlyCompileRun(t *testing.T) {
	requireTool(t, "c++")

	dir := t.TempDir()
	t.Chdir(dir)

	srcPath := filepath.Join(dir, "scratch.cpp")
	src := `#include <iostream>
int main() { std::cout << "only-test-ok\n"; return 0; }
`
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	binPath, err := compileOnly(config.Config{CppVersion: 17, Executable: "main", IncludeDirs: nil}, srcPath)
	if err != nil {
		t.Fatalf("compileOnly() error = %v", err)
	}

	out, err := exec.Command(binPath).CombinedOutput()
	if err != nil {
		t.Fatalf("running compiled scratch binary failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "only-test-ok") {
		t.Errorf("unexpected output: %s", out)
	}
}

func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not found on PATH, skipping integration test", name)
	}
}

func requireEnv(t *testing.T, name string) {
	t.Helper()
	if os.Getenv(name) == "" {
		t.Skipf("%s not set, skipping integration test", name)
	}
}
