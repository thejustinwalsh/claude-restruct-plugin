package prompt

import (
	"strings"
	"testing"
)

func TestBuild_BasicStructure(t *testing.T) {
	b := NewBuilder(0)
	r := b.Build("fix the bug", "## Code Style\n- no hardcoding\n## Do NOT\n- use eval", "Branch: main", "")

	if r.SystemMsg == "" {
		t.Error("system prompt should not be empty")
	}
	if !strings.Contains(r.UserMsg, "## Developer's Request") {
		t.Error("missing Developer's Request section")
	}
	if !strings.Contains(r.UserMsg, "fix the bug") {
		t.Error("missing raw prompt in user message")
	}
	if !strings.Contains(r.UserMsg, "Classify this request") {
		t.Error("instruction should say classify")
	}
}

func TestBuild_NumberedRules(t *testing.T) {
	b := NewBuilder(0)
	r := b.Build("fix the bug", "## Code Style\n- no hardcoding\n- use gofmt\n## Do NOT\n- use eval\n- skip tests", "", "")

	if len(r.Rules.ContextRules) != 2 {
		t.Errorf("expected 2 context rules, got %d", len(r.Rules.ContextRules))
	}
	if len(r.Rules.AntiPatterns) != 2 {
		t.Errorf("expected 2 anti-patterns, got %d", len(r.Rules.AntiPatterns))
	}
	if !strings.Contains(r.UserMsg, "1. no hardcoding") {
		t.Error("rules should be numbered in user message")
	}
}

func TestBuild_AfterEveryChangeAreProcessRules(t *testing.T) {
	rules := "## After Every Change\n- Run tests\n- Run build\n## Code Style\n- no hardcoding"
	b := NewBuilder(0)
	r := b.Build("fix", rules, "", "")

	// "After Every Change" items are process rules (verification guardrails)
	if len(r.Rules.ProcessRules) != 2 {
		t.Errorf("expected 2 process rules from After Every Change, got %d: %v", len(r.Rules.ProcessRules), r.Rules.ProcessRules)
	}
	// Everything else is a context rule for LLM selection
	if len(r.Rules.ContextRules) != 1 {
		t.Errorf("expected 1 context rule from Code Style, got %d: %v", len(r.Rules.ContextRules), r.Rules.ContextRules)
	}
	// Process rules should NOT appear in numbered LLM rules
	if strings.Contains(r.UserMsg, "Run tests") {
		t.Error("process rules should not be in LLM user message")
	}
	// Context rules SHOULD appear numbered
	if !strings.Contains(r.UserMsg, "1. no hardcoding") {
		t.Error("context rules should be in numbered list")
	}
}

func TestBuild_OmitsEmptyRules(t *testing.T) {
	b := NewBuilder(0)
	r := b.Build("fix the bug", "", "Branch: main", "")

	if strings.Contains(r.UserMsg, "Project Rules") {
		t.Error("should omit empty Project Rules section")
	}
	if !strings.Contains(r.UserMsg, "Repository State") {
		t.Error("should still include non-empty git context")
	}
}

func TestBuild_OmitsEmptyGitContext(t *testing.T) {
	b := NewBuilder(0)
	r := b.Build("fix the bug", "## Code\n- test everything", "", "")

	if strings.Contains(r.UserMsg, "## Current Repository State") {
		t.Error("should omit empty Repository State section")
	}
}

func TestBuild_OmitsBothWhenEmpty(t *testing.T) {
	b := NewBuilder(0)
	r := b.Build("fix the bug", "", "", "")

	if strings.Contains(r.UserMsg, "Project Rules") {
		t.Error("should omit empty Project Rules")
	}
	if strings.Contains(r.UserMsg, "Repository State") {
		t.Error("should omit empty Repository State")
	}
	if !strings.Contains(r.UserMsg, "fix the bug") {
		t.Error("prompt must always be present")
	}
}

func TestBuild_PromptNeverTruncated(t *testing.T) {
	longPrompt := strings.Repeat("x", 10000)
	b := NewBuilder(500)

	r := b.Build(longPrompt, "## Code\n- rules content", "git content", "")

	if !strings.Contains(r.UserMsg, longPrompt) {
		t.Error("prompt must never be truncated regardless of budget")
	}
}

func TestBuild_SessionContext(t *testing.T) {
	b := NewBuilder(0)
	r := b.Build("fix the bug", "## Code\n- rules", "Branch: main", "- 2m ago: Fixed auth token expiry\n- 8m ago: Added pagination")

	if !strings.Contains(r.UserMsg, "## Recent Session Context") {
		t.Error("missing Recent Session Context section")
	}
	if !strings.Contains(r.UserMsg, "Fixed auth token expiry") {
		t.Error("session context content missing")
	}
}

func TestBuild_SessionContextOmittedWhenEmpty(t *testing.T) {
	b := NewBuilder(0)
	r := b.Build("fix the bug", "## Code\n- rules", "Branch: main", "")

	if strings.Contains(r.UserMsg, "## Recent Session Context") {
		t.Error("should omit empty session context section")
	}
}

func TestBuild_WhitespaceOnlyTreatedAsEmpty(t *testing.T) {
	b := NewBuilder(0)
	r := b.Build("test", "   \n\t  ", "  \n  ", "")

	if strings.Contains(r.UserMsg, "Project Rules") {
		t.Error("whitespace-only rules should be treated as empty")
	}
	if strings.Contains(r.UserMsg, "Repository State") {
		t.Error("whitespace-only git context should be treated as empty")
	}
}

func TestParseRules_Empty(t *testing.T) {
	pr := ParseRules("")
	if len(pr.ProcessRules) != 0 || len(pr.ContextRules) != 0 || len(pr.AntiPatterns) != 0 {
		t.Error("empty rules should produce empty parsed rules")
	}
}

func TestParseRules_MixedSections(t *testing.T) {
	raw := `## After Every Change
- Run pnpm test
- Run pnpm build
## Code Style
- TypeScript: no as assertions
- Go: pure Go only, no CGO
## Do NOT
- use eval
- skip tests
- add CGO dependencies`

	pr := ParseRules(raw)

	if len(pr.ProcessRules) != 2 {
		t.Errorf("expected 2 process rules from After Every Change, got %d: %v", len(pr.ProcessRules), pr.ProcessRules)
	}
	if len(pr.ContextRules) != 2 {
		t.Errorf("expected 2 context rules from Code Style, got %d: %v", len(pr.ContextRules), pr.ContextRules)
	}
	if len(pr.AntiPatterns) != 3 {
		t.Errorf("expected 3 anti-patterns, got %d: %v", len(pr.AntiPatterns), pr.AntiPatterns)
	}
}

func TestParseRules_LoaderFormat(t *testing.T) {
	// This matches the actual format produced by rules.Loader:
	// "## Rules from CLAUDE.md\n{file content}"
	raw := `## Rules from CLAUDE.md
# Restruct

## Build & Test — ALL commands run from project root
- ` + "`pnpm dev`" + ` — starts xmake watch
- ` + "`pnpm build`" + ` — release build
- ` + "`pnpm test`" + ` — runs all Go tests

## Code Style
- TypeScript: no as type assertions
- Go: pure Go only, no CGO

## Architecture
- CLI writes to SQLite
- All inference is local via Ollama

## After Every Change
- Run pnpm test from root
- Run pnpm build from root

## Do NOT
- Do not use as in TypeScript
- Do not add CGO dependencies
- Do not cd into cli/ or web/`

	pr := ParseRules(raw)

	t.Logf("Process rules: %d", len(pr.ProcessRules))
	for i, r := range pr.ProcessRules {
		t.Logf("  P%d: %s", i+1, r)
	}
	t.Logf("Context rules: %d", len(pr.ContextRules))
	for i, r := range pr.ContextRules {
		t.Logf("  C%d: %s", i+1, r)
	}
	t.Logf("Anti-patterns: %d", len(pr.AntiPatterns))
	for i, r := range pr.AntiPatterns {
		t.Logf("  A%d: %s", i+1, r)
	}

	if len(pr.ContextRules) == 0 {
		t.Error("expected context rules — all rules should be context rules for LLM selection")
	}
	if len(pr.AntiPatterns) == 0 {
		t.Error("expected anti-patterns from Do NOT section")
	}

	llm := pr.FormatForLLM()
	if llm == "" {
		t.Error("FormatForLLM should produce non-empty output")
	}
	t.Logf("\n=== FormatForLLM ===\n%s", llm)
}
