package pipeline

import (
	"regexp"
	"strings"
)

// passthroughExact is the set of prompts that should skip refinement.
// These are follow-ups, affirmatives, and acknowledgments that reference
// prior conversation context — injecting structure would confuse Claude
// about what "that" or "it" refers to.
var passthroughExact = map[string]bool{
	"y":           true,
	"yes":         true,
	"yeah":        true,
	"yep":         true,
	"yup":         true,
	"ok":          true,
	"okay":        true,
	"k":           true,
	"sure":        true,
	"sounds good": true,
	"looks good":  true,
	"lgtm":        true,
	"go ahead":    true,
	"do it":       true,
	"proceed":     true,
	"continue":    true,
	"approved":    true,
	"thanks":      true,
	"thank you":   true,
	"no":          true,
	"nope":        true,
	"nah":         true,
	"cancel":      true,
	"stop":        true,
	"nevermind":   true,
	"never mind":  true,
}

// numberedSelection matches "try option 2", "option 1", "3", "choice 2", etc.
var numberedSelection = regexp.MustCompile(`(?i)^(try\s+)?(option|choice|approach|number)?\s*\d+$`)

// ShouldRefine returns true if the prompt should be sent through the
// refinement pipeline. Returns false for follow-ups, affirmatives,
// slash commands, and numbered selections that reference prior context.
func ShouldRefine(prompt string) bool {
	s := strings.TrimSpace(prompt)
	if s == "" {
		return false
	}

	lower := strings.ToLower(s)

	// Exact match against known passthrough phrases
	if passthroughExact[lower] {
		return false
	}

	// Slash commands (Claude Code built-in or plugin commands)
	if strings.HasPrefix(s, "/") {
		return false
	}

	// Numbered selections: "try option 2", "3", "choice 1"
	if numberedSelection.MatchString(lower) {
		return false
	}

	return true
}
