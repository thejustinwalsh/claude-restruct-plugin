package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/bootstrap"
	"github.com/tjw/restruct/internal/config"
	"github.com/tjw/restruct/internal/db"
	"github.com/tjw/restruct/internal/hook"
	"github.com/tjw/restruct/internal/ollama"
	"github.com/tjw/restruct/internal/session"
	"github.com/tjw/restruct/internal/toggle"
)

func init() {
	rootCmd.AddCommand(bootstrapCmd)
	bootstrapCmd.Flags().Bool("incremental", false, "Re-process a single changed file (FileChanged hook)")
	bootstrapCmd.Flags().Bool("instructions-loaded", false, "Record loaded instructions file (InstructionsLoaded hook)")
	bootstrapCmd.Flags().Bool("standalone", false, "Run without hook input, print project map to stdout")
	bootstrapCmd.Flags().Bool("classify", false, "Force synchronous LLM classification (standalone/debug mode)")
	bootstrapCmd.Flags().Bool("json", false, "Output project map as JSON (standalone mode)")
}

var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap",
	Short: "Bootstrap project context (SessionStart / FileChanged / InstructionsLoaded hook handler)",
	Long: `Discovers all CLAUDE.md and rules files in the project, generates deep-context
documents with classified rules, and builds a project map (index.json).

When called as a SessionStart hook, returns the project map as additionalContext
so Claude starts every session with awareness of available rule documents.

When called with --incremental (FileChanged hook), re-processes only the changed file.
When called with --instructions-loaded, records which file Claude loaded.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		incremental, _ := cmd.Flags().GetBool("incremental")
		instructionsLoaded, _ := cmd.Flags().GetBool("instructions-loaded")
		standalone, _ := cmd.Flags().GetBool("standalone")
		verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose")

		logLevel := slog.LevelWarn
		if verbose {
			logLevel = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

		defer func() {
			if r := recover(); r != nil {
				slog.Error("bootstrap: panic recovered", "panic", r)
			}
		}()

		cfg, err := config.LoadFromViper()
		if err != nil {
			slog.Warn("config error, using defaults", "error", err)
			cfg = config.Defaults()
		}

		// Standalone mode: no hook input, just run and print
		if standalone {
			jsonOutput, _ := cmd.Flags().GetBool("json")
			classify, _ := cmd.Flags().GetBool("classify")
			return runStandalone(cfg, verbose, jsonOutput, classify)
		}

		input, err := hook.ParseInput(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "restruct: bootstrap parse error: %v\n", err)
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

		serverURL := fmt.Sprintf("http://localhost:%s", cfg.Server.Port)

		// Open DB (best-effort)
		var recorder *db.Recorder
		sessionID := db.ResolveSessionID(input.SessionID)
		database, dbErr := db.Open(db.DefaultPath())
		if dbErr != nil {
			slog.Warn("db open failed", "error", dbErr)
		} else {
			defer database.Close()
			recorder = db.NewRecorder(database, serverURL)
		}

		switch {
		case instructionsLoaded:
			return runInstructionsLoaded(input, projectDir, cfg, recorder)
		case incremental:
			return runIncremental(input, projectDir, cfg, recorder)
		default:
			return runFullBootstrap(input, projectDir, cfg, recorder, sessionID)
		}
	},
}

// runFullBootstrap handles the SessionStart hook: discover, generate docs, build map, return context.
func runFullBootstrap(input *hook.HookInput, projectDir string, cfg *config.Config, recorder *db.Recorder, sessionID string) error {
	start := time.Now()

	// Record session (absorbs session start logic)
	cwd := input.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	sessMgr := session.NewManager(cwd)
	if _, err := sessMgr.Start(input.SessionID, cwd, input.TranscriptPath); err != nil {
		slog.Warn("local session start error", "error", err)
	}
	if recorder != nil {
		recorder.RecordSession(input.SessionID, cwd, input.TranscriptPath)
	}

	// Discover rule files
	result, err := bootstrap.Discover(projectDir, cfg.Rules.Files, bootstrap.MaxFiles)
	if err != nil {
		slog.Warn("bootstrap discovery failed", "error", err)
		printSessionStatus(input.SessionID, toggle.IsEnabled(db.DataDir()))
		return hook.WriteOutput(os.Stdout, hook.PassthroughOutput())
	}

	if len(result.Files) == 0 {
		slog.Debug("no rule files discovered")
		printSessionStatus(input.SessionID, toggle.IsEnabled(db.DataDir()))
		return hook.WriteOutput(os.Stdout, hook.PassthroughOutput())
	}

	// Generate documents (with timeout safety — return partial map if running long)
	const timeoutBudget = 8 * time.Second // leave 2s margin of the 10s hook timeout
	linksDir := bootstrap.LinksDir(projectDir)
	var docs []*bootstrap.Document
	partial := false
	for _, file := range result.Files {
		if time.Since(start) > timeoutBudget {
			slog.Warn("bootstrap: timeout budget exceeded, returning partial map",
				"processed", len(docs), "total", len(result.Files))
			partial = true
			break
		}
		doc, err := bootstrap.GenerateDocument(file)
		if err != nil {
			slog.Warn("failed to generate document", "file", file.RelPath, "error", err)
			continue
		}
		if err := bootstrap.WriteDocument(doc, linksDir); err != nil {
			slog.Warn("failed to write document", "file", file.RelPath, "error", err)
		}
		docs = append(docs, doc)
	}

	// Build project map
	pm := bootstrap.BuildMap(docs)
	pm.Partial = partial
	if err := bootstrap.WriteMap(pm, linksDir); err != nil {
		slog.Warn("failed to write project map", "error", err)
	}

	durationUs := time.Since(start).Microseconds()

	// Record bootstrap event
	var bootstrapID int64
	if recorder != nil {
		bootstrapID = recorder.RecordBootstrap(&db.BootstrapEvent{
			SessionID:       sessionID,
			ProjectPath:     projectDir,
			FilesDiscovered: len(result.Files),
			FilesProcessed:  len(docs),
			TotalRules:      pm.TotalRules,
			ClassifyStatus:  "pending",
			DurationUs:      durationUs,
		})
	}

	fmt.Fprintf(os.Stderr, "restruct: bootstrap complete — %d files, %d rules, %dms\n",
		len(docs), pm.TotalRules, durationUs/1000)
	printSessionStatus(input.SessionID, toggle.IsEnabled(db.DataDir()))

	// Return project map as additionalContext (write to stdout BEFORE async work)
	additionalCtx := pm.FormatForClaude()
	if additionalCtx == "" {
		hook.WriteOutput(os.Stdout, hook.PassthroughOutput())
	} else {
		hook.WriteOutput(os.Stdout, hook.SessionStartOutput(additionalCtx))
	}

	// Async LLM classification — runs after stdout is written so the hook returns fast.
	// The classification enriches documents with better summaries and keywords.
	// The sentinel file (.classify-done) signals completion to the refine command.
	startClassifyAsync(docs, linksDir, cfg, recorder, bootstrapID)

	return nil
}

// runIncremental handles FileChanged: re-process a single changed file.
func runIncremental(input *hook.HookInput, projectDir string, cfg *config.Config, recorder *db.Recorder) error {
	if input.FilePath == "" {
		slog.Debug("incremental bootstrap: no file_path in input")
		return nil
	}

	slog.Debug("incremental bootstrap", "file", input.FilePath, "change", input.Change)

	linksDir := bootstrap.LinksDir(projectDir)

	// Debounce: skip if index.json was written less than 1s ago
	indexPath := filepath.Join(linksDir, "index.json")
	if info, err := os.Stat(indexPath); err == nil {
		if time.Since(info.ModTime()) < time.Second {
			slog.Debug("incremental bootstrap: debounced (index.json updated <1s ago)")
			return nil
		}
	}

	// Load existing map. If no map, do a full bootstrap instead of incremental.
	pm, err := bootstrap.LoadMap(linksDir)
	if err != nil || pm == nil {
		slog.Debug("no existing project map, running full discovery for new file")
		// For a new file (created), fall through to process it
		if input.Change != "created" {
			return nil
		}
		pm = &bootstrap.ProjectMap{Version: 1, Generated: time.Now()}
	}

	// Handle deletion: remove from map and clean up document file
	if input.Change == "deleted" {
		// Find and remove the old document file
		for _, f := range pm.Files {
			if f.AbsPath == input.FilePath {
				os.Remove(filepath.Join(linksDir, f.Hash+".md"))
				break
			}
		}
		pm = removeFromMap(pm, input.FilePath, projectDir)
		if err := bootstrap.WriteMap(pm, linksDir); err != nil {
			slog.Warn("failed to update project map after deletion", "error", err)
		}
		// Clear classify sentinel since the map changed
		bootstrap.ClearClassified(linksDir)
		fmt.Fprintf(os.Stderr, "restruct: incremental bootstrap — removed %s\n", input.FilePath)
		return nil
	}

	// Re-process the changed file
	absPath := input.FilePath
	relPath, err := relativeToProject(absPath, projectDir)
	if err != nil {
		slog.Warn("failed to compute relative path", "error", err)
		return nil
	}

	info, err := os.Stat(absPath)
	if err != nil {
		slog.Warn("failed to stat changed file", "error", err)
		return nil
	}

	file := bootstrap.DiscoveredFile{
		AbsPath: absPath,
		RelPath: relPath,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}

	doc, err := bootstrap.GenerateDocument(file)
	if err != nil {
		slog.Warn("failed to generate document for changed file", "error", err)
		return nil
	}

	if err := bootstrap.WriteDocument(doc, linksDir); err != nil {
		slog.Warn("failed to write updated document", "error", err)
	}

	// Update the map entry
	pm = updateMapEntry(pm, doc)
	if err := bootstrap.WriteMap(pm, linksDir); err != nil {
		slog.Warn("failed to update project map", "error", err)
	}

	fmt.Fprintf(os.Stderr, "restruct: incremental bootstrap — updated %s (%d rules)\n",
		relPath, doc.RuleCount)
	return nil
}

// runInstructionsLoaded records which file Claude loaded (observability + secondary discovery).
func runInstructionsLoaded(input *hook.HookInput, projectDir string, cfg *config.Config, recorder *db.Recorder) error {
	if input.FilePath == "" {
		return nil
	}

	slog.Debug("instructions loaded",
		"file", input.FilePath,
		"memory_type", input.MemoryType,
		"load_reason", input.LoadReason)

	// Check if this file is already in our index
	linksDir := bootstrap.LinksDir(projectDir)
	pm, _ := bootstrap.LoadMap(linksDir)
	if pm != nil {
		for _, f := range pm.Files {
			if f.AbsPath == input.FilePath {
				slog.Debug("file already in index", "file", input.FilePath)
				return nil
			}
		}
	}

	// New file discovered — add it to the index
	relPath, err := relativeToProject(input.FilePath, projectDir)
	if err != nil {
		return nil
	}

	info, err := os.Stat(input.FilePath)
	if err != nil {
		return nil
	}

	file := bootstrap.DiscoveredFile{
		AbsPath: input.FilePath,
		RelPath: relPath,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}

	doc, err := bootstrap.GenerateDocument(file)
	if err != nil {
		slog.Warn("failed to generate document for loaded instructions", "error", err)
		return nil
	}

	if err := bootstrap.WriteDocument(doc, linksDir); err != nil {
		slog.Warn("failed to write document", "error", err)
	}

	// Update or create map
	if pm == nil {
		pm = bootstrap.BuildMap([]*bootstrap.Document{doc})
	} else {
		pm = updateMapEntry(pm, doc)
	}
	if err := bootstrap.WriteMap(pm, linksDir); err != nil {
		slog.Warn("failed to update project map", "error", err)
	}

	fmt.Fprintf(os.Stderr, "restruct: discovered new instructions file %s (%d rules)\n",
		relPath, doc.RuleCount)
	return nil
}

// runStandalone runs bootstrap without hook input for debugging.
func runStandalone(cfg *config.Config, verbose, jsonOutput, classify bool) error {
	cwd, _ := os.Getwd()
	projectDir := os.Getenv("CLAUDE_PROJECT_DIR")
	if projectDir == "" {
		projectDir = cwd
	}

	result, err := bootstrap.Discover(projectDir, cfg.Rules.Files, bootstrap.MaxFiles)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Discovered %d files in %s (git root: %s)\n",
		len(result.Files), result.ProjectDir, result.GitRoot)

	linksDir := bootstrap.LinksDir(projectDir)
	var docs []*bootstrap.Document
	for _, file := range result.Files {
		doc, err := bootstrap.GenerateDocument(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  SKIP %s: %v\n", file.RelPath, err)
			continue
		}
		if err := bootstrap.WriteDocument(doc, linksDir); err != nil {
			fmt.Fprintf(os.Stderr, "  WARN write %s: %v\n", file.RelPath, err)
		}
		docs = append(docs, doc)

		if verbose {
			fmt.Fprintf(os.Stderr, "  %s — %d rules, keywords: %v\n",
				file.RelPath, doc.RuleCount, doc.Keywords)
		}
	}

	pm := bootstrap.BuildMap(docs)
	if err := bootstrap.WriteMap(pm, linksDir); err != nil {
		return fmt.Errorf("write map: %w", err)
	}

	// Synchronous classification if requested
	if classify {
		fmt.Fprintf(os.Stderr, "Running synchronous classification...\n")
		client, cErr := ollama.NewClient(
			cfg.Ollama.URL, cfg.Ollama.Model,
			30*time.Second, 120*time.Second, 60*time.Second, cfg.Ollama.KeepAlive,
		)
		if cErr != nil {
			fmt.Fprintf(os.Stderr, "  classify error: %v\n", cErr)
		} else {
			chatFn := bootstrap.ChatFunc(func(ctx context.Context, system, user string, temp float32, max int) (string, error) {
				return client.Chat(ctx, system, user, temp, max)
			})
			classifier := bootstrap.NewClassifier(chatFn, linksDir, float32(cfg.Refinement.Temperature), 512)
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			<-classifier.ClassifyAsync(ctx, docs)

			// Reload enriched map
			if enriched, err := bootstrap.LoadMap(linksDir); err == nil && enriched != nil {
				pm = enriched
			}
		}
	}

	// Output
	if jsonOutput {
		data, _ := json.MarshalIndent(pm, "", "  ")
		fmt.Println(string(data))
	} else {
		fmt.Print(pm.FormatForClaude())
	}
	return nil
}

// printSessionStatus writes session start info to stderr (mirrors session.go behavior).
func printSessionStatus(sessionID string, enabled bool) {
	if enabled {
		fmt.Fprintf(os.Stderr, "restruct: session %s started\n", sessionID)
	} else {
		fmt.Fprintf(os.Stderr, "restruct: session %s started (refinement DISABLED — run /restruct:enable to re-enable)\n", sessionID)
	}
}

// removeFromMap removes a file entry from the project map by absolute path.
func removeFromMap(pm *bootstrap.ProjectMap, absPath, projectDir string) *bootstrap.ProjectMap {
	relPath, _ := relativeToProject(absPath, projectDir)
	var kept []bootstrap.MapEntry
	totalRules := 0
	for _, f := range pm.Files {
		if f.AbsPath == absPath || f.Source == relPath {
			continue
		}
		kept = append(kept, f)
		totalRules += f.RuleCount
	}
	pm.Files = kept
	pm.TotalRules = totalRules
	return pm
}

// updateMapEntry replaces or adds a document entry in the project map.
func updateMapEntry(pm *bootstrap.ProjectMap, doc *bootstrap.Document) *bootstrap.ProjectMap {
	entry := bootstrap.MapEntry{
		Source:     doc.Source,
		AbsPath:   doc.AbsPath,
		Hash:      doc.Hash,
		Keywords:  doc.Keywords,
		Categories: doc.Categories,
		Summary:   doc.Summary,
		RuleCount: doc.RuleCount,
	}

	// Replace existing or append
	found := false
	for i, f := range pm.Files {
		if f.Source == doc.Source || f.AbsPath == doc.AbsPath {
			pm.Files[i] = entry
			found = true
			break
		}
	}
	if !found {
		pm.Files = append(pm.Files, entry)
	}

	// Recompute total
	pm.TotalRules = 0
	for _, f := range pm.Files {
		pm.TotalRules += f.RuleCount
	}
	return pm
}

func relativeToProject(absPath, projectDir string) (string, error) {
	return filepath.Rel(projectDir, absPath)
}

// startClassifyAsync launches background LLM classification.
// This runs AFTER stdout has been written (hook has returned to Claude).
// It enriches documents with better summaries/keywords, updates index.json,
// and records the result to DB for dashboard visibility.
func startClassifyAsync(docs []*bootstrap.Document, linksDir string, cfg *config.Config, recorder *db.Recorder, bootstrapID int64) {
	if len(docs) == 0 {
		return
	}

	// Clear any previous classification sentinel
	bootstrap.ClearClassified(linksDir)

	// Create Ollama client for classification with relaxed timeouts.
	// Classification is async and non-blocking — it can wait for Ollama
	// to finish any in-flight refine requests (single GPU, sequential).
	classifyConnectTimeout := 30 * time.Second  // Ollama may be busy with refine
	classifyRequestTimeout := 120 * time.Second // each doc classification
	classifyStallTimeout := 60 * time.Second    // LLM thinking time
	client, err := ollama.NewClient(
		cfg.Ollama.URL,
		cfg.Ollama.Model,
		classifyConnectTimeout,
		classifyRequestTimeout,
		classifyStallTimeout,
		cfg.Ollama.KeepAlive,
	)
	if err != nil {
		slog.Warn("classify: failed to create ollama client", "error", err)
		updateClassifyStatus(recorder, bootstrapID, "skipped", 0, "ollama client error: "+err.Error())
		return
	}

	// Check if Ollama is available
	ctx := context.Background()
	if !client.IsAvailable(ctx) {
		slog.Debug("classify: ollama not available, skipping")
		updateClassifyStatus(recorder, bootstrapID, "skipped", 0, "ollama not available")
		return
	}

	// Wrap the Ollama Chat method as a ChatFunc
	chatFn := bootstrap.ChatFunc(func(ctx context.Context, system, user string, temperature float32, maxTokens int) (string, error) {
		return client.Chat(ctx, system, user, temperature, maxTokens)
	})

	classifier := bootstrap.NewClassifier(chatFn, linksDir, float32(cfg.Refinement.Temperature), 512)

	// Run synchronously (we're already past the hook return).
	// The process stays alive until this completes or times out.
	classifyCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	start := time.Now()
	done := classifier.ClassifyAsync(classifyCtx, docs)
	<-done // wait for completion

	durationUs := time.Since(start).Microseconds()
	if bootstrap.IsClassified(linksDir) {
		updateClassifyStatus(recorder, bootstrapID, "complete", durationUs, "")
	} else {
		updateClassifyStatus(recorder, bootstrapID, "failed", durationUs, "classification did not complete")
	}
}

func updateClassifyStatus(recorder *db.Recorder, bootstrapID int64, status string, durationUs int64, errMsg string) {
	if recorder == nil || bootstrapID == 0 {
		return
	}
	var errPtr *string
	if errMsg != "" {
		errPtr = &errMsg
	}
	recorder.UpdateBootstrapClassify(bootstrapID, status, durationUs, errPtr)
}
