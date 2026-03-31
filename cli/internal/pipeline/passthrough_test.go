package pipeline

import "testing"

func TestShouldRefine(t *testing.T) {
	tests := []struct {
		prompt string
		want   bool
	}{
		// --- Should NOT refine (passthrough) ---

		// Empty
		{"", false},
		{"   ", false},

		// Explicit bypass with "| " prefix
		{"| fix the bug without refinement", false},
		{"| just do it raw", false},

		// Affirmatives
		{"yes", false},
		{"YES", false},
		{"Yes", false},
		{"y", false},
		{"yeah", false},
		{"yep", false},
		{"yup", false},
		{"ok", false},
		{"okay", false},
		{"OK", false},
		{"k", false},
		{"sure", false},
		{"sounds good", false},
		{"looks good", false},
		{"LGTM", false},
		{"lgtm", false},
		{"go ahead", false},
		{"do it", false},
		{"proceed", false},
		{"continue", false},
		{"approved", false},

		// Negatives/cancellations
		{"no", false},
		{"nope", false},
		{"nah", false},
		{"cancel", false},
		{"stop", false},
		{"nevermind", false},
		{"never mind", false},

		// Thanks
		{"thanks", false},
		{"thank you", false},

		// Numbered selections
		{"1", false},
		{"3", false},
		{"option 1", false},
		{"Option 2", false},
		{"try option 2", false},
		{"Try Option 3", false},
		{"choice 1", false},
		{"approach 2", false},
		{"number 3", false},
		{"try 2", false},

		// Slash commands
		{"/help", false},
		{"/commit", false},
		{"/review-pr 123", false},

		// Whitespace handling
		{"  yes  ", false},
		{" /help ", false},
		{"  3  ", false},

		// --- Should refine ---

		// Actionable requests (even short)
		{"fix the bug", true},
		{"add dark mode", true},
		{"refactor the database layer", true},
		{"why is the app slow", true},

		// "yes please" has extra intent beyond simple affirmative
		{"yes please fix it", true},
		{"yes and also add tests", true},

		// "fix option 2" is an action, not a selection
		{"fix option 2", true},

		// Questions
		{"what does this function do", true},
		{"how does auth work", true},

		// Long prompts
		{"hey can you fix the auth bug where tokens expire too fast", true},

		// Prompts that start with affirmative words but have content
		{"ok so here is what I need", true},
		{"sure but first let me explain", true},
		{"yes I want to add a new endpoint for user profiles", true},

		// Code snippets
		{"update the function: func foo() {}", true},

		// Numbers that aren't selections
		{"add 3 retries to the http client", true},
		{"fix issue 42", true},

		// Pipe without space or in the middle — should still refine
		{"|no space", true},
		{"fix the bug | grep error", true},
	}

	for _, tt := range tests {
		t.Run(tt.prompt, func(t *testing.T) {
			got := ShouldRefine(tt.prompt)
			if got != tt.want {
				t.Errorf("ShouldRefine(%q) = %v, want %v", tt.prompt, got, tt.want)
			}
		})
	}
}
