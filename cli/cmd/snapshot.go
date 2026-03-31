package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/config"
	"github.com/tjw/restruct/internal/db"
	"github.com/tjw/restruct/internal/hook"
	"github.com/tjw/restruct/internal/verify"
)

var snapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Record file state for verification (hook handler)",
	Long: `Reads a hook JSON payload from stdin, snapshots the current file state
into SQLite for later comparison by 'restruct verify'.

Used as a hook handler for UserPromptSubmit and TaskCreated events.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose")
		logLevel := slog.LevelWarn
		if verbose {
			logLevel = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

		input, err := hook.ParseInput(os.Stdin)
		if err != nil {
			slog.Warn("snapshot: parse error", "error", err)
			return nil
		}

		cwd := input.Cwd
		if cwd == "" {
			cwd, _ = os.Getwd()
		}

		// Resolve project root from CLAUDE_PROJECT_DIR (always set by Claude Code)
		projectDir := os.Getenv("CLAUDE_PROJECT_DIR")
		projectDirSource := "CLAUDE_PROJECT_DIR"
		if projectDir == "" {
			projectDir = cwd
			projectDirSource = "cwd_fallback"
		}
		slog.Debug("snapshot: resolved paths", "cwd", cwd, "project_dir", projectDir, "source", projectDirSource)

		// Load verify config from project root
		cfg, err := verify.LoadConfig(projectDir)
		if err != nil {
			slog.Warn("snapshot: config error", "error", err)
			return nil
		}
		if cfg == nil || len(cfg.Checks) == 0 {
			slog.Debug("snapshot: no verify.yaml, skipping")
			return nil
		}

		scope := "prompt"
		if input.TaskID != "" {
			scope = input.TaskID
		}

		if input.SessionID == "" {
			slog.Warn("snapshot: no session_id, skipping")
			return nil
		}

		database, err := db.Open(db.DefaultPath())
		if err != nil {
			slog.Warn("snapshot: db error", "error", err)
			return nil
		}
		defer database.Close()

		snapCfg, _ := config.LoadFromViper()
		if snapCfg == nil {
			snapCfg = config.Defaults()
		}
		serverURL := fmt.Sprintf("http://localhost:%s", snapCfg.Server.Port)

		// Take snapshot with timing
		globs := verify.CollectGlobs(cfg)
		start := time.Now()
		if err := verify.TakeSnapshot(database, input.SessionID, scope, projectDir, globs); err != nil {
			slog.Warn("snapshot: take error", "error", err)
			return nil
		}
		durationUs := time.Since(start).Microseconds()

		// Count files in snapshot
		var fileCount int
		database.Pool().QueryRow(
			"SELECT COUNT(*) FROM snapshots WHERE session_id = ? AND scope = ?",
			input.SessionID, scope,
		).Scan(&fileCount)

		// Link to the most recent refinement (refine runs before snapshot in UserPromptSubmit)
		refinementID := database.LatestRefinementID(input.SessionID)

		slog.Debug("snapshot taken", "session", input.SessionID, "scope", scope,
			"files", fileCount, "duration_us", durationUs, "project_dir", projectDir,
			"refinement_id", refinementID)

		// DB write + SSE broadcast handled by the Recorder
		recorder := db.NewRecorder(database, serverURL)
		recorder.RecordSnapshot(input.SessionID, refinementID, scope, input.HookEventName, cwd, projectDir, fileCount, durationUs)

		// Opportunistic pruning
		pruned, _ := verify.PruneStaleSnapshots(database, 24*time.Hour)
		if pruned > 0 {
			slog.Debug("pruned stale snapshots", "count", pruned)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(snapshotCmd)
}
