package hook

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseInput(t *testing.T) {
	t.Run("valid input", func(t *testing.T) {
		input := `{
			"session_id": "abc-123",
			"transcript_path": "/tmp/transcript.jsonl",
			"cwd": "/home/dev/project",
			"permission_mode": "default",
			"hook_event_name": "UserPromptSubmit",
			"prompt": "fix the auth bug"
		}`
		hi, err := ParseInput(strings.NewReader(input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hi.SessionID != "abc-123" {
			t.Errorf("session_id = %q, want %q", hi.SessionID, "abc-123")
		}
		if hi.TranscriptPath != "/tmp/transcript.jsonl" {
			t.Errorf("transcript_path = %q, want %q", hi.TranscriptPath, "/tmp/transcript.jsonl")
		}
		if hi.Cwd != "/home/dev/project" {
			t.Errorf("cwd = %q, want %q", hi.Cwd, "/home/dev/project")
		}
		if hi.HookEventName != "UserPromptSubmit" {
			t.Errorf("hook_event_name = %q, want %q", hi.HookEventName, "UserPromptSubmit")
		}
		if hi.Prompt != "fix the auth bug" {
			t.Errorf("prompt = %q, want %q", hi.Prompt, "fix the auth bug")
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		_, err := ParseInput(strings.NewReader("{bad json"))
		if err == nil {
			t.Fatal("expected error for malformed JSON")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		_, err := ParseInput(strings.NewReader(""))
		if err == nil {
			t.Fatal("expected error for empty input")
		}
	})

	t.Run("extra fields ignored", func(t *testing.T) {
		input := `{"session_id":"s1","prompt":"hi","unknown_field":"ignored"}`
		hi, err := ParseInput(strings.NewReader(input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hi.SessionID != "s1" {
			t.Errorf("session_id = %q, want %q", hi.SessionID, "s1")
		}
	})
}

func TestWriteOutput(t *testing.T) {
	t.Run("nil output writes nothing", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteOutput(&buf, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if buf.Len() != 0 {
			t.Errorf("expected empty output, got %q", buf.String())
		}
	})

	t.Run("context output", func(t *testing.T) {
		var buf bytes.Buffer
		out := ContextOutput("refined instructions here")
		err := WriteOutput(&buf, out)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var decoded HookOutput
		if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
			t.Fatalf("output is not valid JSON: %v", err)
		}
		if decoded.HookSpecificOutput == nil {
			t.Fatal("hookSpecificOutput is nil")
		}
		if decoded.HookSpecificOutput.HookEventName != "UserPromptSubmit" {
			t.Errorf("hookEventName = %q, want %q", decoded.HookSpecificOutput.HookEventName, "UserPromptSubmit")
		}
		if decoded.HookSpecificOutput.AdditionalContext != "refined instructions here" {
			t.Errorf("additionalContext = %q, want %q", decoded.HookSpecificOutput.AdditionalContext, "refined instructions here")
		}
		if !decoded.HookSpecificOutput.SuppressOutput {
			t.Error("suppressOutput should be true for context output")
		}
	})

	t.Run("block output", func(t *testing.T) {
		var buf bytes.Buffer
		out := BlockOutput("prompt rejected")
		err := WriteOutput(&buf, out)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var decoded HookOutput
		if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
			t.Fatalf("output is not valid JSON: %v", err)
		}
		if decoded.HookSpecificOutput.Decision != "block" {
			t.Errorf("decision = %q, want %q", decoded.HookSpecificOutput.Decision, "block")
		}
		if decoded.HookSpecificOutput.Reason != "prompt rejected" {
			t.Errorf("reason = %q, want %q", decoded.HookSpecificOutput.Reason, "prompt rejected")
		}
	})
}

func TestPassthroughOutput(t *testing.T) {
	out := PassthroughOutput()
	if out != nil {
		t.Error("PassthroughOutput should return nil")
	}
}
