package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"cmaker/internal/explain"
	"cmaker/internal/llm"
)

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Critique your uncommitted changes before committing (LLM-assisted)",
	Long: "Asks an LLM (Anthropic; requires ANTHROPIC_API_KEY) to critique every uncommitted\n" +
		"change (staged or not - the same set 'cmaker commit' would actually commit) for likely\n" +
		"bugs, missing error handling, edge cases, and anything unfinished-looking. Nothing is\n" +
		"ever written to your code - this only ever reads and critiques, the same pure-read\n" +
		"shape as 'cmaker explain', just asking for a critique instead of an explanation.",
	Example: `  cmaker review`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		model, _ := cmd.Flags().GetString("model")
		return runReview(model)
	},
}

func init() {
	reviewCmd.Flags().String("model", "", "override the Anthropic model used (default: "+llm.DefaultImproviseModel+")")
}

func runReview(model string) error {
	content, err := explain.BuildReviewPrompt(".")
	if err != nil {
		return err
	}
	if content == "" {
		infof("No uncommitted changes to review.")
		return nil
	}

	// Defaults to the stronger model, matching 'cmaker improve'/'suggest' -
	// a genuine critique (not just identification/explanation) benefits
	// from the heavier reasoning tier.
	if model == "" {
		model = llm.DefaultImproviseModel
	}
	client, err := llm.NewClientFromEnv(model)
	if err != nil {
		return err
	}

	infof("Asking %s to review your changes...", client.Model)
	review, err := explain.AskReview(context.Background(), client, content)
	if err != nil {
		return err
	}

	fmt.Println(renderMarkdown(review))
	return nil
}
