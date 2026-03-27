package prompt

import (
	"strings"
	"testing"
)

func TestBuild_BasicStructure(t *testing.T) {
	b := NewBuilder(0)
	system, user := b.Build("fix the bug", "rule: no hardcoding", "Branch: main")

	if system == "" {
		t.Error("system prompt should not be empty")
	}
	if !strings.Contains(user, "## Developer's Request") {
		t.Error("missing Developer's Request section")
	}
	if !strings.Contains(user, "fix the bug") {
		t.Error("missing raw prompt in user message")
	}
	if !strings.Contains(user, "## Project Rules") {
		t.Error("missing Project Rules section")
	}
	if !strings.Contains(user, "## Current Repository State") {
		t.Error("missing Repository State section")
	}
	if !strings.Contains(user, "<context_supplement>") {
		t.Error("instruction should reference context_supplement")
	}
	if !strings.Contains(user, "Always include intent and workflow") {
		t.Error("instruction should mandate intent and workflow sections")
	}
}

func TestBuild_OmitsEmptyRules(t *testing.T) {
	b := NewBuilder(0)
	_, user := b.Build("fix the bug", "", "Branch: main")

	if strings.Contains(user, "## Project Rules") {
		t.Error("should omit empty Project Rules section")
	}
	if !strings.Contains(user, "## Current Repository State") {
		t.Error("should still include non-empty git context")
	}
}

func TestBuild_OmitsEmptyGitContext(t *testing.T) {
	b := NewBuilder(0)
	_, user := b.Build("fix the bug", "rule: test everything", "")

	if strings.Contains(user, "## Current Repository State") {
		t.Error("should omit empty Repository State section")
	}
	if !strings.Contains(user, "## Project Rules") {
		t.Error("should still include non-empty rules")
	}
}

func TestBuild_OmitsBothWhenEmpty(t *testing.T) {
	b := NewBuilder(0)
	_, user := b.Build("fix the bug", "", "")

	if strings.Contains(user, "## Project Rules") {
		t.Error("should omit empty Project Rules")
	}
	if strings.Contains(user, "## Current Repository State") {
		t.Error("should omit empty Repository State")
	}
	if !strings.Contains(user, "fix the bug") {
		t.Error("prompt must always be present")
	}
}

func TestBuild_PromptNeverTruncated(t *testing.T) {
	longPrompt := strings.Repeat("x", 10000)
	b := NewBuilder(500) // very small budget: 500 tokens = 2000 chars

	_, user := b.Build(longPrompt, "rules content", "git content")

	if !strings.Contains(user, longPrompt) {
		t.Error("prompt must never be truncated regardless of budget")
	}
}

func TestBuild_GitTruncatedBeforeRules(t *testing.T) {
	prompt := "fix it"
	rules := strings.Repeat("r", 1000)
	git := strings.Repeat("g", 5000)

	b := NewBuilder(600) // 600 tokens = 2400 chars budget

	_, user := b.Build(prompt, rules, git)

	if !strings.Contains(user, rules) {
		t.Error("rules should not be truncated when git can absorb the cut")
	}

	// Git should be truncated or omitted
	if strings.Contains(user, git) {
		t.Error("git context should be truncated before rules")
	}
}

func TestBuild_RulesTruncatedWhenHuge(t *testing.T) {
	prompt := "fix it"
	rules := strings.Repeat("r", 50000)

	b := NewBuilder(500) // 500 tokens = 2000 chars

	_, user := b.Build(prompt, rules, "")

	if strings.Contains(user, rules) {
		t.Error("huge rules should be truncated")
	}
	if !strings.Contains(user, "[truncated]") {
		t.Error("truncated rules should have [truncated] marker")
	}
	// Prompt must survive
	if !strings.Contains(user, prompt) {
		t.Error("prompt must never be truncated")
	}
}

func TestBuild_WhitespaceOnlyTreatedAsEmpty(t *testing.T) {
	b := NewBuilder(0)
	_, user := b.Build("test", "   \n\t  ", "  \n  ")

	if strings.Contains(user, "## Project Rules") {
		t.Error("whitespace-only rules should be treated as empty")
	}
	if strings.Contains(user, "## Current Repository State") {
		t.Error("whitespace-only git context should be treated as empty")
	}
}

func TestBuild_InstructionLineCorrect(t *testing.T) {
	b := NewBuilder(0)
	_, user := b.Build("test", "", "")

	trimmed := strings.TrimSpace(user)
	if !strings.HasSuffix(trimmed, "Include applicable_rules only if project rules were provided above.") {
		t.Errorf("instruction line not found at end of user message, got: %s", trimmed[max(0, len(trimmed)-100):])
	}
}
