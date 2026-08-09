package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"cmaker/internal/explain"
	"cmaker/internal/llm"
	"cmaker/internal/logs"
)

var readCmd = &cobra.Command{
	Use:   "read <target>",
	Short: "Display code with syntax highlighting - a pure read, no LLM by default",
	Long: "Prints the matched code with terminal syntax highlighting. <target> is one of:\n" +
		"\n" +
		"  class=<Name>       show a class/struct definition\n" +
		"  function=<name>    show a function/method definition\n" +
		"  file=<path>        show a whole file\n" +
		"  lastError          show the most recent build/run failure log\n" +
		"\n" +
		"If class=/function= matches more than one definition in the project, you'll be asked\n" +
		"which one you meant. Nothing is written to disk, and by default nothing is sent to an\n" +
		"LLM either - this is a pure local read.\n" +
		"\n" +
		"--explain adds inline explanatory comments around the code (LLM-assisted; requires\n" +
		"ANTHROPIC_API_KEY). The code itself is never rewritten by the model - it only proposes\n" +
		"where a comment belongs, and cmaker splices that comment into the exact original source\n" +
		"deterministically, so what you see is always still the real code.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		explainFlag, _ := cmd.Flags().GetBool("explain")
		model, _ := cmd.Flags().GetString("model")
		return runRead(args[0], explainFlag, model)
	},
}

func init() {
	readCmd.Flags().Bool("explain", false, "add inline explanatory comments around the code (LLM-assisted; code itself is never rewritten)")
	readCmd.Flags().String("model", "", "override the Anthropic model used with --explain (default: "+llm.DefaultModel+")")
}

func runRead(target string, explainFlag bool, model string) error {
	switch {
	case target == "lastError":
		if explainFlag {
			warnf("--explain has no effect on lastError - it's a build/run log, not code to annotate")
		}
		return runReadLastError()
	case strings.HasPrefix(target, "file="):
		return runReadFile(strings.TrimPrefix(target, "file="), explainFlag, model)
	case strings.HasPrefix(target, "function="):
		return runReadSymbol("function", strings.TrimPrefix(target, "function="), explain.FindFunction, func(m explain.FuncMatch) string { return m.Body }, explainFlag, model)
	case strings.HasPrefix(target, "class="):
		return runReadSymbol("class", strings.TrimPrefix(target, "class="), explain.FindClass, func(m explain.ClassMatch) string { return m.Body }, explainFlag, model)
	default:
		return fmt.Errorf("unrecognized read target %q - expected class=<Name>, function=<name>, file=<path>, or lastError", target)
	}
}

// runReadLastError prints the most recent failing build/run log's raw
// content - the same content 'cmaker logs 1' already shows, just
// reachable under 'read' too for a consistent target vocabulary with
// 'cmaker explain lastError'. Not syntax-highlighted (compiler/linker
// output isn't C++ source - running it through a C++ lexer would just
// produce noise, not clarity) and --explain doesn't apply to it.
func runReadLastError() error {
	logPath, err := logs.LatestFailure(".", "")
	if err != nil {
		return err
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", logPath, err)
	}
	fmt.Println(string(data))
	return nil
}

// runReadFile prints path's content, optionally annotated, with syntax
// highlighting.
func runReadFile(path string, explainFlag bool, model string) error {
	if path == "" {
		return fmt.Errorf("file= needs a path, e.g. 'cmaker read file=src/main.cpp'")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	code := string(data)

	if explainFlag {
		code, err = annotateCode(code, model)
		if err != nil {
			return err
		}
	}
	fmt.Println(highlightCode(code, path))
	return nil
}

// runReadSymbol resolves name to a single class/function match (with the
// same ambiguity-resolution UX 'cmaker explain' already established -
// resolveMatch is shared code, not a second copy of it), then prints its
// body, optionally annotated, with syntax highlighting.
func runReadSymbol[M explainMatch](kind, name string, find func(root, name string) ([]M, error), body func(M) string, explainFlag bool, model string) error {
	m, ok, err := resolveMatch("read", kind, name, find)
	if err != nil || !ok {
		return err
	}

	code := body(m)
	if explainFlag {
		code, err = annotateCode(code, model)
		if err != nil {
			return err
		}
	}
	fmt.Println(highlightCode(code, matchFile(m)))
	return nil
}

// annotateCode asks an LLM to propose inline explanatory comments for
// code and splices them into the original, unmodified source - see
// internal/explain.Annotation's own doc for why comments are proposed as
// line-anchored data rather than trusting the model to reproduce the code
// verbatim.
func annotateCode(code, model string) (string, error) {
	client, err := llm.NewClientFromEnv(model)
	if err != nil {
		return "", err
	}

	infof("Asking %s to annotate this code...", client.Model)
	annotations, err := explain.Annotate(context.Background(), client, code)
	if err != nil {
		return "", err
	}
	return explain.InsertAnnotations(code, annotations), nil
}
