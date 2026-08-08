package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"cmaker/internal/config"
)

// resolveOnlyFiles expands pattern (a plain path or a glob like
// 'tests/*.cpp') into the list of source files it matches, sorted for
// deterministic ordering. filepath.Glob handles a plain literal path with
// no metacharacters the same way - it just checks existence - so this is
// the single resolution path for both '--only=file.cpp' and
// '--only=tests/*.cpp'.
func resolveOnlyFiles(pattern string) ([]string, error) {
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("--only: invalid pattern %q: %w", pattern, err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("--only: no files match %q", pattern)
	}
	sort.Strings(files)
	return files, nil
}

// compileOnly ad hoc-compiles every file matching pattern (a plain path or
// a glob like 'tests/*.cpp') into its own binary under
// build/.cmaker_scratch/<name>, without touching the main project's
// CMakeLists.txt or build tree - for scratch files and isolated experiments
// that shouldn't be wired into the main executable target. Each matched
// file is compiled independently and must be self-contained with its own
// main() - this never links multiple matched files together into one
// binary, since each is expected to already have one.
func compileOnly(cfg config.Config, pattern string) ([]string, error) {
	files, err := resolveOnlyFiles(pattern)
	if err != nil {
		return nil, err
	}
	bins := make([]string, 0, len(files))
	for _, file := range files {
		bin, err := compileOneFile(cfg, file)
		if err != nil {
			return nil, err
		}
		bins = append(bins, bin)
	}
	return bins, nil
}

// compileOneFile ad hoc-compiles a single source file into
// build/.cmaker_scratch/<name> - see compileOnly, which this is a helper
// for.
//
// Known limitation: the file is compiled standalone (project include dirs
// only, no linked libraries/dependencies) - it must be self-contained with
// its own main().
func compileOneFile(cfg config.Config, file string) (string, error) {
	if _, err := os.Stat(file); err != nil {
		return "", fmt.Errorf("--only file %q not found: %w", file, err)
	}

	ext := strings.ToLower(filepath.Ext(file))
	var compiler, std string
	switch ext {
	case ".c":
		compiler = adhocCompiler(cfg.Compiler, "c")
		cVersion := cfg.CVersion
		if cVersion == 0 {
			cVersion = 17
		}
		std = fmt.Sprintf("-std=c%d", cVersion)
	case ".cpp", ".cxx", ".cc":
		compiler = adhocCompiler(cfg.Compiler, "cpp")
		cppVersion := cfg.CppVersion
		if cppVersion == 0 {
			cppVersion = 17
		}
		std = fmt.Sprintf("-std=c++%d", cppVersion)
	default:
		return "", fmt.Errorf("--only: unsupported source extension %q (expected .c, .cpp, .cxx, or .cc)", ext)
	}

	scratchDir := filepath.Join("build", ".cmaker_scratch")
	if err := os.MkdirAll(scratchDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create scratch dir: %w", err)
	}
	outBin := filepath.Join(scratchDir, strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)))

	args := []string{std, "-o", outBin, file}
	for _, inc := range cfg.IncludeDirs {
		args = append(args, "-I"+inc)
	}

	infof("Compiling %s ad hoc (not part of the main %q target)...", file, cfg.Executable)
	compileCmd := exec.Command(compiler, args...)
	if err := runWithSpinner("Compiling "+filepath.Base(file), compileCmd); err != nil {
		return "", fmt.Errorf("compilation failed: %w", err)
	}
	return outBin, nil
}

// adhocCompiler picks the compiler for a --only compile: an explicit
// override (cmaker.yaml's compiler: field or --compiler) always wins,
// otherwise fall back to the portable "c++"/"cc" aliases rather than
// guessing at a specific clang++/g++ binary name.
func adhocCompiler(override, lang string) string {
	if override != "" {
		return override
	}
	if lang == "c" {
		return "cc"
	}
	return "c++"
}
