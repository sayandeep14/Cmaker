package cmd

import (
	"context"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"cmaker/internal/packclient"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to cmaker packs via GitHub (device flow)",
	Long: "Logs in to the cmaker packs registry using GitHub's device flow - the same UX as the\n" +
		"gh/docker CLIs: this prints a one-time code and opens GitHub in your browser, then waits\n" +
		"until you finish authorizing there. Your GitHub token itself is never stored - only\n" +
		"cmaker's own opaque API token (~/.cmaker/credentials, separate from the Anthropic API\n" +
		"key file) is saved, used by 'cmaker publish' and friends.",
	Example: "  cmaker login",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		server, _ := cmd.Flags().GetString("server")
		return runLogin(server)
	},
}

func init() {
	loginCmd.Flags().String("server", "", "override the cmaker packs API server URL (default: "+packclient.DefaultBaseURL+")")
}

func runLogin(server string) error {
	client := packclient.NewClient(server, "")

	token, login, err := client.DeviceLogin(context.Background(), func(userCode, verificationURI string) {
		infof("First, copy your one-time code: %s", userCode)
		infof("Then visit: %s", verificationURI)
		if err := openBrowser(verificationURI); err != nil {
			debugf("failed to auto-open browser: %v", err)
		}
		infof("Waiting for authorization...")
	})
	if err != nil {
		return err
	}

	if err := packclient.SaveCredentials(token, login); err != nil {
		return err
	}
	okf("Logged in as %s.", login)
	return nil
}

// openBrowser best-effort opens url in the default browser - a failure
// here is never fatal to login itself, since the user can always open
// the printed URL by hand (see runLogin's debugf, not errorf).
func openBrowser(url string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	return c.Start()
}
