package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cmaker/internal/cmake"
	"cmaker/internal/config"
	"cmaker/internal/registry"
)

// applyExtraScaffolding is a post-scaffoldProject step for --with-benchmarks/
// --with-docs/--with-docker: deliberately kept separate from scaffoldProject
// itself (rather than adding 3 more parameters to an already-large
// signature) since these three are independent, purely additive layers on
// top of an already-complete project, not part of the core template/
// language/target-type decision scaffoldProject makes. A no-op (no
// cmaker.yaml reload) if none of the three flags are set.
func applyExtraScaffolding(root, name string, withBenchmarks, withDocs, withDocker bool) error {
	if !withBenchmarks && !withDocs && !withDocker {
		return nil
	}

	cfg, err := config.Load(filepath.Join(root, "cmaker.yaml"))
	if err != nil {
		return fmt.Errorf("failed to reload cmaker.yaml for extra scaffolding: %w", err)
	}

	if withBenchmarks {
		if err := addBenchmarkScaffold(root, &cfg); err != nil {
			return fmt.Errorf("failed to add benchmark scaffolding: %w", err)
		}
		// Only benchmarks changes cfg (a new dependency) - docs/docker are
		// static files with no cmaker.yaml/CMakeLists.txt impact, so only
		// pay for a rewrite when something actually needs it.
		if err := config.Save(filepath.Join(root, "cmaker.yaml"), cfg); err != nil {
			return fmt.Errorf("failed to update cmaker.yaml: %w", err)
		}
		if err := cmake.Generate(root, cfg); err != nil {
			return fmt.Errorf("failed to regenerate CMakeLists.txt: %w", err)
		}
		okf("Added bench/ (Google Benchmark) - see 'cmaker bench'")
	}

	if withDocs {
		if err := writeDoxyfile(root, cfg.ProjectName); err != nil {
			return fmt.Errorf("failed to add Doxygen scaffolding: %w", err)
		}
		if err := appendGitignore(root, "docs/\n"); err != nil {
			return fmt.Errorf("failed to update .gitignore: %w", err)
		}
		okf("Added Doxyfile - see 'cmaker docs'")
	}

	if withDocker {
		if err := writeDockerScaffold(root, cfg); err != nil {
			return fmt.Errorf("failed to add Docker scaffolding: %w", err)
		}
		okf("Added Dockerfile + .devcontainer/devcontainer.json")
	}

	return nil
}

const benchMainTemplate = `#include <benchmark/benchmark.h>
#include <string>

static void BM_StringCreation(benchmark::State& state) {
    for (auto _ : state) {
        std::string empty_string;
    }
}
BENCHMARK(BM_StringCreation);

// benchmark::benchmark_main (linked via cmaker.yaml's dependencies:)
// supplies main() - no BENCHMARK_MAIN() needed here.
`

// addBenchmarkScaffold adds the registry's "benchmark" dependency to cfg
// and writes a real, working bench/bench_main.cpp - the same
// "not a stub, something you build on" standard cmaker's other scaffolders
// hold themselves to (e.g. the catch2 template's TEST_CASE example).
func addBenchmarkScaffold(root string, cfg *config.Config) error {
	for _, dep := range cfg.Dependencies {
		if strings.EqualFold(dep.Name, "benchmark") {
			return nil // already present, nothing to add
		}
	}
	entry, ok := registry.Find("benchmark")
	if !ok {
		return fmt.Errorf("internal error: \"benchmark\" missing from the built-in registry")
	}
	cfg.Dependencies = append(cfg.Dependencies, entry.ToDependency())

	if err := os.MkdirAll(filepath.Join(root, "bench"), 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "bench", "bench_main.cpp"), []byte(benchMainTemplate), 0644)
}

const doxyfileTemplate = `# Minimal Doxygen config - see https://www.doxygen.nl/manual/config.html for
# every other option; anything not set here uses Doxygen's own default.
PROJECT_NAME           = "%s"
OUTPUT_DIRECTORY       = docs
INPUT                  = include src
RECURSIVE              = YES
EXTRACT_ALL            = YES
GENERATE_HTML          = YES
GENERATE_LATEX         = NO
QUIET                  = YES
`

// writeDoxyfile writes a minimal Doxyfile for projectName. Exported-ish
// (unexported, but shared with cmd/docs.go's runDocs, which writes a
// default one on the fly if none exists yet).
func writeDoxyfile(root, projectName string) error {
	return os.WriteFile(filepath.Join(root, "Doxyfile"), []byte(fmt.Sprintf(doxyfileTemplate, projectName)), 0644)
}

// Installs gcc-14/g++-14 explicitly (available directly in Ubuntu 24.04's
// own repos, no PPA needed) rather than relying on the distro's default
// g++ (13.x on 24.04), and a current CMake via pip rather than apt's -
// caught by actually running `docker build`, twice: gcc-13 rejects the
// default template's own cpp_version (26, deliberately bleeding-edge)
// outright ("does not support this, or CMake does not know the flags to
// enable it"), and even after switching to gcc-14 (which the compiler
// itself confirms accepts -std=gnu++26 directly), Ubuntu 24.04's apt CMake
// (3.28.3) still doesn't know how to map C++26 to a GCC flag - that
// mapping needs a CMake newer than what noble ships, which `pip install
// cmake` (a real, current prebuilt binary from PyPI) resolves without
// needing to manage a third-party apt repo's signing keys.
const dockerfileTemplate = `FROM ubuntu:24.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential gcc-14 g++-14 python3-pip git ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --break-system-packages --quiet cmake

WORKDIR /app
COPY . .

RUN cmake -S . -B build -DCMAKE_POLICY_VERSION_MINIMUM=3.5 -DCMAKE_C_COMPILER=gcc-14 -DCMAKE_CXX_COMPILER=g++-14 && cmake --build build

CMD ["%s"]
`

const devcontainerTemplate = `{
  "name": "cmaker project",
  "build": {
    "dockerfile": "../Dockerfile"
  },
  "customizations": {
    "vscode": {
      "extensions": ["ms-vscode.cpptools", "twxs.cmake"]
    }
  }
}
`

// appendGitignore appends line to root/.gitignore, creating the file if it
// somehow doesn't exist yet (every scaffoldProject-created project already
// has one, but this stays safe to call standalone too).
func appendGitignore(root, line string) error {
	f, err := os.OpenFile(filepath.Join(root, ".gitignore"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

// dockerignoreTemplate excludes the host's own build/ output from the
// image's build context - without this, `COPY . .` picks up
// build/CMakeCache.txt (already configured against the *host's* absolute
// path by cmaker's own pre-flight configure, or any local `cmaker build`),
// and the container's own `cmake -S . -B build` then fails outright
// ("The current CMakeCache.txt directory ... is different than the
// directory ... where CMakeCache.txt was created") since the cached path
// doesn't match the container's filesystem. Caught by actually building the
// generated Dockerfile with a real `docker build`, not just reading it.
const dockerignoreTemplate = `build/
.git/
.cmaker/
docs/
`

// writeDockerScaffold writes a Dockerfile (that builds the project with
// plain cmake + a compiler inside the container - cmaker itself doesn't
// need to be installed there, since CMakeLists.txt is already generated
// and committed), a .dockerignore, and a matching
// .devcontainer/devcontainer.json.
func writeDockerScaffold(root string, cfg config.Config) error {
	cmdLine := "/bin/sh"
	if config.TargetTypeOrDefault(cfg.TargetType) == "executable" {
		cmdLine = "./build/" + cfg.Executable
	}
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte(fmt.Sprintf(dockerfileTemplate, cmdLine)), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, ".dockerignore"), []byte(dockerignoreTemplate), 0644); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, ".devcontainer"), 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, ".devcontainer", "devcontainer.json"), []byte(devcontainerTemplate), 0644)
}
