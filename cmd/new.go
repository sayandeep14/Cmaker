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

// scaffoldFlagSet holds every flag shared by `new`/`init`/`create` - a
// struct rather than another positional return, since that list grew past
// the point a 9+-value multi-return stays readable (see git history for
// how many times this and scaffoldProject's own signature were extended
// one value at a time).
type scaffoldFlagSet struct {
	Template, Lang, Compiler, Runner, TargetType, Describe *string
	WithRust, WithZig, Lib                                 *bool
	WithBenchmarks, WithDocs, WithDocker                   *bool
}

func newScaffoldFlags(c *cobra.Command) *scaffoldFlagSet {
	f := &scaffoldFlagSet{}
	f.Template = c.Flags().String("template", "default", "project template to use (see 'cmaker templates')")
	f.Lang = c.Flags().String("lang", "cpp", "project language: cpp, c, or hybrid (only supported with --template=default)")
	f.Compiler = c.Flags().String("compiler", "", "compiler to use, saved into cmaker.yaml (e.g. clang++-17, or 'zig' to use zig as the C/C++ compiler)")
	f.WithRust = c.Flags().Bool("with-rust", false, "add a Rust crate wired into the build via cargo, linked into whichever template you pick")
	f.WithZig = c.Flags().Bool("with-zig", false, "add a Zig library wired into the build via zig build-lib, linked into whichever template you pick")
	f.Runner = c.Flags().String("with", "", "custom compile-and-run tool to always use for 'cmaker run' in this project (e.g. crun), saved into cmaker.yaml's 'runner'")
	f.TargetType = c.Flags().String("target-type", "executable", "target type: executable, static_library, or shared_library (only supported with --template=default, cpp)")
	f.Lib = c.Flags().Bool("lib", false, "shorthand for --target-type=static_library")
	f.Describe = c.Flags().String("describe", "", "describe the project in plain English and let an LLM pick the template/flags/packages for you (requires ANTHROPIC_API_KEY; conflicts with --template/--lang/--with-rust/--with-zig/--target-type/--lib)")
	f.WithBenchmarks = c.Flags().Bool("with-benchmarks", false, "add a bench/ directory wired to Google Benchmark (bench/*.cpp -> a <name>_bench executable, see 'cmaker bench')")
	f.WithDocs = c.Flags().Bool("with-docs", false, "scaffold a Doxyfile ('cmaker docs' builds API docs from it)")
	f.WithDocker = c.Flags().Bool("with-docker", false, "scaffold a Dockerfile + .devcontainer/devcontainer.json for building/running without a local toolchain")
	return f
}

// describeConflictsWithExplicitFlags reports an error if --describe was
// combined with any flag it's meant to decide for the caller - conflicting
// silently-overridden flags would be confusing, so this fails loudly
// instead (mirrors create.go's --backend/--ml vs --template conflict
// check).
func describeConflictsWithExplicitFlags(cmd *cobra.Command) error {
	for _, name := range []string{"template", "lang", "with-rust", "with-zig", "target-type", "lib"} {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("--describe picks --%s (and the other scaffold flags) for you - remove --%s or drop --describe", name, name)
		}
	}
	return nil
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
	newFlags := newScaffoldFlags(newCmd)
	newCmd.RunE = func(cmd *cobra.Command, args []string) error {
		name := "MyProject"
		if len(args) == 1 {
			name = args[0]
		}
		if *newFlags.Describe != "" {
			if err := describeConflictsWithExplicitFlags(cmd); err != nil {
				return err
			}
			return runDescribeAndScaffold(name, name, *newFlags.Describe, *newFlags.Compiler, *newFlags.Runner)
		}
		if err := scaffoldProject(name, name, *newFlags.Template, *newFlags.Lang, *newFlags.Compiler, *newFlags.WithRust, *newFlags.WithZig, *newFlags.Runner, resolveTargetType(*newFlags.TargetType, *newFlags.Lib)); err != nil {
			return err
		}
		return applyExtraScaffolding(name, name, *newFlags.WithBenchmarks, *newFlags.WithDocs, *newFlags.WithDocker)
	}

	initFlags := newScaffoldFlags(initCmd)
	initCmd.RunE = func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to determine current directory: %w", err)
		}
		name := filepath.Base(cwd)
		if *initFlags.Describe != "" {
			if err := describeConflictsWithExplicitFlags(cmd); err != nil {
				return err
			}
			return runDescribeAndScaffold(".", name, *initFlags.Describe, *initFlags.Compiler, *initFlags.Runner)
		}
		if err := scaffoldProject(".", name, *initFlags.Template, *initFlags.Lang, *initFlags.Compiler, *initFlags.WithRust, *initFlags.WithZig, *initFlags.Runner, resolveTargetType(*initFlags.TargetType, *initFlags.Lib)); err != nil {
			return err
		}
		return applyExtraScaffolding(".", name, *initFlags.WithBenchmarks, *initFlags.WithDocs, *initFlags.WithDocker)
	}
}

// resolveTargetType applies --lib as shorthand for --target-type=static_library,
// taking priority over an explicit --target-type when both are somehow set.
func resolveTargetType(targetType string, lib bool) string {
	if lib {
		return "static_library"
	}
	return targetType
}

// scaffoldProject writes a new project into root (creating it if it doesn't
// already exist), using the given project name and the named template
// (see internal/templates - each embedded subdirectory there is a
// template, described by its own meta.yaml). --lang and a non-executable
// --target-type only apply to the "default" template: every other template
// is a concrete C++ showcase and composing them with a language switch or
// library scaffolding isn't supported yet. --with-rust/--with-zig, by
// contrast, compose with *any* template (§18) - see the withRust/withZig
// handling below and interop.go's injectInteropUsageHint.
func scaffoldProject(root string, name string, templateName string, language string, compiler string, withRust bool, withZig bool, runner string, targetType string) error {
	if !config.ValidLanguages[language] {
		return fmt.Errorf("unknown --lang %q (expected cpp, c, or hybrid)", language)
	}
	targetType = config.TargetTypeOrDefault(targetType)
	if !config.ValidTargetTypes[targetType] {
		return fmt.Errorf("unknown --target-type %q (expected executable, static_library, or shared_library)", targetType)
	}
	if language != "cpp" && templateName != "default" {
		return fmt.Errorf("--lang=%s is only supported with --template=default (template %q is C++-specific)", language, templateName)
	}
	// --with-rust/--with-zig compose with any template (§18): the crate/
	// library is scaffolded and linked exactly the same way regardless of
	// which template it lands in - only *how the demo usage is surfaced*
	// differs (writeInteropDemoMain overwrites the placeholder main() the
	// 'default' template ships; every other template gets
	// injectInteropUsageHint instead, which only ever appends to the
	// template's own real main.cpp, see interop.go).
	if targetType != "executable" {
		if templateName != "default" {
			return fmt.Errorf("--target-type=%s is only supported with --template=default (template %q is C++-specific)", targetType, templateName)
		}
		if language != "cpp" {
			return fmt.Errorf("--target-type=%s doesn't support --lang=%s yet (library scaffolding is C++-only for now)", targetType, language)
		}
		if withRust || withZig {
			return fmt.Errorf("--target-type=%s doesn't compose with --with-rust/--with-zig yet", targetType)
		}
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

	executableName := "main"
	libName := ""
	if targetType != "executable" {
		// A library's CMake target is named after the project, not "main" -
		// add_library(main STATIC ...) would be a confusing target name,
		// and consumers linking against it want target_link_libraries(app
		// PRIVATE <projectname>), not PRIVATE main. filepath.Base guards
		// against 'name' doubling as a path (e.g. 'cmaker new ./sub/mylib'
		// or 'cmaker new /tmp/mylib', where root == name) - a CMake target
		// name and a header directory name can't contain slashes.
		libName = filepath.Base(name)
		executableName = libName
	}

	cfg := config.Config{
		ProjectName:   name,
		SchemaVersion: config.CurrentSchemaVersion,
		Executable:    executableName,
		IncludeDirs:   []string{"include"},
		LinkLibraries: meta.LinkLibraries,
		Dependencies:  meta.Dependencies,
		Compiler:      compiler,
		Runner:        runner,
	}
	if targetType != "executable" {
		cfg.TargetType = targetType
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
		if targetType != "executable" {
			if err := writeLibrarySources(root, libName); err != nil {
				return fmt.Errorf("failed to write library sources: %w", err)
			}
		} else if err := tmpl.WriteFiles(meta, root); err != nil {
			return fmt.Errorf("failed to write template files: %w", err)
		}
	}

	if meta.Testing {
		cfg.Testing = &config.TestingConfig{Enabled: true}
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
		if templateName == "default" {
			if err := writeInteropDemoMain(root, language, withRust, withZig); err != nil {
				return fmt.Errorf("failed to write interop demo main: %w", err)
			}
		} else if err := injectInteropUsageHint(root, withRust, withZig); err != nil {
			return fmt.Errorf("failed to add Rust/Zig usage hint: %w", err)
		}
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal cmaker.yaml: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmaker.yaml"), data, 0644); err != nil {
		return fmt.Errorf("failed to write cmaker.yaml: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("build/\n.cmaker/\n"), 0644); err != nil {
		return fmt.Errorf("failed to write .gitignore: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".clang-format"), []byte(defaultClangFormat), 0644); err != nil {
		return fmt.Errorf("failed to write .clang-format: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".clang-tidy"), []byte(defaultClangTidy), 0644); err != nil {
		return fmt.Errorf("failed to write .clang-tidy: %w", err)
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
	configArgs := append([]string{"-S", root, "-B", filepath.Join(root, "build")}, cmake.StandardConfigureFlags(cfg)...)
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

const libraryHeaderTemplate = `#ifndef %s
#define %s

namespace %s {

int add(int a, int b);

}  // namespace %s

#endif
`

const librarySourceTemplate = `#include "%s/%s.h"

namespace %s {

int add(int a, int b) {
    return a + b;
}

}  // namespace %s
`

const libraryDemoTemplate = `#include <iostream>
#include "%s/%s.h"

int main() {
    std::cout << "%s::add(2, 3) = " << %s::add(2, 3) << "\n";
    return 0;
}
`

// writeLibrarySources scaffolds a real, working example of the
// public/private header split a library needs (unlike an app, where a flat
// include/ works fine): a public header under include/<name>/<name>.h, its
// implementation in src/<name>.cpp, and an examples/demo.cpp consumer so
// "does my library actually work" stays a one-command `cmaker run` answer
// (see internal/cmake.Generate's demo-executable wiring).
func writeLibrarySources(root string, name string) error {
	ident := sanitizeIdentifier(name)
	ns := strings.ToLower(ident)
	guard := strings.ToUpper(ident) + "_H"

	headerDir := filepath.Join(root, "include", name)
	if err := os.MkdirAll(headerDir, 0755); err != nil {
		return fmt.Errorf("failed to create include/%s/: %w", name, err)
	}
	header := fmt.Sprintf(libraryHeaderTemplate, guard, guard, ns, ns)
	if err := os.WriteFile(filepath.Join(headerDir, name+".h"), []byte(header), 0644); err != nil {
		return err
	}

	source := fmt.Sprintf(librarySourceTemplate, name, name, ns, ns)
	if err := os.WriteFile(filepath.Join(root, "src", name+".cpp"), []byte(source), 0644); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Join(root, "examples"), 0755); err != nil {
		return fmt.Errorf("failed to create examples/: %w", err)
	}
	demo := fmt.Sprintf(libraryDemoTemplate, name, name, ns, ns)
	return os.WriteFile(filepath.Join(root, "examples", "demo.cpp"), []byte(demo), 0644)
}

// sanitizeIdentifier turns an arbitrary project name into a valid C++
// identifier (for a namespace name / header guard): non-alphanumeric
// characters become underscores, and a leading digit gets a leading
// underscore prepended.
func sanitizeIdentifier(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	ident := b.String()
	if ident == "" {
		return "_"
	}
	if ident[0] >= '0' && ident[0] <= '9' {
		return "_" + ident
	}
	return ident
}
