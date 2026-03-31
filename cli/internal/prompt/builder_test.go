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

func TestBuild_AfterEveryChangeAreWorkflowRules(t *testing.T) {
	rules := "## After Every Change\n- Run tests\n- Run build\n## Code Style\n- no hardcoding"
	b := NewBuilder(0)
	r := b.Build("fix", rules, "", "")

	// "After Every Change" items are workflow rules (always injected)
	if len(r.Rules.WorkflowRules) != 2 {
		t.Errorf("expected 2 workflow rules from After Every Change, got %d: %v", len(r.Rules.WorkflowRules), r.Rules.WorkflowRules)
	}
	if len(r.Rules.ContextRules) != 1 {
		t.Errorf("expected 1 context rule from Code Style, got %d: %v", len(r.Rules.ContextRules), r.Rules.ContextRules)
	}
	// Workflow rules should NOT appear in numbered LLM rules
	if strings.Contains(r.UserMsg, "Run tests") {
		t.Error("workflow rules should not be in LLM user message")
	}
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
	if len(pr.WorkflowRules) != 0 || len(pr.ConstraintRules) != 0 || len(pr.ContextRules) != 0 || len(pr.AntiPatterns) != 0 {
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

	if len(pr.WorkflowRules) != 2 {
		t.Errorf("expected 2 workflow rules from After Every Change, got %d: %v", len(pr.WorkflowRules), pr.WorkflowRules)
	}
	if len(pr.ContextRules) != 2 {
		t.Errorf("expected 2 context rules from Code Style, got %d: %v", len(pr.ContextRules), pr.ContextRules)
	}
	if len(pr.AntiPatterns) != 3 {
		t.Errorf("expected 3 anti-patterns, got %d: %v", len(pr.AntiPatterns), pr.AntiPatterns)
	}
}

func TestParseRules_LoaderFormat(t *testing.T) {
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

	t.Logf("Workflow rules: %d", len(pr.WorkflowRules))
	for i, r := range pr.WorkflowRules {
		t.Logf("  W%d: %s", i+1, r)
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
		t.Error("expected context rules")
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

func TestParseRules_WorkflowSection(t *testing.T) {
	raw := `## Workflow
- If you added new behavior, add or update tests
- Prefer focused unit tests over broad integration tests
## Do NOT
- skip tests`

	pr := ParseRules(raw)

	if len(pr.WorkflowRules) != 2 {
		t.Errorf("expected 2 workflow rules, got %d: %v", len(pr.WorkflowRules), pr.WorkflowRules)
	}
	if pr.WorkflowRules[0] != "If you added new behavior, add or update tests" {
		t.Errorf("workflow[0] = %q", pr.WorkflowRules[0])
	}
	// Workflow rules should NOT appear in LLM message
	llm := pr.FormatForLLM()
	if strings.Contains(llm, "add or update tests") {
		t.Error("workflow rules should not be in FormatForLLM output")
	}
}

func TestParseRules_ConstraintsSection(t *testing.T) {
	raw := `## Constraints
- Always map to CLI commands
- Performance is critical
## Do NOT
- skip tests`

	pr := ParseRules(raw)

	if len(pr.ConstraintRules) != 2 {
		t.Errorf("expected 2 constraint rules, got %d: %v", len(pr.ConstraintRules), pr.ConstraintRules)
	}
	if pr.ConstraintRules[0] != "Always map to CLI commands" {
		t.Errorf("constraint[0] = %q", pr.ConstraintRules[0])
	}
	// Constraints SHOULD appear in LLM message (they're LLM-selected)
	llm := pr.FormatForLLM()
	if !strings.Contains(llm, "Always map to CLI commands") {
		t.Error("constraints should appear in FormatForLLM for LLM selection")
	}
	if !strings.Contains(llm, "Constraints (design/architectural") {
		t.Error("constraints section should have its own header in LLM output")
	}
}

func TestParseRules_AllFourCategories(t *testing.T) {
	raw := `## Build & Test
- pnpm test
## Workflow
- Add tests for new behavior
## Constraints
- Performance is critical
- Always use CLI commands
## Do NOT
- use eval`

	pr := ParseRules(raw)

	if len(pr.ContextRules) != 1 {
		t.Errorf("context rules: got %d, want 1", len(pr.ContextRules))
	}
	if len(pr.WorkflowRules) != 1 {
		t.Errorf("workflow rules: got %d, want 1", len(pr.WorkflowRules))
	}
	if len(pr.ConstraintRules) != 2 {
		t.Errorf("constraint rules: got %d, want 2", len(pr.ConstraintRules))
	}
	if len(pr.AntiPatterns) != 1 {
		t.Errorf("anti-patterns: got %d, want 1", len(pr.AntiPatterns))
	}
}

func TestParseRules_ConstraintsInLoaderFormat(t *testing.T) {
	raw := `## Rules from CLAUDE.md
# Restruct

## Build & Test
- ` + "`pnpm test`" + ` — runs all Go tests

## Code Style
- TypeScript: no as type assertions

## Workflow
- If you added new behavior, add or update tests to cover it
- Prefer focused unit tests over broad integration tests

## Constraints
- Always map to CLI commands
- Performance is critical

## Do NOT
- Do not use as in TypeScript
- Do not add CGO dependencies`

	pr := ParseRules(raw)

	t.Logf("Workflow rules: %d", len(pr.WorkflowRules))
	t.Logf("Constraint rules: %d", len(pr.ConstraintRules))
	t.Logf("Context rules: %d", len(pr.ContextRules))
	t.Logf("Anti-patterns: %d", len(pr.AntiPatterns))

	if len(pr.WorkflowRules) != 2 {
		t.Errorf("expected 2 workflow rules, got %d: %v", len(pr.WorkflowRules), pr.WorkflowRules)
	}
	if len(pr.ConstraintRules) != 2 {
		t.Errorf("expected 2 constraint rules, got %d: %v", len(pr.ConstraintRules), pr.ConstraintRules)
	}
	if len(pr.AntiPatterns) != 2 {
		t.Errorf("expected 2 anti-patterns, got %d", len(pr.AntiPatterns))
	}

	llm := pr.FormatForLLM()
	// Workflow should NOT be in LLM output
	if strings.Contains(llm, "add or update tests") {
		t.Error("workflow rules should not appear in FormatForLLM")
	}
	// Constraints SHOULD be in LLM output
	if !strings.Contains(llm, "CLI commands") {
		t.Error("constraints should appear in FormatForLLM")
	}
}

func TestParseRules_BackwardCompat_AfterEveryChange(t *testing.T) {
	// Legacy "After Every Change" still maps to workflow
	raw := `## After Every Change
- Run linter
## Workflow
- Add tests`

	pr := ParseRules(raw)

	if len(pr.WorkflowRules) != 2 {
		t.Errorf("expected 2 workflow rules from both legacy and new headers, got %d: %v", len(pr.WorkflowRules), pr.WorkflowRules)
	}
}
