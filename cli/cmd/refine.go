package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/config"
	"github.com/tjw/restruct/internal/db"
	"github.com/tjw/restruct/internal/hook"
	"github.com/tjw/restruct/internal/pipeline"
	"github.com/tjw/restruct/internal/session"
	"github.com/tjw/restruct/internal/sink"
)

var refineCmd = &cobra.Command{
	Use:   "refine",
	Short: "Refine a prompt via local LLM (Claude Code hook entry point)",
	Long: `Reads a Claude Code UserPromptSubmit hook JSON payload from stdin,
refines the prompt via a local LLM, and writes additionalContext to stdout.

The original prompt always reaches Claude unchanged. The refined instructions
are appended as additional context that guides Claude's behavior.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		bypass, _ := cmd.Flags().GetBool("bypass")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose")

		// Configure logging
		logLevel := slog.LevelWarn
		if verbose {
			logLevel = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

		input, err := hook.ParseInput(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "restruct: parse error: %v\n", err)
			return nil
		}

		cfg, err := config.LoadFromViper()
		if err != nil {
			slog.Warn("config error, using defaults", "error", err)
			cfg = config.Defaults()
		}

		cwd := input.Cwd
		if cwd == "" {
			cwd, _ = os.Getwd()
		}

		// Open DB for telemetry (best-effort, don't block on failure)
		var recorder *db.Recorder
		database, dbErr := db.Open(db.DefaultPath())
		if dbErr != nil {
			slog.Warn("failed to open db, telemetry disabled", "error", dbErr)
		} else {
			defer database.Close()
			recorder = db.NewRecorder(database)
			recorder.RecordSession(input.SessionID, cwd, input.TranscriptPath)
		}

		// Track session in .restruct/ (fast local files)
		sessMgr := session.NewManager(cwd)
		if input.SessionID != "" {
			if _, err := sessMgr.Start(input.SessionID, cwd, input.TranscriptPath); err != nil {
				slog.Warn("session tracking error", "error", err)
			}
		}

		// Pass through short prompts or when bypassed
		if bypass || len(strings.Fields(input.Prompt)) < cfg.Refinement.MinWords {
			slog.Debug("passthrough", "bypass", bypass, "words", len(strings.Fields(input.Prompt)))
			if recorder != nil {
				valid := true
				recorder.RecordRefinement(&db.Refinement{
					SessionID:   input.SessionID,
					ProjectPath: cwd,
					RawPrompt:   input.Prompt,
					Passthrough: true,
					OutputValid: &valid,
				})
			}
			return hook.WriteOutput(os.Stdout, hook.PassthroughOutput())
		}

		p, err := pipeline.New(cfg)
		if err != nil {
			slog.Warn("pipeline init error, passing through", "error", err)
			return hook.WriteOutput(os.Stdout, hook.PassthroughOutput())
		}

		// Create pending refinement record before LLM call (needed for streaming ID)
		var refID int64
		if recorder != nil {
			refID = recorder.RecordPendingRefinement(&db.Refinement{
				SessionID:   input.SessionID,
				ProjectPath: cwd,
				RawPrompt:   input.Prompt,
				Model:       cfg.Ollama.Model,
				Temperature: cfg.Refinement.Temperature,
			})
		}

		// Create streaming sink (best-effort, nil if server unavailable)
		serverURL := fmt.Sprintf("http://localhost:%s", cfg.Server.Port)
		tokenSink := sink.NewHttpTokenSink(serverURL, refID, input.SessionID)
		if tokenSink != nil {
			tokenSink.Start(input.Prompt, cfg.Ollama.Model)
		}

		ctx, cancel := context.WithTimeout(context.Background(), cfg.Ollama.RequestTimeout)
		defer cancel()

		result, err := p.Refine(ctx, input.Prompt, tokenSink)
		if err != nil {
			slog.Warn("refinement failed, passing through", "error", err)
			if recorder != nil && refID > 0 {
				valid := false
				recorder.CompleteRefinement(refID, &db.Refinement{
					Model:       cfg.Ollama.Model,
					OutputValid: &valid,
					Status:      "failed",
				})
			}
			return hook.WriteOutput(os.Stdout, hook.PassthroughOutput())
		}

		// Complete the refinement record with final results
		if recorder != nil && refID > 0 {
			valid := true
			recorder.CompleteRefinement(refID, &db.Refinement{
				RefinedPrompt: &result.Refined,
				Model:         cfg.Ollama.Model,
				Temperature:   cfg.Refinement.Temperature,
				LatencyMs:     result.TotalTime.Milliseconds(),
				CacheHit:      result.CacheHit,
				OutputValid:   &valid,
			})
			for _, t := range result.Timings {
				recorder.RecordPipelineEvent(refID, t.Stage, t.Duration.Milliseconds(), true, "")
			}
		}

		// Record in local session file
		if input.SessionID != "" {
			if _, err := sessMgr.RecordRefinement(input.SessionID); err != nil {
				slog.Warn("session record error", "error", err)
			}
		}

		if dryRun {
			fmt.Fprintln(os.Stderr, "--- Refined prompt (dry-run, not injected) ---")
			fmt.Fprintln(os.Stderr, result.Refined)
			if verbose {
				fmt.Fprintln(os.Stderr, "--- Timings ---")
				for _, t := range result.Timings {
					fmt.Fprintf(os.Stderr, "  %s: %s\n", t.Stage, t.Duration)
				}
				fmt.Fprintf(os.Stderr, "  total: %s\n", result.TotalTime)
			}
			return hook.WriteOutput(os.Stdout, hook.PassthroughOutput())
		}

		return hook.WriteOutput(os.Stdout, hook.ContextOutput(result.Refined))
	},
}

func init() {
	rootCmd.AddCommand(refineCmd)
	refineCmd.Flags().Bool("bypass", false, "Skip refinement, pass prompt through")
	refineCmd.Flags().Bool("dry-run", false, "Print refined prompt to stderr without injecting")
}
