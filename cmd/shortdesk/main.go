// Command shortdesk manages desks: desktops for AI agents, governed by charters.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/shortdesk/shortdesk/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := cli.NewRootCommand().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "shortdesk:", err)
		os.Exit(1)
	}
}
