package hook

import (
	"encoding/json"
	"io"
)

// HookInput represents the JSON payload Claude Code sends to hooks via stdin.
// Fields are a superset across hook events; unused fields are omitted/zero.
// Reference: https://docs.anthropic.com/en/docs/claude-code/hooks
type HookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	PermissionMode string `json:"permission_mode"`
	HookEventName  string `json:"hook_event_name"`
	Prompt         string `json:"prompt,omitempty"`

	// TaskCreated / TaskCompleted fields
	TaskID      string `json:"task_id,omitempty"`
	TaskSubject string `json:"task_subject,omitempty"`

	// Stop hook fields
	StopHookActive bool `json:"stop_hook_active,omitempty"`

	// PreToolUse / PostToolUse fields
	ToolName     string         `json:"tool_name,omitempty"`
	ToolInput    map[string]any `json:"tool_input,omitempty"`
	ToolUseID    string         `json:"tool_use_id,omitempty"`
	ToolResponse string         `json:"tool_response,omitempty"` // PostToolUse only

	// FileChanged fields
	FilePath     string `json:"file_path,omitempty"`
	MatcherValue string `json:"matcher_value,omitempty"`
	Change       string `json:"change,omitempty"` // "created", "modified", "deleted"

	// InstructionsLoaded fields
	MemoryType      string   `json:"memory_type,omitempty"`       // "User", "Project", "Local", "Managed"
	LoadReason      string   `json:"load_reason,omitempty"`       // "session_start", "nested_traversal", "path_glob_match", "include", "compact"
	Globs           []string `json:"globs,omitempty"`             // path glob patterns from frontmatter
	TriggerFilePath string   `json:"trigger_file_path,omitempty"` // file that triggered lazy load
	ParentFilePath  string   `json:"parent_file_path,omitempty"`  // parent instruction file (for includes)
}

// HookSpecificOutput contains event-specific fields in the hook response.
// Fields are a superset across hook events; unused fields are omitted.
type HookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext,omitempty"`
	Decision          string `json:"decision,omitempty"`  // "block" to reject the prompt
	Reason            string `json:"reason,omitempty"`    // shown to user when blocking

	// PreToolUse permission fields
	PermissionDecision       string `json:"permissionDecision,omitempty"`       // "allow", "deny"
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"` // explanation for the decision
}

// HookOutput is the JSON response written to stdout for Claude Code to consume.
// For UserPromptSubmit, additionalContext is APPENDED to Claude's context
// alongside the original prompt — it does not replace it.
type HookOutput struct {
	SuppressOutput     bool                `json:"suppressOutput,omitempty"`
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
		SuppressOutput: true,
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName:     "UserPromptSubmit",
			AdditionalContext: context,
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

// SessionStartOutput returns a SessionStart response with additionalContext.
// Unlike ContextOutput, this does not suppress output and sets the correct hook event name.
func SessionStartOutput(context string) *HookOutput {
	return &HookOutput{
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName:     "SessionStart",
			AdditionalContext: context,
		},
	}
}

// PermitOutput returns a PreToolUse permission decision.
// decision is "allow" or "deny". For passthrough (no opinion), use PassthroughOutput().
func PermitOutput(decision, reason string) *HookOutput {
	return &HookOutput{
		SuppressOutput: true,
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName:           "PreToolUse",
			PermissionDecision:       decision,
			PermissionDecisionReason: reason,
		},
	}
}
