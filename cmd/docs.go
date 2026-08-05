package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Build API documentation with Doxygen",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDocs()
	},
}

func runDocs() error {
	if _, err := exec.LookPath("doxygen"); err != nil {
		return fmt.Errorf("doxygen not found on PATH (see 'cmaker doctor')")
	}

	if _, err := os.Stat("Doxyfile"); err != nil {
		infof("No Doxyfile found - writing a default one (see 'cmaker new --with-docs' to scaffold one automatically next time).")
		cfg := loadConfigOrExit()
		if err := writeDoxyfile(".", cfg.ProjectName); err != nil {
			return fmt.Errorf("failed to write a default Doxyfile: %w", err)
		}
	}

	cmd := exec.Command("doxygen", "Doxyfile")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("doxygen failed: %w", err)
	}

	okf("Docs built: %s", filepath.Join("docs", "html", "index.html"))
	return nil
}
