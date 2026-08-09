package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"cmaker/internal/packclient"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show which cmaker packs account you're logged in as",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		server, _ := cmd.Flags().GetString("server")
		return runWhoami(server)
	},
}

func init() {
	whoamiCmd.Flags().String("server", "", "override the cmaker packs API server URL (default: "+packclient.DefaultBaseURL+")")
}

func runWhoami(server string) error {
	token, cachedLogin, err := packclient.LoadCredentials()
	if err != nil {
		return err
	}
	if token == "" {
		infof("Not logged in - run 'cmaker login'.")
		return nil
	}

	client := packclient.NewClient(server, token)
	who, err := client.Whoami(context.Background())
	if err != nil {
		return fmt.Errorf("failed to verify login (token may be stale - try 'cmaker login' again): %w", err)
	}
	if who.Login != cachedLogin {
		debugf("server login %q differs from cached %q", who.Login, cachedLogin)
	}
	okf("Logged in as %s.", who.Login)
	return nil
}
