package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/bootstrap"
	"github.com/tjw/restruct/internal/config"
	"github.com/tjw/restruct/internal/db"
	"github.com/tjw/restruct/internal/hook"
	"github.com/tjw/restruct/internal/pipeline"
	"github.com/tjw/restruct/internal/prompt"
	"github.com/tjw/restruct/internal/session"
	"github.com/tjw/restruct/internal/sink"
	"github.com/tjw/restruct/internal/toggle"
	"github.com/tjw/restruct/internal/verify"
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

		// Hook commands must never exit 1 (undefined for hooks).
		// Recover from panics and degrade gracefully to exit 0 (passthrough).
		defer func() {
			if r := recover(); r != nil {
				slog.Error("refine: panic recovered, passing through", "panic", r)
			}
		}()

		input, err := hook.ParseInput(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "restruct: parse error: %v\n", err)
			return nil
		}

		// Check if restruct is globally disabled
		if !toggle.IsEnabled(db.DataDir()) {
			slog.Debug("restruct disabled, passing through")
			return hook.WriteOutput(os.Stdout, hook.PassthroughOutput())
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

		// Resolve project root for snapshot/verify (CLAUDE_PROJECT_DIR is always set by Claude Code)
		projectDir := os.Getenv("CLAUDE_PROJECT_DIR")
		if projectDir == "" {
			projectDir = cwd
		}

		serverURL := fmt.Sprintf("http://localhost:%s", cfg.Server.Port)

		// Open DB for telemetry (best-effort, don't block on failure)
		var recorder *db.Recorder
		sessionID := db.ResolveSessionID(input.SessionID)
		database, dbErr := db.Open(db.DefaultPath())
		if dbErr != nil {
			slog.Warn("failed to open db, telemetry disabled", "error", dbErr)
		} else {
			defer database.Close()
			recorder = db.NewRecorder(database, serverURL)
			// Auto-heal: ensure session is active (handles resume, missed SessionStart)
			sessionID = recorder.EnsureSession(sessionID, cwd, input.TranscriptPath)
		}

		// Track session in .restruct/ (fast local files)
		sessMgr := session.NewManager(cwd)
		if _, err := sessMgr.Start(sessionID, cwd, input.TranscriptPath); err != nil {
			slog.Warn("session tracking error", "error", err)
		}

		// Pass through short prompts, follow-ups, commands, or when bypassed
		shouldSkip := bypass ||
			len(strings.Fields(input.Prompt)) < cfg.Refinement.MinWords ||
			!pipeline.ShouldRefine(input.Prompt)
		if shouldSkip {
			slog.Debug("passthrough", "bypass", bypass, "words", len(strings.Fields(input.Prompt)))
			var passthroughRefID int64
			if recorder != nil {
				valid := true
				passthroughRefID = recorder.RecordRefinement(&db.Refinement{
					SessionID:   sessionID,
					ProjectPath: cwd,
					RawPrompt:   input.Prompt,
					Passthrough: true,
					OutputValid: &valid,
				})
			}
			// Take snapshot even for passthroughs — verification needs a baseline
			if database != nil && recorder != nil && sessionID != "" && passthroughRefID > 0 {
				takeSnapshotForRefinement(database, recorder, sessionID, passthroughRefID, cwd, projectDir)
			}
			return hook.WriteOutput(os.Stdout, hook.PassthroughOutput())
		}

		p, err := pipeline.New(cfg, cwd)
		if err != nil {
			slog.Warn("pipeline init error, passing through", "error", err)
			return hook.WriteOutput(os.Stdout, hook.PassthroughOutput())
		}

		// Attach deep-context map loader for retrieval-augmented refinement.
		// When available, the LLM sees the project map and selects relevant docs.
		// Lazy re-index: if any rule files changed since last bootstrap, re-process
		// them inline before refinement. This replaces the FileChanged hook which
		// isn't supported in plugin manifests yet.
		linksDir := bootstrap.LinksDir(projectDir)
		if ml := bootstrap.NewMapLoader(linksDir); ml != nil {
			if stale := ml.StaleFiles(); len(stale) > 0 {
				slog.Info("re-indexing stale rule files before refinement", "stale_files", stale)
				bootstrap.ReindexStale(ml.Map(), linksDir, cfg.Rules.Files)
				// Reload the map after re-indexing
				ml = bootstrap.NewMapLoader(linksDir)
			}
			if ml != nil {
				p.SetMapLoader(ml)
				slog.Debug("map loader attached", "files", len(ml.Map().Files))
			}
		}

		// Attach session context provider so the local LLM can see
		// recent conversation history (intents from prior refinements).
		if database != nil && sessionID != "" {
			p.SetSessionProvider(database, sessionID)
		}

		// Create pending refinement record before LLM call (needed for streaming ID)
		var refID int64
		if recorder != nil {
			refID = recorder.RecordPendingRefinement(&db.Refinement{
				SessionID:   sessionID,
				ProjectPath: cwd,
				RawPrompt:   input.Prompt,
				Model:       cfg.Ollama.Model,
				Temperature: cfg.Refinement.Temperature,
			})
		}

		// Create streaming sink (best-effort, nil if server unavailable).
		// All HTTP calls happen in a background goroutine — never blocks the hook.
		tokenSink := sink.NewHttpTokenSink(serverURL, refID, sessionID)

		// Broadcast the LLM input prompt as soon as it's built (before inference)
		p.SetInputReadyCallback(func(inputPrompt string) {
			if tokenSink != nil {
				tokenSink.SendInput(inputPrompt)
			}
		})
		if tokenSink != nil {
			defer tokenSink.Close() // ensure background sends complete before exit
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
			inputPrompt := &result.InputPrompt
			if *inputPrompt == "" {
				inputPrompt = nil
			}
			var llmOutput *string
			if result.LLMOutput != "" {
				llmOutput = &result.LLMOutput
			}
			recorder.CompleteRefinement(refID, &db.Refinement{
				RefinedPrompt: &result.Refined,
				InputPrompt:   inputPrompt,
				LLMOutput:     llmOutput,
				Model:         cfg.Ollama.Model,
				Temperature:   cfg.Refinement.Temperature,
				LatencyMs:     result.TotalTime.Milliseconds(),
				CacheHit:      result.CacheHit,
				OutputValid:   &valid,
			})
			for _, t := range result.Timings {
				recorder.RecordPipelineEvent(refID, t.Stage, t.Duration.Microseconds(), true, "")
			}

			// Record which deep-context documents were selected
			if len(result.SelectedDocs) > 0 && len(result.DocSources) > 0 {
				var selections []db.ContextSelection
				for i, idx := range result.SelectedDocs {
					source := ""
					if i < len(result.DocSources) {
						source = result.DocSources[i]
					}
					_ = idx
					selections = append(selections, db.ContextSelection{
						DocSource: source,
						DocHash:   "", // hash would require re-loading map; source is sufficient
					})
				}
				recorder.RecordContextSelections(refID, selections)
			}
		}

		// Record in local session file
		if sessionID != "" {
			if _, err := sessMgr.RecordRefinement(sessionID); err != nil {
				slog.Warn("session record error", "error", err)
			}
		}

		// Broadcast completion with final context + pipeline timings
		if tokenSink != nil && refID > 0 {
			var timingsData []map[string]interface{}
			for _, t := range result.Timings {
				timingsData = append(timingsData, map[string]interface{}{
					"stage":       t.Stage,
					"duration_us": t.Duration.Microseconds(),
				})
			}
			tokenSink.SendComplete(result.Refined, result.LLMOutput, result.TotalTime.Milliseconds(), timingsData)
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

		// Take snapshot for verification baseline (same DB connection, same process — no race)
		if database != nil && recorder != nil && sessionID != "" && refID > 0 {
			takeSnapshotForRefinement(database, recorder, sessionID, refID, cwd, projectDir)
		}

		// Frame the output for Claude or passthrough if no context needed
		framed := prompt.FrameContext(result.Refined)
		if framed == "" || result.NoContext {
			return hook.WriteOutput(os.Stdout, hook.PassthroughOutput())
		}
		return hook.WriteOutput(os.Stdout, hook.ContextOutput(framed))
	},
}

// takeSnapshotForRefinement takes a file snapshot linked to the given refinement.
// Called at the end of refine to establish the baseline for later verification.
// Best-effort: failures are logged but don't block the hook.
func takeSnapshotForRefinement(database *db.DB, recorder *db.Recorder, sessionID string, refID int64, cwd, projectDir string) {
	cfg, err := verify.LoadConfig(projectDir)
	if err != nil || cfg == nil || len(cfg.Checks) == 0 {
		return
	}

	globs := verify.CollectGlobs(cfg)
	start := time.Now()
	if err := verify.TakeSnapshot(database, sessionID, "prompt", projectDir, globs); err != nil {
		slog.Warn("refine: snapshot error", "error", err)
		return
	}
	durationUs := time.Since(start).Microseconds()

	var fileCount int
	database.Pool().QueryRow(
		"SELECT COUNT(*) FROM snapshots WHERE session_id = ? AND scope = 'prompt'",
		sessionID,
	).Scan(&fileCount)

	slog.Debug("refine: snapshot taken", "refinement_id", refID, "files", fileCount, "duration_us", durationUs)

	// DB write + SSE broadcast handled by the Recorder
	recorder.RecordSnapshot(sessionID, refID, "prompt", "UserPromptSubmit", cwd, projectDir, fileCount, durationUs)

	verify.PruneStaleSnapshots(database, 24*time.Hour)
}

func init() {
	rootCmd.AddCommand(refineCmd)
	refineCmd.Flags().Bool("bypass", false, "Skip refinement, pass prompt through")
	refineCmd.Flags().Bool("dry-run", false, "Print refined prompt to stderr without injecting")
}
