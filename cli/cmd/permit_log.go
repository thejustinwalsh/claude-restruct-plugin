package cmd

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/db"
	"github.com/tjw/restruct/internal/hook"
)

var permitLogCmd = &cobra.Command{
	Use:   "permit-log",
	Short: "Record tool execution outcome (PostToolUse/PostToolUseFailure hook handler)",
	Long: `Reads a PostToolUse or PostToolUseFailure hook JSON payload from stdin
and updates the corresponding tool_decisions row with the execution outcome.

Used as an async hook handler — never blocks tool execution.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose")
		logLevel := slog.LevelWarn
		if verbose {
			logLevel = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

		defer func() {
			if r := recover(); r != nil {
				slog.Error("permit-log: panic recovered", "panic", r)
			}
		}()

		event, _ := cmd.Flags().GetString("event")
		if event == "" {
			slog.Debug("permit-log: no --event flag")
			return nil
		}

		input, err := hook.ParseInput(os.Stdin)
		if err != nil {
			slog.Warn("permit-log: parse error", "error", err)
			return nil
		}

		if input.ToolUseID == "" {
			slog.Debug("permit-log: no tool_use_id, skipping")
			return nil
		}

		database, err := db.Open(db.DefaultPath())
		if err != nil {
			slog.Warn("permit-log: db error", "error", err)
			return nil
		}
		defer database.Close()

		// Auto-heal session
		cwd := input.Cwd
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		database.EnsureSession(input.SessionID, cwd, input.TranscriptPath)

		outcome := "executed"
		if event == "failed" {
			outcome = "failed"
		}

		if err := database.UpdateToolOutcome(input.ToolUseID, outcome, nil); err != nil {
			slog.Warn("permit-log: update error", "error", err, "tool_use_id", input.ToolUseID)
		} else {
			slog.Debug("permit-log: recorded outcome",
				"tool_use_id", input.ToolUseID,
				"tool_name", input.ToolName,
				"outcome", outcome,
			)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(permitLogCmd)
	permitLogCmd.Flags().String("event", "", "Event type: executed or failed")
}
