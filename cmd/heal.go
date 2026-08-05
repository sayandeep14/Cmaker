package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"cmaker/internal/heal"
	"cmaker/internal/llm"
	"cmaker/internal/logs"
)

var healCmd = &cobra.Command{
	Use:   "heal",
	Short: "Suggest a fix for the most recent build/run failure (LLM-assisted, suggest-only)",
	Long: "Reads the most recent failing 'cmaker build'/'cmaker run' log (see 'cmaker logs'), the\n" +
		"file(s) the compiler's error output pointed at, and asks an LLM (Anthropic; requires\n" +
		"ANTHROPIC_API_KEY) to suggest a fix - printed as a diff. Nothing is written to disk;\n" +
		"review the diff and apply it yourself (e.g. via 'git apply' or by hand). Applying a\n" +
		"suggestion automatically (--apply) is a deliberately separate, not-yet-implemented\n" +
		"follow-up (see ROADMAP.md §24 v2) - v1 only ever suggests.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		model, _ := cmd.Flags().GetString("model")
		kind, _ := cmd.Flags().GetString("kind")
		if kind != "" && kind != "build" && kind != "run" {
			return fmt.Errorf("--kind must be 'build' or 'run' (got %q)", kind)
		}
		return runHeal(model, kind)
	},
}

func init() {
	healCmd.Flags().String("model", "", "override the Anthropic model used (default: "+llm.DefaultModel+")")
	healCmd.Flags().String("kind", "", "only consider 'build' or 'run' failures (default: either, most recent wins)")
}

func runHeal(model, kind string) error {
	logPath, err := logs.LatestFailure(".", kind)
	if err != nil {
		return err
	}
	healStatus("Reading %s...", logPath)

	client, err := llm.NewClientFromEnv(model)
	if err != nil {
		return err
	}

	healStatus("Asking %s to suggest a fix...", client.Model)
	suggestion, err := heal.Suggest(context.Background(), client, ".", logPath)
	if err != nil {
		return err
	}

	if suggestion.Diff == "" {
		healStatus("The model couldn't determine a fix from %s (checked: %s).", logPath, strings.Join(suggestion.ReferencedFiles, ", "))
		return nil
	}

	printDiff(suggestion.Diff)
	healStatus("Nothing was written to disk - review the diff above and apply it yourself.")
	return nil
}

// healStatus prints a progress/status message to stderr, not stdout -
// deliberately different from infof (which prints to stdout, fine for
// every other command). 'cmaker heal's stdout is meant to be piped/
// redirected as the actual diff (`cmaker heal > fix.patch`), so status
// chatter has to stay off it entirely, not just be suppressible via -q.
func healStatus(format string, a ...any) {
	if flagQuiet {
		return
	}
	fmt.Fprintln(os.Stderr, colorize(ansiCyan, "-- "+fmt.Sprintf(format, a...)))
}

// printDiff prints a unified diff, with basic +/-/@@ colorization only when
// stdout is an actual terminal. This is deliberately independent of
// colorize()/--no-color: unlike cmaker's other colored output, this diff is
// meant to be piped/redirected and applied (e.g. `cmaker heal > fix.patch
// && git apply fix.patch`) - ANSI escape codes embedded in a redirected
// file would corrupt it into an invalid patch, so plain-text-when-piped
// isn't just cosmetic here, it's required for the output to stay usable.
func printDiff(diff string) {
	colored := term.IsTerminal(int(os.Stdout.Fd())) && !flagNoColor
	for _, line := range strings.Split(diff, "\n") {
		if !colored {
			fmt.Println(line)
			continue
		}
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			fmt.Println(colorize(ansiGreen, line))
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			fmt.Println(colorize(ansiRed, line))
		case strings.HasPrefix(line, "@@"):
			fmt.Println(colorize(ansiCyan, line))
		default:
			fmt.Println(line)
		}
	}
}
