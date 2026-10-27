// Package cli implements the shortdesk command. Commands stay thin: they parse
// arguments, call into internal packages, and print results.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/shortdesk/shortdesk/internal/config"
	"github.com/shortdesk/shortdesk/internal/desk"
)

// Version is set at build time with -ldflags "-X github.com/shortdesk/shortdesk/internal/cli.Version=...".
var Version = "dev"

// NewRootCommand returns the shortdesk command tree.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "shortdesk",
		Short:         "Desktops for AI agents, governed by charters",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newUpCommand(), newDownCommand(), newUpdateCommand(), newBackupCommand(), newRestoreCommand(), newServeCommand(), newDeskCommand())
	return root
}

// manager builds a desk manager from the environment's configuration.
func manager() (*desk.Manager, error) {
	cfg, err := config.FromEnv()
	if err != nil {
		return nil, err
	}
	rt, err := cfg.NewRuntime()
	if err != nil {
		return nil, err
	}
	m := desk.NewManager(rt)
	m.ShareFolders()
	return m, nil
}
