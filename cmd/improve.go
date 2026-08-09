package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"cmaker/internal/explain"
	"cmaker/internal/heal"
	"cmaker/internal/improve"
	"cmaker/internal/llm"
)

var improveCmd = &cobra.Command{
	Use:   "improve <target> --intent=\"...\"",
	Short: "Ask an LLM to improve a single function or class, given an intent (LLM-assisted)",
	Long: "Asks an LLM (Anthropic; requires ANTHROPIC_API_KEY) to improve a single function or\n" +
		"class/struct given an --intent, and shows the proposed change as a diff. <target> is\n" +
		"one of:\n" +
		"\n" +
		"  function=<name>    improve a function/method definition\n" +
		"  class=<Name>       improve a class/struct definition\n" +
		"\n" +
		"Deliberately scoped to a single function or class - never the whole project (see\n" +
		"'cmaker suggest' for whole-project suggestions). If class=/function= matches more\n" +
		"than one definition, you'll be asked which one you meant.\n" +
		"\n" +
		"By default the model sees the *whole* file the target lives in and may change\n" +
		"anywhere within it (add a #include, add a helper function, ...) and/or propose\n" +
		"brand-new files (e.g. a new helper header) - never an existing file other than the\n" +
		"one it was shown, though. --strict instead scopes it to exactly the function/class\n" +
		"body itself, touching nothing else.\n" +
		"\n" +
		"Nothing is written to disk unless --apply is given, and even then only after you\n" +
		"confirm the diff.",
	Example: `  cmaker improve function=parseConfig --intent="extract shared validation into a helper"
  cmaker improve class=Widget --intent="add move semantics" --apply
  cmaker improve function=isPrime --intent="improve time complexity" --strict`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		intent, _ := cmd.Flags().GetString("intent")
		apply, _ := cmd.Flags().GetBool("apply")
		strict, _ := cmd.Flags().GetBool("strict")
		model, _ := cmd.Flags().GetString("model")
		return runImprove(args[0], intent, apply, strict, model)
	},
}

func init() {
	improveCmd.Flags().String("intent", "", "what the improvement should accomplish, e.g. \"improve time complexity\" (required)")
	improveCmd.Flags().Bool("apply", false, "apply the change (after confirmation) instead of just showing the diff")
	improveCmd.Flags().Bool("strict", false, "scope the change to exactly the function/class body - no new headers/helpers/files (default: the model may change anywhere in the same file, or add new files)")
	improveCmd.Flags().String("model", "", "override the Anthropic model used (default: "+llm.DefaultImproviseModel+")")
	improveCmd.MarkFlagRequired("intent")
}

func runImprove(target, intent string, apply, strict bool, model string) error {
	switch {
	case strings.HasPrefix(target, "function="):
		return runImproveSymbol("function", strings.TrimPrefix(target, "function="), explain.FindFunction, intent, apply, strict, model)
	case strings.HasPrefix(target, "class="):
		return runImproveSymbol("class", strings.TrimPrefix(target, "class="), explain.FindClass, intent, apply, strict, model)
	default:
		return fmt.Errorf("unrecognized improve target %q - expected class=<Name> or function=<name> (a whole file/project isn't supported - see 'cmaker suggest')", target)
	}
}

// symbolMatch is the common shape runImproveSymbol needs from either
// explain.ClassMatch or explain.FuncMatch - the byte range within File,
// so the proposed replacement can be spliced back into the exact spot it
// came from.
type symbolMatch interface {
	explain.ClassMatch | explain.FuncMatch
}

func matchRange[M symbolMatch](m M) (file string, start, end int) {
	switch v := any(m).(type) {
	case explain.ClassMatch:
		return v.File, v.Start, v.End
	case explain.FuncMatch:
		return v.File, v.Start, v.End
	default:
		return "", 0, 0
	}
}

func runImproveSymbol[M symbolMatch](kind, name string, find func(root, name string) ([]M, error), intent string, apply, strict bool, model string) error {
	m, ok, err := resolveMatch("improve", kind, name, find)
	if err != nil || !ok {
		return err
	}

	// Defaults to the stronger model (Sonnet), not DefaultModel (Haiku) -
	// same reasoning '--improvise' already established: real code
	// authorship is a heavier reasoning job than single-shot menu
	// selection or identification, and live testing here found Haiku
	// noticeably prone to malformed output on this task specifically.
	if model == "" {
		model = llm.DefaultImproviseModel
	}
	client, err := llm.NewClientFromEnv(model)
	if err != nil {
		return err
	}

	if strict {
		return runImproveStrict(client, kind, name, m, intent, apply)
	}
	return runImproveFile(client, kind, name, m, intent, apply)
}

// runImproveStrict is --strict mode: the proposed replacement is spliced
// verbatim into exactly the symbol's own byte range - nothing else is
// ever touched.
func runImproveStrict[M symbolMatch](client *llm.Client, kind, name string, m M, intent string, apply bool) error {
	relFile, start, end := matchRange(m)
	original, err := os.ReadFile(relFile)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", relFile, err)
	}
	originalSnippet := string(original[start : end+1])

	infof("Asking %s to improve %s %q (--strict: scoped to just this %s)...", client.Model, kind, name, kind)
	replacement, err := improve.Suggest(context.Background(), client, kind, name, originalSnippet, intent)
	if err != nil {
		return err
	}
	if replacement == "" {
		infof("The model didn't propose a change to %s %q.", kind, name)
		return nil
	}

	newContent := string(original[:start]) + replacement + string(original[end+1:])
	diff := heal.UnifiedDiff(relFile, string(original), newContent)
	if diff == "" {
		infof("The proposed change was identical to the current code - nothing to do.")
		return nil
	}
	printDiff(diff)

	if !apply {
		infof("Nothing was written to disk - review the diff above and apply it yourself, or re-run with --apply.")
		return nil
	}
	if !confirmYesNo("Apply this change?") {
		infof("Not applied.")
		return nil
	}

	info, statErr := os.Stat(relFile)
	perm := os.FileMode(0644)
	if statErr == nil {
		perm = info.Mode().Perm()
	}
	if err := os.WriteFile(relFile, []byte(newContent), perm); err != nil {
		return fmt.Errorf("failed to write %s: %w", relFile, err)
	}
	okf("Applied to %s.", filepath.Clean(relFile))
	return nil
}

// fileChange is one file runImproveFile is about to diff/apply - either
// the target file itself (original is its real current content) or a
// brand-new file the model proposed (original is "", isNew is true).
type fileChange struct {
	path       string
	original   string
	newContent string
	isNew      bool
}

// runImproveFile is the default (non-strict) mode: the model sees
// targetFile's entire content and may change anywhere within it, and/or
// propose brand-new files - never an existing file other than the target,
// since it never saw that file's content (enforced here, not just asked
// for in the prompt: any proposed change to a different file that already
// exists on disk is dropped with a warning, not silently trusted).
func runImproveFile[M symbolMatch](client *llm.Client, kind, name string, m M, intent string, apply bool) error {
	relFile, _, _ := matchRange(m)
	original, err := os.ReadFile(relFile)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", relFile, err)
	}

	infof("Asking %s to improve %s %q (may change anywhere in %s, or add new files)...", client.Model, kind, name, relFile)
	blocks, err := improve.SuggestFile(context.Background(), client, kind, name, relFile, string(original), intent)
	if err != nil {
		return err
	}
	if blocks == nil {
		infof("The model didn't propose a change to %s %q.", kind, name)
		return nil
	}

	cleanTarget := filepath.Clean(relFile)
	var changes []fileChange
	for path, newContent := range blocks {
		cleanPath := filepath.Clean(path)
		if cleanPath == cleanTarget {
			changes = append(changes, fileChange{path: relFile, original: string(original), newContent: newContent})
			continue
		}
		if !isSafeNewFilePath(cleanPath) {
			warnf("Ignoring a proposed file at %q - not a safe relative path.", path)
			continue
		}
		if _, statErr := os.Stat(cleanPath); statErr == nil {
			warnf("Ignoring a proposed change to %s - it wasn't shown to the model, so the change can't be trusted (re-run 'cmaker improve' targeting it directly if it needs updating too).", cleanPath)
			continue
		}
		changes = append(changes, fileChange{path: cleanPath, newContent: newContent, isNew: true})
	}

	// Stable order: the target file first, then any new files alphabetically.
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].path == relFile {
			return true
		}
		if changes[j].path == relFile {
			return false
		}
		return changes[i].path < changes[j].path
	})

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
	if combined.Len() == 0 {
		infof("The proposed change was identical to the current code - nothing to do.")
		return nil
	}
	printDiff(combined.String())

	if !apply {
		infof("Nothing was written to disk - review the diff above and apply it yourself, or re-run with --apply.")
		return nil
	}
	if !confirmYesNo("Apply this change?") {
		infof("Not applied.")
		return nil
	}

	names := make([]string, 0, len(toApply))
	for _, c := range toApply {
		if c.isNew {
			if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
				return fmt.Errorf("failed to create directory for %s: %w", c.path, err)
			}
			if err := os.WriteFile(c.path, []byte(c.newContent), 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", c.path, err)
			}
		} else {
			perm := os.FileMode(0644)
			if info, statErr := os.Stat(c.path); statErr == nil {
				perm = info.Mode().Perm()
			}
			if err := os.WriteFile(c.path, []byte(c.newContent), perm); err != nil {
				return fmt.Errorf("failed to write %s: %w", c.path, err)
			}
		}
		names = append(names, c.path)
	}
	okf("Applied to %s.", strings.Join(names, ", "))
	return nil
}

// isSafeNewFilePath rejects an absolute path or one that escapes the
// project root (e.g. "../../etc/passwd") - a model proposing a brand-new
// file's path is new trust surface non-strict mode introduces, so this is
// checked defensively rather than just relying on the prompt asking for
// "a sensible path relative to the project root."
func isSafeNewFilePath(cleanPath string) bool {
	if filepath.IsAbs(cleanPath) {
		return false
	}
	return cleanPath != ".." && !strings.HasPrefix(cleanPath, ".."+string(filepath.Separator))
}
