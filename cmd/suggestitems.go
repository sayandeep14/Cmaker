package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"cmaker/internal/suggest"
)

// suggestNextCmd/suggestListCmd/suggestAddCmd/suggestRemoveCmd/
// suggestDoneCmd/suggestUndoneCmd let a 'cmaker suggest --export' checklist
// be worked with directly from the command line - viewing what's queued
// up, and adding/removing/marking items by hand - without needing an LLM
// call (or 'cmaker codegen --intent-from') for bookkeeping that's often
// quicker to just type. All six share --file, defaulting to the same
// defaultSuggestionsFile 'cmaker codegen --intent-from' falls back to
// (cmd/codegen.go), so a bare 'cmaker suggest next' and a bare 'cmaker
// codegen --intent-from' agree on which file they mean without either
// having to say so.

var suggestNextCmd = &cobra.Command{
	Use:   "next [flags]",
	Short: "Show the next unmarked suggestion in the queue",
	Long: "Prints the first not-yet-checked-off item in a 'cmaker suggest --export' checklist -\n" +
		"the same item 'cmaker codegen --intent-from' (with no :<num>) would pick up next. Use\n" +
		"'cmaker suggest list' to see every suggestion, not just the next one.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		file, _ := cmd.Flags().GetString("file")
		return runSuggestNext(resolveSuggestionsFile(file))
	},
}

var suggestListCmd = &cobra.Command{
	Use:     "list [flags]",
	Aliases: []string{"ls"},
	Short:   "List every suggestion in the queue, checked and unchecked",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		file, _ := cmd.Flags().GetString("file")
		return runSuggestList(resolveSuggestionsFile(file))
	},
}

var suggestAddCmd = &cobra.Command{
	Use:   "add \"<text>\" [flags]",
	Short: "Manually add a suggestion to the queue",
	Long: "Appends a new unchecked item to a 'cmaker suggest --export' checklist, in the same\n" +
		"format an LLM-generated suggestion would be - so it's indistinguishable to 'cmaker\n" +
		"suggest next'/'cmaker codegen --intent-from' from one the model proposed. Creates the\n" +
		"file (and its parent directory) if it doesn't exist yet.",
	Example: `  cmaker suggest add "Add bounds checking to parseArgs"
  cmaker suggest add "Extract shared validation logic" --detail="Duplicated across parseA and parseB"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		file, _ := cmd.Flags().GetString("file")
		detail, _ := cmd.Flags().GetString("detail")
		return runSuggestAdd(resolveSuggestionsFile(file), args[0], detail)
	},
}

var suggestRemoveCmd = &cobra.Command{
	Use:   "remove <num> [flags]",
	Short: "Manually remove a suggestion from the queue",
	Long: "Deletes the num'th (1-based, as shown by 'cmaker suggest list') top-level suggestion -\n" +
		"its own line, any detail paragraph, and any sub-tasks a breakdown inserted under it.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		file, _ := cmd.Flags().GetString("file")
		num, err := parseSuggestionNum(args[0])
		if err != nil {
			return err
		}
		return runSuggestRemove(resolveSuggestionsFile(file), num)
	},
}

var suggestDoneCmd = &cobra.Command{
	Use:   "done <num> [flags]",
	Short: "Manually mark a suggestion done",
	Long: "Checks off the num'th (1-based, as shown by 'cmaker suggest list') top-level\n" +
		"suggestion - and every one of its sub-tasks, if it's been broken down.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		file, _ := cmd.Flags().GetString("file")
		num, err := parseSuggestionNum(args[0])
		if err != nil {
			return err
		}
		return runSuggestSetDone(resolveSuggestionsFile(file), num, true)
	},
}

var suggestUndoneCmd = &cobra.Command{
	Use:   "undone <num> [flags]",
	Short: "Manually mark a suggestion not done",
	Long: "Unchecks the num'th (1-based, as shown by 'cmaker suggest list') top-level\n" +
		"suggestion - and every one of its sub-tasks, if it's been broken down.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		file, _ := cmd.Flags().GetString("file")
		num, err := parseSuggestionNum(args[0])
		if err != nil {
			return err
		}
		return runSuggestSetDone(resolveSuggestionsFile(file), num, false)
	},
}

func init() {
	for _, c := range []*cobra.Command{suggestNextCmd, suggestListCmd, suggestAddCmd, suggestRemoveCmd, suggestDoneCmd, suggestUndoneCmd} {
		c.Flags().String("file", "", "the checklist file to use (default: "+defaultSuggestionsFile+")")
	}
	suggestAddCmd.Flags().String("detail", "", "an optional one or two sentences of specifics for this suggestion")
	suggestCmd.AddCommand(suggestNextCmd, suggestListCmd, suggestAddCmd, suggestRemoveCmd, suggestDoneCmd, suggestUndoneCmd)
}

// resolveSuggestionsFile applies defaultSuggestionsFile's fallback (see
// cmd/codegen.go) so every one of these subcommands agrees with 'cmaker
// codegen --intent-from' on which file a bare --file (or no --file at
// all) means.
func resolveSuggestionsFile(file string) string {
	if file == "" {
		return defaultSuggestionsFile
	}
	return file
}

// parseSuggestionNum validates a <num> argument up front, so a typo like
// "cmaker suggest done abc" fails with a clear message instead of an
// unrelated one from further down the call stack.
func parseSuggestionNum(raw string) (int, error) {
	num, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("invalid suggestion number %q", raw)
	}
	return num, nil
}

func readChecklistFile(file string) ([]byte, []*suggest.Item, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read %s: %w (run 'cmaker suggest --export=%s' or 'cmaker suggest add' first)", file, err, file)
	}
	items := suggest.ParseChecklist(data)
	return data, items, nil
}

func runSuggestNext(file string) error {
	_, items, err := readChecklistFile(file)
	if err != nil {
		return err
	}
	item := suggest.FindFirstUnchecked(items)
	if item == nil {
		infof("Nothing left to do in %s - every suggestion is checked off.", file)
		return nil
	}
	printSuggestionItem(item)
	return nil
}

func runSuggestList(file string) error {
	_, items, err := readChecklistFile(file)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		infof("%s has no suggestions yet. Add one with 'cmaker suggest add \"...\"'.", file)
		return nil
	}
	for i, item := range items {
		printChecklistLine(fmt.Sprintf("%d.", i+1), item)
		for _, c := range item.Children {
			printChecklistLine("   -", c)
		}
	}
	return nil
}

func printChecklistLine(prefix string, item *suggest.Item) {
	mark := " "
	if item.Checked {
		mark = "x"
	}
	fmt.Printf("%s [%s] %s\n", prefix, mark, item.Text)
	if item.Detail != "" {
		fmt.Printf("      %s\n", item.Detail)
	}
}

func printSuggestionItem(item *suggest.Item) {
	fmt.Println(item.Text)
	if item.Detail != "" {
		fmt.Printf("  %s\n", item.Detail)
	}
	if item.Parent != nil {
		infof("(sub-task of %q)", item.Parent.Text)
	}
}

func runSuggestAdd(file, title, detail string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("suggestion text must not be empty")
	}

	data, err := os.ReadFile(file)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to read %s: %w", file, err)
		}
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", file, err)
		}
		data = []byte("# cmaker suggest\n\n")
	}

	updated := suggest.AppendItem(data, title, detail)
	if err := os.WriteFile(file, updated, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", file, err)
	}
	okf("Added %q to %s.", title, file)
	return nil
}

func runSuggestRemove(file string, num int) error {
	data, items, err := readChecklistFile(file)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("%s has no checklist items to remove", file)
	}
	if num < 1 || num > len(items) {
		return fmt.Errorf("suggestion #%d out of range - there are %d top-level suggestions (see 'cmaker suggest list')", num, len(items))
	}
	removedText := items[num-1].Text
	updated, err := suggest.RemoveTopLevelItem(data, items, num)
	if err != nil {
		return err
	}
	if err := os.WriteFile(file, updated, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", file, err)
	}
	okf("Removed %q from %s.", removedText, file)
	return nil
}

func runSuggestSetDone(file string, num int, done bool) error {
	data, items, err := readChecklistFile(file)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("%s has no checklist items to mark", file)
	}
	if num < 1 || num > len(items) {
		return fmt.Errorf("suggestion #%d out of range - there are %d top-level suggestions (see 'cmaker suggest list')", num, len(items))
	}
	text := items[num-1].Text
	updated, err := suggest.SetTopLevelChecked(data, items, num, done)
	if err != nil {
		return err
	}
	if err := os.WriteFile(file, updated, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", file, err)
	}
	if done {
		okf("Marked %q done in %s.", text, file)
	} else {
		okf("Marked %q not done in %s.", text, file)
	}
	return nil
}
