package prompt

import "strings"

const framingHeader = `[Project rules analysis for the request above. Follow these constraints during implementation.]

`

// FrameContext wraps the local LLM's structured output with a brief
// preamble that orients Claude about what the additional context represents.
// The footer with section-specific instructions is now built dynamically
// by composeContext based on which sections are present.
// Returns empty string if the output is the NO_ADDITIONAL_CONTEXT sentinel.
func FrameContext(rawOutput string) string {
	trimmed := strings.TrimSpace(rawOutput)
	if trimmed == "" || trimmed == "NO_ADDITIONAL_CONTEXT" {
		return ""
	}
	return framingHeader + trimmed
}
