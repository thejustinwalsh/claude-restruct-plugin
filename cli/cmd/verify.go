package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/config"
	"github.com/tjw/restruct/internal/db"
	"github.com/tjw/restruct/internal/hook"
	"github.com/tjw/restruct/internal/verify"
)

// checkRunJSON is the JSON-serializable form of a check result for DB storage.
type checkRunJSON struct {
	Name       string `json:"name"`
	Command    string `json:"command"`
	Passed     bool   `json:"passed"`
	Output     string `json:"output"`
	DurationMs int64  `json:"duration_ms"`
}

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Run verification checks (hook handler for TaskCompleted/Stop)",
	Long: `Reads a hook JSON payload from stdin, diffs file state against the
snapshot taken at prompt/task start, and runs verification checks on
changed files. If any check fails, exits with code 2 and writes the
error to stderr so Claude is forced to fix the issue.

Used as a hook handler for TaskCompleted and Stop events.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose")
		logLevel := slog.LevelWarn
		if verbose {
			logLevel = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

		verifyStart := time.Now()

		input, err := hook.ParseInput(os.Stdin)
		if err != nil {
			slog.Warn("verify: parse error", "error", err)
			return nil
		}

		cwd := input.Cwd
		if cwd == "" {
			cwd, _ = os.Getwd()
		}

		// Resolve project root from CLAUDE_PROJECT_DIR
		projectDir := os.Getenv("CLAUDE_PROJECT_DIR")
		projectDirSource := "CLAUDE_PROJECT_DIR"
		if projectDir == "" {
			projectDir = cwd
			projectDirSource = "cwd_fallback"
		}
		slog.Debug("verify: resolved paths", "cwd", cwd, "project_dir", projectDir, "source", projectDirSource)

		// Load verify config from project root
		cfg, err := verify.LoadConfig(projectDir)
		if err != nil {
			slog.Warn("verify: config error", "error", err)
			return nil
		}
		if cfg == nil || len(cfg.Checks) == 0 {
			slog.Debug("verify: no verify.yaml, allowing")
			return nil
		}

		// Prevent infinite loops for Stop hook
		if input.HookEventName == "Stop" && input.StopHookActive {
			slog.Debug("verify: stop_hook_active, allowing stop")
			return nil
		}

		if input.SessionID == "" {
			slog.Warn("verify: no session_id, allowing")
			return nil
		}

		database, err := db.Open(db.DefaultPath())
		if err != nil {
			slog.Warn("verify: db error, allowing", "error", err)
			return nil
		}
		defer database.Close()

		verifyCfg, _ := config.LoadFromViper()
		if verifyCfg == nil {
			verifyCfg = config.Defaults()
		}
		serverURL := fmt.Sprintf("http://localhost:%s", verifyCfg.Server.Port)

		recorder := db.NewRecorder(database, serverURL)
		refinementID := database.LatestRefinementID(input.SessionID)

		scope := "prompt"
		if input.TaskID != "" {
			scope = input.TaskID
		}

		// Check if we have a snapshot for this scope
		has, err := verify.HasSnapshot(database, input.SessionID, scope)
		if err != nil {
			slog.Warn("verify: snapshot check error", "error", err)
			return nil
		}
		if !has {
			if scope != "prompt" {
				has, _ = verify.HasSnapshot(database, input.SessionID, "prompt")
				if has {
					scope = "prompt"
				}
			}
			if !has {
				slog.Debug("verify: no snapshot, allowing")
				recorder.RecordVerification(input.SessionID, refinementID, scope, input.HookEventName, cwd, projectDir, "", "", "skip", time.Since(verifyStart).Microseconds())
				return nil
			}
		}

		// Diff against snapshot using project root
		changedFiles, err := verify.DiffSnapshot(database, input.SessionID, scope, projectDir)
		if err != nil {
			slog.Warn("verify: diff error", "error", err)
			return nil
		}

		if len(changedFiles) == 0 {
			slog.Debug("verify: no file changes detected")
			recorder.RecordVerification(input.SessionID, refinementID, scope, input.HookEventName, cwd, projectDir, "", "", "pass", time.Since(verifyStart).Microseconds())
			return nil
		}

		slog.Debug("verify: files changed", "count", len(changedFiles), "files", changedFiles)

		changedFilesJSON, _ := json.Marshal(changedFiles)

		// Filter checks by changed files
		relevant := verify.FilterChecks(cfg.Checks, changedFiles)
		if len(relevant) == 0 {
			slog.Debug("verify: no relevant checks for changed files")
			recorder.RecordVerification(input.SessionID, refinementID, scope, input.HookEventName, cwd, projectDir, string(changedFilesJSON), "", "pass", time.Since(verifyStart).Microseconds())
			if scope != "prompt" {
				globs := verify.CollectGlobs(cfg)
				verify.TakeSnapshot(database, input.SessionID, "prompt", projectDir, globs)
				verify.CleanSnapshot(database, input.SessionID, scope)
			}
			return nil
		}

		// Run checks with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
		defer cancel()

		results, err := verify.RunChecks(ctx, relevant, projectDir)
		if err != nil {
			slog.Warn("verify: run error", "error", err)
			return nil
		}

		// Build checks_run JSON
		checksJSON := buildChecksRunJSON(results, relevant)

		// Check for failures
		for _, r := range results {
			if !r.Passed {
				// Record BEFORE exit
				recorder.RecordVerification(input.SessionID, refinementID, scope, input.HookEventName, cwd, projectDir, string(changedFilesJSON), checksJSON, "fail", time.Since(verifyStart).Microseconds())

				check := verify.FindCheck(relevant, r.Name)
				command := r.Name
				if check != nil {
					command = check.Command
				}
				msg := verify.FormatFailure(r, command)
				fmt.Fprint(os.Stderr, msg)
				os.Exit(2)
			}
		}

		// All passed
		slog.Debug("verify: all checks passed")
		recorder.RecordVerification(input.SessionID, refinementID, scope, input.HookEventName, cwd, projectDir, string(changedFilesJSON), checksJSON, "pass", time.Since(verifyStart).Microseconds())

		globs := verify.CollectGlobs(cfg)

		if scope != "prompt" {
			verify.TakeSnapshot(database, input.SessionID, "prompt", projectDir, globs)
			verify.CleanSnapshot(database, input.SessionID, scope)
		} else {
			verify.TakeSnapshot(database, input.SessionID, "prompt", projectDir, globs)
		}

		return nil
	},
}

func buildChecksRunJSON(results []verify.CheckResult, checks []verify.CheckConfig) string {
	var runs []checkRunJSON
	for _, r := range results {
		command := r.Name
		if c := verify.FindCheck(checks, r.Name); c != nil {
			command = c.Command
		}
		runs = append(runs, checkRunJSON{
			Name:       r.Name,
			Command:    command,
			Passed:     r.Passed,
			Output:     truncateForJSON(r.Output, 500),
			DurationMs: r.DurationMs,
		})
	}
	data, _ := json.Marshal(runs)
	return string(data)
}

func truncateForJSON(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max]) + "\n[truncated]"
}

func init() {
	rootCmd.AddCommand(verifyCmd)
}
