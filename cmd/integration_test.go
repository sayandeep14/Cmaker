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
)

// TestIntegrationScaffoldBuildRun scaffolds a real project into a temp dir,
// configures/builds it with the real cmake + compiler on PATH, runs the
// resulting binary, and checks its output - exercising the actual
// scaffold -> cmake.Generate -> cmake -> compiler -> binary pipeline, not
// just the Go-level string generation unit tests already cover.
func TestIntegrationScaffoldBuildRun(t *testing.T) {
	requireTool(t, "cmake")

	dir := t.TempDir()
	if err := scaffoldProject(dir, "integrationtest", "default", "cpp", "", false, false); err != nil {
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
