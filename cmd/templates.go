package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	tmpl "cmaker/internal/templates"
)

var templatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "List available project templates",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		metas, err := tmpl.List()
		if err != nil {
			return err
		}
		for _, m := range metas {
			deps := ""
			if len(m.Dependencies) > 0 {
				deps = " (fetches: "
				for i, d := range m.Dependencies {
					if i > 0 {
						deps += ", "
					}
					deps += d.Name
				}
				deps += ")"
			}
			fmt.Printf("  %s\n      %s%s\n", colorize(ansiCyan, m.Name), m.Description, deps)
		}
		return nil
	},
}
