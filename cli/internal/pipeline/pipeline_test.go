package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tjw/restruct/internal/config"
	"github.com/tjw/restruct/internal/db"
	gitpkg "github.com/tjw/restruct/internal/git"
	"github.com/tjw/restruct/internal/ollama"
	"github.com/tjw/restruct/internal/prompt"
	"github.com/tjw/restruct/internal/rules"
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
	ctx    *gitpkg.Context
	getErr error
}

func (m *mockGit) GetContext() (*gitpkg.Context, error) { return m.ctx, m.getErr }

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

type mockSession struct {
	clips  []db.SessionClip
	getErr error
}

func (m *mockSession) GetRecentIntents(sessionID string, limit int) ([]db.SessionClip, error) {
	return m.clips, m.getErr
}

func defaultCfg() *config.Config {
	cfg := config.Defaults()
	return cfg
}

// validJSON is a helper that produces valid LLM JSON output for tests.
func validJSON(t string, intent string) string {
	return fmt.Sprintf(`{"type":"%s","intent":"%s","analysis":["test observation"],"relevant_rules":[],"relevant_anti_patterns":[],"clarification":[]}`, t, intent)
}

// --- Tests ---

func TestRefine_HappyPath(t *testing.T) {
	llm := &mockLLM{
		available:    true,
		chatResponse: validJSON("code_change", "Fix the authentication token expiry bug"),
	}
	rules := &mockRules{content: "## Code\n- Always test auth changes", hash: "abc123"}
	gitP := &mockGit{ctx: &gitpkg.Context{Branch: "main", RecentCommits: []string{"abc fix auth"}}}
	cache := newMockCache()

	p := NewWithDeps(llm, rules, gitP, cache, defaultCfg())
	result, err := p.Refine(context.Background(), "fix the auth bug where tokens expire too fast", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Refined == "" {
		t.Fatal("expected non-empty refined output")
	}
	if !strings.Contains(result.Refined, "<context type=") {
		t.Error("refined output should contain <context type=...> XML")
	}
	if !strings.Contains(result.Refined, "<intent>") {
		t.Error("refined output should contain <intent>")
	}
	if result.CacheHit {
		t.Error("should not be a cache hit on first call")
	}
	if len(result.Timings) == 0 {
		t.Error("expected timing data")
	}
}

func TestRefine_CacheHit(t *testing.T) {
	llm := &mockLLM{available: true}
	rules := &mockRules{hash: "abc123"}
	gitCtx := &gitpkg.Context{}
	cache := newMockCache()

	key := buildCacheKey("fix the auth bug", "abc123")
	cache.store[key] = "cached refinement"

	p := NewWithDeps(llm, rules, &mockGit{ctx: gitCtx}, cache, defaultCfg())
	result, err := p.Refine(context.Background(), "fix the auth bug", nil)
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
	p := NewWithDeps(llm, &mockRules{}, &mockGit{ctx: &gitpkg.Context{}}, newMockCache(), defaultCfg())
	_, err := p.Refine(context.Background(), "fix the auth bug where tokens expire", nil)
	if err == nil {
		t.Fatal("expected error when Ollama unavailable")
	}
}

func TestRefine_OllamaError(t *testing.T) {
	llm := &mockLLM{
		available: true,
		chatErr:   fmt.Errorf("503 Service Unavailable"),
	}
	p := NewWithDeps(llm, &mockRules{}, &mockGit{ctx: &gitpkg.Context{}}, newMockCache(), defaultCfg())
	_, err := p.Refine(context.Background(), "fix the auth bug where tokens expire", nil)
	if err == nil {
		t.Fatal("expected error on Ollama failure")
	}
}

func TestRefine_EmptyOutput(t *testing.T) {
	llm := &mockLLM{
		available:    true,
		chatResponse: "",
	}
	p := NewWithDeps(llm, &mockRules{}, &mockGit{ctx: &gitpkg.Context{}}, newMockCache(), defaultCfg())
	_, err := p.Refine(context.Background(), "fix the auth bug where tokens expire", nil)
	if err == nil {
		t.Fatal("expected parse error for empty output")
	}
}

func TestRefine_InvalidJSON(t *testing.T) {
	llm := &mockLLM{
		available:    true,
		chatResponse: "this is not json",
	}
	p := NewWithDeps(llm, &mockRules{}, &mockGit{ctx: &gitpkg.Context{}}, newMockCache(), defaultCfg())
	_, err := p.Refine(context.Background(), "fix the auth bug where tokens expire", nil)
	if err == nil {
		t.Fatal("expected parse error for invalid JSON")
	}
}

func TestRefine_EmptyRules(t *testing.T) {
	llm := &mockLLM{
		available:    true,
		chatResponse: validJSON("code_change", "Fix the bug in the auth module"),
	}
	rules := &mockRules{content: "", loadErr: fmt.Errorf("no rules files found")}
	p := NewWithDeps(llm, rules, &mockGit{ctx: &gitpkg.Context{}}, newMockCache(), defaultCfg())
	result, err := p.Refine(context.Background(), "fix the auth bug where tokens expire", nil)
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
		chatResponse: validJSON("debug", "Investigate the auth token issue"),
	}
	gitP := &mockGit{ctx: &gitpkg.Context{}, getErr: fmt.Errorf("not a git repo")}
	p := NewWithDeps(llm, &mockRules{}, gitP, newMockCache(), defaultCfg())
	result, err := p.Refine(context.Background(), "fix the auth bug where tokens expire", nil)
	if err != nil {
		t.Fatalf("should succeed without git context, got: %v", err)
	}
	if result.Refined == "" {
		t.Error("should still produce output without git context")
	}
}

func TestRefine_ContextCancelled(t *testing.T) {
	llm := &mockLLM{
		available: true,
		chatDelay: 5 * time.Second,
	}
	p := NewWithDeps(llm, &mockRules{}, &mockGit{ctx: &gitpkg.Context{}}, newMockCache(), defaultCfg())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := p.Refine(ctx, "fix the auth bug where tokens expire", nil)
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
}

func TestRefine_ModelEnsureFailsGracefully(t *testing.T) {
	llm := &mockLLM{
		available:    true,
		ensureErr:    fmt.Errorf("model not found"),
		chatResponse: validJSON("code_change", "Fix the authentication issue"),
	}
	p := NewWithDeps(llm, &mockRules{}, &mockGit{ctx: &gitpkg.Context{}}, newMockCache(), defaultCfg())
	result, err := p.Refine(context.Background(), "fix the auth bug where tokens expire", nil)
	if err != nil {
		t.Fatalf("should attempt chat even after ensure failure: %v", err)
	}
	if result.Refined == "" {
		t.Error("expected output")
	}
}

func TestRefine_WithSessionProvider(t *testing.T) {
	llm := &mockLLM{
		available:    true,
		chatResponse: validJSON("code_change", "Fix the authentication token expiry bug"),
	}
	session := &mockSession{
		clips: []db.SessionClip{
			{Intent: "Previous task context", AgoSec: 300},
		},
	}
	p := NewWithDeps(llm, &mockRules{}, &mockGit{ctx: &gitpkg.Context{}}, newMockCache(), defaultCfg())
	p.SetSessionProvider(session, "test-session")

	result, err := p.Refine(context.Background(), "fix the auth bug where tokens expire", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Refined == "" {
		t.Error("expected non-empty refined output")
	}
}

func TestRefine_NoContextSentinel(t *testing.T) {
	llm := &mockLLM{
		available:    true,
		chatResponse: "NO_ADDITIONAL_CONTEXT",
	}
	p := NewWithDeps(llm, &mockRules{}, &mockGit{ctx: &gitpkg.Context{}}, newMockCache(), defaultCfg())
	result, err := p.Refine(context.Background(), "add a README", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.NoContext {
		t.Error("expected NoContext=true for NO_ADDITIONAL_CONTEXT sentinel")
	}
	if result.Refined != "" {
		t.Errorf("expected empty Refined, got %q", result.Refined)
	}
}

// --- parseLLMOutput tests ---

func TestParseLLMOutput_Valid(t *testing.T) {
	input := `{"type":"code_change","intent":"Fix the bug","analysis":["check auth"],"relevant_rules":[1,3],"relevant_anti_patterns":[2],"clarification":[]}`
	c, err := parseLLMOutput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Type != "code_change" {
		t.Errorf("type = %q, want code_change", c.Type)
	}
	if c.Intent != "Fix the bug" {
		t.Errorf("intent = %q, want 'Fix the bug'", c.Intent)
	}
	if len(c.RelevantRules) != 2 {
		t.Errorf("relevant_rules length = %d, want 2", len(c.RelevantRules))
	}
}

func TestParseLLMOutput_MarkdownFences(t *testing.T) {
	input := "```json\n" + `{"type":"question","intent":"How does caching work?","analysis":[],"relevant_rules":[],"relevant_anti_patterns":[],"clarification":[]}` + "\n```"
	c, err := parseLLMOutput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Type != "question" {
		t.Errorf("type = %q, want question", c.Type)
	}
}

func TestParseLLMOutput_ExtraText(t *testing.T) {
	input := `Here is the classification: {"type":"debug","intent":"Investigate the crash","analysis":[],"relevant_rules":[],"relevant_anti_patterns":[],"clarification":[]} hope that helps!`
	c, err := parseLLMOutput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Type != "debug" {
		t.Errorf("type = %q, want debug", c.Type)
	}
}

func TestParseLLMOutput_Empty(t *testing.T) {
	_, err := parseLLMOutput("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseLLMOutput_NoJSON(t *testing.T) {
	_, err := parseLLMOutput("this has no json at all")
	if err == nil {
		t.Error("expected error for non-JSON input")
	}
}

func TestParseLLMOutput_MissingIntent(t *testing.T) {
	_, err := parseLLMOutput(`{"type":"code_change","analysis":[]}`)
	if err == nil {
		t.Error("expected error for missing intent")
	}
}

func TestParseLLMOutput_UnknownType(t *testing.T) {
	c, err := parseLLMOutput(`{"type":"banana","intent":"Do something","analysis":[],"relevant_rules":[],"relevant_anti_patterns":[],"clarification":[]}`)
	if err != nil {
		t.Fatalf("unknown type should not error, got: %v", err)
	}
	if c.Type != "code_change" {
		t.Errorf("unknown type should default to code_change, got %q", c.Type)
	}
}

// --- composeContext tests ---

func TestComposeContext_CodeChange(t *testing.T) {
	c := &LLMClassification{
		Type:           "code_change",
		Intent:         "Fix the auth bug",
		RecentActivity: "Auth middleware refactor, token refresh fix.",
		Analysis:       []string{"Check token config", "Review middleware"},
		RelevantRules:    []int{1, 2},
		RelevantAntiPats: []int{1},
	}
	rules := &prompt.ParsedRules{
		ContextRules: []string{"JWT config in config/auth.ts", "Use RefreshTokenService"},
		AntiPatterns: []string{"Do not hardcode tokens"},
	}

	result := composeContext(c, rules, "main")

	if !strings.Contains(result, `type="code_change"`) {
		t.Error("missing type attribute")
	}
	if !strings.Contains(result, "Fix the auth bug") || !strings.Contains(result, "<intent>") {
		t.Error("missing intent")
	}
	if !strings.Contains(result, "JWT config") {
		t.Error("missing selected context rule")
	}
	if !strings.Contains(result, "Plan first") {
		t.Error("missing protocol for code_change")
	}
	if !strings.Contains(result, "Check token config") {
		t.Error("missing analysis")
	}
	if !strings.Contains(result, "Do not hardcode tokens") {
		t.Error("missing anti-pattern")
	}
	if !strings.Contains(result, "Branch: main") || !strings.Contains(result, "Auth middleware refactor") {
		t.Error("missing repo_state with branch + recent activity")
	}
	if !strings.Contains(result, "<!-- How to approach this task") {
		t.Error("missing protocol annotation")
	}
}

func TestComposeContext_Question(t *testing.T) {
	c := &LLMClassification{
		Type:             "question",
		Intent:           "How does caching work?",
		Analysis:         []string{"Spans cache/ and pipeline/ packages"},
		RelevantAntiPats: []int{1}, // anti-patterns available for all types
	}
	rules := &prompt.ParsedRules{
		ContextRules: []string{"Some rule"},
		AntiPatterns: []string{"Some anti-pattern"},
	}

	result := composeContext(c, rules, "main")

	if !strings.Contains(result, `type="question"`) {
		t.Error("missing type attribute")
	}
	if !strings.Contains(result, "<intent>") {
		t.Error("questions should have intent")
	}
	// Protocol is universal — questions get it too
	if !strings.Contains(result, "<protocol>") {
		t.Error("questions should still get protocol")
	}
	// Workflow is impl-only
	if strings.Contains(result, "<workflow>") {
		t.Error("questions should NOT get workflow")
	}
	if !strings.Contains(result, "<anti_patterns>") {
		t.Error("questions SHOULD get anti_patterns when LLM selects them")
	}
	if !strings.Contains(result, "<analysis>") {
		t.Error("questions should still get analysis")
	}
}

func TestComposeContext_OutOfBoundsIndex(t *testing.T) {
	c := &LLMClassification{
		Type:          "code_change",
		Intent:        "Fix something",
		RelevantRules: []int{1, 99}, // 99 is out of bounds
	}
	rules := &prompt.ParsedRules{
		ContextRules: []string{"Only one rule"},
	}

	result := composeContext(c, rules, "main")

	if !strings.Contains(result, "Only one rule") {
		t.Error("valid index should be included")
	}
	// Should not panic on out-of-bounds
}

func TestComposeContext_WithWorkflow(t *testing.T) {
	c := &LLMClassification{
		Type:   "code_change",
		Intent: "Add new feature",
	}
	rules := &prompt.ParsedRules{
		WorkflowRules: []string{
			"If you added new behavior, add or update tests",
			"Prefer focused unit tests",
		},
	}

	result := composeContext(c, rules, "main")

	if !strings.Contains(result, "<workflow>") {
		t.Error("code_change with WorkflowRules should include <workflow>")
	}
	if !strings.Contains(result, "add or update tests") {
		t.Error("missing first workflow rule")
	}
	if !strings.Contains(result, "focused unit tests") {
		t.Error("missing second workflow rule")
	}
}

func TestComposeContext_WithLLMSelectedConstraints(t *testing.T) {
	c := &LLMClassification{
		Type:                "code_change",
		Intent:              "Optimize query performance",
		RelevantConstraints: []int{1, 2},
	}
	rules := &prompt.ParsedRules{
		ConstraintRules: []string{
			"Performance is critical",
			"Always map to CLI commands",
			"Third constraint not selected",
		},
	}

	result := composeContext(c, rules, "main")

	if !strings.Contains(result, "<constraints>") {
		t.Error("should include <constraints> when LLM selects them")
	}
	if !strings.Contains(result, "Performance is critical") {
		t.Error("missing first selected constraint")
	}
	if !strings.Contains(result, "Always map to CLI commands") {
		t.Error("missing second selected constraint")
	}
	if strings.Contains(result, "Third constraint") {
		t.Error("unselected constraint should not appear")
	}
}

func TestComposeContext_NoWorkflowForQuestion(t *testing.T) {
	c := &LLMClassification{
		Type:   "question",
		Intent: "How does X work?",
	}
	rules := &prompt.ParsedRules{
		WorkflowRules: []string{"Add tests for new behavior"},
	}

	result := composeContext(c, rules, "main")

	if strings.Contains(result, "<workflow>") {
		t.Error("question type should NOT include workflow")
	}
}

func TestBuildCacheKey_DeterministicForSameInput(t *testing.T) {
	key1 := buildCacheKey("fix the bug", "abc123")
	key2 := buildCacheKey("fix the bug", "abc123")
	if key1 != key2 {
		t.Error("same inputs should produce same cache key")
	}

	key3 := buildCacheKey("fix the bug", "def456")
	if key1 == key3 {
		t.Error("different rules hash should produce different cache key")
	}
}

func TestFormatSessionClips(t *testing.T) {
	clips := []db.SessionClip{
		{Intent: "Fix auth", AgoSec: 120},
		{Intent: "", RawPrompt: "do the thing", AgoSec: 3700},
	}
	result := formatSessionClips(clips)

	if !strings.Contains(result, "2m ago") {
		t.Error("expected '2m ago'")
	}
	if !strings.Contains(result, "1h ago") {
		t.Error("expected '1h ago'")
	}
	if !strings.Contains(result, "do the thing") {
		t.Error("expected raw prompt fallback")
	}
}

func TestFormatSessionClips_Empty(t *testing.T) {
	if formatSessionClips(nil) != "" {
		t.Error("expected empty for nil clips")
	}
}

// --- Integration tests using real project data ---

// repoRoot walks up from the test file's location to find the git root.
func repoRoot(t *testing.T) string {
	t.Helper()
	// Tests run from cli/ — walk up to find .git
	dir, err := os.Getwd()
	if err != nil {
		t.Skipf("cannot get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("not in a git repo, skipping integration test")
		}
		dir = parent
	}
}

func TestIntegration_RealCLAUDEmd(t *testing.T) {
	root := repoRoot(t)

	// Use the real rules loader against the project's CLAUDE.md
	rl := rules.NewLoader(root, []string{"CLAUDE.md"})
	content, err := rl.Load()
	if err != nil {
		t.Fatalf("rules.Load: %v", err)
	}
	if content == "" {
		t.Fatal("CLAUDE.md should produce non-empty rules content")
	}

	// Parse rules — should find all three categories
	parsed := prompt.ParseRules(content)

	t.Logf("Workflow: %d, Constraints: %d, Context: %d, Anti-patterns: %d",
		len(parsed.WorkflowRules), len(parsed.ConstraintRules), len(parsed.ContextRules), len(parsed.AntiPatterns))

	if len(parsed.ContextRules) == 0 {
		t.Error("expected context rules from CLAUDE.md (Code Style, Architecture)")
	}
	if len(parsed.AntiPatterns) == 0 {
		t.Error("expected anti-patterns from CLAUDE.md (Do NOT)")
	}

	// FormatForLLM should produce numbered lists
	llmStr := parsed.FormatForLLM()
	if !strings.Contains(llmStr, "1.") {
		t.Error("FormatForLLM should contain numbered items")
	}
	if !strings.Contains(llmStr, "Project Rules") {
		t.Error("FormatForLLM should contain Project Rules header")
	}
	if !strings.Contains(llmStr, "Anti-Patterns") {
		t.Error("FormatForLLM should contain Anti-Patterns header")
	}
	t.Logf("FormatForLLM output (%d chars):\n%s", len(llmStr), llmStr)
}

func TestIntegration_RealGitContext(t *testing.T) {
	root := repoRoot(t)

	gp := gitpkg.NewContextProvider(root)
	ctx, err := gp.GetContext()
	if err != nil {
		t.Fatalf("git.GetContext: %v", err)
	}

	if ctx.Branch == "" {
		t.Error("expected non-empty branch name")
	}
	if len(ctx.RecentCommits) == 0 {
		t.Error("expected at least one recent commit")
	}

	str := ctx.String()
	t.Logf("Git context (%d chars):\n%s", len(str), str)

	// Should only have branch + commits, no file lists
	if strings.Contains(str, "Staged") || strings.Contains(str, "Modified files") {
		t.Error("git context should only have branch + commits, no file lists")
	}
}

func TestIntegration_RulesLoaderWalksUp(t *testing.T) {
	root := repoRoot(t)

	// Simulate the bug: rules loader starting from cli/ subdirectory
	cliDir := filepath.Join(root, "cli")
	rl := rules.NewLoader(cliDir, []string{"CLAUDE.md"})
	content, err := rl.Load()
	if err != nil {
		t.Fatalf("rules.Load from cli/: %v", err)
	}
	if content == "" {
		t.Fatal("rules loader should walk up from cli/ and find CLAUDE.md at repo root")
	}
	if !strings.Contains(content, "Restruct") {
		t.Error("loaded content should contain project name from CLAUDE.md")
	}
}

func TestIntegration_FullBuildCompose(t *testing.T) {
	root := repoRoot(t)

	// Load real rules
	rl := rules.NewLoader(root, []string{"CLAUDE.md"})
	rulesContent, _ := rl.Load()

	// Get real git context
	gp := gitpkg.NewContextProvider(root)
	gitCtx, _ := gp.GetContext()

	// Build LLM input
	builder := prompt.NewBuilder(2048)
	result := builder.Build(
		"fix the auth bug where tokens expire too fast",
		rulesContent,
		gitCtx.String(),
		"- 2m ago: Added pagination to users endpoint",
	)

	// Verify the user message has all sections
	if !strings.Contains(result.UserMsg, "## Developer's Request") {
		t.Error("missing Developer's Request")
	}
	if !strings.Contains(result.UserMsg, "## Project Rules") {
		t.Error("missing Project Rules — rules not formatted for LLM")
	}
	if !strings.Contains(result.UserMsg, "## Anti-Patterns") {
		t.Error("missing Anti-Patterns section")
	}
	if !strings.Contains(result.UserMsg, "## Current Repository State") {
		t.Error("missing git context")
	}
	if !strings.Contains(result.UserMsg, "## Recent Session Context") {
		t.Error("missing session context")
	}

	// Simulate LLM selecting rules by index (including constraints)
	classification := &LLMClassification{
		Type:                "code_change",
		Intent:              "Fix premature auth token expiration",
		Analysis:            []string{"Check token TTL config", "Review recent middleware changes"},
		RelevantRules:       []int{1, 2},
		RelevantConstraints: []int{1},
		RelevantAntiPats:    []int{1},
	}

	// Compose final output
	composed := composeContext(classification, result.Rules, gitCtx.Branch)

	t.Logf("Composed output (%d chars):\n%s", len(composed), composed)

	// Verify core sections present
	if !strings.Contains(composed, "<intent>") {
		t.Error("composed output missing intent")
	}
	if !strings.Contains(composed, "<applicable_rules>") {
		t.Error("composed output missing applicable_rules")
	}
	if !strings.Contains(composed, "Plan first") {
		t.Error("composed output missing protocol directive")
	}
	if !strings.Contains(composed, "<analysis>") {
		t.Error("composed output missing analysis")
	}
	if !strings.Contains(composed, "<anti_patterns>") {
		t.Error("composed output missing anti_patterns")
	}

	// Workflow should appear for code_change if CLAUDE.md has ## Workflow
	if len(result.Rules.WorkflowRules) > 0 && !strings.Contains(composed, "<workflow>") {
		t.Error("composed output missing workflow (CLAUDE.md has ## Workflow)")
	}

	// Constraints should appear if LLM selected them and CLAUDE.md has ## Constraints
	if len(result.Rules.ConstraintRules) > 0 && !strings.Contains(composed, "<constraints>") {
		t.Error("composed output missing constraints (LLM selected [1] but they weren't resolved)")
	}
}
