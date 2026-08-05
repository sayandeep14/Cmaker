package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"cmaker/internal/codegen"
	"cmaker/internal/llm"
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "LLM-assisted code generation tools",
}

var generateAccessorsCmd = &cobra.Command{
	Use:   "accessors [file] [class]",
	Short: "Generate getter/setter accessors for a class's non-public members",
	Long: "Reads the named class/struct's body from a file, asks an LLM (Anthropic; requires\n" +
		"ANTHROPIC_API_KEY) to identify its non-public data members, then deterministically\n" +
		"renders and inserts getX()/setX(...) accessor methods. The LLM only ever identifies\n" +
		"members - it never writes code directly into your file; cmaker renders the actual\n" +
		"C++ text itself from a fixed template.\n\n" +
		"Re-running on the same class replaces its previously generated block in place\n" +
		"(marked with a greppable '// --- cmaker generated accessors ---' comment) instead\n" +
		"of duplicating it.",
	Example: `  cmaker generate accessors Pqr.cpp Abc
  cmaker generate accessors --file=include/Pqr.hpp --class=Abc --dry-run`,
	Args: cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		file, _ := cmd.Flags().GetString("file")
		class, _ := cmd.Flags().GetString("class")
		model, _ := cmd.Flags().GetString("model")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if len(args) >= 1 && file == "" {
			file = args[0]
		}
		if len(args) >= 2 && class == "" {
			class = args[1]
		}
		if file == "" || class == "" {
			return fmt.Errorf("usage: cmaker generate accessors <file> <class>  (or --file=<file> --class=<class>)")
		}

		return runGenerateAccessors(file, class, model, dryRun)
	},
}

func init() {
	generateAccessorsCmd.Flags().StringP("file", "f", "", "source/header file containing the class")
	generateAccessorsCmd.Flags().StringP("class", "c", "", "class or struct name to generate accessors for")
	generateAccessorsCmd.Flags().String("model", "", "override the Anthropic model used (default: "+llm.DefaultModel+")")
	generateAccessorsCmd.Flags().Bool("dry-run", false, "print the generated accessors without writing the file")
	generateCmd.AddCommand(generateAccessorsCmd)
}

func runGenerateAccessors(file, class, model string, dryRun bool) error {
	src, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", file, err)
	}

	bodyOpen, bodyClose, err := codegen.ExtractClassBody(src, class)
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}

	client, err := llm.NewClientFromEnv(model)
	if err != nil {
		return err
	}

	infof("Asking %s to identify accessor-eligible members of %q...", client.Model, class)
	members, err := codegen.ExtractMembers(context.Background(), client, class, string(src[bodyOpen+1:bodyClose]))
	if err != nil {
		return err
	}
	if len(members) == 0 {
		infof("No accessor-eligible members found in %q - nothing to generate.", class)
		return nil
	}

	names := make([]string, len(members))
	for i, m := range members {
		names[i] = m.Name
	}

	if dryRun {
		infof("Would generate accessors for: %s", strings.Join(names, ", "))
		fmt.Print(codegen.RenderAccessors(members, "    "))
		return nil
	}

	newSrc, err := codegen.InsertAccessors(src, class, members)
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	if err := os.WriteFile(file, newSrc, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", file, err)
	}
	okf("Generated accessors for %s in %s: %s", class, file, strings.Join(names, ", "))
	return nil
}
