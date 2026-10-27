package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/shortdesk/shortdesk/internal/backup"
)

func shortdeskHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".shortdesk"), nil
}

func newBackupCommand() *cobra.Command {
	var to string
	var files bool
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Save settings, keys, the vault, logins and chats to one file",
		Long: `Save everything Shortdesk keeps in ~/.shortdesk to one file: settings and keys, the
vault with your passwords, saved logins, chats and charters. With --files, the shared
folders (Developer and Files) are included too. Shortdesk can keep running.

The file contains your passwords and keys: keep it somewhere private.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := shortdeskHome()
			if err != nil {
				return err
			}
			if _, err := os.Stat(home); err != nil {
				return fmt.Errorf("nothing to back up: %s doesn't exist", home)
			}
			if to == "" {
				to = "shortdesk-backup-" + time.Now().Format("2006-01-02-150405") + ".tar.gz"
			}
			out := cmd.OutOrStdout()
			log := func(f string, a ...any) { fmt.Fprintf(out, f+"\n", a...) }
			if err := backup.Create(cmd.Context(), home, to, files, log); err != nil {
				return err
			}
			abs, _ := filepath.Abs(to)
			fmt.Fprintf(out, "Saved %s\nIt contains your passwords and keys (only you can read it): keep it private.\n"+
				"Restore with: shortdesk restore %s\n", abs, filepath.Base(to))
			return nil
		},
	}
	cmd.Flags().StringVarP(&to, "to", "o", "", "file to write (default shortdesk-backup-<date>.tar.gz here)")
	cmd.Flags().BoolVar(&files, "files", false, "include the shared folders (Developer and Files)")
	return cmd
}

func newRestoreCommand() *cobra.Command {
	var files bool
	cmd := &cobra.Command{
		Use:   "restore <file>",
		Short: "Restore a backup made with shortdesk backup",
		Long: `Restore a backup made with shortdesk backup. Stop Shortdesk first. Your current
~/.shortdesk is moved aside (never deleted). With --files, shared folders in the backup
are restored into folders that are empty.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := shortdeskHome()
			if err != nil {
				return err
			}
			if backup.Running(cmd.Context(), "127.0.0.1:7070") {
				return errors.New("Shortdesk is running: stop it first (Ctrl+C where shortdesk up runs, then shortdesk down)")
			}
			out := cmd.OutOrStdout()
			log := func(f string, a ...any) { fmt.Fprintf(out, f+"\n", a...) }
			moved, err := backup.Restore(cmd.Context(), args[0], home, files, log)
			if moved != "" {
				fmt.Fprintf(out, "Your previous data is in %s\n", moved)
			}
			if err != nil {
				return err
			}
			fmt.Fprintln(out, "Restored. Start Shortdesk with: shortdesk up")
			return nil
		},
	}
	cmd.Flags().BoolVar(&files, "files", false, "also restore the shared folders, if the backup has them")
	return cmd
}
