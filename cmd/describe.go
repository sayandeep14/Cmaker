package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"cmaker/internal/describe"
	"cmaker/internal/improvise"
	"cmaker/internal/llm"
)

// describeOptions bundles runDescribeAndScaffold's growing parameter list
// into a struct - mirrors scaffoldFlagSet's own reasoning (see its doc
// comment): once a positional argument list grows past the point it stays
// readable, a struct is clearer than another multi-value signature change.
type describeOptions struct {
	Root, Name, Description, Compiler, Runner string
	Improvise                                 bool
	Model                                     string // overrides both the plan-selection and (if Improvise) the improvise model when non-empty
	NoGit                                     bool
}

// runDescribeAndScaffold turns a natural-language description into a Plan
// (internal/describe.Describe), prints it for review, scaffolds the
// project from it (reusing scaffoldProject exactly as a --template/--lang/
// --with-rust/--with-zig/--target-type invocation would), and installs any
// packages the plan called for.
//
// Unlike `cmaker heal`, the plan-selection/scaffolding steps don't gate
// behind a separate --apply step: scaffolding into a brand-new (typically
// empty) directory is inherently low-risk and trivially reversible (delete
// it and try again), unlike patching a user's existing source - so
// printing the plan and then acting on it in one command matches how every
// other `cmaker new` invocation already behaves. §28's --improvise is
// different: it can go on to have an LLM modify the scaffolded code itself
// (see improviseScaffold below), which - unlike menu-selection - genuinely
// is asking the LLM to author real logic, so that step *does* gate behind
// a shown diff and a confirmation, mirroring `cmaker heal`'s own posture
// instead.
func runDescribeAndScaffold(opts describeOptions) error {
	planClient, err := llm.NewClientFromEnv(opts.Model)
	if err != nil {
		return err
	}

	finalDescription := opts.Description
	var improviseClient *llm.Client
	if opts.Improvise {
		improviseModel := opts.Model
		if improviseModel == "" {
			improviseModel = llm.DefaultImproviseModel
		}
		improviseClient, err = llm.NewClientFromEnv(improviseModel)
		if err != nil {
			return err
		}

		infof("Asking %s whether more information is needed...", improviseClient.Model)
		proceed, questions, err := improvise.Clarify(context.Background(), improviseClient, opts.Description)
		if err != nil {
			return err
		}
		if !proceed {
			infof("A few quick questions first:")
			answers := askClarifyingQuestions(questions)
			finalDescription = opts.Description + "\n\nAdditional details from the user:\n" + answers
		}
	}

	infof("Asking %s to plan a project for: %q", planClient.Model, finalDescription)
	plan, err := describe.Describe(context.Background(), planClient, finalDescription)
	if err != nil {
		return err
	}

	infof("Plan: template=%s language=%s with_rust=%v with_zig=%v target_type=%s", plan.Template, plan.Language, plan.WithRust, plan.WithZig, plan.TargetType)
	if len(plan.Packages) > 0 {
		infof("Packages: %s", strings.Join(plan.Packages, ", "))
	}
	if plan.Reasoning != "" {
		infof("Reasoning: %s", plan.Reasoning)
	}

	if err := scaffoldProject(opts.Root, opts.Name, plan.Template, plan.Language, opts.Compiler, plan.WithRust, plan.WithZig, opts.Runner, plan.TargetType); err != nil {
		return err
	}

	// Every remaining step (package install, scaffold refinement) operates
	// on the freshly scaffolded project itself, so enter it once, here,
	// unconditionally - this process exits right after, so there's no
	// caller's cwd to preserve. `cmaker init` already scaffolds into "."
	// and needs no chdir.
	if opts.Root != "." && opts.Root != "" {
		if err := os.Chdir(opts.Root); err != nil {
			return fmt.Errorf("failed to enter %s to finish scaffolding: %w", opts.Root, err)
		}
	}

	for _, pkg := range plan.Packages {
		infof("Installing planned package %q...", pkg)
		if err := runInstall(pkg, "", "", nil, nil, false); err != nil {
			warnf("failed to install %q: %v (continuing)", pkg, err)
		}
	}

	if opts.Improvise {
		if err := improviseScaffold(improviseClient, finalDescription, plan.Reasoning); err != nil {
			return err
		}
	}

	// "." here, not opts.Root - the chdir above (when opts.Root wasn't
	// already ".") already moved into the scaffolded project, so this
	// picks up the *final* state (including any --improvise refinement)
	// for the initial commit, not a stale pre-improvise snapshot.
	maybeInitGit(".", opts.NoGit)
	return nil
}

// improviseScaffold asks an LLM to refine the just-scaffolded project's own
// source files (relative to the current directory - see
// runDescribeAndScaffold's chdir above) to better match description,
// reusing internal/improvise.ModifyScaffold (itself built on internal/
// heal's diff-computation machinery, per this package's own doc). Shows
// the proposed diff and requires explicit confirmation before writing
// anything - real LLM-authored code, not just a menu selection, so this
// deliberately doesn't act immediately the way plan-selection/scaffolding
// above does.
//
// Applies by writing each changed file's full proposed content directly
// (not via `git apply`, unlike `cmaker heal`) - a freshly scaffolded
// project isn't a git repository yet, and ModifyScaffold already has the
// exact target content for each file, so there's no need to round-trip
// through a diff to apply it, only to display it.
func improviseScaffold(client *llm.Client, description, reasoning string) error {
	files, err := improvise.CollectScaffoldFiles(".")
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}

	infof("Asking %s to refine the scaffold...", client.Model)
	diff, changed, err := improvise.ModifyScaffold(context.Background(), client, ".", description, reasoning, files)
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		infof("No scaffold changes proposed.")
		return nil
	}

	printDiff(diff)
	if !confirmYesNo("Apply these changes?") {
		infof("Not applied.")
		return nil
	}

	for rel, content := range changed {
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if err := os.WriteFile(rel, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", rel, err)
		}
	}
	okf("Applied scaffold changes to %d file(s).", len(changed))
	return nil
}

// askClarifyingQuestions renders questions interactively on the terminal
// (a prompt per question on stdout, an answer read from stdin) and returns
// a human-readable summary (one line per question) to append to the
// original description before re-planning.
//
// No TUI integration in this v1 - ROADMAP.md §28 left open whether
// --improvise invoked from the TUI's New Project flow should reuse its own
// form machinery (see §10's compiler picker) instead of this plain CLI
// prompt; not attempted here, since --improvise itself is CLI-only for now
// (cmd/new.go's scaffoldFlagSet, not yet threaded into internal/tui).
func askClarifyingQuestions(questions []improvise.Question) string {
	var answers []string
	for i, q := range questions {
		fmt.Printf("\n%d. %s\n", i+1, q.Prompt)
		switch q.Type {
		case improvise.QuestionSingleSelect:
			for j, opt := range q.Options {
				fmt.Printf("   %d) %s\n", j+1, opt)
			}
			answers = append(answers, fmt.Sprintf("%s: %s", q.Prompt, readSingleSelectAnswer(stdinReader, q.Options)))
		case improvise.QuestionMultiSelect:
			for j, opt := range q.Options {
				fmt.Printf("   %d) %s\n", j+1, opt)
			}
			answers = append(answers, fmt.Sprintf("%s: %s", q.Prompt, readMultiSelectAnswer(stdinReader, q.Options)))
		default: // free_text
			fmt.Print("   > ")
			line, _ := stdinReader.ReadString('\n')
			answers = append(answers, fmt.Sprintf("%s: %s", q.Prompt, strings.TrimSpace(line)))
		}
	}
	return strings.Join(answers, "\n")
}

// readSingleSelectAnswer reads one line from reader and, if it parses as a
// 1-based index into options, resolves it to that option's text; otherwise
// (free-typed text instead of a number, or an out-of-range index) the raw
// input is used as-is - a typo here shouldn't hard-fail an interactive
// prompt, just pass through what the user actually typed.
func readSingleSelectAnswer(reader *bufio.Reader, options []string) string {
	fmt.Print("   > ")
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if idx, err := strconv.Atoi(line); err == nil && idx >= 1 && idx <= len(options) {
		return options[idx-1]
	}
	return line
}

// readMultiSelectAnswer reads one line of comma-separated 1-based indices
// (e.g. "1,3") and resolves each to its option text; any token that isn't a
// valid index is dropped silently unless nothing at all parsed, in which
// case the raw input is used as-is (same free-text fallback reasoning as
// readSingleSelectAnswer).
func readMultiSelectAnswer(reader *bufio.Reader, options []string) string {
	fmt.Print("   > (comma-separated numbers) ")
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)

	var picked []string
	for _, part := range strings.Split(line, ",") {
		part = strings.TrimSpace(part)
		if idx, err := strconv.Atoi(part); err == nil && idx >= 1 && idx <= len(options) {
			picked = append(picked, options[idx-1])
		}
	}
	if len(picked) == 0 {
		return line
	}
	return strings.Join(picked, ", ")
}
