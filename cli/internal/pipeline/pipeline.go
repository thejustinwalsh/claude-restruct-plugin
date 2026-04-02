package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/tjw/restruct/internal/cache"
	"github.com/tjw/restruct/internal/config"
	"github.com/tjw/restruct/internal/db"
	"github.com/tjw/restruct/internal/git"
	"github.com/tjw/restruct/internal/ollama"
	"github.com/tjw/restruct/internal/prompt"
	"github.com/tjw/restruct/internal/rules"
)

// LLMClient is the interface the pipeline uses for LLM communication.
type LLMClient interface {
	IsAvailable(ctx context.Context) bool
	EnsureModel(ctx context.Context) error
	ChatWithRetry(ctx context.Context, system, user string, temperature float32, maxTokens int, sink ollama.TokenSink) (string, error)
}

// RulesLoader is the interface for loading project rules.
type RulesLoader interface {
	Load() (string, error)
	Hash() (string, error)
}

// GitProvider is the interface for gathering git context.
type GitProvider interface {
	GetContext() (*git.Context, error)
}

// SessionProvider retrieves recent session context for the local LLM.
type SessionProvider interface {
	GetRecentIntents(sessionID string, limit int) ([]db.SessionClip, error)
}

// CacheStore is the interface for prompt caching.
type CacheStore interface {
	Get(rawPrompt, rulesHash string) (string, bool)
	Put(rawPrompt, rulesHash, refined string) error
}

// TimingResult records how long a pipeline stage took.
type TimingResult struct {
	Stage    string
	Duration time.Duration
}

// RefineResult contains the pipeline output plus metadata.
type RefineResult struct {
	Refined      string
	InputPrompt  string // system + user message sent to the local LLM
	LLMOutput    string // raw output from the local LLM (before parsing/composition)
	CacheHit     bool
	NoContext    bool // LLM determined no additional context needed
	Timings      []TimingResult
	TotalTime    time.Duration
	SelectedDocs []int    // indices of deep-context docs selected by the LLM
	DocSources   []string // source paths of selected docs (for recording)
}

// NoContextSentinel is the literal string the local LLM outputs when
// the request is clear and no project rules apply.
const NoContextSentinel = "NO_ADDITIONAL_CONTEXT"

// MapLoader loads deep-context documents selected by the LLM.
type MapLoader interface {
	FormatMapForLLM() string
	LoadSelected(docIndices []int) (string, map[string]*prompt.ParsedRules, error)
	SelectedSources(docIndices []int) []string
}

// Pipeline orchestrates the prompt refinement process.
type Pipeline struct {
	llm            LLMClient
	rules          RulesLoader
	git            GitProvider
	session        SessionProvider
	sessionID      string
	builder        *prompt.Builder
	cache          CacheStore
	cfg            *config.Config
	mapLoader      MapLoader
	onInputReady   func(inputPrompt string) // called after prompt build, before LLM inference
}

// New creates a Pipeline from the given configuration.
func New(cfg *config.Config, cwd string) (*Pipeline, error) {
	if cwd == "" {
		cwd = "."
	}
	client, err := ollama.NewClient(
		cfg.Ollama.URL,
		cfg.Ollama.Model,
		cfg.Ollama.ConnectTimeout,
		cfg.Ollama.RequestTimeout,
		cfg.Ollama.StallTimeout,
		cfg.Ollama.KeepAlive,
	)
	if err != nil {
		return nil, fmt.Errorf("create ollama client: %w", err)
	}
	return &Pipeline{
		llm:     client,
		rules:   rules.NewLoader(cwd, cfg.Rules.Files),
		git:     git.NewContextProvider(cwd),
		builder: prompt.NewBuilder(cfg.Refinement.MaxTokens),
		cache:   cache.NewStore(cfg.Cache.Dir, cfg.Cache.Enabled),
		cfg:     cfg,
	}, nil
}

// SetSessionProvider attaches a session context provider and session ID.
func (p *Pipeline) SetSessionProvider(sp SessionProvider, sessionID string) {
	p.session = sp
	p.sessionID = sessionID
}

// SetMapLoader attaches a deep-context map loader for retrieval-augmented refinement.
func (p *Pipeline) SetMapLoader(ml MapLoader) {
	p.mapLoader = ml
}

// SetInputReadyCallback sets a function called after the LLM prompt is built
// but before inference starts. Used to broadcast the input prompt in real-time.
func (p *Pipeline) SetInputReadyCallback(fn func(inputPrompt string)) {
	p.onInputReady = fn
}

// NewWithDeps creates a Pipeline with injected dependencies (for testing).
func NewWithDeps(llm LLMClient, rl RulesLoader, gp GitProvider, cs CacheStore, cfg *config.Config) *Pipeline {
	return &Pipeline{
		llm:     llm,
		rules:   rl,
		git:     gp,
		builder: prompt.NewBuilder(cfg.Refinement.MaxTokens),
		cache:   cs,
		cfg:     cfg,
	}
}

// Refine takes a raw user prompt and returns structured, rules-aware additional context.
func (p *Pipeline) Refine(ctx context.Context, rawPrompt string, sink ollama.TokenSink) (*RefineResult, error) {
	start := time.Now()
	result := &RefineResult{}
	timer := func(stage string, fn func()) {
		t0 := time.Now()
		fn()
		d := time.Since(t0)
		result.Timings = append(result.Timings, TimingResult{Stage: stage, Duration: d})
		slog.Debug("pipeline stage complete", "stage", stage, "duration", d)
	}

	// 1. Load project rules
	var rulesContent string
	var rulesHash string
	timer("rules_load", func() {
		var err error
		rulesContent, err = p.rules.Load()
		if err != nil {
			slog.Warn("rules load failed, continuing without rules", "error", err)
			rulesContent = ""
		}
		rulesHash, _ = p.rules.Hash()
		if rulesContent != "" {
			hashPreview := rulesHash
			if len(hashPreview) > 12 {
				hashPreview = hashPreview[:12]
			}
			slog.Debug("rules loaded", "hash", hashPreview, "length", len(rulesContent))
		} else {
			slog.Debug("no rules files found")
		}
	})

	// 2. Gather git context
	var gitCtx *git.Context
	timer("git_context", func() {
		var err error
		gitCtx, err = p.git.GetContext()
		if err != nil {
			slog.Warn("git context failed, continuing without", "error", err)
			gitCtx = &git.Context{}
			return
		}
		if gitCtx.Branch != "" {
			slog.Debug("git context gathered", "branch", gitCtx.Branch, "commits", len(gitCtx.RecentCommits))
		}
	})

	// 3. Build cache key
	cacheKey := buildCacheKey(rawPrompt, rulesHash)

	// 4. Check cache
	timer("cache_check", func() {
		if cached, ok := p.cache.Get(cacheKey, ""); ok {
			result.Refined = cached
			result.CacheHit = true
			slog.Info("cache hit", "prompt_words", len(strings.Fields(rawPrompt)))
		}
	})
	if result.CacheHit {
		result.TotalTime = time.Since(start)
		return result, nil
	}

	// 5. Gather session context
	var sessionCtx string
	timer("session_context", func() {
		if p.session == nil || p.sessionID == "" {
			return
		}
		clips, err := p.session.GetRecentIntents(p.sessionID, 5)
		if err != nil {
			slog.Warn("session context failed, continuing without", "error", err)
			return
		}
		sessionCtx = formatSessionClips(clips)
		if sessionCtx != "" {
			slog.Debug("session context gathered", "clips", len(clips))
		}
	})

	// 6. Prepare project map for LLM (if available)
	var projectMapStr string
	timer("map_load", func() {
		if p.mapLoader != nil {
			projectMapStr = p.mapLoader.FormatMapForLLM()
			if projectMapStr != "" {
				slog.Debug("project map loaded for LLM", "length", len(projectMapStr))
			}
		}
	})

	// 7. Build LLM messages (with numbered rules + project map)
	var buildResult *prompt.BuildResult
	timer("prompt_build", func() {
		buildResult = p.builder.Build(rawPrompt, rulesContent, gitCtx.String(), sessionCtx, projectMapStr)
	})
	result.InputPrompt = "## System Prompt\n" + buildResult.SystemMsg + "\n\n## User Message\n" + buildResult.UserMsg

	// Notify that the input prompt is ready (for real-time broadcast)
	if p.onInputReady != nil {
		p.onInputReady(result.InputPrompt)
	}

	// 7. Check Ollama availability
	timer("ollama_check", func() {
		if !p.llm.IsAvailable(ctx) {
			slog.Warn("ollama not available, cannot refine")
		}
	})
	if !p.llm.IsAvailable(ctx) {
		return nil, fmt.Errorf("ollama is not available at configured URL")
	}

	// 8. Ensure model is loaded
	timer("model_ensure", func() {
		if err := p.llm.EnsureModel(ctx); err != nil {
			slog.Warn("model ensure failed", "error", err)
		}
	})

	// 9. Call LLM
	promptWords := len(strings.Fields(rawPrompt))
	slog.Info("refining prompt", "words", promptWords, "model", p.cfg.Ollama.Model)

	var llmRaw string
	var llmErr error
	timer("ollama_inference", func() {
		llmRaw, llmErr = p.llm.ChatWithRetry(
			ctx, buildResult.SystemMsg, buildResult.UserMsg,
			float32(p.cfg.Refinement.Temperature),
			p.cfg.Refinement.MaxTokens,
			sink,
		)
	})
	if llmErr != nil {
		return nil, fmt.Errorf("ollama inference: %w", llmErr)
	}
	result.LLMOutput = llmRaw

	// 10. Check for no-context sentinel
	if strings.TrimSpace(llmRaw) == NoContextSentinel {
		result.NoContext = true
		result.Refined = ""
		result.TotalTime = time.Since(start)
		slog.Info("LLM determined no additional context needed")
		return result, nil
	}

	// 11. Parse LLM JSON output
	var classification *LLMClassification
	timer("parse", func() {
		var err error
		classification, err = parseLLMOutput(llmRaw)
		if err != nil {
			slog.Warn("LLM output parse failed", "error", err)
			llmErr = fmt.Errorf("parse: %w", err)
		}
	})
	if llmErr != nil {
		return nil, llmErr
	}

	// 12. Load scoped rules from selected documents (if LLM selected any)
	var scopedRules map[string]*prompt.ParsedRules
	if p.mapLoader != nil && len(classification.RelevantDocs) > 0 {
		timer("doc_load", func() {
			var err error
			_, scopedRules, err = p.mapLoader.LoadSelected(classification.RelevantDocs)
			if err != nil {
				slog.Warn("failed to load selected documents", "error", err)
			}
			result.SelectedDocs = classification.RelevantDocs
			result.DocSources = p.mapLoader.SelectedSources(classification.RelevantDocs)
			slog.Debug("loaded scoped rules", "docs", len(classification.RelevantDocs), "sources", result.DocSources)
		})
	}

	// 13. Compose final context XML from classification + static data + git
	var composed string
	timer("compose", func() {
		composed = composeContext(classification, buildResult.Rules, scopedRules, gitCtx.Branch, rawPrompt)
	})

	// 13. Cache result
	timer("cache_write", func() {
		if err := p.cache.Put(cacheKey, "", composed); err != nil {
			slog.Warn("cache write failed", "error", err)
		}
	})

	result.Refined = composed
	result.TotalTime = time.Since(start)

	slog.Info("refinement complete",
		"input_words", promptWords,
		"output_words", len(strings.Fields(composed)),
		"type", classification.Type,
		"total_time", result.TotalTime,
		"cache_hit", false,
	)

	return result, nil
}

// --- LLM Output Parsing ---

// LLMClassification is the JSON structure the local LLM produces.
type LLMClassification struct {
	Type                 string   `json:"type"`
	Intent               string   `json:"intent"`
	RecentActivity       string   `json:"recent_activity"`
	Analysis             []string `json:"analysis"`
	RelevantRules        []int    `json:"relevant_rules"`
	RelevantConstraints  []int    `json:"relevant_constraints"`
	RelevantAntiPats     []int    `json:"relevant_anti_patterns"`
	Clarification        []string `json:"clarification"`
	RelevantDocs         []int    `json:"relevant_docs"` // indices into project map
}

// validTypes is the set of recognized request types.
var validTypes = map[string]bool{
	"code_change": true,
	"refactor":    true,
	"debug":       true,
	"question":    true,
	"docs":        true,
}

// parseLLMOutput extracts the JSON classification from the LLM's raw output.
// Handles common LLM quirks: markdown fences, trailing text, BOM.
func parseLLMOutput(raw string) (*LLMClassification, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty output")
	}

	// Strip markdown code fences if present
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		// Remove first line (```json or ```) and last line (```)
		start := 1
		end := len(lines)
		if end > 0 && strings.TrimSpace(lines[end-1]) == "```" {
			end--
		}
		raw = strings.Join(lines[start:end], "\n")
	}

	// Find JSON object boundaries
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON object found in output")
	}
	raw = raw[start : end+1]

	var c LLMClassification
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Validate required fields
	if c.Intent == "" {
		return nil, fmt.Errorf("missing required field: intent")
	}
	if !validTypes[c.Type] {
		// Default to code_change if type is unrecognized
		slog.Warn("unrecognized type, defaulting to code_change", "type", c.Type)
		c.Type = "code_change"
	}

	return &c, nil
}

// --- Helpers ---

func buildCacheKey(rawPrompt, rulesHash string) string {
	h := sha256.New()
	h.Write([]byte(rawPrompt))
	h.Write([]byte(rulesHash))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func formatSessionClips(clips []db.SessionClip) string {
	if len(clips) == 0 {
		return ""
	}
	var b strings.Builder
	for _, c := range clips {
		age := formatAge(c.AgoSec)
		intent := c.Intent
		if intent == "" {
			intent = c.RawPrompt
			if len(intent) > 100 {
				intent = intent[:100] + "..."
			}
		}
		fmt.Fprintf(&b, "- %s ago: %s\n", age, intent)
	}
	return b.String()
}

func formatAge(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	return fmt.Sprintf("%dh", seconds/3600)
}
