package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// removeGitCmd deletes .git outright - distinct from 'cmaker git clean -fd'
// (which forwards to the real 'git clean', touching only untracked working-
// tree files; git itself never lets that remove .git). This is the actual
// teardown for a scratch repository 'cmaker dummygit' made, or one
// ensureGitBaseline (see cmd/codegen.go) bootstrapped automatically for
// 'cmaker heal --apply'/'cmaker codegen'/'cmaker fix'/'cmaker migrate' when
// none existed yet and the run succeeded (see gitBaseline.finish).
var removeGitCmd = &cobra.Command{
	Use:   "git [flags]",
	Short: "Delete .git from the current project",
	Long: "Deletes .git entirely - every commit, branch, and stash in this repository, not just\n" +
		"the untracked files 'cmaker clean --git -fd' (git clean) touches; git clean never\n" +
		"removes .git itself. Irreversible unless you have a remote or another local copy to\n" +
		"recover from - asks for confirmation unless --force is given.\n" +
		"\n" +
		"Useful for tearing down a scratch repository - one made by 'cmaker dummygit', or\n" +
		"bootstrapped automatically because none existed yet by 'cmaker heal --apply'/'cmaker\n" +
		"codegen'/'cmaker fix'/'cmaker migrate' (see their own --help) - once you're done with\n" +
		"it and don't want to keep its history.",
	Example: `  cmaker remove git
  cmaker remove git --force`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		return runRemoveGit(force)
	},
}

func init() {
	removeGitCmd.Flags().Bool("force", false, "skip the confirmation prompt")
	removeCmd.AddCommand(removeGitCmd)
}

func runRemoveGit(force bool) error {
	if _, err := os.Stat(".git"); err != nil {
		return fmt.Errorf("no .git here - nothing to remove")
	}
	if !force {
		warnf("This deletes .git entirely - every commit, branch, and stash in this repository, not just untracked files.")
		if !confirmYesNo("Remove .git?") {
			infof("Not removed.")
			return nil
		}
	}
	if err := os.RemoveAll(".git"); err != nil {
		return fmt.Errorf("failed to remove .git: %w", err)
	}
	okf("Removed .git.")
	return nil
}
