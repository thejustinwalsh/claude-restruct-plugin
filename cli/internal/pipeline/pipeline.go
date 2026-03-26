package pipeline

import (
	"context"
	"fmt"

	"github.com/tjw/restruct/internal/cache"
	"github.com/tjw/restruct/internal/config"
	"github.com/tjw/restruct/internal/git"
	"github.com/tjw/restruct/internal/ollama"
	"github.com/tjw/restruct/internal/prompt"
	"github.com/tjw/restruct/internal/rules"
)

// Pipeline orchestrates the prompt refinement process.
type Pipeline struct {
	rules   *rules.Loader
	git     *git.ContextProvider
	ollama  *ollama.Client
	builder *prompt.Builder
	cache   *cache.Store
	cfg     *config.Config
}

// New creates a Pipeline from the given configuration.
func New(cfg *config.Config) (*Pipeline, error) {
	cwd := "."
	client, err := ollama.NewClient(cfg.Ollama.URL, cfg.Ollama.Model, cfg.Ollama.Timeout, cfg.Ollama.KeepAlive)
	if err != nil {
		return nil, fmt.Errorf("create ollama client: %w", err)
	}
	return &Pipeline{
		rules:   rules.NewLoader(cwd, cfg.Rules.Files),
		git:     git.NewContextProvider(cwd),
		ollama:  client,
		builder: prompt.NewBuilder(),
		cache:   cache.NewStore(cfg.Cache.Dir, cfg.Cache.Enabled),
		cfg:     cfg,
	}, nil
}

// Refine takes a raw user prompt and returns a structured, rules-aware prompt.
func (p *Pipeline) Refine(ctx context.Context, rawPrompt string) (string, error) {
	// Load project rules
	rulesContent, err := p.rules.Load()
	if err != nil {
		rulesContent = ""
	}

	// Check cache
	rulesHash, _ := p.rules.Hash()
	if cached, ok := p.cache.Get(rawPrompt, rulesHash); ok {
		return cached, nil
	}

	// Gather git context
	gitCtx, err := p.git.GetContext()
	gitStr := ""
	if err == nil {
		gitStr = gitCtx.String()
	}

	// Build the LLM messages
	systemMsg, userMsg := p.builder.Build(rawPrompt, rulesContent, gitStr)

	// Check Ollama availability
	if !p.ollama.IsAvailable(ctx) {
		return "", fmt.Errorf("ollama is not available")
	}

	// Call the local LLM
	refined, err := p.ollama.Chat(ctx, systemMsg, userMsg,
		float32(p.cfg.Refinement.Temperature),
		p.cfg.Refinement.MaxTokens,
	)
	if err != nil {
		return "", err
	}

	// Cache the result
	p.cache.Put(rawPrompt, rulesHash, refined)

	return refined, nil
}
