package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"cmaker/internal/packclient"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Log out of cmaker packs (revokes the current token)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		server, _ := cmd.Flags().GetString("server")
		return runLogout(server)
	},
}

func init() {
	logoutCmd.Flags().String("server", "", "override the cmaker packs API server URL (default: "+packclient.DefaultBaseURL+")")
}

func runLogout(server string) error {
	token, _, err := packclient.LoadCredentials()
	if err != nil {
		return err
	}
	if token == "" {
		infof("Not logged in.")
		return nil
	}

	client := packclient.NewClient(server, token)
	if err := client.Logout(context.Background()); err != nil {
		warnf("failed to revoke the token on the server: %v (clearing the local credential anyway)", err)
	}
	if err := packclient.ClearCredentials(); err != nil {
		return err
	}
	okf("Logged out.")
	return nil
}
