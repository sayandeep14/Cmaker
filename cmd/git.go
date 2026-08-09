package cmd

import (
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

// gitCmd is the universal, explicit git escape hatch: `cmaker git <args>`
// forwards everything after "git" straight to the real `git` binary,
// unconditionally - including the three verbs (init/add/clean) that
// already have their own cmaker meaning when invoked bare, since there's
// no ambiguity once they're under this explicit namespace. DisableFlagParsing
// is what makes this work for arbitrary git flags (e.g. `cmaker git commit
// -am "..."`): cobra never tries to interpret them as cmaker's own flags.
var gitCmd = &cobra.Command{
	Use:   "git -- <git args...>",
	Short: "Run any git command directly - the universal escape hatch, no name collisions",
	Long: "Forwards everything after 'git' straight to the real 'git' binary. Use this for any\n" +
		"git subcommand cmaker doesn't already forward bare (see the top-level command list),\n" +
		"or for the three that already have their own cmaker meaning: 'cmaker git init',\n" +
		"'cmaker git add -A', and 'cmaker git clean -fd' all run the real git command, distinct\n" +
		"from bare 'cmaker init'/'add'/'clean' (which keep their existing cmaker behavior - or\n" +
		"pass --git to those directly, e.g. 'cmaker clean --git -fd').",
	Example: `  cmaker git init
  cmaker git add -A
  cmaker git log --oneline -5`,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGitPassthrough(args)
	},
}

// runGitPassthrough execs `git <args...>` with stdio wired directly to
// this process's own stdio, so it behaves exactly like invoking git
// directly - interactive prompts, color, pagers, everything - and
// propagates git's real exit code rather than flattening every failure to
// a generic 1.
func runGitPassthrough(args []string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}

// maybeDispatchGitOverride intercepts `cmaker {init,add,clean} ... --git
// ...` before cobra ever parses the arguments - init/add/clean already
// have well-established cmaker meanings, so `--git` is handled here
// rather than as a normal cobra flag on those commands: cobra's own flag
// parser would otherwise reject a real git flag those commands never
// declared (e.g. `-fd` for `git clean`). `--git` itself is stripped from
// the forwarded args; everything else after the verb is passed to the
// real `git <verb> ...` verbatim. Returns handled=false (a no-op) for any
// other invocation, so Execute() falls through to cobra's normal
// dispatch.
func maybeDispatchGitOverride(args []string) (handled bool, err error) {
	if len(args) == 0 {
		return false, nil
	}
	verb := args[0]
	if verb != "init" && verb != "add" && verb != "clean" {
		return false, nil
	}

	rest := args[1:]
	gitFlagIdx := -1
	for i, a := range rest {
		if a == "--git" {
			gitFlagIdx = i
			break
		}
	}
	if gitFlagIdx == -1 {
		return false, nil
	}

	forwardArgs := make([]string, 0, len(rest))
	forwardArgs = append(forwardArgs, verb)
	forwardArgs = append(forwardArgs, rest[:gitFlagIdx]...)
	forwardArgs = append(forwardArgs, rest[gitFlagIdx+1:]...)
	return true, runGitPassthrough(forwardArgs)
}
