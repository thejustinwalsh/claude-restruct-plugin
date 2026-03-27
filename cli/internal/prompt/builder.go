package prompt

import (
	"strings"
)

// Builder assembles the system and user messages for the refinement LLM.
type Builder struct {
	// MaxContextChars is the approximate character budget for the user
	// message sent to the local LLM. Truncation priority: git context
	// first, then rules, never the prompt. 0 means no limit.
	MaxContextChars int
}

// NewBuilder creates a Builder with an approximate token budget.
// The maxTokens value is converted to chars using a 1:4 ratio.
func NewBuilder(maxTokens int) *Builder {
	return &Builder{
		MaxContextChars: maxTokens * 4,
	}
}

// Build returns the system prompt and user message for the local LLM.
// Sections with no content are omitted to avoid confusing the LLM.
func (b *Builder) Build(rawPrompt, rules, gitContext string) (system string, user string) {
	system = SystemPrompt()

	rules = strings.TrimSpace(rules)
	gitContext = strings.TrimSpace(gitContext)

	// Apply token budget: truncate git first, then rules, never prompt
	if b.MaxContextChars > 0 {
		promptChars := len(rawPrompt)
		remaining := b.MaxContextChars - promptChars - 200 // overhead for headers/instruction

		if remaining < 0 {
			// Prompt alone exceeds budget — keep it all, drop everything else
			rules = ""
			gitContext = ""
		} else {
			// Fit rules first, then git
			if len(rules) > remaining {
				rules = rules[:remaining] + "\n[truncated]"
				gitContext = ""
			} else {
				gitRemaining := remaining - len(rules)
				if len(gitContext) > gitRemaining {
					if gitRemaining > 100 {
						gitContext = gitContext[:gitRemaining] + "\n[truncated]"
					} else {
						gitContext = ""
					}
				}
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("## Developer's Request\n")
	sb.WriteString(rawPrompt)
	sb.WriteString("\n")

	if rules != "" {
		sb.WriteString("\n## Project Rules\n")
		sb.WriteString(rules)
		sb.WriteString("\n")
	}

	if gitContext != "" {
		sb.WriteString("\n## Current Repository State\n")
		sb.WriteString(gitContext)
		sb.WriteString("\n")
	}

	sb.WriteString("\nAnalyze this request. Output ONLY the <context_supplement> block. Always include intent and workflow sections. Include applicable_rules only if project rules were provided above.")

	user = sb.String()
	return system, user
}
