package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// createCmd is a richer, composable evolution of `new`: flags compose
// instead of forcing one --template string to represent every combination
// (e.g. `cmaker create myapi --backend --with-rust` - "C++ handles HTTP
// routing, Rust handles whatever you want fast/safe", §18). --backend/--ml
// are shorthand for --template=backend/--template=ml-eigen (§13's domain
// templates); every flag here, including --with-rust/--with-zig, composes
// with any of them since §18's split of "link the crate" from "write a
// demo main()" landed (see interop.go's injectInteropUsageHint).
var createCmd = &cobra.Command{
	Use:   "create <name> [flags]",
	Short: "Composable project scaffolding (--lang, --template, --with-rust, --with-zig, --backend, --ml, ...)",
	Args:  cobra.ExactArgs(1),
}

func init() {
	template, lang, compiler, withRust, withZig, runner, targetType, lib := newScaffoldFlags(createCmd)
	backend := createCmd.Flags().Bool("backend", false, "shorthand for --template=backend (a cpp-httplib HTTP service, see ROADMAP.md §13)")
	ml := createCmd.Flags().Bool("ml", false, "shorthand for --template=ml-eigen (an Eigen linear-algebra starter, see ROADMAP.md §13)")

	createCmd.RunE = func(cmd *cobra.Command, args []string) error {
		name := args[0]
		selectedTemplate := *template

		switch {
		case *backend && *ml:
			return fmt.Errorf("--backend and --ml are mutually exclusive")
		case *backend:
			selectedTemplate = "backend"
		case *ml:
			selectedTemplate = "ml-eigen"
		}
		if (*backend || *ml) && cmd.Flags().Changed("template") && *template != selectedTemplate {
			return fmt.Errorf("--template=%s conflicts with --backend/--ml (which imply --template=%s)", *template, selectedTemplate)
		}

		return scaffoldProject(name, name, selectedTemplate, *lang, *compiler, *withRust, *withZig, *runner, resolveTargetType(*targetType, *lib))
	}
}
