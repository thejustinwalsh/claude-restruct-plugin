package pipeline

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tjw/restruct/internal/config"
	"github.com/tjw/restruct/internal/git"
	"github.com/tjw/restruct/internal/ollama"
)

// --- Mock implementations ---

type mockLLM struct {
	available    bool
	ensureErr    error
	chatResponse string
	chatErr      error
	chatDelay    time.Duration
}

func (m *mockLLM) IsAvailable(ctx context.Context) bool { return m.available }
func (m *mockLLM) EnsureModel(ctx context.Context) error { return m.ensureErr }
func (m *mockLLM) ChatWithRetry(ctx context.Context, system, user string, temp float32, maxTokens int, sink ollama.TokenSink) (string, error) {
	if m.chatDelay > 0 {
		select {
		case <-time.After(m.chatDelay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return m.chatResponse, m.chatErr
}

type mockRules struct {
	content string
	hash    string
	loadErr error
}

func (m *mockRules) Load() (string, error) { return m.content, m.loadErr }
func (m *mockRules) Hash() (string, error) {
	if m.hash != "" {
		return m.hash, nil
	}
	return "fakehash", nil
}

type mockGit struct {
	ctx    *git.Context
	getErr error
}

func (m *mockGit) GetContext() (*git.Context, error) { return m.ctx, m.getErr }

type mockCache struct {
	store map[string]string
}

func newMockCache() *mockCache          { return &mockCache{store: make(map[string]string)} }
func (m *mockCache) Get(raw, hash string) (string, bool) {
	v, ok := m.store[raw+hash]
	return v, ok
}
func (m *mockCache) Put(raw, hash, refined string) error {
	m.store[raw+hash] = refined
	return nil
}

func defaultCfg() *config.Config {
	cfg := config.Defaults()
	return cfg
}

// --- Tests ---

func TestRefine_HappyPath(t *testing.T) {
	llm := &mockLLM{
		available:    true,
		chatResponse: "<structured_prompt><objective>Fix the authentication token expiry bug</objective></structured_prompt>",
	}
	rules := &mockRules{content: "## Rules\n- Always test auth changes", hash: "abc123"}
	gitP := &mockGit{ctx: &git.Context{Branch: "main", RecentCommits: []string{"abc fix auth"}}}
	cache := newMockCache()

	p := NewWithDeps(llm, rules, gitP, cache, defaultCfg())
	result, err := p.Refine(context.Background(), "fix the auth bug where tokens expire too fast")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Refined == "" {
		t.Fatal("expected non-empty refined output")
	}
	if result.CacheHit {
		t.Error("should not be a cache hit on first call")
	}
	if len(result.Timings) == 0 {
		t.Error("expected timing data")
	}
	if result.TotalTime == 0 {
		t.Error("expected non-zero total time")
	}
}

func TestRefine_CacheHit(t *testing.T) {
	llm := &mockLLM{available: true}
	rules := &mockRules{hash: "abc123"}
	cache := newMockCache()
	cache.store["fix the auth bugabc123"] = "cached refinement"

	p := NewWithDeps(llm, rules, &mockGit{ctx: &git.Context{}}, cache, defaultCfg())
	result, err := p.Refine(context.Background(), "fix the auth bug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.CacheHit {
		t.Error("expected cache hit")
	}
	if result.Refined != "cached refinement" {
		t.Errorf("got %q, want %q", result.Refined, "cached refinement")
	}
}

func TestRefine_OllamaUnavailable(t *testing.T) {
	llm := &mockLLM{available: false}
	p := NewWithDeps(llm, &mockRules{}, &mockGit{ctx: &git.Context{}}, newMockCache(), defaultCfg())
	_, err := p.Refine(context.Background(), "fix the auth bug where tokens expire")
	if err == nil {
		t.Fatal("expected error when Ollama unavailable")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("error should mention availability, got: %v", err)
	}
}

func TestRefine_OllamaError(t *testing.T) {
	llm := &mockLLM{
		available: true,
		chatErr:   fmt.Errorf("503 Service Unavailable"),
	}
	p := NewWithDeps(llm, &mockRules{}, &mockGit{ctx: &git.Context{}}, newMockCache(), defaultCfg())
	_, err := p.Refine(context.Background(), "fix the auth bug where tokens expire")
	if err == nil {
		t.Fatal("expected error on Ollama failure")
	}
}

func TestRefine_EmptyOutput(t *testing.T) {
	llm := &mockLLM{
		available:    true,
		chatResponse: "",
	}
	p := NewWithDeps(llm, &mockRules{}, &mockGit{ctx: &git.Context{}}, newMockCache(), defaultCfg())
	_, err := p.Refine(context.Background(), "fix the auth bug where tokens expire")
	if err == nil {
		t.Fatal("expected validation error for empty output")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention empty, got: %v", err)
	}
}

func TestRefine_OutputShorterThanInput(t *testing.T) {
	llm := &mockLLM{
		available:    true,
		chatResponse: "ok",
	}
	p := NewWithDeps(llm, &mockRules{}, &mockGit{ctx: &git.Context{}}, newMockCache(), defaultCfg())
	_, err := p.Refine(context.Background(), "fix the authentication token expiry bug in the user session module")
	if err == nil {
		t.Fatal("expected validation error for too-short output")
	}
	if !strings.Contains(err.Error(), "shorter") {
		t.Errorf("error should mention shorter, got: %v", err)
	}
}

func TestRefine_SystemPromptLeak(t *testing.T) {
	llm := &mockLLM{
		available:    true,
		chatResponse: "You are a Prompt Architect that helps transform prompts into structured format with proper objectives and constraints",
	}
	p := NewWithDeps(llm, &mockRules{}, &mockGit{ctx: &git.Context{}}, newMockCache(), defaultCfg())
	_, err := p.Refine(context.Background(), "fix the auth bug")
	if err == nil {
		t.Fatal("expected validation error for system prompt leak")
	}
	if !strings.Contains(err.Error(), "leak") {
		t.Errorf("error should mention leak, got: %v", err)
	}
}

func TestRefine_EmptyRules(t *testing.T) {
	llm := &mockLLM{
		available:    true,
		chatResponse: "<structured_prompt><objective>Fix the bug</objective><workflow>1. investigate</workflow></structured_prompt>",
	}
	rules := &mockRules{content: "", loadErr: fmt.Errorf("no rules files found")}
	p := NewWithDeps(llm, rules, &mockGit{ctx: &git.Context{}}, newMockCache(), defaultCfg())
	result, err := p.Refine(context.Background(), "fix the auth bug where tokens expire")
	if err != nil {
		t.Fatalf("should succeed without rules, got: %v", err)
	}
	if result.Refined == "" {
		t.Error("should still produce output without rules")
	}
}

func TestRefine_GitContextFails(t *testing.T) {
	llm := &mockLLM{
		available:    true,
		chatResponse: "<structured_prompt><objective>Fix the bug</objective><workflow>1. investigate</workflow></structured_prompt>",
	}
	gitP := &mockGit{ctx: &git.Context{}, getErr: fmt.Errorf("not a git repo")}
	p := NewWithDeps(llm, &mockRules{}, gitP, newMockCache(), defaultCfg())
	result, err := p.Refine(context.Background(), "fix the auth bug where tokens expire")
	if err != nil {
		t.Fatalf("should succeed without git context, got: %v", err)
	}
	if result.Refined == "" {
		t.Error("should still produce output without git context")
	}
}

func TestRefine_ContextCancelled(t *testing.T) {
	llm := &mockLLM{
		available:  true,
		chatDelay:  5 * time.Second,
	}
	p := NewWithDeps(llm, &mockRules{}, &mockGit{ctx: &git.Context{}}, newMockCache(), defaultCfg())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := p.Refine(ctx, "fix the auth bug where tokens expire")
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
}

func TestRefine_LongRunningSuccess(t *testing.T) {
	// A refinement that takes 200ms should complete (not be aborted by timeouts)
	llm := &mockLLM{
		available:    true,
		chatResponse: "<structured_prompt><objective>Fix the authentication token expiry bug in the user session module</objective></structured_prompt>",
		chatDelay:    200 * time.Millisecond,
	}
	p := NewWithDeps(llm, &mockRules{}, &mockGit{ctx: &git.Context{}}, newMockCache(), defaultCfg())
	result, err := p.Refine(context.Background(), "fix the auth bug")
	if err != nil {
		t.Fatalf("long-running refinement should succeed, got: %v", err)
	}
	if result.TotalTime < 200*time.Millisecond {
		t.Errorf("expected at least 200ms, got %v", result.TotalTime)
	}
}

func TestRefine_ModelEnsureFailsGracefully(t *testing.T) {
	llm := &mockLLM{
		available:    true,
		ensureErr:    fmt.Errorf("model not found"),
		chatResponse: "<structured_prompt><objective>Fix it</objective><workflow>1. investigate</workflow></structured_prompt>",
	}
	p := NewWithDeps(llm, &mockRules{}, &mockGit{ctx: &git.Context{}}, newMockCache(), defaultCfg())
	// Should still attempt chat even if ensure fails (model might already be loaded)
	result, err := p.Refine(context.Background(), "fix the auth bug where tokens expire")
	if err != nil {
		t.Fatalf("should attempt chat even after ensure failure: %v", err)
	}
	if result.Refined == "" {
		t.Error("expected output")
	}
}

func TestValidateOutput(t *testing.T) {
	tests := []struct {
		name    string
		refined string
		raw     string
		wantErr bool
	}{
		{"valid", "<structured_prompt>good output here</structured_prompt>", "fix bug", false},
		{"empty", "", "fix bug", true},
		{"whitespace only", "   \n  ", "fix bug", true},
		{"shorter than input", "ok", "fix the authentication bug in the module", true},
		{"system prompt leak", "You are a Prompt Architect and you should...", "fix", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOutput(tt.refined, tt.raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateOutput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
