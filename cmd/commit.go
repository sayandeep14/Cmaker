package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"cmaker/internal/commitmsg"
	"cmaker/internal/llm"
)

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Stage all changes and commit, generating the message via LLM unless -m is given",
	Long: "Stages every change in the working tree (`git add -A`) and commits it. Without -m, asks\n" +
		"an LLM (Anthropic; requires ANTHROPIC_API_KEY) to write the commit message from the\n" +
		"staged diff - printed before committing either way, with a y/N confirmation unless\n" +
		"--auto or --preview is given.\n" +
		"\n" +
		"--auto skips the confirmation and commits immediately - the same shared step 'cmaker\n" +
		"push --auto' and 'cmaker newbranch --autocommit'/'--autopush' use internally.\n" +
		"\n" +
		"--preview opens the message (generated or given via -m) in an inline editor right in\n" +
		"the terminal before committing: edit freely, Ctrl+S to commit, Esc/Ctrl+C to cancel -\n" +
		"editing it there is itself the confirmation step, so --auto has no effect alongside it.",
	Example: `  cmaker commit
  cmaker commit -m "Fix off-by-one in LinkedList::sum"
  cmaker commit --preview
  cmaker commit --auto`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		message, _ := cmd.Flags().GetString("message")
		preview, _ := cmd.Flags().GetBool("preview")
		auto, _ := cmd.Flags().GetBool("auto")
		model, _ := cmd.Flags().GetString("model")
		return runCommit(message, preview, auto, model)
	},
}

func init() {
	commitCmd.Flags().StringP("message", "m", "", "commit message (skips LLM generation)")
	commitCmd.Flags().Bool("preview", false, "edit the message inline in the terminal before committing")
	commitCmd.Flags().Bool("auto", false, "commit immediately, skipping the confirmation prompt")
	commitCmd.Flags().String("model", "", "override the Anthropic model used to generate the message (default: "+llm.DefaultModel+")")
}

func runCommit(message string, preview, auto bool, model string) error {
	if !isInsideGitRepo(".") {
		return fmt.Errorf("not a git repository - run 'git init' first (or scaffold with 'cmaker new'/'init' without --nogit)")
	}

	diff, err := stagedDiff()
	if err != nil {
		return err
	}
	if strings.TrimSpace(diff) == "" {
		infof("Nothing to commit - working tree is clean.")
		return nil
	}

	if message == "" {
		message, err = generateCommitMessage(model, diff)
		if err != nil {
			return err
		}
	}

	if preview {
		edited, ok, err := previewAndEditMessage(message)
		if err != nil {
			return err
		}
		if !ok {
			infof("Commit cancelled.")
			return nil
		}
		message = edited
		if strings.TrimSpace(message) == "" {
			return fmt.Errorf("commit message is empty - nothing to commit")
		}
	} else {
		infof("Commit message:\n%s", message)
		if !auto && !confirmYesNo("Commit with this message?") {
			infof("Commit cancelled.")
			return nil
		}
	}

	if err := gitCommitMessage(message); err != nil {
		return err
	}
	okf("Committed.")
	return nil
}

// stagedDiff stages every change (`git add -A`) and returns the resulting
// staged diff - the shared first step of every commit path (interactive
// or auto).
func stagedDiff() (string, error) {
	if err := runGit("add", "-A"); err != nil {
		return "", fmt.Errorf("git add failed: %w", err)
	}
	out, err := exec.Command("git", "diff", "--staged").Output()
	if err != nil {
		return "", fmt.Errorf("git diff failed: %w", err)
	}
	return string(out), nil
}

// generateCommitMessage asks an LLM to write a commit message from diff.
func generateCommitMessage(model, diff string) (string, error) {
	client, err := llm.NewClientFromEnv(model)
	if err != nil {
		return "", err
	}
	infof("Asking %s to write a commit message...", client.Model)
	return commitmsg.Generate(context.Background(), client, diff)
}

// gitCommitMessage runs `git commit -m message` against whatever's
// currently staged.
func gitCommitMessage(message string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("git", "commit", "-q", "-m", message)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git commit failed: %w\n%s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// autoCommitStaged stages all changes, generates a commit message via
// LLM, and commits with no confirmation prompt - the shared "auto commit"
// step reused by `commit --auto`'s sibling commands, `push --auto` and
// `newbranch --autocommit`/`--autopush`, neither of which have their own
// -m/--preview surface. Returns committed=false (no error) if there was
// nothing staged to commit.
func autoCommitStaged(model string) (committed bool, err error) {
	diff, err := stagedDiff()
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(diff) == "" {
		return false, nil
	}
	message, err := generateCommitMessage(model, diff)
	if err != nil {
		return false, err
	}
	infof("Commit message:\n%s", message)
	if err := gitCommitMessage(message); err != nil {
		return false, err
	}
	okf("Committed.")
	return true, nil
}

// runGit runs a git subcommand rooted at the current directory, discarding
// its stdout but surfacing stderr on failure - a small shared helper for
// the handful of plain git invocations cmaker's own commands make
// directly (as opposed to the general 'cmaker git <args>' passthrough).
func runGit(args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return err
	}
	return nil
}
