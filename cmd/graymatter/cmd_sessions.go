package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/angelnicolasc/graymatter/cmd/graymatter/internal/harness"
)

func sessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Manage background agent sessions",
		Long:  "List, inspect, and control background agent sessions.",
	}
	cmd.AddCommand(
		sessionsListCmd(),
		sessionsLogsCmd(),
		sessionsKillCmd(),
	)
	return cmd
}

func sessionsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all sessions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sessions, err := harness.ListSessions(dataDir)
			if err != nil {
				return fmt.Errorf("list sessions: %w", err)
			}

			if jsonOut {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(sessions)
			}

			if len(sessions) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No sessions found.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tAGENT\tSTATUS\tSTARTED\tPID")
			fmt.Fprintln(w, "--\t-----\t------\t-------\t---")
			for _, s := range sessions {
				pid := ""
				if s.PID > 0 {
					pid = fmt.Sprintf("%d", s.PID)
				}
				age := formatAge(s.StartedAt)
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					s.ID, s.AgentID, s.Status, age, pid)
			}
			return w.Flush()
		},
	}
}

func sessionsLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs <session-id>",
		Short: "Print the log of a background session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]
			if err := harness.StreamLogs(sessionID, dataDir, cmd.OutOrStdout()); err != nil {
				return err
			}
			return nil
		},
	}
}

func sessionsKillCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kill <session-id>",
		Short: "Stop a running background session",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			sessionID := args[0]

			// Resolve "latest" to a concrete ID.
			if sessionID == "latest" {
				rc, err := harness.Resume(context.Background(), "latest", dataDir)
				if err != nil {
					return fmt.Errorf("resolve latest: %w", err)
				}
				sessionID = rc.ResumeID
			}

			if err := harness.KillSession(sessionID, dataDir); err != nil {
				return err
			}
			fmt.Printf("Session %s signalled.\n", sessionID)
			return nil
		},
	}
}

// formatAge returns a human-readable "started N ago" string.
func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return t.Format("2006-01-02 15:04")
	}
}

// Ensure os is used (needed for json output path via os.Stdout fallback).
var _ = os.Stdout
