package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"cmaker/internal/docsgen"
	"cmaker/internal/explain"
	"cmaker/internal/llm"
)

var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Build API documentation with Doxygen, or generate LLM-authored narrative docs",
	Long: "Without --narrative, builds API documentation with Doxygen (writes a default Doxyfile\n" +
		"first if none exists - see 'cmaker new --with-docs' to scaffold one automatically).\n" +
		"\n" +
		"--narrative instead asks an LLM (Anthropic; requires ANTHROPIC_API_KEY) to write real\n" +
		"prose documentation: an architecture overview plus a per-file explanation of what each\n" +
		"file does and why - written as markdown files under --out, not generated from Doxygen\n" +
		"comment blocks. --file scopes it to just one file (cheap, fast); without it, the whole\n" +
		"project is covered up to --max-files (one LLM call per file, so this is capped by\n" +
		"default the same way 'cmaker suggest' bounds how much source it sends in one request).",
	Example: `  cmaker docs
  cmaker docs --narrative
  cmaker docs --narrative --file=src/main.cpp`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		narrative, _ := cmd.Flags().GetBool("narrative")
		if !narrative {
			return runDocs()
		}
		file, _ := cmd.Flags().GetString("file")
		outDir, _ := cmd.Flags().GetString("out")
		maxFiles, _ := cmd.Flags().GetInt("max-files")
		model, _ := cmd.Flags().GetString("model")
		if maxFiles < 1 {
			return fmt.Errorf("--max-files must be at least 1")
		}
		return runNarrativeDocs(file, outDir, maxFiles, model)
	},
}

func init() {
	docsCmd.Flags().Bool("narrative", false, "generate LLM-authored narrative docs instead of building Doxygen HTML")
	docsCmd.Flags().String("file", "", "with --narrative: only narrate this one file, instead of the whole project")
	docsCmd.Flags().String("out", "docs/narrative", "with --narrative: output directory for generated docs")
	docsCmd.Flags().Int("max-files", 15, "with --narrative (whole-project mode): cap how many files get their own narrative doc")
	docsCmd.Flags().String("model", "", "override the Anthropic model used with --narrative (default: "+llm.DefaultModel+")")
}

// runNarrativeDocs writes an architecture overview (BuildArchitecturePrompt
// + NarrateArchitecture, one LLM call) and, unless scoped to a single
// onlyFile, a per-file narrative doc for up to maxFiles source files
// (largest first - the same "biggest files are most likely to matter"
// heuristic internal/suggest already uses), one LLM call each.
func runNarrativeDocs(onlyFile, outDir string, maxFiles int, model string) error {
	if model == "" {
		model = llm.DefaultModel
	}
	client, err := llm.NewClientFromEnv(model)
	if err != nil {
		return err
	}

	if onlyFile != "" {
		data, err := os.ReadFile(onlyFile)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", onlyFile, err)
		}
		infof("Asking %s to narrate %s...", client.Model, onlyFile)
		doc, err := docsgen.NarrateFile(context.Background(), client, onlyFile, string(data))
		if err != nil {
			return err
		}
		return writeNarrativeDoc(outDir, onlyFile, doc)
	}

	infof("Asking %s for an architecture overview...", client.Model)
	archPrompt, err := docsgen.BuildArchitecturePrompt(".")
	if err != nil {
		return err
	}
	archDoc, err := docsgen.NarrateArchitecture(context.Background(), client, archPrompt)
	if err != nil {
		return err
	}
	archPath := filepath.Join(outDir, "ARCHITECTURE.md")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", outDir, err)
	}
	if err := os.WriteFile(archPath, []byte(archDoc), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", archPath, err)
	}
	okf("Wrote %s", archPath)

	files, err := explain.WalkSourceFiles(".")
	if err != nil {
		return err
	}
	selected := selectFilesToNarrate(files, maxFiles)
	if len(files) > len(selected) {
		warnf("Project has %d source files - only narrating the %d largest (re-run with --file=<path> for others, or raise --max-files).", len(files), len(selected))
	}

	for _, f := range selected {
		data, err := os.ReadFile(f)
		if err != nil {
			warnf("failed to read %s: %v", f, err)
			continue
		}
		infof("Asking %s to narrate %s...", client.Model, f)
		doc, err := docsgen.NarrateFile(context.Background(), client, f, string(data))
		if err != nil {
			warnf("failed to narrate %s: %v", f, err)
			continue
		}
		if err := writeNarrativeDoc(outDir, f, doc); err != nil {
			warnf("%v", err)
		}
	}
	okf("Narrative docs written to %s", outDir)
	return nil
}

// selectFilesToNarrate caps files at max, largest-first - the same
// "biggest files are the most likely to matter" heuristic
// internal/suggest.BuildPrompt already applies.
func selectFilesToNarrate(files []string, max int) []string {
	type fileInfo struct {
		rel  string
		size int64
	}
	infos := make([]fileInfo, 0, len(files))
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil {
			continue
		}
		infos = append(infos, fileInfo{rel: f, size: fi.Size()})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].size > infos[j].size })
	if len(infos) > max {
		infos = infos[:max]
	}
	selected := make([]string, len(infos))
	for i, fi := range infos {
		selected[i] = fi.rel
	}
	return selected
}

// writeNarrativeDoc writes doc to outDir, mirroring srcPath's own
// relative path with ".md" appended (e.g. src/main.cpp ->
// docs/narrative/src/main.cpp.md), so a large project's narrative docs
// stay organized the same way its source tree already is.
func writeNarrativeDoc(outDir, srcPath, doc string) error {
	target := filepath.Join(outDir, srcPath+".md")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", target, err)
	}
	if err := os.WriteFile(target, []byte(doc), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", target, err)
	}
	okf("Wrote %s", target)
	return nil
}

func runDocs() error {
	if _, err := exec.LookPath("doxygen"); err != nil {
		return fmt.Errorf("doxygen not found on PATH (see 'cmaker doctor')")
	}

	if _, err := os.Stat("Doxyfile"); err != nil {
		infof("No Doxyfile found - writing a default one (see 'cmaker new --with-docs' to scaffold one automatically next time).")
		cfg := loadConfigOrExit()
		if err := writeDoxyfile(".", cfg.ProjectName); err != nil {
			return fmt.Errorf("failed to write a default Doxyfile: %w", err)
		}
	}

	cmd := exec.Command("doxygen", "Doxyfile")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("doxygen failed: %w", err)
	}

	okf("Docs built: %s", filepath.Join("docs", "html", "index.html"))
	return nil
}
