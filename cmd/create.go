package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// createCmd is a richer, composable evolution of `new`: flags compose
// instead of forcing one --template string to represent every combination
// (e.g. `cmaker create myproj --with-rust --with-zig --backend`).
// --lang/--template/--compiler/--with-rust/--with-zig are real today;
// --backend/--ml are accepted (so the CLI surface and shell completions
// already exist) but fail with a clear "not implemented yet" pointing at
// the roadmap section that will land them (§13), rather than silently
// ignoring the flag or faking support.
var createCmd = &cobra.Command{
	Use:   "create <name> [flags]",
	Short: "Composable project scaffolding (--lang, --template, --with-rust, --with-zig, --backend, --ml, ...)",
	Args:  cobra.ExactArgs(1),
}

func init() {
	template, lang, compiler, withRust, withZig, runner := newScaffoldFlags(createCmd)
	backend := createCmd.Flags().Bool("backend", false, "scaffold a backend/service template (not implemented yet - see ROADMAP.md §13)")
	ml := createCmd.Flags().Bool("ml", false, "scaffold an ML template, e.g. libtorch/ONNX (not implemented yet - see ROADMAP.md §13)")

	createCmd.RunE = func(cmd *cobra.Command, args []string) error {
		name := args[0]
		switch {
		case *backend:
			return fmt.Errorf("--backend is not implemented yet (see ROADMAP.md §13 Domain-specific scaffolds)")
		case *ml:
			return fmt.Errorf("--ml is not implemented yet (see ROADMAP.md §13 Domain-specific scaffolds)")
		}
		return scaffoldProject(name, name, *template, *lang, *compiler, *withRust, *withZig, *runner)
	}
}
