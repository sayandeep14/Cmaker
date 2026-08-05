package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"cmaker/internal/config"
)

var coverageCmd = &cobra.Command{
	Use:   "coverage",
	Short: "Build with coverage instrumentation, run the project, and produce an HTML report",
	Long: "Requires 'coverage: true' in cmaker.yaml. Builds, runs ctest (if 'testing.enabled') or the\n" +
		"main executable/demo otherwise to generate coverage data, then uses gcovr to produce an\n" +
		"HTML report - gcovr understands the gcov-compatible .gcda/.gcno files both gcc and clang\n" +
		"produce for --coverage, so this works regardless of which compiler actually built the project.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCoverage()
	},
}

func runCoverage() error {
	cfg := loadConfigOrExit()
	if !cfg.Coverage {
		return fmt.Errorf("coverage isn't enabled for this project - add 'coverage: true' to cmaker.yaml first")
	}
	if _, err := exec.LookPath("gcovr"); err != nil {
		return fmt.Errorf("gcovr not found on PATH (see 'cmaker doctor') - needed to produce the HTML report")
	}

	if err := runBuild(false, "", 0); err != nil {
		return err
	}

	if cfg.Testing != nil && cfg.Testing.Enabled {
		infof("Running ctest to generate coverage data...")
		ctestCmd := exec.Command("ctest", "--test-dir", "build", "--output-on-failure")
		ctestCmd.Stdout, ctestCmd.Stderr = os.Stdout, os.Stderr
		if err := ctestCmd.Run(); err != nil {
			warnf("ctest reported failures - generating a coverage report from what ran anyway: %v", err)
		}
	} else {
		targetType := config.TargetTypeOrDefault(cfg.TargetType)
		exeName, err := runnableBinaryName(cfg, targetType)
		if err != nil {
			return fmt.Errorf("%w (coverage needs something to actually run - add 'testing: {enabled: true}', or an examples/demo.cpp for a library)", err)
		}
		infof("Running %s to generate coverage data...", exeName)
		runExec := exec.Command(filepath.Join("build", exeName))
		runExec.Stdout, runExec.Stderr = os.Stdout, os.Stderr
		if err := runExec.Run(); err != nil {
			warnf("the program exited non-zero - generating a coverage report from what ran anyway: %v", err)
		}
	}

	reportDir := filepath.Join("build", "coverage")
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", reportDir, err)
	}

	// gcovr's --root needs to be absolute, not ".": the compiler on this
	// machine (and apparently commonly) embeds absolute source paths in the
	// .gcno/.gcda debug data gcov produces, and gcovr's file-inclusion
	// filter compares its --root against those verbatim - a relative "."
	// never matches an absolute embedded path, so gcovr silently excludes
	// every real source file and reports "All coverage data is filtered
	// out" even though the coverage data itself was captured correctly.
	// Caught live: this reproduced on a real project before this fix.
	absRoot, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("failed to resolve an absolute project root for gcovr: %w", err)
	}

	reportPath := filepath.Join(reportDir, "index.html")
	gcovrCmd := exec.Command("gcovr", "--root", absRoot, "--html", "--html-details", "-o", reportPath, "build")
	gcovrCmd.Stdout, gcovrCmd.Stderr = os.Stdout, os.Stderr
	if err := gcovrCmd.Run(); err != nil {
		return fmt.Errorf("gcovr failed: %w", err)
	}

	okf("Coverage report: %s", reportPath)
	return nil
}
