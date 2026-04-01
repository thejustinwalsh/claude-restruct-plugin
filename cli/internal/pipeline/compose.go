package pipeline

import (
	"fmt"
	"strings"

	"github.com/tjw/restruct/internal/prompt"
)

// needsImplementationGuardrails returns true for request types that
// involve changing code and benefit from plan/verify directives.
func needsImplementationGuardrails(requestType string) bool {
	return requestType == "code_change" || requestType == "refactor" || requestType == "debug"
}

// composeContext assembles the final XML context block from the LLM's
// classification and statically available data. Inline XML comments
// annotate each section so Claude understands how to use them.
// scopedRules is optional — when provided (from deep-context doc selection),
// rules are sourced from specific documents with source attribution.
func composeContext(c *LLMClassification, rules *prompt.ParsedRules, scopedRules map[string]*prompt.ParsedRules, gitBranch string) string {
	var sb strings.Builder
	implGuards := needsImplementationGuardrails(c.Type)

	fmt.Fprintf(&sb, "<context type=%q>\n", c.Type)
	sb.WriteString("<!-- type: classifies this request and determines which guardrails are active -->\n")

	// Intent — always included
	sb.WriteString("\n<intent>\n")
	sb.WriteString("<!-- Precise interpretation of the request. Verify it matches what the user meant. -->\n")
	sb.WriteString(c.Intent)
	sb.WriteString("\n</intent>\n")

	// Applicable rules — LLM-selected, ranked by relevance, capped.
	// When scoped rules are available (from deep-context doc selection),
	// use them with source attribution. Otherwise fall back to flat rules.
	const maxRules = 3
	if len(scopedRules) > 0 {
		// Collect rules from scoped documents with source attribution
		var scopedEntries []string
		for source, pr := range scopedRules {
			for _, r := range pr.ContextRules {
				scopedEntries = append(scopedEntries, fmt.Sprintf("[%s] %s", source, r))
			}
		}
		if len(scopedEntries) > maxRules {
			scopedEntries = scopedEntries[:maxRules]
		}
		if len(scopedEntries) > 0 {
			sb.WriteString("\n<applicable_rules>\n")
			sb.WriteString("<!-- Project rules relevant to this change. Follow them. -->\n")
			for _, entry := range scopedEntries {
				fmt.Fprintf(&sb, "- %s\n", entry)
			}
			sb.WriteString("</applicable_rules>\n")
		}
	} else {
		selectedRules := c.RelevantRules
		if len(selectedRules) > maxRules {
			selectedRules = selectedRules[:maxRules]
		}
		if len(selectedRules) > 0 {
			sb.WriteString("\n<applicable_rules>\n")
			sb.WriteString("<!-- Project rules relevant to this change. Follow them. -->\n")
			for _, idx := range selectedRules {
				if idx >= 1 && idx <= len(rules.ContextRules) {
					fmt.Fprintf(&sb, "- %s\n", rules.ContextRules[idx-1])
				}
			}
			sb.WriteString("</applicable_rules>\n")
		}
	}

	// Protocol — universal reasoning directives, always injected
	sb.WriteString("\n<protocol>\n")
	sb.WriteString("<!-- How to approach this task. Follow this before acting. -->\n")
	sb.WriteString("- Plan first. Even simple requests benefit from a plan before determining the solution.\n")
	sb.WriteString("- Explore options. When multiple approaches exist, sketch a few, weigh tradeoffs, and select the best before implementing. Planning is cheap.\n")
	sb.WriteString("- Use agents to research. When you need a summary of a system, feature, or resource, delegate to a sub-agent instead of inline searching.\n")
	sb.WriteString("- Use agents to parallelize. When work can be done concurrently, spawn agents rather than working sequentially.\n")
	sb.WriteString("- Ask when uncertain. If the request is ambiguous or you're unsure about the approach, ask the user for clarity before proceeding.\n")
	sb.WriteString("- Verify before assuming. Check the current state of code, config, and dependencies rather than guessing from memory.\n")
	sb.WriteString("</protocol>\n")

	// Workflow — process steps, always injected for code changes.
	// Merge workflow rules from scoped documents when available.
	if implGuards {
		var workflowRules []string
		if len(scopedRules) > 0 {
			for _, pr := range scopedRules {
				workflowRules = append(workflowRules, pr.WorkflowRules...)
			}
		}
		if len(workflowRules) == 0 {
			workflowRules = rules.WorkflowRules
		}
		if len(workflowRules) > 0 {
			sb.WriteString("\n<workflow>\n")
			sb.WriteString("<!-- Process steps to follow for this change. -->\n")
			for _, r := range workflowRules {
				fmt.Fprintf(&sb, "- %s\n", r)
			}
			sb.WriteString("</workflow>\n")
		}
	}

	// Constraints — LLM-selected or scoped, capped
	const maxConstraints = 3
	if len(scopedRules) > 0 {
		var scopedConstraints []string
		for source, pr := range scopedRules {
			for _, r := range pr.ConstraintRules {
				scopedConstraints = append(scopedConstraints, fmt.Sprintf("[%s] %s", source, r))
			}
		}
		if len(scopedConstraints) > maxConstraints {
			scopedConstraints = scopedConstraints[:maxConstraints]
		}
		if len(scopedConstraints) > 0 {
			sb.WriteString("\n<constraints>\n")
			sb.WriteString("<!-- Design and architectural constraints relevant to this change. -->\n")
			for _, entry := range scopedConstraints {
				fmt.Fprintf(&sb, "- %s\n", entry)
			}
			sb.WriteString("</constraints>\n")
		}
	} else {
		selectedConstraints := c.RelevantConstraints
		if len(selectedConstraints) > maxConstraints {
			selectedConstraints = selectedConstraints[:maxConstraints]
		}
		if len(selectedConstraints) > 0 {
			sb.WriteString("\n<constraints>\n")
			sb.WriteString("<!-- Design and architectural constraints relevant to this change. -->\n")
			for _, idx := range selectedConstraints {
				if idx >= 1 && idx <= len(rules.ConstraintRules) {
					fmt.Fprintf(&sb, "- %s\n", rules.ConstraintRules[idx-1])
				}
			}
			sb.WriteString("</constraints>\n")
		}
	}

	// Analysis — from LLM
	if len(c.Analysis) > 0 {
		sb.WriteString("\n<analysis>\n")
		sb.WriteString("<!-- Non-obvious factors to reason about before committing to an approach. -->\n")
		for _, a := range c.Analysis {
			fmt.Fprintf(&sb, "- %s\n", a)
		}
		sb.WriteString("</analysis>\n")
	}

	// Clarification — only when genuinely ambiguous
	if len(c.Clarification) > 0 {
		sb.WriteString("\n<clarification_needed>\n")
		sb.WriteString("<!-- STOP. Ask these questions before proceeding. Do NOT guess. -->\n")
		for _, q := range c.Clarification {
			fmt.Fprintf(&sb, "- %s\n", q)
		}
		sb.WriteString("</clarification_needed>\n")
	}

	// Anti-patterns — available for ALL types, ranked, capped
	const maxAntiPatterns = 6
	if len(scopedRules) > 0 {
		var scopedAnti []string
		for source, pr := range scopedRules {
			for _, r := range pr.AntiPatterns {
				scopedAnti = append(scopedAnti, fmt.Sprintf("[%s] %s", source, r))
			}
		}
		if len(scopedAnti) > maxAntiPatterns {
			scopedAnti = scopedAnti[:maxAntiPatterns]
		}
		if len(scopedAnti) > 0 {
			sb.WriteString("\n<anti_patterns>\n")
			sb.WriteString("<!-- Specific things to avoid for this request. -->\n")
			for _, entry := range scopedAnti {
				fmt.Fprintf(&sb, "- %s\n", entry)
			}
			sb.WriteString("</anti_patterns>\n")
		}
	} else {
		selectedAntiPats := c.RelevantAntiPats
		if len(selectedAntiPats) > maxAntiPatterns {
			selectedAntiPats = selectedAntiPats[:maxAntiPatterns]
		}
		if len(selectedAntiPats) > 0 {
			sb.WriteString("\n<anti_patterns>\n")
			sb.WriteString("<!-- Specific things to avoid for this request. -->\n")
			for _, idx := range selectedAntiPats {
				if idx >= 1 && idx <= len(rules.AntiPatterns) {
					fmt.Fprintf(&sb, "- %s\n", rules.AntiPatterns[idx-1])
				}
			}
			sb.WriteString("</anti_patterns>\n")
		}
	}

	// Repo state — branch + LLM-summarized recent activity
	if gitBranch != "" || c.RecentActivity != "" {
		sb.WriteString("\n<repo_state>\n")
		sb.WriteString("<!-- Current branch and recent development activity for situational awareness. -->\n")
		if gitBranch != "" {
			fmt.Fprintf(&sb, "Branch: %s", gitBranch)
		}
		if c.RecentActivity != "" {
			if gitBranch != "" {
				sb.WriteString(" | ")
			}
			sb.WriteString(c.RecentActivity)
		}
		sb.WriteString("\n</repo_state>\n")
	}

	sb.WriteString("</context>")
	return sb.String()
}
