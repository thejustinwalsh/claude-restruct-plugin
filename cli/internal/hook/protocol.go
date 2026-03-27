package hook

import (
	"encoding/json"
	"io"
)

// HookInput represents the JSON payload Claude Code sends to UserPromptSubmit hooks via stdin.
// Reference: https://docs.anthropic.com/en/docs/claude-code/hooks
type HookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	PermissionMode string `json:"permission_mode"`
	HookEventName  string `json:"hook_event_name"`
	Prompt         string `json:"prompt"`
}

// HookSpecificOutput contains fields specific to the UserPromptSubmit hook event.
type HookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext,omitempty"`
	Decision          string `json:"decision,omitempty"`  // "block" to reject the prompt
	Reason            string `json:"reason,omitempty"`    // shown to user when blocking
	SuppressOutput    bool   `json:"suppressOutput,omitempty"`
}

// HookOutput is the JSON response written to stdout for Claude Code to consume.
// For UserPromptSubmit, additionalContext is APPENDED to Claude's context
// alongside the original prompt — it does not replace it.
type HookOutput struct {
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// ParseInput reads and decodes the hook input from a reader (typically stdin).
func ParseInput(r io.Reader) (*HookInput, error) {
	var input HookInput
	if err := json.NewDecoder(r).Decode(&input); err != nil {
		return nil, err
	}
	return &input, nil
}

// WriteOutput encodes and writes the hook output to a writer (typically stdout).
// If out is nil, writes nothing (clean passthrough).
func WriteOutput(w io.Writer, out *HookOutput) error {
	if out == nil {
		return nil
	}
	return json.NewEncoder(w).Encode(out)
}

// PassthroughOutput returns nil — writing nothing to stdout with exit 0
// means Claude Code proceeds with the original prompt unmodified.
func PassthroughOutput() *HookOutput {
	return nil
}

// ContextOutput returns a response that appends additional context to Claude's
// conversation alongside the user's original prompt.
func ContextOutput(context string) *HookOutput {
	return &HookOutput{
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName:     "UserPromptSubmit",
			AdditionalContext: context,
			SuppressOutput:    true,
		},
	}
}

// BlockOutput returns a response that blocks the prompt from being processed.
// The reason is shown to the user. The hook must exit with code 2 for this to work.
func BlockOutput(reason string) *HookOutput {
	return &HookOutput{
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName: "UserPromptSubmit",
			Decision:      "block",
			Reason:        reason,
		},
	}
}
