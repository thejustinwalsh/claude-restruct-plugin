package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/db"
	"github.com/tjw/restruct/internal/hook"
	"github.com/tjw/restruct/internal/session"
	"github.com/tjw/restruct/internal/toggle"
)

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage Claude Code session tracking",
}

var sessionStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Record a session start (called by SessionStart hook)",
	RunE: func(cmd *cobra.Command, args []string) error {
		input, err := hook.ParseInput(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "restruct: session start parse error: %v\n", err)
			return nil
		}

		cwd := input.Cwd
		if cwd == "" {
			cwd, _ = os.Getwd()
		}

		// Local session file
		mgr := session.NewManager(cwd)
		if _, err := mgr.Start(input.SessionID, cwd, input.TranscriptPath); err != nil {
			slog.Warn("local session start error", "error", err)
		}

		// DB record
		database, err := db.Open(db.DefaultPath())
		if err != nil {
			slog.Warn("db open failed", "error", err)
		} else {
			defer database.Close()
			recorder := db.NewRecorder(database, "")
			recorder.RecordSession(input.SessionID, cwd, input.TranscriptPath)
		}

		if toggle.IsEnabled(db.DataDir()) {
			fmt.Fprintf(os.Stderr, "restruct: session %s started\n", input.SessionID)
		} else {
			fmt.Fprintf(os.Stderr, "restruct: session %s started (refinement DISABLED — run /restruct:enable to re-enable)\n", input.SessionID)
		}
		return nil
	},
}

var sessionEndCmd = &cobra.Command{
	Use:   "end",
	Short: "Record a session end (called by SessionEnd hook)",
	RunE: func(cmd *cobra.Command, args []string) error {
		input, err := hook.ParseInput(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "restruct: session end parse error: %v\n", err)
			return nil
		}

		cwd := input.Cwd
		if cwd == "" {
			cwd, _ = os.Getwd()
		}

		// Local session file
		mgr := session.NewManager(cwd)
		if err := mgr.End(input.SessionID); err != nil {
			slog.Warn("local session end error", "error", err)
		}

		// DB record
		database, err := db.Open(db.DefaultPath())
		if err != nil {
			slog.Warn("db open failed", "error", err)
		} else {
			defer database.Close()
			recorder := db.NewRecorder(database, "")
			recorder.EndSession(input.SessionID)
		}

		fmt.Fprintf(os.Stderr, "restruct: session %s ended\n", input.SessionID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(sessionCmd)
	sessionCmd.AddCommand(sessionStartCmd)
	sessionCmd.AddCommand(sessionEndCmd)
}
