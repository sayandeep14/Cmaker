package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"cmaker/internal/explain"
	"cmaker/internal/llm"
)

var explainCmd = &cobra.Command{
	Use:   "explain <target>",
	Short: "Explain something about this project in plain English (LLM-assisted)",
	Long: "Asks an LLM (Anthropic; requires ANTHROPIC_API_KEY) to explain something about the\n" +
		"current project. <target> is one of:\n" +
		"\n" +
		"  class=<Name>       explain a class/struct definition\n" +
		"  function=<name>    explain a function/method definition\n" +
		"  file=<path>        explain what a whole file does\n" +
		"  dependency=<name>  explain a registered dependency and how this project uses it\n" +
		"  lastError          explain the most recent build/run failure (see 'cmaker logs')\n" +
		"  diff               explain the current working-tree 'git diff'\n" +
		"  config             explain cmaker.yaml and the generated CMakeLists.txt\n" +
		"\n" +
		"If class=/function= matches more than one definition in the project, you'll be asked\n" +
		"which one you meant. Nothing is written to disk - this is a pure read.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		model, _ := cmd.Flags().GetString("model")
		return runExplain(args[0], model)
	},
}

func init() {
	explainCmd.Flags().String("model", "", "override the Anthropic model used (default: "+llm.DefaultModel+")")
}

func runExplain(target, model string) error {
	userContent, err := buildExplainPrompt(target)
	if err != nil {
		return err
	}
	if userContent == "" {
		// A target reported nothing to explain (e.g. 'diff' with a clean
		// working tree) - already communicated to the user, nothing more
		// to do and no reason to spend an LLM call.
		return nil
	}

	client, err := llm.NewClientFromEnv(model)
	if err != nil {
		return err
	}

	infof("Asking %s to explain...", client.Model)
	answer, err := explain.Ask(context.Background(), client, userContent)
	if err != nil {
		return err
	}

	fmt.Println(answer)
	return nil
}

// buildExplainPrompt parses target into one of explain's seven supported
// shapes and returns the user-content string ready for explain.Ask - empty
// (with no error) only for the 'diff' target when there are no working-tree
// changes, since that case is fully handled (a message is printed) without
// needing an LLM call at all.
func buildExplainPrompt(target string) (string, error) {
	switch {
	case target == "lastError":
		return explain.BuildLastErrorPrompt(".")
	case target == "diff":
		content, err := explain.BuildDiffPrompt(".")
		if err != nil {
			return "", err
		}
		if content == "" {
			infof("No working-tree changes to explain ('git diff' is empty).")
		}
		return content, nil
	case target == "config":
		return explain.BuildConfigPrompt(".")
	case strings.HasPrefix(target, "class="):
		return buildAmbiguousPrompt("class", strings.TrimPrefix(target, "class="), explain.FindClass, explain.BuildClassPrompt)
	case strings.HasPrefix(target, "function="):
		return buildAmbiguousPrompt("function", strings.TrimPrefix(target, "function="), explain.FindFunction, explain.BuildFunctionPrompt)
	case strings.HasPrefix(target, "file="):
		return explain.BuildFilePrompt(".", strings.TrimPrefix(target, "file="))
	case strings.HasPrefix(target, "dependency="):
		return explain.BuildDependencyPrompt(".", strings.TrimPrefix(target, "dependency="))
	default:
		return "", fmt.Errorf("unrecognized explain target %q - expected class=<Name>, function=<name>, file=<path>, dependency=<name>, lastError, diff, or config", target)
	}
}

// explainMatch is the common shape buildAmbiguousPrompt needs from either
// explain.ClassMatch or explain.FuncMatch - just enough to list candidates
// and let the user pick one.
type explainMatch interface {
	explain.ClassMatch | explain.FuncMatch
}

// buildAmbiguousPrompt runs find(".", name) and turns the result into a
// single prompt string via buildPrompt: zero matches is reported as "not
// found in the current project" (the exact wording requested for class=/
// function=), a single match is used directly, and more than one triggers
// an interactive disambiguation question listing each match's file so the
// user can pick which one they meant - the same numbered-selection UX
// 'cmaker new --describe --improvise' already established in
// readSingleSelectAnswer, generalized here to a plain slice of match
// descriptions instead of improvise.Question's options.
func buildAmbiguousPrompt[M explainMatch](kind, name string, find func(root, name string) ([]M, error), buildPrompt func(name string, m M) string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("explain %s= needs a name, e.g. 'cmaker explain %s=Foo'", kind, kind)
	}

	matches, err := find(".", name)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		infof("%s %q not found in the current project.", strings.ToUpper(kind[:1])+kind[1:], name)
		return "", nil
	}
	if len(matches) == 1 {
		return buildPrompt(name, matches[0]), nil
	}

	files := make([]string, len(matches))
	for i, m := range matches {
		files[i] = matchFile(m)
	}
	fmt.Printf("Found %d definitions of %s %q:\n", len(matches), kind, name)
	for i, f := range files {
		fmt.Printf("  %d) %s\n", i+1, f)
	}
	idx := selectIndex(files)
	return buildPrompt(name, matches[idx]), nil
}

// matchFile extracts the File field from either explain.ClassMatch or
// explain.FuncMatch - both have one, but Go generics need an explicit
// accessor since the type parameter isn't a concrete struct.
func matchFile[M explainMatch](m M) string {
	switch v := any(m).(type) {
	case explain.ClassMatch:
		return v.File
	case explain.FuncMatch:
		return v.File
	default:
		return ""
	}
}

// selectIndex prompts on stdout and reads a 1-based index from stdin,
// defaulting to the first match on anything else (empty input, a read
// failure, an out-of-range or non-numeric answer) - mirrors
// cmd/describe.go's readSingleSelectAnswer, but resolves straight to an
// index (0-based) rather than option text, since callers here need to index
// back into the original []M match slice, not just echo a label.
func selectIndex(options []string) int {
	fmt.Print("Which one? [1] > ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)
	if idx, err := strconv.Atoi(line); err == nil && idx >= 1 && idx <= len(options) {
		return idx - 1
	}
	return 0
}
