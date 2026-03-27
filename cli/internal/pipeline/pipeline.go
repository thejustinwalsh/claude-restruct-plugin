package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/tjw/restruct/internal/cache"
	"github.com/tjw/restruct/internal/config"
	"github.com/tjw/restruct/internal/git"
	"github.com/tjw/restruct/internal/ollama"
	"github.com/tjw/restruct/internal/prompt"
	"github.com/tjw/restruct/internal/rules"
)

// LLMClient is the interface the pipeline uses for LLM communication.
// Allows mocking in tests.
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
	Refined   string
	CacheHit  bool
	NoContext bool // LLM determined no additional context needed
	Timings   []TimingResult
	TotalTime time.Duration
}

// NoContextSentinel is the literal string the local LLM outputs when
// the request is clear and no project rules apply.
const NoContextSentinel = "NO_ADDITIONAL_CONTEXT"

// Pipeline orchestrates the prompt refinement process.
type Pipeline struct {
	llm     LLMClient
	rules   RulesLoader
	git     GitProvider
	builder *prompt.Builder
	cache   CacheStore
	cfg     *config.Config
}

// New creates a Pipeline from the given configuration.
func New(cfg *config.Config) (*Pipeline, error) {
	cwd := "."
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
// On any failure, returns an error — the caller (cmd/refine.go) decides to passthrough.
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

	// 2. Check cache
	timer("cache_check", func() {
		if cached, ok := p.cache.Get(rawPrompt, rulesHash); ok {
			result.Refined = cached
			result.CacheHit = true
			slog.Info("cache hit", "prompt_words", len(strings.Fields(rawPrompt)))
		}
	})
	if result.CacheHit {
		result.TotalTime = time.Since(start)
		return result, nil
	}

	// 3. Gather git context
	var gitStr string
	timer("git_context", func() {
		gitCtx, err := p.git.GetContext()
		if err != nil {
			slog.Warn("git context failed, continuing without", "error", err)
			return
		}
		gitStr = gitCtx.String()
		if gitStr != "" {
			slog.Debug("git context gathered", "branch", gitCtx.Branch, "commits", len(gitCtx.RecentCommits))
		}
	})

	// 4. Build LLM messages
	var systemMsg, userMsg string
	timer("prompt_build", func() {
		systemMsg, userMsg = p.builder.Build(rawPrompt, rulesContent, gitStr)
	})

	// 5. Check Ollama availability (fast — uses connect timeout)
	timer("ollama_check", func() {
		if !p.llm.IsAvailable(ctx) {
			slog.Warn("ollama not available, cannot refine")
		}
	})
	if !p.llm.IsAvailable(ctx) {
		return nil, fmt.Errorf("ollama is not available at configured URL")
	}

	// 6. Ensure model is loaded
	timer("model_ensure", func() {
		if err := p.llm.EnsureModel(ctx); err != nil {
			slog.Warn("model ensure failed", "error", err)
		}
	})

	// 7. Call LLM (streaming with retry and stall detection)
	promptWords := len(strings.Fields(rawPrompt))
	slog.Info("refining prompt", "words", promptWords, "model", p.cfg.Ollama.Model)

	var refined string
	var llmErr error
	timer("ollama_inference", func() {
		refined, llmErr = p.llm.ChatWithRetry(
			ctx, systemMsg, userMsg,
			float32(p.cfg.Refinement.Temperature),
			p.cfg.Refinement.MaxTokens,
			sink,
		)
	})
	if llmErr != nil {
		return nil, fmt.Errorf("ollama inference: %w", llmErr)
	}

	// 8. Check for no-context sentinel
	if strings.TrimSpace(refined) == NoContextSentinel {
		result.NoContext = true
		result.Refined = ""
		result.TotalTime = time.Since(start)
		slog.Info("LLM determined no additional context needed")
		return result, nil
	}

	// 9. Validate output
	timer("validation", func() {
		if err := validateOutput(refined, rawPrompt); err != nil {
			slog.Warn("output validation failed, discarding", "error", err)
			llmErr = fmt.Errorf("validation: %w", err)
		}
	})
	if llmErr != nil {
		return nil, llmErr
	}

	// 9. Cache result
	timer("cache_write", func() {
		if err := p.cache.Put(rawPrompt, rulesHash, refined); err != nil {
			slog.Warn("cache write failed", "error", err)
		}
	})

	result.Refined = refined
	result.TotalTime = time.Since(start)

	slog.Info("refinement complete",
		"input_words", promptWords,
		"output_words", len(strings.Fields(refined)),
		"total_time", result.TotalTime,
		"cache_hit", false,
	)

	return result, nil
}

// validateOutput checks that the LLM's output is usable.
func validateOutput(refined, rawPrompt string) error {
	refined = strings.TrimSpace(refined)
	if refined == "" {
		return fmt.Errorf("empty output")
	}
	if len(refined) < len(rawPrompt) {
		return fmt.Errorf("output shorter than input (%d < %d bytes)", len(refined), len(rawPrompt))
	}
	// Check for system prompt leakage
	leakMarkers := []string{
		"You generate supplementary execution context",
		"## What to produce",
		"output is appended AFTER the developer",
	}
	for _, marker := range leakMarkers {
		if strings.Contains(refined, marker) {
			return fmt.Errorf("system prompt leak detected: contains %q", marker)
		}
	}
	return nil
}
