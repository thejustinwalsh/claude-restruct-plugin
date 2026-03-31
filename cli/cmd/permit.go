package cmd

import (
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/hook"
	"github.com/tjw/restruct/internal/permit"
)

var permitCmd = &cobra.Command{
	Use:   "permit",
	Short: "Auto-approve safe tool operations (PreToolUse hook handler)",
	Long: `Reads a PreToolUse hook JSON payload from stdin, classifies the tool
operation by security tier, and returns allow/deny/passthrough.

Deterministic classification only — no DB, no LLM, no network calls.
Target latency: <50ms.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose")
		logLevel := slog.LevelWarn
		if verbose {
			logLevel = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

		// Hook commands must never exit 1 (undefined for hooks).
		// Recover from panics and degrade gracefully to exit 0 (passthrough).
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

		if decision.Action == "" {
			// Passthrough: no opinion — Claude Code's native permission system handles it
			return nil
		}

		if decision.Action == "deny" {
			return hook.WriteOutput(os.Stdout, hook.PermitOutput("deny", decision.Reason))
		}

		// Allow
		return hook.WriteOutput(os.Stdout, hook.PermitOutput("allow", decision.Reason))
	},
}

func init() {
	rootCmd.AddCommand(permitCmd)
}
