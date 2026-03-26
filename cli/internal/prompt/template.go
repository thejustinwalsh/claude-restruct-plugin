package prompt

import (
	_ "embed"
)

//go:embed system_prompt.tmpl
var systemPromptTemplate string

// SystemPrompt returns the meta-prompt refinement system prompt.
func SystemPrompt() string {
	return systemPromptTemplate
}
