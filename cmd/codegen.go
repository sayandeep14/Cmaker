package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"cmaker/internal/agentic"
	"cmaker/internal/explain"
	"cmaker/internal/heal"
	"cmaker/internal/llm"
	"cmaker/internal/logs"
	"cmaker/internal/suggest"
)

var codegenCmd = &cobra.Command{
	Use:   "codegen --intent=\"...\" | --intent-from=[<file>][:<num>]",
	Short: "Agentic, whole-project code change from a free-form intent (LLM-assisted)",
	Long: "Unlike 'cmaker improve' (scoped to one named function/class), codegen finds the\n" +
		"relevant files itself and can touch as many of them as needed, plus create new ones -\n" +
		"the agentic layer described in this project's own backlog.\n" +
		"\n" +
		"--intent=\"...\" is a single free-form change request. --intent-from=[<file>][:<num>]\n" +
		"instead pulls a task from a 'cmaker suggest --export' checklist (default location\n" +
		".cmaker/suggestions.md if <file> is omitted; first unmarked item if <num> is omitted) -\n" +
		"a task judged too large for one shot is broken into sub-checkboxes in that same file,\n" +
		"and the first sub-task is taken up. On success the completed item is checked off.\n" +
		"\n" +
		"--deep uses a stronger model and asks clarifying questions (if it has any) before\n" +
		"implementing anything, instead of guessing. --plan asks for (and shows you) a short\n" +
		"per-file plan of what it intends to change first, as a cheap sanity check before it\n" +
		"spends the effort actually authoring the change - a second, earlier confirmation gate\n" +
		"on top of the usual diff confirmation, not a replacement for it.\n" +
		"\n" +
		"--watch (only with --intent-from, and only without a specific :<num>) keeps pulling and\n" +
		"completing tasks from the checklist, one after another, until none remain or one fails\n" +
		"or is declined - instead of one task per invocation.\n" +
		"\n" +
		"Requires a clean git working tree (commit or stash first) - after showing the diff and\n" +
		"getting your confirmation, it applies, rebuilds, and retries against the build error\n" +
		"(up to --max-attempts) if that fails, rather than treating a broken build as done. A\n" +
		"successful run ends with its own commit (a checkpoint - see 'cmaker commit'); if every\n" +
		"attempt still fails to build, every change this run made is reverted and reported as a\n" +
		"failure, never left half-applied.",
	Example: `  cmaker codegen --intent="add a --verbose flag that prints every cmake invocation"
  cmaker codegen --intent="refactor error handling to use Result<T>" --deep --plan
  cmaker codegen --intent-from=.cmaker/suggestions.md
  cmaker codegen --intent-from=:3
  cmaker codegen --intent-from --watch`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		intent, _ := cmd.Flags().GetString("intent")
		intentFrom, _ := cmd.Flags().GetString("intent-from")
		intentFromSet := cmd.Flags().Changed("intent-from")
		deep, _ := cmd.Flags().GetBool("deep")
		plan, _ := cmd.Flags().GetBool("plan")
		watch, _ := cmd.Flags().GetBool("watch")
		model, _ := cmd.Flags().GetString("model")
		maxAttempts, _ := cmd.Flags().GetInt("max-attempts")
		if maxAttempts < 1 {
			return fmt.Errorf("--max-attempts must be at least 1")
		}
		if watch && !intentFromSet {
			return fmt.Errorf("--watch only applies to --intent-from")
		}
		if watch {
			if _, numStr := parseIntentFromArg(intentFrom); numStr != "" {
				return fmt.Errorf("--watch can't be combined with a specific :<num> - it always works through the whole file")
			}
		}

		switch {
		case intentFromSet && intent != "":
			return fmt.Errorf("--intent and --intent-from are mutually exclusive")
		case intentFromSet:
			return runCodegenIntentFrom(intentFrom, deep, plan, model, maxAttempts, watch)
		case intent != "":
			_, err := runCodegenCore(intent, deep, plan, model, maxAttempts)
			return err
		default:
			return fmt.Errorf("either --intent=\"...\" or --intent-from is required")
		}
	},
}

// defaultSuggestionsFile is where --intent-from looks when no <file> is
// given - matches 'cmaker suggest --export=.cmaker/suggestions.md's own
// natural home alongside .cmaker/logs.
const defaultSuggestionsFile = ".cmaker/suggestions.md"

// gitBaseline tracks which temporary git bootstrap step (if any)
// ensureGitBaseline used to get a clean working tree - shared by
// codegen/fix (via runCodegenCore) and migrate, since every agentic
// command that ends with its own checkpoint commit needs the identical
// clean-baseline guarantee.
type gitBaseline struct {
	usedScratchRepo  bool
	usedSafetyCommit bool
	baseRef          string
}

// ensureGitBaseline makes "." ready for an agentic command's apply/build/
// commit cycle: bootstraps a temporary git repo if there's none at all, or
// makes a temporary commit over existing uncommitted changes if there is
// one but it's dirty - the same bootstrap-or-safety-commit dance 'cmaker
// heal --apply' already established (see cmd/heal.go's runHeal), reused
// here instead of the hard "refuse unless already clean" cmaker
// codegen/fix/migrate used to have.
func ensureGitBaseline() (gitBaseline, error) {
	if !heal.HasGitRepo(".") {
		infof("No git repository here - bootstrapping a temporary one so this has a clean baseline (see 'cmaker dummygit')...")
		if err := heal.InitScratchRepo("."); err != nil {
			return gitBaseline{}, err
		}
		return gitBaseline{usedScratchRepo: true}, nil
	}
	clean, err := heal.WorkingTreeClean(".")
	if err != nil {
		return gitBaseline{}, err
	}
	if !clean {
		ref, err := heal.CurrentHead(".")
		if err != nil {
			return gitBaseline{}, err
		}
		infof("Uncommitted changes found - making a temporary commit so this has a clean baseline (undone again once it finishes)...")
		if err := heal.SafetyCommit("."); err != nil {
			return gitBaseline{}, err
		}
		return gitBaseline{usedSafetyCommit: true, baseRef: ref}, nil
	}
	return gitBaseline{}, nil
}

// finish tears down whatever ensureGitBaseline set up, once the caller's
// whole run (apply/build/checkpoint-commit) is over. A scratch repo
// bootstrapped for a project that had none is only removed again if
// nothing of value was left on top of its baseline commit (succeeded);
// otherwise it's kept, so the checkpoint commit the caller just made
// survives instead of being wiped along with .git. A safety commit is
// always undone (soft reset to baseRef), landing every commit made since
// then - the safety commit itself, plus a real checkpoint commit on top of
// it if the run succeeded - back in the working tree, staged, exactly like
// 'cmaker heal --apply' does with its own safety commit.
func (b gitBaseline) finish(succeeded bool) {
	if b.usedScratchRepo {
		if succeeded {
			infof("Initialized a git repository for this project (it had none) - committed on top of a baseline commit; see 'cmaker dummygit --help'.")
			return
		}
		if err := heal.RemoveScratchRepo("."); err != nil {
			warnf("failed to remove the temporary git repository: %v (remove .git by hand)", err)
		}
		return
	}
	if b.usedSafetyCommit {
		if err := heal.ResetSoftTo(".", b.baseRef); err != nil {
			warnf("failed to undo the temporary WIP commit: %v (run 'git reset --soft %s' by hand)", err, b.baseRef)
			return
		}
		if succeeded {
			warnf("Undid the temporary commit made before this run - your original uncommitted changes, plus the change just applied, are back in the working tree, staged (commit them yourself, e.g. with 'cmaker commit').")
		} else {
			warnf("Undid the temporary commit made before this run - your original uncommitted changes are back in the working tree, staged.")
		}
	}
}

// commitFileOnly stages and commits exactly path (not the whole working
// tree) - used for cmaker's own bookkeeping writes to a suggestions
// checklist (breaking a task down, checking one off) so they don't sit
// around as uncommitted noise that would otherwise make the *next*
// codegen/fix/migrate invocation think the working tree is dirty. Failures
// are the caller's to decide how to handle (e.g. no git repo yet, or git
// not configured with a user identity) - this only runs the two git
// commands and reports success or a wrapped error.
func commitFileOnly(path, message string) error {
	if err := exec.Command("git", "add", "--", path).Run(); err != nil {
		return fmt.Errorf("git add %s failed: %w", path, err)
	}
	if err := exec.Command("git", "commit", "-q", "-m", message, "--", path).Run(); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}
	return nil
}

// maxBuildLogChars bounds how much of a failing build log gets fed back
// into a build-fix retry - matches internal/heal's own log-truncation
// philosophy (the tail is what matters most).
const maxBuildLogChars = 4000

func init() {
	codegenCmd.Flags().String("intent", "", "a single free-form change request, e.g. \"add input validation to parseConfig\"")
	codegenCmd.Flags().String("intent-from", "", "pull a task from a 'cmaker suggest --export' checklist: [<file>][:<num>] (default file: "+defaultSuggestionsFile+"; default: first unmarked item)")
	codegenCmd.Flags().Bool("deep", false, "use a stronger model and ask clarifying questions before implementing")
	codegenCmd.Flags().Bool("plan", false, "ask for and confirm a short per-file plan before authoring the actual change")
	codegenCmd.Flags().Bool("watch", false, "with --intent-from (no :<num>): keep completing tasks until none remain or one fails")
	codegenCmd.Flags().String("model", "", "override the Anthropic model used (default: "+llm.DefaultImproviseModel+", or "+llm.DefaultOpusModel+" with --deep)")
	codegenCmd.Flags().Int("max-attempts", 3, "give up (and revert) after this many failed build-fix attempts - the cost/turn guardrail for the retry loop")
}

// runCodegenCore runs one full codegen task end to end: relevant-file
// selection, (optionally) clarifying questions, propose/diff/confirm/
// apply, and the build-fix retry loop with a checkpoint commit on success
// or a full revert on exhausting every attempt. Shared by both --intent
// and --intent-from (the latter via runCodegenIntentFrom, which resolves
// a checklist item down to a task string first).
// The bool return distinguishes "actually applied and committed a working
// change" from every other nil-error outcome (declined confirmation, the
// model proposed nothing, an identical no-op) - callers that need to know
// whether real work happened (runCodegenIntentFrom, deciding whether to
// check off a checklist item) must not treat "no error" alone as success.
// Caught live: without this distinction, declining the confirmation
// prompt for a broken-down sub-task still got it marked done in the
// checklist file, having made no actual change at all.
func runCodegenCore(intent string, deep, plan bool, model string, maxAttempts int) (succeeded bool, err error) {
	baseline, err := ensureGitBaseline()
	if err != nil {
		return false, err
	}
	defer func() { baseline.finish(succeeded) }()

	if model == "" {
		if deep {
			model = llm.DefaultOpusModel
		} else {
			model = llm.DefaultImproviseModel
		}
	}
	client, err := llm.NewClientFromEnv(model)
	if err != nil {
		return false, err
	}

	candidates, err := explain.WalkSourceFiles(".")
	if err != nil {
		return false, err
	}

	if deep {
		infof("Asking %s if it has clarifying questions...", client.Model)
		questions, err := agentic.AskClarifyingQuestions(context.Background(), client, intent, candidates)
		if err != nil {
			return false, err
		}
		if len(questions) > 0 {
			intent = intent + "\n\n" + collectAnswers(questions)
		}
	}

	infof("Asking %s which files this needs...", client.Model)
	relevant, err := agentic.SuggestRelevantFiles(context.Background(), client, intent, candidates)
	if err != nil {
		return false, err
	}
	if len(relevant) == 0 {
		return false, fmt.Errorf("couldn't determine which files this intent needs - be more specific, or scope to a single function/class with 'cmaker improve' instead")
	}
	infof("Relevant files: %s", strings.Join(relevant, ", "))

	if plan {
		infof("Asking %s for a plan...", client.Model)
		planText, err := agentic.AskPlan(context.Background(), client, intent, relevant)
		if err != nil {
			return false, err
		}
		fmt.Println(renderMarkdown(planText))
		if !confirmYesNo("Proceed with this plan?") {
			infof("Not proceeding.")
			return false, nil
		}
	}

	filesContent := make(map[string]string, len(relevant))
	for _, f := range relevant {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		filesContent[f] = string(data)
	}
	if len(filesContent) == 0 {
		return false, fmt.Errorf("none of the selected files could be read")
	}

	touched := map[string]*touchedRecord{}
	currentIntent := intent

	// succeeded (the named return) is only set true right before the one
	// success path returns (after a working build and its checkpoint
	// commit) - every other return from this loop (an error mid-attempt,
	// a declined confirmation before anything was touched, attempts
	// exhausted) needs whatever's already been applied reverted first, so
	// a defer covering every exit path is safer than manually reverting
	// before each individual early return (a real bug this caught live: a
	// mid-loop agentic.ProposeChanges error on a retry used to return
	// immediately, leaving the previous attempt's already-applied,
	// never-built change sitting dirty in the working tree).
	defer func() {
		if !succeeded && len(touched) > 0 {
			revertTouched(touched)
			warnf("Reverted every change this run made.")
		}
	}()

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt == 1 {
			infof("Asking %s to propose changes...", client.Model)
		} else {
			healStatus("Asking %s to fix the build failure (attempt %d/%d)...", client.Model, attempt, maxAttempts)
		}
		proposed, err := agentic.ProposeChanges(context.Background(), client, currentIntent, filesContent)
		if err != nil {
			return false, err
		}

		applied := false
		if len(proposed) > 0 {
			changes := buildCodegenFileChanges(proposed, filesContent)
			var combined strings.Builder
			var toApply []fileChange
			for _, c := range changes {
				d := heal.UnifiedDiff(c.path, c.original, c.newContent)
				if d == "" {
					continue
				}
				if combined.Len() > 0 {
					combined.WriteString("\n")
				}
				combined.WriteString(d)
				toApply = append(toApply, c)
			}

			if combined.Len() > 0 {
				printDiff(combined.String())
				if attempt == 1 {
					if !confirmYesNo("Apply this change?") {
						infof("Not applied.")
						return false, nil
					}
				}
				for _, c := range toApply {
					if _, ok := touched[c.path]; !ok {
						touched[c.path] = &touchedRecord{original: c.original, isNew: c.isNew}
					}
					if err := writeCodegenFile(c); err != nil {
						return false, err
					}
					filesContent[c.path] = c.newContent
				}
				okf("Applied.")
				applied = true
			}
		}

		if !applied {
			if attempt == 1 {
				infof("The model didn't propose a change.")
				return false, nil
			}
			warnf("The model didn't propose a fix on attempt %d.", attempt)
		}

		healStatus("Rebuilding to verify...")
		if buildErr := runBuild(false, "", 0, ""); buildErr == nil {
			okf("Build succeeded.")
			if baseline.usedSafetyCommit {
				// The working tree wasn't clean to begin with, so there's
				// no clean baseline to diff a checkpoint commit against -
				// baseline.finish (deferred above) will reset back to
				// baseRef instead, landing this change in the working
				// tree, staged, for the caller to commit themselves.
				return true, nil
			}
			committed, cErr := autoCommitStaged(model)
			if cErr != nil {
				return false, cErr
			}
			if !committed {
				infof("Nothing to commit.")
			}
			return true, nil
		}

		if attempt >= maxAttempts {
			warnf("Still failing after %d attempt(s).", maxAttempts)
			return false, fmt.Errorf("codegen couldn't produce a working build after %d attempt(s); all changes from this run were reverted", maxAttempts)
		}

		currentIntent = fmt.Sprintf("%s\n\nA previous attempt resulted in this build failure - fix it while still satisfying the original intent:\n%s", intent, latestBuildLogSnippet())
	}
	return false, nil // unreachable: the loop above always returns
}

// touchedRecord is what buildCodegenFileChanges/revertTouched need to
// undo a codegen run's changes if every build-fix attempt fails: an
// existing file's pre-run content, or (isNew) that it didn't exist at
// all before this run and should simply be removed.
type touchedRecord struct {
	original string
	isNew    bool
}

// buildCodegenFileChanges turns proposed (path -> new content, from
// agentic.ProposeChanges) into fileChange values against allowed (the
// files the model was actually shown) - reusing the exact fileChange
// shape and the same defensive rules cmd/improve.go's runImproveFile
// already established for its own non-strict mode: a path matching one
// of allowed is an edit to that file; otherwise it's only accepted as a
// brand-new file if it's a safe relative path AND doesn't already exist
// on disk (an existing file codegen wasn't shown is never trusted,
// dropped with a warning instead).
func buildCodegenFileChanges(proposed map[string]string, allowed map[string]string) []fileChange {
	cleanedAllowed := make(map[string]string, len(allowed))
	for path := range allowed {
		cleanedAllowed[filepath.Clean(path)] = path
	}

	var changes []fileChange
	for path, newContent := range proposed {
		cleanPath := filepath.Clean(path)
		if origKey, ok := cleanedAllowed[cleanPath]; ok {
			changes = append(changes, fileChange{path: origKey, original: allowed[origKey], newContent: newContent})
			continue
		}
		if !isSafeNewFilePath(cleanPath) {
			warnf("Ignoring a proposed file at %q - not a safe relative path.", path)
			continue
		}
		if _, statErr := os.Stat(cleanPath); statErr == nil {
			warnf("Ignoring a proposed change to %s - it wasn't shown to the model, so the change can't be trusted.", cleanPath)
			continue
		}
		changes = append(changes, fileChange{path: cleanPath, newContent: newContent, isNew: true})
	}
	return changes
}

// writeCodegenFile applies one fileChange to disk - a new file (with its
// parent directory created if needed) or an overwrite of an existing one,
// preserving its current permissions.
func writeCodegenFile(c fileChange) error {
	if c.isNew {
		if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", c.path, err)
		}
		if err := os.WriteFile(c.path, []byte(c.newContent), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", c.path, err)
		}
		return nil
	}
	perm := os.FileMode(0644)
	if info, statErr := os.Stat(c.path); statErr == nil {
		perm = info.Mode().Perm()
	}
	if err := os.WriteFile(c.path, []byte(c.newContent), perm); err != nil {
		return fmt.Errorf("failed to write %s: %w", c.path, err)
	}
	return nil
}

// revertTouched undoes every change a codegen run made (see
// touchedRecord) after exhausting every build-fix attempt without a
// working build - restoring each existing file's original content, and
// removing each brand-new file it created, rather than leaving a broken
// build half-applied in the working tree.
func revertTouched(touched map[string]*touchedRecord) {
	for path, rec := range touched {
		if rec.isNew {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				warnf("failed to remove %s while reverting: %v", path, err)
			}
			continue
		}
		if err := os.WriteFile(path, []byte(rec.original), 0644); err != nil {
			warnf("failed to restore %s while reverting: %v", path, err)
		}
	}
}

// latestBuildLogSnippet reads the most recent build failure log (the one
// runBuild's own failure just wrote) and returns its tail, truncated to
// maxBuildLogChars - empty on any failure to find/read it, since the
// build-fix retry can still proceed on the original intent alone.
func latestBuildLogSnippet() string {
	logPath, err := logs.LatestFailure(".", "build")
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	text := string(data)
	if len(text) > maxBuildLogChars {
		text = "...(truncated)...\n" + text[len(text)-maxBuildLogChars:]
	}
	return text
}

// collectAnswers prints each clarifying question and reads a free-text
// answer from stdin, returning them as Q/A pairs to append to the intent
// - --deep's "ask before implementing" step.
func collectAnswers(questions []string) string {
	var b strings.Builder
	b.WriteString("Clarifications:\n")
	for _, q := range questions {
		fmt.Printf("%s\n> ", q)
		line, _ := stdinReader.ReadString('\n')
		fmt.Fprintf(&b, "Q: %s\nA: %s\n", q, strings.TrimSpace(line))
	}
	return b.String()
}

// maxWatchTasks bounds how many tasks a single --watch invocation will
// pull and complete in one run - a safety cap against a runaway loop
// (e.g. a breakdown step that somehow never converges to a leaf task
// getting marked done), the same "hard cap so a bad loop can't run away"
// guardrail principle as --max-attempts.
const maxWatchTasks = 20

// runCodegenIntentFrom resolves raw ([<file>][:<num>]) to a checklist
// item (from a 'cmaker suggest --export' file), assesses whether it needs
// breaking down first (only for a fresh, not-yet-broken-down top-level
// item), runs it through runCodegenCore, and - only on success - checks
// it off in place (cascading to its parent if this was the last unchecked
// child of a broken-down item). With watch, repeats this for the next
// unchecked task (always re-reading the file fresh each time) until none
// remain, one is declined, one fails, or maxWatchTasks is hit - numStr is
// rejected together with watch by the caller (cmd/codegen.go's RunE),
// since --watch always works through the whole file, not one specific
// item.
func runCodegenIntentFrom(raw string, deep, plan bool, model string, maxAttempts int, watch bool) error {
	filePath, numStr := parseIntentFromArg(raw)
	if filePath == "" {
		filePath = defaultSuggestionsFile
	}

	for tasksDone := 0; ; tasksDone++ {
		if watch && tasksDone >= maxWatchTasks {
			warnf("Reached the safety cap of %d tasks in one --watch run - stopping. Run again to continue.", maxWatchTasks)
			return nil
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w (run 'cmaker suggest --export=%s' first)", filePath, err, filePath)
		}
		items := suggest.ParseChecklist(data)
		if len(items) == 0 {
			return fmt.Errorf("%s has no checklist items to work from", filePath)
		}

		var item *suggest.Item
		if numStr != "" {
			num, convErr := strconv.Atoi(numStr)
			if convErr != nil {
				return fmt.Errorf("invalid suggestion number %q in --intent-from", numStr)
			}
			item, err = suggest.FindByIndex(items, num)
			if err != nil {
				return err
			}
			if item == nil {
				infof("Suggestion #%d is already done.", num)
				return nil
			}
		} else {
			item = suggest.FindFirstUnchecked(items)
			if item == nil {
				if watch && tasksDone > 0 {
					okf("All done - completed %d task(s), nothing left in %s.", tasksDone, filePath)
				} else {
					infof("Nothing left to do in %s - every suggestion is checked off.", filePath)
				}
				return nil
			}
		}

		if item.Parent == nil {
			item, data, err = maybeBreakDownItem(filePath, data, item, model)
			if err != nil {
				return err
			}
		}

		infof("Working on: %s", item.Text)
		succeeded, err := runCodegenCore(item.FullTask(), deep, plan, model, maxAttempts)
		if err != nil {
			if watch {
				return fmt.Errorf("stopped after %d task(s) - %q failed: %w", tasksDone, item.Text, err)
			}
			return err
		}
		if !succeeded {
			infof("No changes were made - leaving %q unchecked in %s.", item.Text, filePath)
			if watch {
				infof("Stopping --watch after %d task(s) (the last one was declined or was a no-op).", tasksDone)
			}
			return nil
		}

		if err := markChecklistItemDone(filePath, data, item); err != nil {
			return err
		}

		if !watch {
			return nil
		}
		okf("Moving to the next task...")
	}
}

// maybeBreakDownItem asks whether a fresh top-level item needs to be
// split into sub-tasks first (internal/agentic.AssessBreakdown); if so,
// inserts them into filePath in place and returns the first sub-task as
// the item to actually work on. item.Line is unaffected by
// suggest.InsertSubtasks (it only ever inserts after item's own line), so
// re-locating item itself in the reparsed file is a simple line-number
// match, not a fuzzier text-based search.
func maybeBreakDownItem(filePath string, data []byte, item *suggest.Item, model string) (*suggest.Item, []byte, error) {
	candidates, err := explain.WalkSourceFiles(".")
	if err != nil {
		return nil, nil, err
	}
	breakdownModel := model
	if breakdownModel == "" {
		breakdownModel = llm.DefaultImproviseModel
	}
	client, err := llm.NewClientFromEnv(breakdownModel)
	if err != nil {
		return nil, nil, err
	}

	infof("Asking %s whether %q needs to be broken down...", client.Model, item.Text)
	subtasks, err := agentic.AssessBreakdown(context.Background(), client, item.FullTask(), candidates)
	if err != nil {
		return nil, nil, err
	}
	if len(subtasks) == 0 {
		return item, data, nil
	}

	updated := suggest.InsertSubtasks(data, item, subtasks)
	if err := os.WriteFile(filePath, updated, 0644); err != nil {
		return nil, nil, fmt.Errorf("failed to write %s: %w", filePath, err)
	}
	okf("Broke %q into %d sub-task(s) in %s.", item.Text, len(subtasks), filePath)
	// Commit this bookkeeping edit on its own, if there's already a git
	// repo to commit it to - otherwise it would sit around as uncommitted
	// noise that made the very next step (ensureGitBaseline, inside
	// runCodegenCore) think the working tree was dirty, when all that
	// actually happened was cmaker updating its own checklist file. A
	// failure here is a soft warning, not fatal - runCodegenCore's own
	// baseline bootstrap handles a still-dirty tree gracefully either way.
	if heal.HasGitRepo(".") {
		if err := commitFileOnly(filePath, fmt.Sprintf("cmaker: break down suggestion %q into sub-tasks", item.Text)); err != nil {
			warnf("failed to commit the checklist breakdown: %v (continuing anyway)", err)
		}
	}

	reparsed := suggest.ParseChecklist(updated)
	parent := findItemByLine(reparsed, item.Line)
	if parent == nil || len(parent.Children) == 0 {
		return nil, nil, fmt.Errorf("internal error: couldn't re-locate %q after inserting sub-tasks", item.Text)
	}
	return parent.Children[0], updated, nil
}

// markChecklistItemDone re-reads filePath (rather than trusting the data
// this call was handed, in case something else touched it in the
// meantime - cheap insurance for a file this run doesn't otherwise own
// exclusively), checks item off, and cascades to its parent if this was
// the last unchecked child of a broken-down item.
func markChecklistItemDone(filePath string, _ []byte, item *suggest.Item) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read %s to mark %q done: %w", filePath, item.Text, err)
	}

	items := suggest.ParseChecklist(data)
	target := findItemByLine(items, item.Line)
	if target == nil {
		warnf("Couldn't re-locate %q in %s to mark it done - mark it by hand.", item.Text, filePath)
		return nil
	}

	updated := suggest.SetChecked(data, target, true)
	if target.Parent != nil {
		reparsed := suggest.ParseChecklist(updated)
		if parent := findItemByLine(reparsed, target.Parent.Line); parent != nil && allChildrenChecked(parent) {
			updated = suggest.SetChecked(updated, parent, true)
		}
	}

	if err := os.WriteFile(filePath, updated, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", filePath, err)
	}
	okf("Marked %q done in %s.", item.Text, filePath)
	// Same reasoning as maybeBreakDownItem's own commit: keep this
	// bookkeeping edit from lingering as uncommitted noise ahead of the
	// next --watch iteration's own ensureGitBaseline check.
	if heal.HasGitRepo(".") {
		if err := commitFileOnly(filePath, fmt.Sprintf("cmaker: mark suggestion %q done", item.Text)); err != nil {
			warnf("failed to commit the checklist update: %v (continuing anyway)", err)
		}
	}
	return nil
}

func allChildrenChecked(item *suggest.Item) bool {
	for _, c := range item.Children {
		if !c.Checked {
			return false
		}
	}
	return len(item.Children) > 0
}

// findItemByLine searches items (and their children) for the one whose
// Line matches - line numbers are a stable identity for an *suggest.Item
// across a SetChecked rewrite (which never adds/removes lines) and, for a
// top-level item specifically, across InsertSubtasks too (which only
// ever inserts after that line), so this is safe to use for re-locating
// an item after either mutation instead of a fuzzier text-based search.
func findItemByLine(items []*suggest.Item, line int) *suggest.Item {
	for _, it := range items {
		if it.Line == line {
			return it
		}
		for _, c := range it.Children {
			if c.Line == line {
				return c
			}
		}
	}
	return nil
}

// parseIntentFromArg splits raw ([<file>][:<num>]) into its file and
// suggestion-number parts - a trailing ":<digits>" is treated as num,
// anything before it (or all of raw, if there's no such suffix) is file.
func parseIntentFromArg(raw string) (file, num string) {
	if raw == "" {
		return "", ""
	}
	if idx := strings.LastIndex(raw, ":"); idx != -1 {
		if maybeNum := raw[idx+1:]; maybeNum != "" {
			if _, err := strconv.Atoi(maybeNum); err == nil {
				return raw[:idx], maybeNum
			}
		}
	}
	return raw, ""
}
