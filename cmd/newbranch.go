package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"cmaker/internal/commitmsg"
	"cmaker/internal/llm"
)

var newBranchCmd = &cobra.Command{
	Use:   "newbranch",
	Short: "Create and check out a new branch, optionally auto-naming/committing/pushing it",
	Long: "Creates a new branch (git checkout -b) and switches to it.\n" +
		"\n" +
		"--from picks the branch to create from (default: whichever branch you're currently on).\n" +
		"--name picks the new branch's name; without it, an LLM (Anthropic; requires\n" +
		"ANTHROPIC_API_KEY) is asked for a short 'f-<slug>' name based on your current\n" +
		"changes - uncommitted working-tree changes first, then (if there are none) whatever's\n" +
		"been committed on the current branch but not yet on --from. If there's nothing to\n" +
		"infer a name from either way, you'll need to pass --name explicitly.\n" +
		"\n" +
		"--autocommit stages and commits everything on the new branch (like 'cmaker commit\n" +
		"--auto'). --autopush does that and also pushes, setting the upstream on this first\n" +
		"push ('cmaker push --auto').",
	Example: `  cmaker newbranch --name=my-feature
  cmaker newbranch --from=main
  cmaker newbranch --autopush`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		from, _ := cmd.Flags().GetString("from")
		autopush, _ := cmd.Flags().GetBool("autopush")
		autocommit, _ := cmd.Flags().GetBool("autocommit")
		model, _ := cmd.Flags().GetString("model")
		return runNewBranch(name, from, autopush, autocommit, model)
	},
}

func init() {
	newBranchCmd.Flags().String("name", "", "new branch name (default: LLM-generated 'f-<slug>' from your current changes)")
	newBranchCmd.Flags().String("from", "", "branch to create from (default: the current branch)")
	newBranchCmd.Flags().Bool("autopush", false, "also auto-commit and push the new branch upstream")
	newBranchCmd.Flags().Bool("autocommit", false, "also auto-commit (no push) on the new branch")
	newBranchCmd.Flags().String("model", "", "override the Anthropic model used for name/message generation (default: "+llm.DefaultModel+")")
}

func runNewBranch(name, from string, autopush, autocommit bool, model string) error {
	if !isInsideGitRepo(".") {
		return fmt.Errorf("not a git repository - run 'git init' first (or scaffold with 'cmaker new'/'init' without --nogit)")
	}

	if from == "" {
		branch, err := currentBranch(".")
		if err != nil {
			return fmt.Errorf("failed to determine the current branch: %w", err)
		}
		from = branch
	}

	if name == "" {
		slug, err := inferBranchSlug(from, model)
		if err != nil {
			return err
		}
		name = "f-" + slug
	}

	if err := runGit("checkout", "-b", name, from); err != nil {
		return fmt.Errorf("failed to create branch %q from %q: %w", name, from, err)
	}
	okf("Created and switched to branch %q (from %q).", name, from)

	if autopush {
		committed, err := autoCommitStaged(model)
		if err != nil {
			return err
		}
		if !committed {
			infof("Nothing to commit.")
		}
		return runGitPassthrough([]string{"push", "-u", "origin", name})
	}
	if autocommit {
		if _, err := autoCommitStaged(model); err != nil {
			return err
		}
	}
	return nil
}

// inferBranchSlug asks an LLM for a branch-name slug (see
// commitmsg.GenerateBranchSlug), preferring uncommitted working-tree
// changes and falling back to committed-but-not-on-from changes when
// there are none - the two "there's a real change here to name" sources
// available before the new branch itself has anything on it.
func inferBranchSlug(from, model string) (string, error) {
	diff, err := combinedWorkingDiff()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(diff) == "" {
		diff, err = diffAgainst(from)
		if err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(diff) == "" {
		return "", fmt.Errorf("no uncommitted or unmerged changes to infer a branch name from - pass --name explicitly")
	}

	client, err := llm.NewClientFromEnv(model)
	if err != nil {
		return "", err
	}
	infof("Asking %s to suggest a branch name...", client.Model)
	return commitmsg.GenerateBranchSlug(context.Background(), client, diff)
}

// combinedWorkingDiff returns unstaged plus staged working-tree changes.
func combinedWorkingDiff() (string, error) {
	unstaged, err := exec.Command("git", "diff").Output()
	if err != nil {
		return "", fmt.Errorf("git diff failed: %w", err)
	}
	staged, err := exec.Command("git", "diff", "--staged").Output()
	if err != nil {
		return "", fmt.Errorf("git diff --staged failed: %w", err)
	}
	return string(unstaged) + string(staged), nil
}

// diffAgainst returns the diff between ref and HEAD (commits on the
// current branch not yet on ref).
func diffAgainst(ref string) (string, error) {
	out, err := exec.Command("git", "diff", ref+"..HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git diff %s..HEAD failed: %w", ref, err)
	}
	return string(out), nil
}
