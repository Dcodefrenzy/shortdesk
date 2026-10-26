package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/shortdesk/shortdesk/internal/charter"
	"github.com/shortdesk/shortdesk/internal/clipboard"
	"github.com/shortdesk/shortdesk/internal/desk"
	"github.com/shortdesk/shortdesk/internal/runtime"
)

const readyTimeout = 90 * time.Second

func newDeskCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "desk",
		Short: "Create, view and manage desks",
	}
	cmd.AddCommand(
		newDeskUpCommand(),
		newDeskListCommand(),
		newDeskOpenCommand(),
		newDeskURLCommand(),
		newDeskShellCommand(),
		newDeskPasteCommand(),
		newDeskCopyCommand(),
		newDeskDownCommand(),
	)
	return cmd
}

func newDeskUpCommand() *cobra.Command {
	var charterPath, name string
	var open bool
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start a desk from a charter",
		Example: `  shortdesk desk up --charter charters/examples/assistant.yaml
  shortdesk desk up --charter charters/examples/assistant.yaml --name ayo --open`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := charter.Load(charterPath)
			if err != nil {
				return err
			}
			m, err := manager()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			d, err := m.Up(ctx, c, name)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Starting desk %s (charter %s, runtime %s)...\n", d.Name, d.Charter, m.Runtime().Name())

			readyCtx, cancel := context.WithTimeout(ctx, readyTimeout)
			defer cancel()
			if err := m.WaitReady(readyCtx, d); err != nil {
				return fmt.Errorf("%w (remove it with `shortdesk desk down %s`)", err, d.Name)
			}
			url, err := m.ViewerURL(ctx, d)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Desk %s is ready: %s\n", d.Name, url)
			if !d.ExpiresAt.IsZero() {
				fmt.Fprintf(cmd.OutOrStdout(), "It will be removed at %s.\n", d.ExpiresAt.Local().Format("15:04 on Jan 2"))
			}
			if open {
				return openBrowser(url)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&charterPath, "charter", "c", "", "charter file (required)")
	cmd.Flags().StringVarP(&name, "name", "n", "", "desk name (default: <charter>-<random>)")
	cmd.Flags().BoolVar(&open, "open", false, "open the desk in the browser when ready")
	cmd.MarkFlagRequired("charter")
	return cmd
}

func newDeskListCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List desks",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := manager()
			if err != nil {
				return err
			}
			desks, err := m.List(cmd.Context())
			if err != nil {
				return err
			}
			if len(desks) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No desks. Start one with `shortdesk desk up --charter <file>`.")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tCHARTER\tTENANT\tSTATE\tAGE\tEXPIRES IN")
			for _, d := range desks {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					d.Name, d.Charter, dash(d.Tenant), d.State, ago(d.CreatedAt), until(d.ExpiresAt))
			}
			return w.Flush()
		},
	}
}

func newDeskOpenCommand() *cobra.Command {
	return deskCommand("open [desk]", "Open a desk's screen in the browser",
		func(cmd *cobra.Command, m *desk.Manager, d desk.Desk) error {
			url, err := m.ViewerURL(cmd.Context(), d)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), url)
			return openBrowser(url)
		})
}

func newDeskURLCommand() *cobra.Command {
	return deskCommand("url [desk]", "Print the link to a desk's screen",
		func(cmd *cobra.Command, m *desk.Manager, d desk.Desk) error {
			url, err := m.ViewerURL(cmd.Context(), d)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), url)
			return nil
		})
}

func newDeskShellCommand() *cobra.Command {
	return deskCommand("shell [desk]", "Open the desk's terminal here",
		func(cmd *cobra.Command, m *desk.Manager, d desk.Desk) error {
			sh, ok := m.Runtime().(runtime.Shell)
			if !ok {
				return fmt.Errorf("the %s runtime does not support interactive shells", m.Runtime().Name())
			}
			argv, err := sh.ShellCommand(d.ID)
			if err != nil {
				return err
			}
			c := exec.CommandContext(cmd.Context(), argv[0], argv[1:]...)
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			return c.Run()
		})
}

func newDeskPasteCommand() *cobra.Command {
	var text string
	cmd := deskCommand("paste [desk]", "Send this machine's clipboard (or --text) to the desk's clipboard",
		func(cmd *cobra.Command, m *desk.Manager, d desk.Desk) error {
			if !cmd.Flags().Changed("text") {
				var err error
				if text, err = clipboard.Read(); err != nil {
					return err
				}
			}
			if err := m.Paste(cmd.Context(), d, text); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Sent %d characters to %s's clipboard. Paste with Ctrl+V (Ctrl+Shift+V in the terminal).\n",
				len([]rune(text)), d.Name)
			return nil
		})
	cmd.Flags().StringVar(&text, "text", "", "text to send instead of the clipboard")
	return cmd
}

func newDeskCopyCommand() *cobra.Command {
	return deskCommand("copy [desk]", "Copy the desk's clipboard to this machine's clipboard",
		func(cmd *cobra.Command, m *desk.Manager, d desk.Desk) error {
			text, err := m.Copy(cmd.Context(), d)
			if err != nil {
				return err
			}
			if err := clipboard.Write(text); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Copied %d characters from %s.\n", len([]rune(text)), d.Name)
			return nil
		})
}

func newDeskDownCommand() *cobra.Command {
	return deskCommand("down [desk]", "Stop and remove a desk",
		func(cmd *cobra.Command, m *desk.Manager, d desk.Desk) error {
			if err := m.Down(cmd.Context(), d); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed desk %s.\n", d.Name)
			return nil
		})
}

// deskCommand builds a command that acts on one desk, named by the optional argument.
func deskCommand(use, short string, run func(*cobra.Command, *desk.Manager, desk.Desk) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := manager()
			if err != nil {
				return err
			}
			ref := ""
			if len(args) == 1 {
				ref = args[0]
			}
			d, err := m.Find(cmd.Context(), ref)
			if errors.Is(err, desk.ErrNotFound) && ref == "" {
				return errors.New("no desks running; start one with `shortdesk desk up --charter <file>`")
			}
			if err != nil {
				return err
			}
			return run(cmd, m, d)
		},
	}
}

func openBrowser(url string) error {
	switch goruntime.GOOS {
	case "darwin":
		return exec.Command("open", url).Run()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Run()
	}
	return exec.Command("xdg-open", url).Run()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return time.Since(t).Round(time.Minute).String()
}

func until(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	if d := time.Until(t); d > 0 {
		return d.Round(time.Minute).String()
	}
	return "expired"
}
