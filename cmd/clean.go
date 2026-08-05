package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove and recreate the build/ directory",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := os.RemoveAll("build"); err != nil {
			return fmt.Errorf("failed to remove build/: %w", err)
		}
		if err := os.Mkdir("build", 0755); err != nil {
			return fmt.Errorf("failed to recreate build/: %w", err)
		}
		okf("🧹 Build folder cleared.")
		return nil
	},
}
