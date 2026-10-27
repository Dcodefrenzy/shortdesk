package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/shortdesk/shortdesk/internal/stack"
)

func newUpCommand() *cobra.Command {
	var o serveOptions
	var rebuild, noOpen bool
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start Shortdesk: everything it needs, then the dashboard",
		Long: `Start Shortdesk on this machine. Needs only Docker.

On first run this downloads the desk images (a few minutes), then it starts OpenSandbox
(which creates and guards desks), the vault, and the dashboard. Everything is configured
automatically and kept in ~/.shortdesk. Run it again any time; it picks up where it left off.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			noteNewVersion(cmd.Context(), out)
			if _, err := stack.Up(cmd.Context(), stack.Options{
				Dir:           filepath.Join(home, ".shortdesk"),
				Version:       Version,
				RebuildImages: rebuild,
				Log:           func(f string, a ...any) { fmt.Fprintf(out, f+"\n", a...) },
			}); err != nil {
				return err
			}
			o.onListening = func(url, token, loginLink string) {
				if noOpen {
					fmt.Fprintf(out, "\nOpen %s and log in with this token:\n\n    %s\n", url, token)
				} else {
					fmt.Fprintf(out, "\nOpening %s in your browser (already logged in).\nOn another browser, log in with this token:\n\n    %s\n", url, token)
					if err := openBrowser(loginLink); err != nil {
						fmt.Fprintf(out, "Couldn't open the browser (%v); open %s yourself.\n", err, url)
					}
				}
				fmt.Fprintln(out, "\nStop with Ctrl+C (desks keep their files and logins).")
			}
			return runServe(cmd, o)
		},
	}
	cmd.Flags().StringVar(&o.addr, "addr", "127.0.0.1:7070", "dashboard address")
	cmd.Flags().BoolVar(&rebuild, "rebuild-images", false, "build the desk images here instead of downloading them")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "don't open the dashboard in the browser")
	return cmd
}

func newDownCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop OpenSandbox and the vault (desks, files, logins and chats are kept)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := stack.Down(cmd.Context()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Stopped. Run `shortdesk up` to start again.")
			return nil
		},
	}
}
