package prompt

import "fmt"

// Builder assembles the system and user messages for the refinement LLM.
type Builder struct{}

func NewBuilder() *Builder {
	return &Builder{}
}

// Build returns the system prompt and user message for the local LLM.
func (b *Builder) Build(rawPrompt, rules, gitContext string) (system string, user string) {
	system = SystemPrompt()

	user = fmt.Sprintf(`## Developer's Request
%s

## Project Rules
%s

## Current Repository State
%s

Transform this into a structured prompt. Output ONLY the <structured_prompt> block.`,
		rawPrompt, rules, gitContext)

	return system, user
}
