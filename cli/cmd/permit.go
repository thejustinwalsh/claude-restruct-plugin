package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/config"
	"github.com/tjw/restruct/internal/db"
	"github.com/tjw/restruct/internal/hook"
	"github.com/tjw/restruct/internal/permit"
)

var permitCmd = &cobra.Command{
	Use:   "permit",
	Short: "Auto-approve safe tool operations (PreToolUse hook handler)",
	Long: `Reads a PreToolUse hook JSON payload from stdin, classifies the tool
operation by security tier, and returns allow/deny/passthrough.

Deterministic classification only — no LLM, no network calls.
Target latency: <50ms. DB write is best-effort after the decision.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose")
		logLevel := slog.LevelWarn
		if verbose {
			logLevel = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

		defer func() {
			if r := recover(); r != nil {
				slog.Error("permit: panic recovered, passing through", "panic", r)
			}
		}()

		start := time.Now()

		input, err := hook.ParseInput(os.Stdin)
		if err != nil {
			slog.Warn("permit: parse error", "error", err)
			return nil
		}

		if input.ToolName == "" {
			slog.Debug("permit: no tool_name, passing through")
			return nil
		}

		cwd := input.Cwd
		if cwd == "" {
			cwd, _ = os.Getwd()
		}

		projectDir := os.Getenv("CLAUDE_PROJECT_DIR")
		if projectDir == "" {
			projectDir = cwd
		}

		cfg, err := permit.LoadConfig(projectDir)
		if err != nil {
			slog.Warn("permit: config error, passing through", "error", err)
			return nil
		}

		decision := permit.Decide(
			input.ToolName,
			input.ToolInput,
			input.PermissionMode,
			projectDir,
			cfg,
		)

		elapsed := time.Since(start)
		slog.Debug("permit: decision",
			"tool", input.ToolName,
			"action", decision.Action,
			"tier", decision.Tier,
			"reason", decision.Reason,
			"elapsed_us", elapsed.Microseconds(),
		)

		// Record decision to DB (best-effort, after the fast path)
		go recordToolDecision(input, projectDir, decision, elapsed)

		if decision.Action == "" {
			return nil
		}

		if decision.Action == "deny" {
			return hook.WriteOutput(os.Stdout, hook.PermitOutput("deny", decision.Reason))
		}

		return hook.WriteOutput(os.Stdout, hook.PermitOutput("allow", decision.Reason))
	},
}

// recordToolDecision writes the decision to SQLite. Best-effort — never blocks the hook.
func recordToolDecision(input *hook.HookInput, projectDir string, decision permit.Decision, elapsed time.Duration) {
	database, err := db.Open(db.DefaultPath())
	if err != nil {
		slog.Debug("permit: db open error (skipping record)", "error", err)
		return
	}
	defer database.Close()

	hookDecision := decision.Action
	if hookDecision == "" {
		hookDecision = "passthrough"
	}
	tier := decision.Tier
	reason := decision.Reason
	durationUs := elapsed.Microseconds()

	verifyCfg, _ := config.LoadFromViper()
	if verifyCfg == nil {
		verifyCfg = config.Defaults()
	}
	serverURL := fmt.Sprintf("http://localhost:%s", verifyCfg.Server.Port)

	recorder := db.NewRecorder(database, serverURL)
	recorder.RecordToolDecision(&db.ToolDecision{
		SessionID:        input.SessionID,
		ProjectPath:      projectDir,
		ToolName:         input.ToolName,
		ToolInputSummary: summarizeToolInput(input.ToolName, input.ToolInput),
		ToolUseID:        input.ToolUseID,
		HookDecision:     &hookDecision,
		HookTier:         &tier,
		HookReason:       &reason,
		HookDurationUs:   &durationUs,
	})
}

// summarizeToolInput creates a short display string for the tool input.
func summarizeToolInput(toolName string, input map[string]any) string {
	switch toolName {
	case "Bash":
		cmd, _ := input["command"].(string)
		if len(cmd) > 120 {
			return cmd[:120] + "..."
		}
		return cmd
	case "Read", "Write", "Edit":
		path, _ := input["file_path"].(string)
		return path
	case "Glob":
		pattern, _ := input["pattern"].(string)
		return pattern
	case "Grep":
		pattern, _ := input["pattern"].(string)
		path, _ := input["path"].(string)
		return pattern + " in " + path
	case "WebFetch":
		url, _ := input["url"].(string)
		return url
	case "WebSearch":
		query, _ := input["query"].(string)
		return query
	default:
		// Collect first few string values
		var parts []string
		for k, v := range input {
			if s, ok := v.(string); ok && s != "" {
				parts = append(parts, k+"="+s)
				if len(parts) >= 2 {
					break
				}
			}
		}
		return strings.Join(parts, ", ")
	}
}

func init() {
	rootCmd.AddCommand(permitCmd)
}
