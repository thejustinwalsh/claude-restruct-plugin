package hook

import (
	"encoding/json"
	"io"
)

// HookInput represents the JSON payload from Claude Code's hook system.
type HookInput struct {
	HookName  string `json:"hook_name"`
	Prompt    string `json:"prompt"`
	SessionID string `json:"session_id"`
}

// HookOutput is the JSON response written back to Claude Code.
type HookOutput struct {
	OK            bool   `json:"ok"`
	RefinedPrompt string `json:"refined_prompt,omitempty"`
	Error         string `json:"error,omitempty"`
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
func WriteOutput(w io.Writer, out *HookOutput) error {
	return json.NewEncoder(w).Encode(out)
}

// OKOutput returns a passthrough response (no refinement).
func OKOutput() *HookOutput {
	return &HookOutput{OK: true}
}

// RefinedOutput returns a response with the refined prompt replacing the original.
func RefinedOutput(refined string) *HookOutput {
	return &HookOutput{OK: true, RefinedPrompt: refined}
}

// ErrorOutput returns an error response.
func ErrorOutput(msg string) *HookOutput {
	return &HookOutput{OK: false, Error: msg}
}
