package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"cmaker/internal/explain"
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
		"If the model can't find a fix from the file(s) the log points at, you're asked whether\n" +
		"to escalate - first by expanding context (the model picks other project files likely to\n" +
		"help, from a real file list - never an invented path), then by trying stronger models\n" +
		"in turn (Sonnet, then Opus), each step asked separately. Nothing escalates without your\n" +
		"say-so, and pinning --model skips the model-escalation steps (context expansion is still\n" +
		"offered) since you've already told it which model to use.\n" +
		"\n" +
		"--apply needs a clean git working tree to safely apply a patch against. If there's no\n" +
		"git repository at all, one is bootstrapped temporarily (everything committed as a\n" +
		"baseline) and removed again once heal finishes - see 'cmaker dummygit'. If there's a\n" +
		"real repository but it's dirty, a temporary commit is made and undone afterward (with\n" +
		"a warning) instead, so your uncommitted work is never lost. If a previous 'cmaker heal'\n" +
		"run already diagnosed this exact failure, --apply reuses that diagnosis directly (you\n" +
		"already reviewed it once). Otherwise it diagnoses fresh, shows you the diff, and asks\n" +
		"for confirmation before applying anything. Either way, it rebuilds immediately after\n" +
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
	healCmd.Flags().String("model", "", "override the Anthropic model used (default: "+llm.DefaultModel+"; skips model-escalation steps, keeping this model throughout)")
	healCmd.Flags().String("kind", "", "only consider 'build' or 'run' failures (default: either, most recent wins)")
	healCmd.Flags().Bool("apply", false, "apply the suggested fix (after confirmation, unless reusing an already-reviewed diagnosis) and rebuild to verify it - bootstraps or temporarily commits to get a clean git baseline if needed")
}

func runHeal(model, kind string, apply bool) error {
	logPath, err := logs.LatestFailure(".", kind)
	if err != nil {
		return err
	}

	if apply {
		usedScratchRepo := false
		usedSafetyCommit := false
		switch {
		case !heal.HasGitRepo("."):
			healStatus("No git repository here - bootstrapping a temporary one so --apply has a clean baseline (removed again once heal finishes; see 'cmaker dummygit')...")
			if err := heal.InitScratchRepo("."); err != nil {
				return err
			}
			usedScratchRepo = true
		default:
			clean, err := heal.WorkingTreeClean(".")
			if err != nil {
				return err
			}
			if !clean {
				healStatus("Uncommitted changes found - making a temporary commit so --apply has a clean baseline (undone again once heal finishes)...")
				if err := heal.SafetyCommit("."); err != nil {
					return err
				}
				usedSafetyCommit = true
			}
		}
		// Registered once the scratch/safety state (if any) is known, so
		// it covers every return path below - the cached-diagnosis reuse
		// branch immediately after this, and every return inside the
		// fresh-diagnosis flow further down.
		defer func() {
			if usedScratchRepo {
				if err := heal.RemoveScratchRepo("."); err != nil {
					warnf("failed to remove the temporary git repository: %v (remove .git by hand)", err)
				}
				return
			}
			if usedSafetyCommit {
				if err := heal.UndoSafetyCommit("."); err != nil {
					warnf("failed to undo the temporary WIP commit: %v (run 'git reset --soft HEAD~1' by hand)", err)
					return
				}
				warnf("Undid the temporary commit made before healing - your original uncommitted changes (plus any fix heal applied) are back in the working tree, staged.")
			}
		}()

		if cached, ok := heal.LoadSuggestionFor(".", logPath); ok {
			healStatus("Reusing the diagnosis from a previous 'cmaker heal' run for %s (already reviewed).", logPath)
			printDiff(cached.Diff)
			return applySuggestion(".", cached.Diff)
		}
	}

	healStatus("Reading %s...", logPath)

	suggestion, err := runHealLadder(model, logPath)
	if err != nil {
		return err
	}
	if suggestion == nil {
		// Every tier declined or exhausted - already reported to the
		// user by runHealLadder itself.
		return nil
	}

	printDiff(suggestion.Diff)

	if cacheErr := heal.SaveSuggestion(".", logPath, *suggestion); cacheErr != nil {
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

// runHealLadder is 'cmaker heal's escalation ladder: haiku on just the
// log-referenced file(s) -> (if that fails) the same model with expanded
// context, chosen by asking the model itself which other project files
// would help -> (if that still fails, and no --model was pinned) Sonnet
// with the same expanded context -> Opus with the same expanded context.
// Each escalation is a separate, explicit confirmation - nothing beyond
// the first attempt happens without the user saying yes to that specific
// step. Returns nil (with no error) if every offered step was declined or
// none found a fix - the "give up" message is printed here, not left to
// the caller, since only this function knows which tier was actually
// reached.
func runHealLadder(model, logPath string) (*heal.Suggestion, error) {
	client, err := llm.NewClientFromEnv(model)
	if err != nil {
		return nil, err
	}

	healStatus("Asking %s to suggest a fix...", client.Model)
	suggestion, err := heal.Suggest(context.Background(), client, ".", logPath, nil)
	if err != nil {
		return nil, err
	}
	if suggestion.Diff != "" {
		return &suggestion, nil
	}
	healStatus("The model couldn't determine a fix from %s (checked: %s).", logPath, strings.Join(suggestion.ReferencedFiles, ", "))

	if !confirmYesNo("Expand context (let the model pick other project files that might help) and try again?") {
		return nil, nil
	}
	extraFiles, err := pickRelatedFiles(client, logPath, suggestion.ReferencedFiles)
	if err != nil {
		return nil, err
	}
	if len(extraFiles) == 0 {
		healStatus("Couldn't find any other files in this project likely to help.")
		return nil, nil
	}
	healStatus("Including: %s", strings.Join(extraFiles, ", "))

	healStatus("Asking %s to suggest a fix, with expanded context...", client.Model)
	suggestion, err = heal.Suggest(context.Background(), client, ".", logPath, extraFiles)
	if err != nil {
		return nil, err
	}
	if suggestion.Diff != "" {
		return &suggestion, nil
	}
	healStatus("Still no fix, even with expanded context.")

	// Escalating the *model* overrides whatever --model the user asked
	// for, so it's only offered when they didn't pin one - context
	// expansion above still applies either way, since that's orthogonal
	// to which model is being used.
	if model != "" {
		healStatus("This doesn't appear to be automatically fixable - manual debugging may be needed.")
		return nil, nil
	}

	for _, strongerModel := range []string{llm.DefaultImproviseModel, llm.DefaultOpusModel} {
		if !confirmYesNo(fmt.Sprintf("Try a stronger model (%s)?", strongerModel)) {
			return nil, nil
		}
		strongerClient, err := llm.NewClientFromEnv(strongerModel)
		if err != nil {
			return nil, err
		}
		healStatus("Asking %s to suggest a fix...", strongerClient.Model)
		suggestion, err = heal.Suggest(context.Background(), strongerClient, ".", logPath, extraFiles)
		if err != nil {
			return nil, err
		}
		if suggestion.Diff != "" {
			return &suggestion, nil
		}
		healStatus("Still no fix with %s.", strongerClient.Model)
	}

	healStatus("This doesn't appear to be automatically fixable - manual debugging may be needed.")
	return nil, nil
}

// pickRelatedFiles asks client which other project files (beyond
// referencedFiles, already tried) would help diagnose logPath's failure,
// using heal.SuggestRelatedFiles - grounded to the project's real file
// list (internal/explain.WalkSourceFiles, the same lookup 'cmaker
// explain'/'read'/'improve' already share) rather than letting the model
// invent a plausible-looking path.
func pickRelatedFiles(client *llm.Client, logPath string, referencedFiles []string) ([]string, error) {
	logData, err := os.ReadFile(logPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", logPath, err)
	}

	alreadyRead := make(map[string]string, len(referencedFiles))
	alreadyReadSet := make(map[string]bool, len(referencedFiles))
	for _, f := range referencedFiles {
		alreadyReadSet[f] = true
		if data, err := os.ReadFile(f); err == nil {
			alreadyRead[f] = string(data)
		}
	}

	allFiles, err := explain.WalkSourceFiles(".")
	if err != nil {
		return nil, err
	}
	candidates := make([]string, 0, len(allFiles))
	for _, f := range allFiles {
		if !alreadyReadSet[f] {
			candidates = append(candidates, f)
		}
	}

	healStatus("Asking %s which other files might help...", client.Model)
	return heal.SuggestRelatedFiles(context.Background(), client, string(logData), alreadyRead, candidates)
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

// stdinReader is the one shared bufio.Reader every interactive stdin
// prompt in cmd/ reads from (confirmYesNo, selectIndex,
// askClarifyingQuestions, collectAnswers) - never construct a second,
// independent bufio.NewReader(os.Stdin) anywhere else. bufio.Reader reads
// from the underlying fd in chunks, not strictly one line at a time: when
// stdin is a pipe with multiple answers already available (piped input,
// as opposed to a live TTY where each answer only arrives once typed), a
// fresh reader's first ReadString call can silently buffer-ahead and
// consume a later prompt's answer too, which is then lost the moment that
// reader goes out of scope - the next prompt's own fresh reader has no
// way to get it back and just blocks/EOFs. Caught live: 'cmaker codegen
// --plan' (the first flow to ever ask two sequential confirmYesNo
// prompts in one run) silently treated its second prompt as "no" when
// driven by two piped "y" answers, because the first confirmYesNo call's
// now-discarded reader had already buffered both lines. A single
// process-lifetime reader fixes it for every current and future
// multi-prompt flow at once.
var stdinReader = bufio.NewReader(os.Stdin)

// confirmYesNo prompts on stderr (stdout is reserved for the diff itself,
// see printDiff) and reads a line from stdin. Anything other than an
// explicit "y"/"yes" - including a read failure, e.g. stdin isn't a
// terminal - is treated as "no": --apply should never proceed on an
// ambiguous answer.
func confirmYesNo(prompt string) bool {
	fmt.Fprint(os.Stderr, colorize(ansiCyan, "-- "+prompt+" [y/N] "))
	line, err := stdinReader.ReadString('\n')
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
	for line := range strings.SplitSeq(diff, "\n") {
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
