package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

// defaultClangFormat is written into every scaffolded project (see
// scaffoldProject in new.go) - a single blessed style so `cmaker fmt`
// produces the same output everywhere, instead of every project
// hand-rolling its own .clang-format (or having none at all). 4-space
// indent, braces attached to the same line - matches the style cmaker's
// own template/scaffold code already uses (see e.g. new.go's
// libraryHeaderTemplate), so a fresh project's generated example code
// doesn't itself violate the style it ships with.
const defaultClangFormat = `BasedOnStyle: LLVM
IndentWidth: 4
ColumnLimit: 100
AccessModifierOffset: -4
AllowShortFunctionsOnASingleLine: Empty
BreakBeforeBraces: Attach
SpaceBeforeParens: ControlStatements
PointerAlignment: Left
NamespaceIndentation: None
`

// defaultClangTidy is a deliberately curated, low-noise check list -
// bugprone/performance/clang-analyzer plus a couple of high-value
// modernize/readability checks - rather than every check clang-tidy ships
// with (which includes several very opinionated ones, e.g.
// cppcoreguidelines-*/readability-magic-numbers, that would bury real
// findings in style noise on day one).
const defaultClangTidy = `Checks: >
  -*,
  bugprone-*,
  performance-*,
  clang-analyzer-*,
  modernize-use-nullptr,
  modernize-use-override,
  readability-braces-around-statements
WarningsAsErrors: ''
`

var fmtCmd = &cobra.Command{
	Use:   "fmt",
	Short: "Format every tracked source file with clang-format",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		check, _ := cmd.Flags().GetBool("check")
		return runFmt(check)
	},
}

var lintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Lint every tracked source file with clang-tidy",
	Long:  "Runs clang-tidy against build/compile_commands.json (see -DCMAKE_EXPORT_COMPILE_COMMANDS=ON,\nwired into every configure) - run 'cmaker build' first if it doesn't exist yet.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLint()
	},
}

func init() {
	fmtCmd.Flags().Bool("check", false, "check formatting without modifying files (non-zero exit if anything would change) - useful in CI")
}

// runFmt runs clang-format over every tracked source file, in place unless
// check is set (a dry-run, CI-friendly mode: nothing is written, and a
// non-zero exit means something would have been reformatted).
func runFmt(check bool) error {
	if _, err := exec.LookPath("clang-format"); err != nil {
		return fmt.Errorf("clang-format not found on PATH (see 'cmaker doctor')")
	}

	files, err := sourceFiles(".")
	if err != nil {
		return err
	}
	if len(files) == 0 {
		infof("No tracked source files found.")
		return nil
	}

	args := []string{}
	if check {
		args = append(args, "--dry-run", "--Werror")
	} else {
		args = append(args, "-i")
	}
	args = append(args, files...)

	cmd := exec.Command("clang-format", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if check {
			return fmt.Errorf("one or more files are not correctly formatted (run 'cmaker fmt' to fix)")
		}
		return fmt.Errorf("clang-format failed: %w", err)
	}

	if check {
		okf("All %d file(s) are correctly formatted.", len(files))
	} else {
		okf("Formatted %d file(s).", len(files))
	}
	return nil
}

// runLint runs clang-tidy against every tracked source file, using
// build/compile_commands.json as its compilation database (see
// ExportCompileCommandsFlag - wired into every configure, so this exists
// as soon as 'cmaker build' has run at least once).
func runLint() error {
	if _, err := exec.LookPath("clang-tidy"); err != nil {
		return fmt.Errorf("clang-tidy not found on PATH (see 'cmaker doctor')")
	}
	if _, err := os.Stat(filepath.Join("build", "compile_commands.json")); err != nil {
		return fmt.Errorf("build/compile_commands.json not found - run 'cmaker build' first so clang-tidy has a compilation database to work from")
	}

	files, err := sourceFiles(".")
	if err != nil {
		return err
	}
	if len(files) == 0 {
		infof("No tracked source files found.")
		return nil
	}

	args := append([]string{"-p", "build"}, files...)
	cmd := exec.Command("clang-tidy", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clang-tidy reported issues (see above)")
	}

	okf("clang-tidy found nothing to flag in %d file(s).", len(files))
	return nil
}

// sourceFiles walks root collecting every .c/.cpp/.cxx/.h/.hpp file cmaker
// itself would treat as project source (mirrors the extension set
// isBuildRequired already tracks in run.go), skipping build/ and .git/.
func sourceFiles(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "build" || info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".c", ".cpp", ".cxx", ".h", ".hpp":
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
