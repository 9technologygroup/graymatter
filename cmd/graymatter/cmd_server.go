package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/angelnicolasc/graymatter/pkg/server"
)

func serverCmd() *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run the GrayMatter REST API server",
		Long: `Start an HTTP server that exposes GrayMatter memory operations
over a JSON REST API. Useful for integrating non-Go agents (Python, Shell, etc.)
with the same persistent memory store.

Routes:
  POST   /remember        {"agent":"<id>","text":"<text>"}
  GET    /recall           ?agent=<id>&q=<query>[&k=<int>]
  POST   /consolidate      {"agent":"<id>"}
  GET    /facts            ?agent=<id>[&limit=<int>]
  DELETE /forget           {"agent":"<id>","query":"<query>"}
  GET    /healthz`,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := slog.New(slog.NewTextHandler(cmd.OutOrStderr(), nil))
			srv := server.New(addr, dataDir, logger)

			// Graceful shutdown on SIGINT / SIGTERM.
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			errCh := make(chan error, 1)
			go func() { errCh <- srv.ListenAndServe() }()

			select {
			case <-ctx.Done():
				if !quiet {
					fmt.Fprintln(cmd.OutOrStderr(), "shutting down...")
				}
				return srv.Shutdown(context.Background())
			case err := <-errCh:
				return err
			}
		},
	}

	cmd.Flags().StringVarP(&addr, "addr", "a", ":8080", "listen address (host:port)")
	return cmd
}
