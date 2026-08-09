package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// pushCmd is a real registered command (not the generic knownGitVerbs
// bare-verb passthrough every other git verb uses) purely so it can offer
// --auto: without it, this behaves exactly like the passthrough would -
// `cmaker push <any git push args>` forwards straight to `git push`,
// arbitrary flags and all, via DisableFlagParsing.
var pushCmd = &cobra.Command{
	Use:   "push -- [git push args...] [--auto]",
	Short: "Push to the remote - or, with --auto, stage+commit+push in one step",
	Long: "Without --auto, forwards straight to 'git push' with whatever args you give it - the\n" +
		"same as every other bare git-verb passthrough (see 'cmaker git --help').\n" +
		"\n" +
		"--auto additionally stages every change, generates a commit message via LLM (Anthropic;\n" +
		"requires ANTHROPIC_API_KEY), and commits (no confirmation - same as 'cmaker commit\n" +
		"--auto') before pushing - 'cmaker push --auto' on a brand-new branch also passes\n" +
		"-u origin <branch>, so the upstream gets set up on the first push.",
	Example: `  cmaker push
  cmaker push -u origin main
  cmaker push --auto`,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPush(cmd, args, "")
	},
}

// runPush intercepts --auto (and --help/-h) out of args itself
// (DisableFlagParsing means cobra never parsed args at all) rather than
// declaring them as normal flags - the same reasoning
// gitCmd/maybeDispatchGitOverride already established: cobra's flag
// parser doesn't know git push's own flags, so nothing here can be a
// normal cobra.Command flag without breaking passthrough for arbitrary
// git push invocations. --help/-h specifically needs its own check
// (rather than just letting it forward to `git push --help`, which is
// what DisableFlagParsing would otherwise do): unlike gitCmd, which is a
// pure passthrough where showing git's own help is fine, pushCmd has
// real cmaker-specific behavior (--auto) that only cmaker's own help
// text documents.
func runPush(cmd *cobra.Command, args []string, model string) error {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			return cmd.Help()
		}
	}

	autoIdx := -1
	for i, a := range args {
		if a == "--auto" {
			autoIdx = i
			break
		}
	}
	if autoIdx == -1 {
		return runGitPassthrough(append([]string{"push"}, args...))
	}

	rest := make([]string, 0, len(args)-1)
	rest = append(rest, args[:autoIdx]...)
	rest = append(rest, args[autoIdx+1:]...)

	if !isInsideGitRepo(".") {
		return fmt.Errorf("not a git repository - run 'git init' first (or scaffold with 'cmaker new'/'init' without --nogit)")
	}
	committed, err := autoCommitStaged(model)
	if err != nil {
		return err
	}
	if !committed {
		infof("Nothing to commit - pushing existing history.")
	}

	if len(rest) == 0 {
		rest = defaultPushArgsForCurrentBranch()
	}
	return runGitPassthrough(append([]string{"push"}, rest...))
}

// defaultPushArgsForCurrentBranch adds '-u origin <branch>' when no
// explicit push args were given - a bare 'git push' fails on a branch
// with no upstream yet, which is the common case right after
// 'cmaker push --auto' committed on a brand-new branch.
func defaultPushArgsForCurrentBranch() []string {
	branch, err := currentBranch(".")
	if err != nil || branch == "" {
		return nil
	}
	return []string{"-u", "origin", branch}
}
