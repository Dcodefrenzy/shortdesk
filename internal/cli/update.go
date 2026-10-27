package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/shortdesk/shortdesk/internal/stack"
	"github.com/shortdesk/shortdesk/internal/update"
)

func newUpdateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update Shortdesk to the latest release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if !isRelease(Version) {
				return errors.New("this is a development build; update it with `git pull && make all`")
			}
			latest, err := update.Latest(cmd.Context())
			if err != nil {
				return fmt.Errorf("couldn't check for updates: %w", err)
			}
			if !update.Newer(latest, Version) {
				fmt.Fprintf(out, "Shortdesk %s is the latest version.\n", Version)
				return nil
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			if exe, err = filepath.EvalSymlinks(exe); err != nil {
				return err
			}
			fmt.Fprintf(out, "Updating Shortdesk %s → %s…\n", Version, latest)
			if err := update.Apply(cmd.Context(), latest, exe); err != nil {
				return err
			}
			fmt.Fprintf(out, "Updated to %s. If Shortdesk is running, stop it (Ctrl+C) and run `shortdesk up` again;\n"+
				"it downloads the new desk images on start. Your desks, chats and logins are kept.\n", latest)
			return nil
		},
	}
}

func isRelease(v string) bool { return stack.IsRelease(v) }

// noteNewVersion tells the user when a newer release exists. It gives up quickly, so a
// slow or offline network never delays starting.
func noteNewVersion(ctx context.Context, out io.Writer) {
	if !isRelease(Version) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if latest, err := update.Latest(ctx); err == nil && update.Newer(latest, Version) {
		fmt.Fprintf(out, "A new version of Shortdesk is available (%s → %s). Update with: shortdesk update\n", Version, latest)
	}
}
