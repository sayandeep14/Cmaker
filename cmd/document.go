package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"cmaker/internal/docsgen"
	"cmaker/internal/explain"
	"cmaker/internal/heal"
	"cmaker/internal/llm"
)

var documentCmd = &cobra.Command{
	Use:   "document <target>",
	Short: "Generate a Doxygen comment block for a function or class (LLM-assisted)",
	Long: "Asks an LLM (Anthropic; requires ANTHROPIC_API_KEY) to write a Doxygen comment block\n" +
		"(@brief/@param/@return/@throws) for a single function or class/struct, and shows the\n" +
		"proposed insertion as a diff. <target> is one of:\n" +
		"\n" +
		"  function=<name>    document a function/method definition\n" +
		"  class=<Name>       document a class/struct definition\n" +
		"\n" +
		"Deliberately scoped like 'cmaker improve' - a single symbol, never a whole file or\n" +
		"project. If class=/function= matches more than one definition, you'll be asked which\n" +
		"one you meant. Only the comment block is ever proposed - the code itself is never\n" +
		"touched. Nothing is written to disk unless --apply is given, and even then only after\n" +
		"you confirm the diff.",
	Example: `  cmaker document function=parseConfig
  cmaker document class=Widget --apply`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		apply, _ := cmd.Flags().GetBool("apply")
		model, _ := cmd.Flags().GetString("model")
		return runDocument(args[0], apply, model)
	},
}

func init() {
	documentCmd.Flags().Bool("apply", false, "apply the generated comment (after confirmation) instead of just showing the diff")
	documentCmd.Flags().String("model", "", "override the Anthropic model used (default: "+llm.DefaultModel+")")
}

func runDocument(target string, apply bool, model string) error {
	switch {
	case strings.HasPrefix(target, "function="):
		return runDocumentSymbol("function", strings.TrimPrefix(target, "function="), explain.FindFunction, apply, model)
	case strings.HasPrefix(target, "class="):
		return runDocumentSymbol("class", strings.TrimPrefix(target, "class="), explain.FindClass, apply, model)
	default:
		return fmt.Errorf("unrecognized document target %q - expected class=<Name> or function=<name>", target)
	}
}

// runDocumentSymbol resolves name to a single class/function match (the
// same resolveMatch UX 'cmaker improve' already uses), asks for a
// Doxygen comment block, and splices it in immediately above the
// symbol's own byte range at matching indentation - matchRange/fileChange
// (declared in improve.go) and heal.UnifiedDiff/confirmYesNo/printDiff
// are all reused unchanged, since this is the same "resolve, propose,
// diff, confirm, apply" shape improve's --strict mode already
// established, just for a comment instead of a code replacement.
func runDocumentSymbol[M symbolMatch](kind, name string, find func(root, name string) ([]M, error), apply bool, model string) error {
	m, ok, err := resolveMatch("document", kind, name, find)
	if err != nil || !ok {
		return err
	}

	relFile, start, end := matchRange(m)
	original, err := os.ReadFile(relFile)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", relFile, err)
	}
	// matchRange's start is the symbol's own first byte, never any
	// comment already sitting above it - insertionStart extends that
	// backward over an existing Doxygen block, if there is one, so (a)
	// the model actually gets to see it (without this, it could never
	// recognize ALREADY_DOCUMENTED at all, since the comment simply
	// wasn't part of what it was shown - caught live: it kept proposing
	// a second, duplicate block right on top of a real, adequate one),
	// and (b) a genuinely stale comment gets replaced in place rather
	// than stacked under a new one.
	insertionStart := precedingCommentStart(original, start)
	originalSnippet := string(original[insertionStart : end+1])

	if model == "" {
		model = llm.DefaultModel
	}
	client, err := llm.NewClientFromEnv(model)
	if err != nil {
		return err
	}

	infof("Asking %s to document %s %q...", client.Model, kind, name)
	comment, err := docsgen.GenerateDoxygenComment(context.Background(), client, kind, name, originalSnippet)
	if err != nil {
		return err
	}
	if comment == "" {
		infof("%s %q already has an adequate Doxygen comment.", strings.ToUpper(kind[:1])+kind[1:], name)
		return nil
	}

	// The tail is sliced from start, not insertionStart - discarding
	// whatever sat between them (an old comment block, if there was
	// one) rather than leaving it in place alongside the new block.
	insertion := indentCommentBlock(comment, indentPrefix(original, insertionStart))
	newContent := string(original[:insertionStart]) + insertion + string(original[start:])
	diff := heal.UnifiedDiff(relFile, string(original), newContent)
	if diff == "" {
		infof("The proposed comment was identical to what's already there - nothing to do.")
		return nil
	}
	printDiff(diff)

	if !apply {
		infof("Nothing was written to disk - review the diff above and apply it yourself, or re-run with --apply.")
		return nil
	}
	if !confirmYesNo("Apply this comment?") {
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

// precedingCommentStart returns the byte offset (past any leading
// whitespace, matching how matchRange's own start values are shaped) of
// a "/**"-style Doxygen comment block sitting immediately above start's
// own line - just start itself (no comment found) if the line directly
// above isn't part of one, so callers can use this unconditionally
// without a separate "was there one?" branch.
func precedingCommentStart(src []byte, start int) int {
	lines := strings.Split(string(src[:start]), "\n")
	i := len(lines) - 2 // the line directly above start's own (possibly partial) line
	if i < 0 || !strings.HasSuffix(strings.TrimSpace(lines[i]), "*/") {
		return start
	}
	for j := i; j >= 0; j-- {
		if !strings.HasPrefix(strings.TrimSpace(lines[j]), "/**") {
			continue
		}
		offset := 0
		for k := 0; k < j; k++ {
			offset += len(lines[k]) + 1 // +1 for the '\n' Split consumed
		}
		trimmed := strings.TrimLeft(lines[j], " \t")
		return offset + (len(lines[j]) - len(trimmed))
	}
	return start
}

// indentPrefix returns the whitespace src's line containing byte offset
// start is indented with - "" if that line has any non-whitespace before
// start (start isn't at the true start of a line's content) or start is
// at column 0. Used to match a generated comment block's indentation to
// the symbol it's being inserted above (a class method sitting inside a
// class body, for instance).
func indentPrefix(src []byte, start int) string {
	lineStart := start
	for lineStart > 0 && src[lineStart-1] != '\n' {
		lineStart--
	}
	prefix := src[lineStart:start]
	for _, b := range prefix {
		if b != ' ' && b != '\t' {
			return ""
		}
	}
	return string(prefix)
}

// indentCommentBlock prepares comment for insertion immediately before a
// symbol whose own first line already sits after indent in the source
// (so comment's first line must NOT get an extra indent prefix - it
// lands right after the existing indentation already present in the
// unmodified text before the insertion point) - only continuation lines
// need indent applied, plus a final newline (+ indent) to separate the
// comment from the symbol's own first line.
func indentCommentBlock(comment, indent string) string {
	lines := strings.Split(comment, "\n")
	var b strings.Builder
	b.WriteString(lines[0])
	for _, l := range lines[1:] {
		b.WriteString("\n")
		if l != "" {
			b.WriteString(indent)
		}
		b.WriteString(l)
	}
	b.WriteString("\n")
	b.WriteString(indent)
	return b.String()
}
