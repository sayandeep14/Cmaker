package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"cmaker/internal/llm"
)

var fixCmd = &cobra.Command{
	Use:   "fix \"<bug description>\"",
	Short: "Natural-language bug report -> LLM-proposed fix, no failing build log required (LLM-assisted)",
	Long: "'cmaker heal's sibling for bugs that don't show up as a compile/run failure: describe\n" +
		"what's wrong in plain English and this finds the relevant file(s) itself and proposes a\n" +
		"fix - the exact same propose/diff/confirm/apply/build-fix-loop/checkpoint machinery as\n" +
		"'cmaker codegen' (see its own --help for the full behavior and flag meanings), just\n" +
		"entered via a bug report instead of a free-form intent.\n" +
		"\n" +
		"Use 'cmaker heal' instead when there's already a failing 'cmaker build'/'cmaker run' log\n" +
		"to diagnose from - that's a narrower, more targeted starting point than this command's\n" +
		"project-wide file search.",
	Example: `  cmaker fix "the linked list's sum() function returns the wrong total for an empty list"
  cmaker fix "crashes on startup if the config file has an empty description field" --deep`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		description := strings.TrimSpace(args[0])
		if description == "" {
			return fmt.Errorf("bug description must not be empty")
		}
		deep, _ := cmd.Flags().GetBool("deep")
		plan, _ := cmd.Flags().GetBool("plan")
		model, _ := cmd.Flags().GetString("model")
		maxAttempts, _ := cmd.Flags().GetInt("max-attempts")
		if maxAttempts < 1 {
			return fmt.Errorf("--max-attempts must be at least 1")
		}

		intent := "This is a bug report, not a feature request - fix the following described " +
			"faulty behavior with a minimal, targeted change; don't add unrelated features or " +
			"refactor unrelated code.\n\nBug: " + description
		_, err := runCodegenCore(intent, deep, plan, model, maxAttempts)
		return err
	},
}

func init() {
	fixCmd.Flags().Bool("deep", false, "use a stronger model and ask clarifying questions before implementing")
	fixCmd.Flags().Bool("plan", false, "ask for and confirm a short per-file plan before authoring the actual fix")
	fixCmd.Flags().String("model", "", "override the Anthropic model used (default: "+llm.DefaultImproviseModel+", or "+llm.DefaultOpusModel+" with --deep)")
	fixCmd.Flags().Int("max-attempts", 3, "give up (and revert) after this many failed build-fix attempts")
}
