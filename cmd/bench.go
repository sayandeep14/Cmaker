package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

var benchCmd = &cobra.Command{
	Use:   "bench",
	Short: "Build (Release) and run the project's benchmarks",
	Long: "Runs bench/*.cpp (see 'cmaker new --with-benchmarks') as a Release build - benchmark\n" +
		"numbers from an unoptimized Debug build are not meaningful.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBench()
	},
}

func runBench() error {
	cfg := loadConfigOrExit()

	if matches, _ := filepath.Glob(filepath.Join("bench", "*.cpp")); len(matches) == 0 {
		return fmt.Errorf("no bench/*.cpp files found - see 'cmaker new --with-benchmarks' (or add them and 'cmaker install benchmark' yourself)")
	}

	if err := runBuild(true, "", 0, ""); err != nil {
		return err
	}

	exeName := cfg.Executable + "_bench"
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	binPath := filepath.Join("build", exeName)
	runPath := binPath
	if runtime.GOOS != "windows" {
		runPath = "./" + binPath
	}

	infof("Running %s:\n", binPath)
	child := exec.Command(runPath)
	child.Stdout, child.Stderr, child.Stdin = os.Stdout, os.Stderr, os.Stdin
	if err := child.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}
