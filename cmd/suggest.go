package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"cmaker/internal/llm"
	"cmaker/internal/suggest"
)

var suggestCmd = &cobra.Command{
	Use:   "suggest",
	Short: "Ask an LLM for whole-project improvement suggestions (LLM-assisted)",
	Long: "Asks an LLM (Anthropic; requires ANTHROPIC_API_KEY) to review the current project and\n" +
		"suggest concrete improvements - unlike 'cmaker improve', which is scoped to a single\n" +
		"function or class, this looks at the whole project. Nothing is ever written to your\n" +
		"code - this only ever reads and suggests.\n" +
		"\n" +
		"--export writes the suggestions as a markdown checklist instead of just printing them,\n" +
		"designed to be worked through by hand one item at a time (and, later, to feed an\n" +
		"agentic 'cmaker codegen --intent-from' that pulls tasks from it automatically).",
	Example: `  cmaker suggest
  cmaker suggest --export=SUGGESTIONS.md`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		exportPath, _ := cmd.Flags().GetString("export")
		model, _ := cmd.Flags().GetString("model")
		return runSuggest(exportPath, model)
	},
}

func init() {
	suggestCmd.Flags().String("export", "", "write the suggestions as a markdown checklist to this file instead of printing them")
	suggestCmd.Flags().String("model", "", "override the Anthropic model used (default: "+llm.DefaultImproviseModel+")")
}

func runSuggest(exportPath, model string) error {
	// Defaults to the stronger model, matching 'cmaker improve' - judging
	// a whole project and proposing specific, grounded suggestions is a
	// heavier reasoning task than single-shot menu selection.
	if model == "" {
		model = llm.DefaultImproviseModel
	}
	client, err := llm.NewClientFromEnv(model)
	if err != nil {
		return err
	}

	infof("Asking %s for project improvement suggestions...", client.Model)
	suggestions, err := suggest.Suggest(context.Background(), client, ".")
	if err != nil {
		return err
	}
	if len(suggestions) == 0 {
		infof("No suggestions - nothing obviously worth changing right now.")
		return nil
	}

	if exportPath != "" {
		if err := os.WriteFile(exportPath, []byte(suggest.RenderChecklist(suggestions)), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", exportPath, err)
		}
		okf("Wrote %d suggestion(s) to %s", len(suggestions), exportPath)
		return nil
	}

	for i, s := range suggestions {
		fmt.Printf("%d. %s\n", i+1, s.Title)
		if s.Detail != "" {
			fmt.Printf("   %s\n", s.Detail)
		}
	}
	return nil
}
