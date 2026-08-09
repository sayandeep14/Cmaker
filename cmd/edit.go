package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"cmaker/internal/explain"
	"cmaker/internal/heal"
)

var editCmd = &cobra.Command{
	Use:   "edit <target>",
	Short: "Edit code directly in the terminal, with live syntax highlighting - no LLM involved",
	Long: "Opens the matched code in an inline, nano-like terminal editor - arrow keys to move,\n" +
		"type to insert, Backspace/Delete to remove, Ctrl+S to save and exit, Esc/Ctrl+C to\n" +
		"cancel without writing anything. <target> is one of:\n" +
		"\n" +
		"  class=<Name>       edit a class/struct definition\n" +
		"  function=<name>    edit a function/method definition\n" +
		"  file=<path>        edit a whole file\n" +
		"\n" +
		"If class=/function= matches more than one definition in the project, you'll be asked\n" +
		"which one you meant. This is a purely local, manual edit - no LLM is involved, unlike\n" +
		"'cmaker improve'.",
	Example: `  cmaker edit function=parseConfig
  cmaker edit class=Widget
  cmaker edit file=src/main.cpp`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runEdit(args[0])
	},
}

func runEdit(target string) error {
	switch {
	case strings.HasPrefix(target, "file="):
		return runEditFile(strings.TrimPrefix(target, "file="))
	case strings.HasPrefix(target, "function="):
		return runEditSymbol("function", strings.TrimPrefix(target, "function="), explain.FindFunction)
	case strings.HasPrefix(target, "class="):
		return runEditSymbol("class", strings.TrimPrefix(target, "class="), explain.FindClass)
	default:
		return fmt.Errorf("unrecognized edit target %q - expected class=<Name>, function=<name>, or file=<path>", target)
	}
}

// runEditFile edits path's whole content and, on save, overwrites it
// entirely.
func runEditFile(path string) error {
	if path == "" {
		return fmt.Errorf("file= needs a path, e.g. 'cmaker edit file=src/main.cpp'")
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	edited, ok, err := runLineEditor(string(original), path)
	if err != nil {
		return err
	}
	if !ok {
		infof("Not saved.")
		return nil
	}
	return writeEditedFile(path, string(original), edited)
}

// runEditSymbol resolves name to a single class/function match (the same
// resolveMatch UX 'cmaker read'/'improve' already use), edits just that
// span, and splices the result back into its exact original byte range on
// save - matchRange (declared in improve.go) supplies that range for
// either match type.
func runEditSymbol[M symbolMatch](kind, name string, find func(root, name string) ([]M, error)) error {
	m, ok, err := resolveMatch("edit", kind, name, find)
	if err != nil || !ok {
		return err
	}

	relFile, start, end := matchRange(m)
	original, err := os.ReadFile(relFile)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", relFile, err)
	}
	originalSnippet := string(original[start : end+1])

	edited, ok, err := runLineEditor(originalSnippet, relFile)
	if err != nil {
		return err
	}
	if !ok {
		infof("Not saved.")
		return nil
	}

	newContent := string(original[:start]) + edited + string(original[end+1:])
	return writeEditedFile(relFile, string(original), newContent)
}

// writeEditedFile diffs original against newContent (purely to report
// what changed - the write itself already happened by the user's own
// explicit Ctrl+S, so there's no further confirmation step here, matching
// 'cmaker commit --preview's Ctrl+S-is-the-confirmation precedent) and
// writes newContent to path, preserving its existing permissions.
func writeEditedFile(path, original, newContent string) error {
	if newContent == original {
		infof("No changes made.")
		return nil
	}
	if diff := heal.UnifiedDiff(path, original, newContent); diff != "" {
		printDiff(diff)
	}

	perm := os.FileMode(0644)
	if info, statErr := os.Stat(path); statErr == nil {
		perm = info.Mode().Perm()
	}
	if err := os.WriteFile(path, []byte(newContent), perm); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	okf("Saved %s.", filepath.Clean(path))
	return nil
}
