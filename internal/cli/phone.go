package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/shortdesk/shortdesk/internal/phone"
	"github.com/shortdesk/shortdesk/internal/webui"
	"github.com/shortdesk/shortdesk/shortapps"
)

// newPhoneHostCommand runs the Phone Desk's host. It runs inside the Phone Desk
// container, which shortdesk up starts; people don't run it themselves.
func newPhoneHostCommand() *cobra.Command {
	var addr, dir string
	cmd := &cobra.Command{
		Use:    "phone-host",
		Short:  "Run the Phone Desk (inside its container)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			secret := os.Getenv(phone.SecretEnv)
			if len(secret) < 16 {
				return errors.New(phone.SecretEnv + " must be set")
			}
			log := slog.New(slog.NewTextHandler(os.Stderr, nil))
			h, err := phone.New(phone.Config{Dir: dir, Secret: []byte(secret), Builtin: shortapps.Builtin(),
				Shell: webui.FS(), SDK: shortapps.SDK, Log: log})
			if err != nil {
				return err
			}
			defer h.Close()
			srv := &http.Server{Addr: addr, Handler: h.Handler(), ReadHeaderTimeout: 10 * time.Second}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			go func() {
				<-ctx.Done()
				shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				srv.Shutdown(shutdown)
			}()
			log.Info("phone desk running", "addr", addr, "dir", dir)
			if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("phone host: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", ":7071", "address to listen on")
	cmd.Flags().StringVar(&dir, "dir", "/data", "where apps and their data live")
	return cmd
}
