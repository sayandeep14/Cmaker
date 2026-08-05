package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"cmaker/internal/cmake"
	"cmaker/internal/config"
	tmpl "cmaker/internal/templates"
)

func newScaffoldFlags(c *cobra.Command) (template *string, lang *string, compiler *string, withRust *bool, withZig *bool, runner *string) {
	template = c.Flags().String("template", "default", "project template to use (see 'cmaker templates')")
	lang = c.Flags().String("lang", "cpp", "project language: cpp, c, or hybrid (only supported with --template=default)")
	compiler = c.Flags().String("compiler", "", "compiler to use, saved into cmaker.yaml (e.g. clang++-17, or 'zig' to use zig as the C/C++ compiler)")
	withRust = c.Flags().Bool("with-rust", false, "add a Rust crate wired into the build via cargo (only supported with --template=default)")
	withZig = c.Flags().Bool("with-zig", false, "add a Zig library wired into the build via zig build-lib (only supported with --template=default)")
	runner = c.Flags().String("with", "", "custom compile-and-run tool to always use for 'cmaker run' in this project (e.g. crun), saved into cmaker.yaml's 'runner'")
	return
}

var newCmd = &cobra.Command{
	Use:   "new [name]",
	Short: "Scaffold a new project into ./<name>",
	Args:  cobra.MaximumNArgs(1),
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold a project into the current directory",
	Args:  cobra.NoArgs,
}

func init() {
	newTemplate, newLang, newCompiler, newWithRust, newWithZig, newRunner := newScaffoldFlags(newCmd)
	newCmd.RunE = func(cmd *cobra.Command, args []string) error {
		name := "MyProject"
		if len(args) == 1 {
			name = args[0]
		}
		return scaffoldProject(name, name, *newTemplate, *newLang, *newCompiler, *newWithRust, *newWithZig, *newRunner)
	}

	initTemplate, initLang, initCompiler, initWithRust, initWithZig, initRunner := newScaffoldFlags(initCmd)
	initCmd.RunE = func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to determine current directory: %w", err)
		}
		name := filepath.Base(cwd)
		return scaffoldProject(".", name, *initTemplate, *initLang, *initCompiler, *initWithRust, *initWithZig, *initRunner)
	}
}

// scaffoldProject writes a new project into root (creating it if it doesn't
// already exist), using the given project name and the named template
// (see internal/templates - each embedded subdirectory there is a
// template, described by its own meta.yaml). --lang, --with-rust, and
// --with-zig only apply to the "default" template: every other template is
// a concrete C++ dependency showcase (SFML/raylib/Catch2/...) and composing
// them with a language switch or a Rust/Zig crate isn't supported yet.
func scaffoldProject(root string, name string, templateName string, language string, compiler string, withRust bool, withZig bool, runner string) error {
	if !config.ValidLanguages[language] {
		return fmt.Errorf("unknown --lang %q (expected cpp, c, or hybrid)", language)
	}
	if language != "cpp" && templateName != "default" {
		return fmt.Errorf("--lang=%s is only supported with --template=default (template %q is C++-specific)", language, templateName)
	}
	if withRust && templateName != "default" {
		return fmt.Errorf("--with-rust is only supported with --template=default (template %q is C++-specific)", templateName)
	}
	if withZig && templateName != "default" {
		return fmt.Errorf("--with-zig is only supported with --template=default (template %q is C++-specific)", templateName)
	}

	meta, err := tmpl.LoadMeta(templateName)
	if err != nil {
		available, _ := tmpl.List()
		names := make([]string, len(available))
		for i, m := range available {
			names[i] = m.Name
		}
		return fmt.Errorf("unknown template %q (available: %v)", templateName, names)
	}

	infof("Initializing %s (Template: %s, Language: %s)...", name, meta.Name, language)

	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		return fmt.Errorf("failed to create src/: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "include"), 0755); err != nil {
		return fmt.Errorf("failed to create include/: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "build"), 0755); err != nil {
		return fmt.Errorf("failed to create build/: %w", err)
	}

	cfg := config.Config{
		ProjectName:   name,
		SchemaVersion: config.CurrentSchemaVersion,
		Executable:    "main",
		IncludeDirs:   []string{"include"},
		LinkLibraries: meta.LinkLibraries,
		Dependencies:  meta.Dependencies,
		Compiler:      compiler,
		Runner:        runner,
	}

	switch language {
	case "c":
		cfg.Language = "c"
		cfg.CVersion = 17
		if err := writeCSources(root); err != nil {
			return fmt.Errorf("failed to write C sources: %w", err)
		}
	case "hybrid":
		cfg.Language = "hybrid"
		cfg.CVersion = 17
		cfg.CppVersion = meta.CppVersion
		if err := writeHybridSources(root); err != nil {
			return fmt.Errorf("failed to write hybrid sources: %w", err)
		}
	default:
		cfg.CppVersion = meta.CppVersion
		cfg.CMakeExtra = meta.CMakeExtra
		if err := tmpl.WriteFiles(meta.Name, root); err != nil {
			return fmt.Errorf("failed to write template files: %w", err)
		}
	}

	if withRust {
		cfg.Rust = &config.RustConfig{Enabled: true, CrateDir: "rust"}
		if err := addRustCrate(root); err != nil {
			return fmt.Errorf("failed to write Rust crate: %w", err)
		}
	}
	if withZig {
		cfg.Zig = &config.ZigConfig{Enabled: true, SrcDir: "zig"}
		if err := addZigLib(root); err != nil {
			return fmt.Errorf("failed to write Zig library: %w", err)
		}
	}
	if withRust || withZig {
		if err := writeInteropDemoMain(root, language, withRust, withZig); err != nil {
			return fmt.Errorf("failed to write interop demo main: %w", err)
		}
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal cmaker.yaml: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmaker.yaml"), data, 0644); err != nil {
		return fmt.Errorf("failed to write cmaker.yaml: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("build/\n"), 0644); err != nil {
		return fmt.Errorf("failed to write .gitignore: %w", err)
	}
	if err := cmake.Generate(root, cfg); err != nil {
		return fmt.Errorf("failed to write CMakeLists.txt: %w", err)
	}

	if err := cmake.ValidateCompilerSupportsStandard(cfg.Compiler, cfg.Language, cfg.CppVersion, cfg.CVersion); err != nil {
		warnf("%v", err)
	}

	// PRE-FLIGHT: run CMake configuration immediately. A failure here is a
	// soft warning (e.g. missing compiler, or a dependency-fetching template
	// run offline) - the scaffold itself is still valid, so this does not
	// fail the command.
	infof("Running initial CMake configuration...")
	configArgs := []string{"-S", root, "-B", filepath.Join(root, "build"), cmake.PolicyVersionMinFlag}
	configArgs = append(configArgs, cmake.CompilerArgs(cfg.Compiler, cfg.Language)...)
	configCmd := exec.Command("cmake", configArgs...)
	var stderr bytes.Buffer
	configCmd.Stderr = &stderr
	if flagVerbose {
		configCmd.Stdout = os.Stdout
		configCmd.Stderr = os.Stderr
	}

	if err := configCmd.Run(); err != nil {
		warnf("initial CMake config failed: %s", firstLine(stderr.String()))
		infof("Re-run with -v to see the full CMake output, or check 'cmaker doctor'.")
	} else {
		okf("Project initialized and build folder primed.")
	}
	return nil
}

// firstLine trims a captured stderr blob down to the first non-empty line,
// so the default (non -v) error stays a short, actionable one-liner instead
// of dumping raw CMake output.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return "(no output captured)"
}

const cHelloWorld = `#include <stdio.h>

int main(void) {
    printf("Hello from cmaker (C project)!\n");
    return 0;
}
`

func writeCSources(root string) error {
	return os.WriteFile(filepath.Join(root, "src", "main.c"), []byte(cHelloWorld), 0644)
}

const hybridHeader = `#ifndef MATHLIB_H
#define MATHLIB_H

#ifdef __cplusplus
extern "C" {
#endif

int mathlib_add(int a, int b);

#ifdef __cplusplus
}
#endif

#endif
`

const hybridCSource = `#include "mathlib.h"

int mathlib_add(int a, int b) {
    return a + b;
}
`

const hybridMainCpp = `#include <iostream>
#include "mathlib.h"

int main() {
    std::cout << "Hello from cmaker (hybrid C/C++ project)! 2 + 3 = "
              << mathlib_add(2, 3) << "\n";
    return 0;
}
`

// writeHybridSources scaffolds a minimal but real example of the pattern a
// hybrid project needs: a C library (mathlib) exposed through an
// extern "C" header, consumed from a C++ main().
func writeHybridSources(root string) error {
	if err := os.WriteFile(filepath.Join(root, "include", "mathlib.h"), []byte(hybridHeader), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "src", "mathlib.c"), []byte(hybridCSource), 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "src", "main.cpp"), []byte(hybridMainCpp), 0644)
}
