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

// ParsedRules holds rules split into process rules (always included)
// and context rules (numbered for LLM selection).
type ParsedRules struct {
	ProcessRules []string // build, test, lint, verify commands
	ContextRules []string // numbered context rules for LLM to select from
	AntiPatterns []string // numbered anti-patterns for LLM to select from
}

// ParseRules splits raw rules content into process rules, context rules,
// and anti-patterns. Returns the parsed structure and the formatted string
// for the LLM (with numbered context rules and anti-patterns).
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

		// "After Every Change" rules are verification guardrails —
		// always injected, never LLM-selected. Everything else goes
		// to context for LLM selection.
		if strings.HasPrefix(lower, "## after every change") {
			currentSection = "process"
			continue
		}
		if strings.HasPrefix(lower, "## do not") ||
			strings.HasPrefix(lower, "## don't") ||
			strings.HasPrefix(lower, "## do not") {
			currentSection = "anti"
			continue
		}
		if strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "# ") {
			currentSection = "context"
			continue
		}

		// Collect lines based on section
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			entry := strings.TrimLeft(trimmed, "-* ")
			if entry == "" {
				continue
			}
			switch currentSection {
			case "process":
				pr.ProcessRules = append(pr.ProcessRules, entry)
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
func (pr *ParsedRules) FormatForLLM() string {
	if len(pr.ContextRules) == 0 && len(pr.AntiPatterns) == 0 {
		return ""
	}

	var sb strings.Builder
	if len(pr.ContextRules) > 0 {
		sb.WriteString("## Project Rules (context — select by number)\n")
		for i, r := range pr.ContextRules {
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
func (b *Builder) Build(rawPrompt, rules, gitContext, sessionContext string) *BuildResult {
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
