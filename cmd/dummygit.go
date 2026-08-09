package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"cmaker/internal/heal"
)

var dummygitCmd = &cobra.Command{
	Use:   "dummygit",
	Short: "Bootstrap a throwaway git repo so git-dependent tooling (e.g. heal --apply) has one",
	Long: "Initializes a temporary git repository in the current directory and commits everything\n" +
		"in it as a baseline - the same scratch-repo bootstrap 'cmaker heal --apply' runs\n" +
		"automatically when there's no git repository at all (see its own --help). Refuses to\n" +
		"run if this directory is already a real git repository, since wiping .git there would\n" +
		"destroy real history - this only ever creates one where none exists yet.\n" +
		"\n" +
		"Nothing here removes the scratch repo automatically (unlike heal --apply's internal\n" +
		"use, which tears it down itself once done) - run 'cmaker remove git' when you're\n" +
		"finished with it.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDummyGit()
	},
}

func runDummyGit() error {
	if heal.HasGitRepo(".") {
		return fmt.Errorf("this directory is already a git repository - refusing to run ('cmaker dummygit' only bootstraps a throwaway repo where none exists yet; use plain 'git'/'cmaker git' commands here instead)")
	}
	if err := heal.InitScratchRepo("."); err != nil {
		return err
	}
	okf("Created a temporary git repository with everything committed as a baseline. Remove it with 'cmaker remove git' when you no longer need it.")
	return nil
}
