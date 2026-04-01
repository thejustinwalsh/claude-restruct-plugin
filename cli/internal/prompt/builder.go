package prompt

import (
	"fmt"
	"strings"
)

// Builder assembles the system and user messages for the refinement LLM.
type Builder struct {
	// MaxContextChars is the approximate character budget for the user
	// message sent to the local LLM. Truncation priority: git context
	// first, then session, then rules, never the prompt. 0 means no limit.
	MaxContextChars int
}

// NewBuilder creates a Builder with an approximate token budget.
// The maxTokens value is converted to chars using a 1:4 ratio.
func NewBuilder(maxTokens int) *Builder {
	return &Builder{
		MaxContextChars: maxTokens * 4,
	}
}

// ParsedRules holds rules split by semantic category.
//
//   - WorkflowRules: process steps, always injected for code changes (## Workflow)
//   - ConstraintRules: design/architectural constraints, LLM-selected (## Constraints)
//   - ContextRules: general project rules, LLM-selected (all other sections)
//   - AntiPatterns: things to avoid, LLM-selected (## Do NOT)
type ParsedRules struct {
	WorkflowRules   []string // always injected for code_change/refactor/debug
	ConstraintRules []string // LLM-selected design/architectural constraints
	ContextRules    []string // LLM-selected context rules
	AntiPatterns    []string // LLM-selected anti-patterns
}

// ParseRules splits raw rules content into workflow, constraints, context rules,
// and anti-patterns based on section headers.
func ParseRules(raw string) *ParsedRules {
	if strings.TrimSpace(raw) == "" {
		return &ParsedRules{}
	}

	pr := &ParsedRules{}
	lines := strings.Split(raw, "\n")

	var currentSection string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		// Section header detection — order matters for prefix matching
		if strings.HasPrefix(lower, "## workflow") ||
			strings.HasPrefix(lower, "## after every change") {
			currentSection = "workflow"
			continue
		}
		if strings.HasPrefix(lower, "## constraints") {
			currentSection = "constraints"
			continue
		}
		if strings.HasPrefix(lower, "## do not") ||
			strings.HasPrefix(lower, "## don't") ||
			strings.HasPrefix(lower, "## anti-patterns") ||
			strings.HasPrefix(lower, "## anti_patterns") {
			currentSection = "anti"
			continue
		}
		if strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "# ") {
			currentSection = "context"
			continue
		}

		// Collect bullet-point lines based on section
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			entry := strings.TrimLeft(trimmed, "-* ")
			if entry == "" {
				continue
			}
			switch currentSection {
			case "workflow":
				pr.WorkflowRules = append(pr.WorkflowRules, entry)
			case "constraints":
				pr.ConstraintRules = append(pr.ConstraintRules, entry)
			case "anti":
				pr.AntiPatterns = append(pr.AntiPatterns, entry)
			default:
				pr.ContextRules = append(pr.ContextRules, entry)
			}
		}
	}

	return pr
}

// FormatForLLM returns the numbered rules string for the LLM user message.
// Includes context rules, constraints, and anti-patterns — all LLM-selectable.
// Workflow rules are NOT included here (they're always injected, not selected).
func (pr *ParsedRules) FormatForLLM() string {
	if len(pr.ContextRules) == 0 && len(pr.ConstraintRules) == 0 && len(pr.AntiPatterns) == 0 {
		return ""
	}

	var sb strings.Builder
	if len(pr.ContextRules) > 0 {
		sb.WriteString("## Project Rules (context — select by number)\n")
		for i, r := range pr.ContextRules {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, r)
		}
	}
	if len(pr.ConstraintRules) > 0 {
		sb.WriteString("\n## Constraints (design/architectural — select by number)\n")
		for i, r := range pr.ConstraintRules {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, r)
		}
	}
	if len(pr.AntiPatterns) > 0 {
		sb.WriteString("\n## Anti-Patterns (select by number)\n")
		for i, r := range pr.AntiPatterns {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, r)
		}
	}
	return sb.String()
}

// BuildResult holds the outputs of Build for use in context composition.
type BuildResult struct {
	SystemMsg string
	UserMsg   string
	Rules     *ParsedRules
}

// Build returns the system prompt and user message for the local LLM.
// The rules are parsed into numbered lists so the LLM can reference by index.
// projectMap is the formatted project document map (from bootstrap) — empty string if unavailable.
func (b *Builder) Build(rawPrompt, rules, gitContext, sessionContext string, projectMap ...string) *BuildResult {
	system := SystemPrompt()
	parsed := ParseRules(rules)

	gitContext = strings.TrimSpace(gitContext)
	sessionContext = strings.TrimSpace(sessionContext)
	rulesForLLM := parsed.FormatForLLM()

	// Apply token budget. Priority: rules > session > git. Never truncate prompt.
	if b.MaxContextChars > 0 {
		remaining := b.MaxContextChars - len(rawPrompt) - 200
		if remaining < 0 {
			remaining = 0
		}

		// Rules always fit (they're the whole point). Drop git/session first.
		used := len(rulesForLLM)
		afterRules := remaining - used

		// Cap session context
		if len(sessionContext) > 400 {
			sessionContext = sessionContext[:400]
		}
		if afterRules < len(sessionContext) {
			sessionContext = ""
		}
		afterSession := afterRules - len(sessionContext)

		// Git gets whatever's left
		if afterSession < len(gitContext) {
			if afterSession > 100 {
				gitContext = gitContext[:afterSession]
			} else {
				gitContext = ""
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("## Developer's Request\n")
	sb.WriteString(rawPrompt)
	sb.WriteString("\n")

	if rulesForLLM != "" {
		sb.WriteString("\n")
		sb.WriteString(rulesForLLM)
	}

	// Project document map (from bootstrap) — enables document selection
	pm := ""
	if len(projectMap) > 0 {
		pm = strings.TrimSpace(projectMap[0])
	}
	if pm != "" {
		sb.WriteString("\n")
		sb.WriteString(pm)
		sb.WriteString("\n")
	}

	if gitContext != "" {
		sb.WriteString("\n## Current Repository State\n")
		sb.WriteString(gitContext)
		sb.WriteString("\n")
	}

	if sessionContext != "" {
		sb.WriteString("\n## Recent Session Context\n")
		sb.WriteString(sessionContext)
		sb.WriteString("\n")
	}

	sb.WriteString("\nClassify this request and output JSON only.")

	return &BuildResult{
		SystemMsg: system,
		UserMsg:   sb.String(),
		Rules:     parsed,
	}
}
