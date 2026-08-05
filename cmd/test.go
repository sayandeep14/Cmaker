package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Build (if needed) and run the project's ctest suite",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		release, _ := cmd.Flags().GetBool("release")
		return runTest(release)
	},
}

func init() {
	testCmd.Flags().Bool("release", false, "build with CMAKE_BUILD_TYPE=Release (-O3) before testing")
}

// runTest builds the project (see runBuild) and then runs its ctest suite,
// requiring 'testing.enabled: true' in cmaker.yaml (see config.TestingConfig)
// rather than letting an unconfigured project hit ctest's cryptic
// "No tests were found!!!" failure.
func runTest(release bool) error {
	cfg := loadConfigOrExit()
	if cfg.Testing == nil || !cfg.Testing.Enabled {
		return fmt.Errorf("no tests configured - set 'testing: { enabled: true }' in cmaker.yaml (the catch2 template does this for you)")
	}

	if err := runBuild(release, "", 0); err != nil {
		return err
	}

	infof("Running ctest...")
	ctestCmd := exec.Command("ctest", "--test-dir", "build", "--output-on-failure")
	ctestCmd.Stdout = os.Stdout
	ctestCmd.Stderr = os.Stderr
	if err := ctestCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return fmt.Errorf("failed to run ctest: %w", err)
	}
	okf("All tests passed!")
	return nil
}
