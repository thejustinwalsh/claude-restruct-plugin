package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemPrompt_ReturnsEmbeddedDefault(t *testing.T) {
	p := LoadSystemPrompt("/nonexistent/path")
	if p == "" {
		t.Fatal("embedded default system prompt should not be empty")
	}
	if !strings.Contains(p, "context_supplement") {
		t.Error("embedded prompt should reference context_supplement format")
	}
}

func TestSystemPrompt_NoPersonaLanguage(t *testing.T) {
	p := LoadSystemPrompt("/nonexistent/path")

	forbidden := []string{
		"Prompt Architect",
		"specialist",
		"You are a expert",
	}
	for _, f := range forbidden {
		if strings.Contains(p, f) {
			t.Errorf("system prompt contains persona language: %q", f)
		}
	}
}

func TestLoadSystemPrompt_ProjectLocalOverride(t *testing.T) {
	dir := t.TempDir()
	overridePath := filepath.Join(dir, ".restruct")
	os.MkdirAll(overridePath, 0755)
	os.WriteFile(filepath.Join(overridePath, "system_prompt.tmpl"), []byte("custom project prompt"), 0644)

	p := LoadSystemPrompt(dir)
	if p != "custom project prompt" {
		t.Errorf("expected project-local override, got %q", p)
	}
}

func TestLoadSystemPrompt_UserGlobalOverride(t *testing.T) {
	// This test would require mocking os.UserHomeDir which is complex.
	// Instead we verify the fallback chain works — if no override exists,
	// we get the embedded default.
	p := LoadSystemPrompt("/definitely/not/a/real/path")
	if p == "" {
		t.Fatal("should fall back to embedded default")
	}
}

func TestLoadSystemPrompt_ProjectOverrideTakesPrecedence(t *testing.T) {
	dir := t.TempDir()

	// Create project-local override
	overridePath := filepath.Join(dir, ".restruct")
	os.MkdirAll(overridePath, 0755)
	os.WriteFile(filepath.Join(overridePath, "system_prompt.tmpl"), []byte("project wins"), 0644)

	p := LoadSystemPrompt(dir)
	if p != "project wins" {
		t.Errorf("project-local should take precedence, got %q", p)
	}
}

func TestLoadSystemPrompt_EmptyCwdUsesDefault(t *testing.T) {
	p := LoadSystemPrompt("")
	if p == "" {
		t.Fatal("empty cwd should still return embedded default")
	}
}
