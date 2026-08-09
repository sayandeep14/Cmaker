package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"cmaker/internal/codegen"
	"cmaker/internal/config"
	"cmaker/internal/docsgen"
	"cmaker/internal/explain"
	"cmaker/internal/llm"
	"cmaker/internal/registry"
	"cmaker/internal/suggest"
)

// statusCmd is the project dashboard - deliberately a different thing from
// plain git status (see 'cmaker git status' for that): a read-only summary
// of everything a developer would otherwise have to piece together from
// several other commands (cmaker.yaml, cmaker list, cmaker search --remote/
// packs, cmaker suggest list, git status, du -sh) in one formatted view.
// Fully local and fast by default; --detailed adds a few slower/optional
// signals (coverage report presence, an LLM-assessed documentation
// verdict) clearly separated from the fast path.
var statusCmd = &cobra.Command{
	Use:   "status [flags]",
	Short: "Show a project dashboard: config, git, dependencies, packs, suggestions, disk usage",
	Long: "A read-only project dashboard - everything a developer would otherwise piece together\n" +
		"from several other commands in one formatted view: the project's config (language,\n" +
		"target, toolchain), a git summary (branch, ahead/behind, dirty file counts, last\n" +
		"commit), dependencies and installed packs, how many suggestions are queued (see\n" +
		"'cmaker suggest'), and a disk-usage breakdown (source, build/, executable, .git).\n" +
		"\n" +
		"This is deliberately not the same thing as 'cmaker git status' - that still gives you\n" +
		"plain git's own output; this gives you the whole project's state at a glance.\n" +
		"\n" +
		"--detailed adds a few slower or optional signals: whether a coverage report exists (see\n" +
		"'cmaker coverage'), how much code sits inside a cmaker-generated block (see 'cmaker\n" +
		"generate accessors'), and - if ANTHROPIC_API_KEY is configured - an LLM-assessed verdict\n" +
		"on documentation quality. Every detailed check degrades gracefully when its prerequisite\n" +
		"isn't met (no report yet, no API key) rather than failing the whole command.",
	Example: `  cmaker status
  cmaker status --detailed`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		detailed, _ := cmd.Flags().GetBool("detailed")
		model, _ := cmd.Flags().GetString("model")
		return runStatus(detailed, model)
	},
}

func init() {
	statusCmd.Flags().Bool("detailed", false, "also show coverage, generated-code, and documentation-quality signals")
	statusCmd.Flags().String("model", "", "override the Anthropic model used for --detailed's documentation-quality verdict (default: "+llm.DefaultModel+")")
}

func runStatus(detailed bool, model string) error {
	cfg := loadConfigOrExit()

	printStatusHeader(cfg)
	printGitStatusSection()
	printDependenciesStatusSection(cfg)
	printPacksStatusSection()
	printSuggestionsStatusSection()
	printDiskUsageStatusSection(cfg)

	if detailed {
		printCoverageStatusSection(cfg)
		printGeneratedCodeStatusSection()
		printDocumentationStatusSection(model)
	}
	fmt.Println()
	return nil
}

// --- section header ---

func statusSection(title string) {
	fmt.Println()
	fmt.Println(colorize(ansiBold, title))
	fmt.Println(colorize(ansiBold, strings.Repeat("─", len(title))))
}

func statusField(label string, a ...any) {
	fmt.Printf("  %-16s %s\n", colorize(ansiCyan, label+":"), fmt.Sprint(a...))
}

// --- project header ---

func printStatusHeader(cfg config.Config) {
	renderBanner(cfg.ProjectName, ansiCyan, ansiBlue)
	fmt.Println()

	lang := config.LanguageOrDefault(cfg.Language)
	targetType := config.TargetTypeOrDefault(cfg.TargetType)
	version := ""
	switch lang {
	case "c":
		if cfg.CVersion > 0 {
			version = fmt.Sprintf(" (C%d)", cfg.CVersion)
		}
	default:
		if cfg.CppVersion > 0 {
			version = fmt.Sprintf(" (C++%d)", cfg.CppVersion)
		}
	}
	statusField("Language", lang, version)
	statusField("Target", targetType)

	compiler := cfg.Compiler
	if compiler == "" {
		compiler = "auto-detected"
	}
	statusField("Compiler", compiler)

	if cfg.Runner != "" {
		statusField("Runner", cfg.Runner)
	}

	var extras []string
	if cfg.Rust != nil && cfg.Rust.Enabled {
		extras = append(extras, "Rust")
	}
	if cfg.Zig != nil && cfg.Zig.Enabled {
		extras = append(extras, "Zig")
	}
	if cfg.Testing != nil && cfg.Testing.Enabled {
		extras = append(extras, "tests")
	}
	if cfg.Coverage {
		extras = append(extras, "coverage")
	}
	if len(cfg.Sanitizers) > 0 {
		extras = append(extras, "sanitizers: "+strings.Join(cfg.Sanitizers, ","))
	}
	if cfg.WarningsAsErrors {
		extras = append(extras, "warnings-as-errors")
	}
	if cfg.Workspace != nil {
		extras = append(extras, fmt.Sprintf("workspace (%d members)", len(cfg.Workspace.Members)))
	}
	if len(extras) > 0 {
		statusField("Features", strings.Join(extras, ", "))
	}
}

// --- git ---

func printGitStatusSection() {
	statusSection("Git")
	if !isInsideGitRepo(".") {
		fmt.Println(colorize(ansiYellow, "  No git repository here (run 'git init' or scaffold with 'cmaker new'/'init' without --nogit)."))
		return
	}

	branch, err := currentBranch(".")
	if err != nil {
		branch = "(unknown)"
	}
	branchLine := branch
	if ahead, behind, ok := gitAheadBehind(); ok {
		switch {
		case ahead > 0 && behind > 0:
			branchLine += colorize(ansiYellow, fmt.Sprintf(" (%d ahead, %d behind)", ahead, behind))
		case ahead > 0:
			branchLine += colorize(ansiGreen, fmt.Sprintf(" (%d ahead)", ahead))
		case behind > 0:
			branchLine += colorize(ansiYellow, fmt.Sprintf(" (%d behind)", behind))
		}
	}
	statusField("Branch", branchLine)

	staged, modified, untracked := gitStatusCounts()
	if staged == 0 && modified == 0 && untracked == 0 {
		statusField("Working tree", colorize(ansiGreen, "clean"))
	} else {
		statusField("Working tree", colorize(ansiYellow, fmt.Sprintf("%d staged, %d modified, %d untracked", staged, modified, untracked)))
	}

	if subject, when, ok := gitLastCommit(); ok {
		statusField("Last commit", fmt.Sprintf("%s (%s)", subject, when))
	}
}

// gitAheadBehind reports how many commits HEAD is ahead/behind its
// upstream - ok is false if there's no upstream configured (a common,
// unremarkable case, not an error) or the git call otherwise fails.
func gitAheadBehind() (ahead, behind int, ok bool) {
	out, err := exec.Command("git", "rev-list", "--left-right", "--count", "HEAD...@{upstream}").Output()
	if err != nil {
		return 0, 0, false
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) != 2 {
		return 0, 0, false
	}
	ahead, aErr := strconv.Atoi(fields[0])
	behind, bErr := strconv.Atoi(fields[1])
	if aErr != nil || bErr != nil {
		return 0, 0, false
	}
	return ahead, behind, true
}

// gitStatusCounts tallies `git status --porcelain` entries into staged
// (index differs from HEAD), modified (working tree differs from index),
// and untracked - a file can count as both staged and modified (staged
// one change, then dirtied again), matching git's own porcelain semantics.
func gitStatusCounts() (staged, modified, untracked int) {
	out, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return 0, 0, 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 2 {
			continue
		}
		x, y := line[0], line[1]
		if x == '?' && y == '?' {
			untracked++
			continue
		}
		if x != ' ' {
			staged++
		}
		if y != ' ' {
			modified++
		}
	}
	return staged, modified, untracked
}

// gitLastCommit returns HEAD's subject line and a human relative age
// ("3 hours ago") - ok is false on any failure, notably an empty
// repository with no commits yet.
func gitLastCommit() (subject, when string, ok bool) {
	out, err := exec.Command("git", "log", "-1", "--format=%s\x1f%cr").Output()
	if err != nil {
		return "", "", false
	}
	fields := strings.SplitN(strings.TrimSpace(string(out)), "\x1f", 2)
	if len(fields) != 2 {
		return "", "", false
	}
	return fields[0], fields[1], true
}

// --- dependencies ---

func printDependenciesStatusSection(cfg config.Config) {
	statusSection(fmt.Sprintf("Dependencies (%d)", len(cfg.Dependencies)))
	if len(cfg.Dependencies) == 0 {
		fmt.Println(colorize(ansiYellow, "  None. See 'cmaker search <term>' / 'cmaker install <name>'."))
		return
	}
	for _, dep := range cfg.Dependencies {
		ref := dep.Tag
		if ref == "" {
			ref = "(unpinned)"
		}
		fmt.Printf("  %s %s\n", colorize(ansiGreen, "•"), fmt.Sprintf("%s@%s", dep.Name, ref))
	}
}

// --- packs ---

func printPacksStatusSection() {
	lf, err := registry.LoadLockfile(".")
	if err != nil || len(lf.Packs) == 0 {
		statusSection("Packs (0)")
		fmt.Println(colorize(ansiYellow, "  None installed. See 'cmaker search <term> --remote' / 'cmaker install <name>'."))
		return
	}

	names := make([]string, 0, len(lf.Packs))
	for name := range lf.Packs {
		names = append(names, name)
	}
	sort.Strings(names)

	statusSection(fmt.Sprintf("Packs (%d)", len(names)))
	for _, name := range names {
		p := lf.Packs[name]
		fmt.Printf("  %s %s@%s\n", colorize(ansiGreen, "•"), name, p.Version)
	}
}

// --- suggestions ---

func printSuggestionsStatusSection() {
	statusSection("Suggestions")
	data, err := os.ReadFile(defaultSuggestionsFile)
	if err != nil {
		fmt.Println(colorize(ansiYellow, "  None queued. See 'cmaker suggest --export' / 'cmaker suggest add'."))
		return
	}
	items := suggest.ParseChecklist(data)
	total, done := 0, 0
	for _, it := range items {
		if len(it.Children) > 0 {
			for _, c := range it.Children {
				total++
				if c.Checked {
					done++
				}
			}
			continue
		}
		total++
		if it.Checked {
			done++
		}
	}
	if total == 0 {
		fmt.Println(colorize(ansiYellow, "  None queued. See 'cmaker suggest --export' / 'cmaker suggest add'."))
		return
	}
	remaining := total - done
	statusField("Queued", fmt.Sprintf("%d done, %d remaining (of %d) — %s", done, remaining, total, defaultSuggestionsFile))
	if remaining > 0 {
		if next := suggest.FindFirstUnchecked(items); next != nil {
			statusField("Next up", next.Text)
		}
	}
}

// --- disk usage ---

func printDiskUsageStatusSection(cfg config.Config) {
	statusSection("Disk usage")

	sourceSize := dirSize("src") + dirSize("include")
	statusField("Source", humanSize(sourceSize))

	if buildSize, ok := dirSizeOK("build"); ok {
		statusField("Build", humanSize(buildSize))
	} else {
		statusField("Build", colorize(ansiYellow, "not built yet"))
	}

	if exeSize, exeName, ok := executableSize(cfg); ok {
		statusField("Executable", fmt.Sprintf("%s (%s)", humanSize(exeSize), exeName))
	}

	if gitSize, ok := dirSizeOK(".git"); ok {
		statusField(".git", humanSize(gitSize))
	}

	statusField("Total", humanSize(dirSize(".")))
}

// executableSize resolves and stats the project's main runnable binary
// (see runnableBinaryName) - ok is false for a library with no
// examples/demo.cpp, or before the first build, neither of which is an
// error worth surfacing here (the "Build" line above already covers "not
// built yet").
func executableSize(cfg config.Config) (size int64, name string, ok bool) {
	targetType := config.TargetTypeOrDefault(cfg.TargetType)
	exeName, err := runnableBinaryName(cfg, targetType)
	if err != nil {
		return 0, "", false
	}
	info, err := os.Stat(filepath.Join("build", exeName))
	if err != nil {
		return 0, "", false
	}
	return info.Size(), exeName, true
}

// dirSize sums regular file sizes under path, recursively - 0 if path
// doesn't exist. Symlinks are stat'd (not followed into) via Lstat's own
// size, avoiding both double-counting and a cycle through a symlink loop.
func dirSize(path string) int64 {
	size, _ := dirSizeOK(path)
	return size
}

func dirSizeOK(path string) (int64, bool) {
	if _, err := os.Lstat(path); err != nil {
		return 0, false
	}
	var total int64
	filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // best-effort: skip unreadable entries rather than aborting the whole walk
		}
		if d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		return nil
	})
	return total, true
}

// humanSize formats n bytes as a short, human-readable string (KB/MB/GB) -
// no external dependency for something this small.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// --- detailed: coverage ---

func printCoverageStatusSection(cfg config.Config) {
	statusSection("Code coverage")
	if !cfg.Coverage {
		fmt.Println(colorize(ansiYellow, "  Not enabled. Add 'coverage: true' to cmaker.yaml, then run 'cmaker coverage'."))
		return
	}
	reportPath := filepath.Join("build", "coverage", "index.html")
	info, err := os.Stat(reportPath)
	if err != nil {
		fmt.Println(colorize(ansiYellow, "  Enabled, but no report yet. Run 'cmaker coverage'."))
		return
	}
	statusField("Report", fmt.Sprintf("%s (generated %s)", reportPath, humanRelativeTime(info.ModTime())))
	if pct, ok := parseCoveragePercentFromHTML(reportPath); ok {
		statusField("Lines covered", fmt.Sprintf("%.1f%%", pct))
	}
}

var coveragePercentRe = regexp.MustCompile(`(?is)lines[^%]{0,40}?(\d{1,3}(?:\.\d+)?)\s*%`)

// parseCoveragePercentFromHTML makes a best-effort attempt at extracting
// gcovr's own overall line-coverage percentage out of the HTML report it
// already wrote - a heuristic regex against gcovr's default theme, not a
// real HTML parse (a new dependency for one number), so ok is false
// (rather than a wrong number) whenever the pattern isn't found, e.g. a
// custom gcovr theme/version cmaker hasn't been checked against.
func parseCoveragePercentFromHTML(path string) (float64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	m := coveragePercentRe.FindSubmatch(data)
	if m == nil {
		return 0, false
	}
	pct, err := strconv.ParseFloat(string(m[1]), 64)
	if err != nil {
		return 0, false
	}
	return pct, true
}

// --- detailed: generated code ---

func printGeneratedCodeStatusSection() {
	statusSection("Generated code")
	files, err := explain.WalkSourceFiles(".")
	if err != nil {
		fmt.Println(colorize(ansiYellow, "  Couldn't scan source files."))
		return
	}
	var totalBlocks, totalLines, totalFiles int
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		blocks, lines := codegen.CountGeneratedBlocksInContent(string(data))
		if blocks > 0 {
			totalBlocks += blocks
			totalLines += lines
			totalFiles++
		}
	}
	if totalBlocks == 0 {
		fmt.Println(colorize(ansiYellow, "  None found (see 'cmaker generate accessors')."))
		return
	}
	statusField("cmaker-generated", fmt.Sprintf("%d block(s), ~%d line(s), across %d file(s)", totalBlocks, totalLines, totalFiles))
	fmt.Println(colorize(ansiYellow, "  Note: this only counts 'generate accessors' output - codegen/fix/migrate/improve don't leave a distinguishing marker."))
}

// --- detailed: documentation ---

func printDocumentationStatusSection(model string) {
	statusSection("Documentation")

	files, err := explain.WalkSourceFiles(".")
	if err != nil {
		fmt.Println(colorize(ansiYellow, "  Couldn't scan source files."))
		return
	}
	var totalBlocks, driftIssues int
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		content := string(data)
		totalBlocks += docsgen.CountDocCommentBlocks(content)
		driftIssues += len(docsgen.FindDriftIssues(f, content))
	}
	statusField("Doc comments", fmt.Sprintf("%d block(s) found", totalBlocks))
	if driftIssues > 0 {
		statusField("Drift", colorize(ansiYellow, fmt.Sprintf("%d issue(s) - see 'cmaker doctor --docs'", driftIssues)))
	} else {
		statusField("Drift", colorize(ansiGreen, "none found"))
	}

	if model == "" {
		model = llm.DefaultModel
	}
	client, err := llm.NewClientFromEnv(model)
	if err != nil {
		fmt.Println(colorize(ansiYellow, "  Set ANTHROPIC_API_KEY for an AI-assessed quality verdict (see 'cmaker doctor --ai')."))
		return
	}
	prompt, err := docsgen.BuildQualityPrompt(".")
	if err != nil {
		return
	}
	promptHash := docsgen.HashPrompt(prompt)

	if cached, ok := docsgen.LoadQuality(".", promptHash, client.Model); ok {
		statusField("AI verdict", fmt.Sprintf("%d/10 — %s %s", cached.Score, cached.Summary, colorize(ansiYellow, "(cached - source unchanged since last check)")))
		return
	}

	infof("Asking %s to assess documentation quality...", client.Model)
	quality, err := docsgen.AssessQuality(context.Background(), client, prompt)
	if err != nil {
		warnf("documentation quality check failed: %v", err)
		return
	}
	if err := docsgen.SaveQuality(".", promptHash, client.Model, quality); err != nil {
		debugf("status: failed to cache documentation quality verdict: %v", err)
	}
	statusField("AI verdict", fmt.Sprintf("%d/10 — %s", quality.Score, quality.Summary))
}

// humanRelativeTime formats t as a short "N units ago" string - a small
// local equivalent of git's own %cr used for gitLastCommit, since a plain
// file mtime has no git command to ask for it.
func humanRelativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}
