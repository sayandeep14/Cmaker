package cmd

import (
	"bufio"
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
	Short: "Suggest a fix for the most recent build/run failure (LLM-assisted)",
	Long: "Reads the most recent failing 'cmaker build'/'cmaker run' log (see 'cmaker logs'), the\n" +
		"file(s) the compiler's error output pointed at, and asks an LLM (Anthropic; requires\n" +
		"ANTHROPIC_API_KEY) to suggest a fix - printed as a diff. By default nothing is written\n" +
		"to disk; review the diff and apply it yourself (e.g. via 'git apply' or by hand).\n" +
		"\n" +
		"--apply requires a clean git working tree. If a previous 'cmaker heal' run already\n" +
		"diagnosed this exact failure, --apply reuses that diagnosis directly (you already\n" +
		"reviewed it once). Otherwise it diagnoses fresh, shows you the diff, and asks for\n" +
		"confirmation before applying anything. Either way, it rebuilds immediately after\n" +
		"applying and reports whether the fix actually worked.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		model, _ := cmd.Flags().GetString("model")
		kind, _ := cmd.Flags().GetString("kind")
		apply, _ := cmd.Flags().GetBool("apply")
		if kind != "" && kind != "build" && kind != "run" {
			return fmt.Errorf("--kind must be 'build' or 'run' (got %q)", kind)
		}
		return runHeal(model, kind, apply)
	},
}

func init() {
	healCmd.Flags().String("model", "", "override the Anthropic model used (default: "+llm.DefaultModel+")")
	healCmd.Flags().String("kind", "", "only consider 'build' or 'run' failures (default: either, most recent wins)")
	healCmd.Flags().Bool("apply", false, "apply the suggested fix (after confirmation, unless reusing an already-reviewed diagnosis) and rebuild to verify it - requires a clean git working tree")
}

func runHeal(model, kind string, apply bool) error {
	logPath, err := logs.LatestFailure(".", kind)
	if err != nil {
		return err
	}

	if apply {
		clean, err := heal.WorkingTreeClean(".")
		if err != nil {
			return err
		}
		if !clean {
			return fmt.Errorf("'cmaker heal --apply' requires a clean git working tree (uncommitted changes found) - commit or stash first, then retry")
		}

		if cached, ok := heal.LoadSuggestionFor(".", logPath); ok {
			healStatus("Reusing the diagnosis from a previous 'cmaker heal' run for %s (already reviewed).", logPath)
			printDiff(cached.Diff)
			return applySuggestion(".", cached.Diff)
		}
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

	if cacheErr := heal.SaveSuggestion(".", logPath, suggestion); cacheErr != nil {
		debugf("heal: failed to cache suggestion: %v", cacheErr)
	}

	if !apply {
		healStatus("Nothing was written to disk - review the diff above and apply it yourself, or re-run with --apply.")
		return nil
	}

	if !confirmYesNo("Apply this diff?") {
		healStatus("Not applied.")
		return nil
	}

	return applySuggestion(".", suggestion.Diff)
}

// applySuggestion runs the actual `git apply` + rebuild-to-verify sequence
// shared by both the --apply paths (a fresh diagnosis the user just
// confirmed, and a reused prior diagnosis) - the only two ways runHeal ever
// reaches here, always with a diff that's either just been shown or was
// already reviewed in an earlier 'cmaker heal' run.
func applySuggestion(root, diff string) error {
	if err := heal.Apply(root, diff); err != nil {
		return err
	}
	// The diff is now live in the working tree - clear the cache so a
	// second 'cmaker heal --apply' for the same log path never silently
	// reapplies it (git apply would just reject it as already-applied
	// anyway, but this makes the next run re-diagnose instead of trying).
	heal.ClearSuggestion(root)

	healStatus("Applied. Rebuilding to verify...")
	if buildErr := runBuild(false, "", 0, ""); buildErr != nil {
		errorf("Applied the fix, but the rebuild still failed - it may be incomplete: %v", buildErr)
		healStatus("The diff is still applied to your working tree - use 'git diff' to inspect, or 'git checkout -- .' to revert it.")
		return nil
	}
	okf("Fix verified: the rebuild succeeded.")
	return nil
}

// confirmYesNo prompts on stderr (stdout is reserved for the diff itself,
// see printDiff) and reads a line from stdin. Anything other than an
// explicit "y"/"yes" - including a read failure, e.g. stdin isn't a
// terminal - is treated as "no": --apply should never proceed on an
// ambiguous answer.
func confirmYesNo(prompt string) bool {
	fmt.Fprint(os.Stderr, colorize(ansiCyan, "-- "+prompt+" [y/N] "))
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
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
