package prompt

import (
	"strings"
	"testing"
)

func TestFrameContext(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // empty means expect empty return
	}{
		{
			name:  "NO_ADDITIONAL_CONTEXT sentinel (legacy)",
			input: "NO_ADDITIONAL_CONTEXT",
			want:  "",
		},
		{
			name:  "sentinel with whitespace (legacy)",
			input: "  NO_ADDITIONAL_CONTEXT  \n",
			want:  "",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace only",
			input: "   \n\t  ",
			want:  "",
		},
		{
			name:  "valid XML output",
			input: "<context>\n<intent>Fix the auth bug</intent>\n</context>",
			want:  "context", // just check it's present
		},
		{
			name:  "output with leading whitespace",
			input: "\n  <context>\n<intent>Test</intent>\n</context>\n  ",
			want:  "context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FrameContext(tt.input)

			if tt.want == "" {
				if got != "" {
					t.Errorf("expected empty, got %q", got)
				}
				return
			}

			// Non-empty cases: verify framing header is present
			if !strings.HasPrefix(got, "[Project rules analysis") {
				t.Errorf("missing framing header, got: %q", got[:min(80, len(got))])
			}

			// Verify the LLM output is present
			if !strings.Contains(got, tt.want) {
				t.Errorf("output missing expected content %q", tt.want)
			}
		})
	}
}

func TestFrameContext_NoPersonaLanguage(t *testing.T) {
	output := "<context><intent>Test</intent></context>"
	framed := FrameContext(output)

	forbidden := []string{
		"Prompt Architect",
		"specialist",
		"expert",
		"role",
	}
	for _, word := range forbidden {
		if strings.Contains(strings.ToLower(framed), word) {
			t.Errorf("framing contains persona language: %q", word)
		}
	}
}
