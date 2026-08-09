package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove and recreate the build/ directory",
	Long: "Wipes and recreates build/ - cmaker's own meaning for 'clean', unrelated to git's.\n" +
		"For git's clean (remove untracked files from the working tree), use --git: 'cmaker\n" +
		"clean --git' forwards straight to 'git clean' with any other flags you give it (e.g.\n" +
		"'cmaker clean --git -fd'), bypassing this command's own flag parsing entirely.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := os.RemoveAll("build"); err != nil {
			return fmt.Errorf("failed to remove build/: %w", err)
		}
		if err := os.Mkdir("build", 0755); err != nil {
			return fmt.Errorf("failed to recreate build/: %w", err)
		}
		okf("Build folder cleared.")
		return nil
	},
}

func init() {
	// Handled entirely by maybeDispatchGitOverride in git.go, before cobra
	// ever parses this command's flags - registered here only so `cmaker
	// clean --help` documents it exists.
	cleanCmd.Flags().Bool("git", false, "forward to 'git clean' instead (with any other flags given, e.g. -fd)")
}
