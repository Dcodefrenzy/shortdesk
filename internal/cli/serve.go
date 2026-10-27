package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	chartersfs "github.com/shortdesk/shortdesk/charters"
	"github.com/shortdesk/shortdesk/internal/agent"
	"github.com/shortdesk/shortdesk/internal/logins"
	"github.com/shortdesk/shortdesk/internal/server"
	"github.com/shortdesk/shortdesk/internal/vault"
	"github.com/shortdesk/shortdesk/internal/webui"
	"github.com/shortdesk/shortdesk/internal/workspace"
)

const envAdminToken = "SHORTDESK_ADMIN_TOKEN"

// loginCaptureEvery is how often running desks are checked for new logins.
const loginCaptureEvery = time.Minute

// serveOptions configure the API server (shared by `serve` and `up`).
type serveOptions struct {
	addr, chartersDir, webDir, workspacePath string
	secure                                   bool
	onListening                              func(url, token string) // called once the server accepts connections
}

func newServeCommand() *cobra.Command {
	var o serveOptions
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the Shortdesk API and dashboard (use `shortdesk up` to also start everything it needs)",
		Long: `Run the Shortdesk API server and dashboard. Desk screens are proxied through it, so
they are only reachable after logging in.

The admin token comes from $SHORTDESK_ADMIN_TOKEN, or is generated once and kept in
~/.shortdesk/admin-token.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runServe(cmd, o) },
	}
	cmd.Flags().StringVar(&o.addr, "addr", "127.0.0.1:7070", "listen address")
	cmd.Flags().StringVar(&o.chartersDir, "charters", "", "directory of charters desks can be started from (default ~/.shortdesk/charters)")
	cmd.Flags().StringVar(&o.webDir, "web", "", "serve the dashboard from this directory instead of the built-in one")
	cmd.Flags().BoolVar(&o.secure, "secure-cookies", false, "mark the session cookie Secure (use behind HTTPS)")
	cmd.Flags().StringVar(&o.workspacePath, "workspace", "", "workspace settings file (default ~/.shortdesk/workspace.json)")
	return cmd
}

func runServe(cmd *cobra.Command, o serveOptions) error {
	m, err := manager()
	if err != nil {
		return err
	}
	token, source, err := adminToken()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	base := filepath.Join(home, ".shortdesk")
	if o.workspacePath == "" {
		o.workspacePath = filepath.Join(base, "workspace.json")
	}
	if o.chartersDir == "" {
		o.chartersDir = filepath.Join(base, "charters")
		if err := seedCharters(o.chartersDir); err != nil {
			return err
		}
	}
	ws, err := workspace.Open(o.workspacePath)
	if err != nil {
		return err
	}
	m.SetExtraAllowedHosts(ws.ModelHosts)
	chats, err := agent.OpenStore(filepath.Join(base, "chats"))
	if err != nil {
		return err
	}
	runner := agent.NewRunner(m.Runtime(), chats, agent.Config{Providers: ws.Providers}, log)
	lg, err := logins.New(m.Runtime(), filepath.Join(base, "logins"), log)
	if err != nil {
		return err
	}
	v := vault.New(filepath.Join(base, "vault"), ws)
	if err := v.Start(cmd.Context()); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: vault not started (%v); credentials won't work until it is.\n", err)
	} else if err := v.Provision(cmd.Context()); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: vault not set up (%v); credentials won't work until it is.\n", err)
	}
	cfg := server.Config{Token: token, ChartersDir: o.chartersDir, WebDir: o.webDir, SecureCookies: o.secure,
		AccessFile: filepath.Join(base, "access.json")}
	if o.webDir == "" {
		cfg.Web = webui.FS()
	}
	app := server.New(cfg, m, runner, ws, lg, v, log)
	go app.CaptureLogins(cmd.Context(), loginCaptureEvery)
	go app.RefreshDesks(cmd.Context())

	ln, err := net.Listen("tcp", o.addr)
	if err != nil {
		return fmt.Errorf("can't listen on %s (is Shortdesk already running?): %w", o.addr, err)
	}
	srv := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second}
	url := "http://" + strings.Replace(o.addr, "127.0.0.1", "localhost", 1)
	fmt.Fprintf(cmd.OutOrStdout(), "Shortdesk is running: %s (runtime %s)\n", url, m.Runtime().Name())
	fmt.Fprintf(cmd.OutOrStdout(), "Admin token %s\n", source)
	fmt.Fprintf(cmd.OutOrStdout(), "Charters: %s\n", o.chartersDir)
	if o.onListening != nil {
		o.onListening(url, token)
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-cmd.Context().Done():
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

// seedCharters gives a new installation the example charters.
func seedCharters(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := fs.ReadDir(chartersfs.Examples, "examples")
	if err != nil {
		return err
	}
	for _, e := range entries {
		b, err := fs.ReadFile(chartersfs.Examples, "examples/"+e.Name())
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// adminToken returns the token and a description of where it came from, creating
// ~/.shortdesk/admin-token on first use.
func adminToken() (token, source string, err error) {
	if t := os.Getenv(envAdminToken); t != "" {
		return t, "from $" + envAdminToken, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	path := filepath.Join(home, ".shortdesk", "admin-token")
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, "in " + path, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = hex.EncodeToString(b)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", "", err
	}
	return token, "created in " + path, nil
}
