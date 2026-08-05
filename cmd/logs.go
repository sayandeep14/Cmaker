package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"cmaker/internal/logs"
)

var logsCmd = &cobra.Command{
	Use:   "logs [n]",
	Short: "List recent cmaker build/run logs, or print one",
	Long: "Every 'cmaker build'/'cmaker run' captures its combined output under .cmaker/logs/ (§24) -\n" +
		"the last 5 by default (cmaker.yaml's logs_keep to change that). With no argument, lists\n" +
		"them newest first; with a number, prints that log's full content (see also 'cmaker heal',\n" +
		"which reads the most recent failing one automatically).",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			n, err := strconv.Atoi(args[0])
			if err != nil || n < 1 {
				return fmt.Errorf("expected a positive log number (see 'cmaker logs' for the list)")
			}
			return runShowLog(n)
		}
		return runListLogs()
	},
}

func runListLogs() error {
	names, err := logs.List(".")
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", logs.Dir, err)
	}
	if len(names) == 0 {
		infof("No logs captured yet - run 'cmaker build' or 'cmaker run' first.")
		return nil
	}
	for i, n := range names {
		fmt.Printf("%d. %s\n", i+1, n)
	}
	infof("Run 'cmaker logs <n>' to print one.")
	return nil
}

func runShowLog(n int) error {
	names, err := logs.List(".")
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", logs.Dir, err)
	}
	if n > len(names) {
		return fmt.Errorf("only %d log(s) captured (see 'cmaker logs')", len(names))
	}
	data, err := os.ReadFile(filepath.Join(logs.Dir, names[n-1]))
	if err != nil {
		return fmt.Errorf("failed to read log: %w", err)
	}
	fmt.Print(string(data))
	return nil
}
