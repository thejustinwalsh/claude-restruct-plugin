package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstall(t *testing.T) {
	t.Run("fresh install creates settings", func(t *testing.T) {
		dir := t.TempDir()
		if err := Install(dir, false); err != nil {
			t.Fatalf("Install: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
		if err != nil {
			t.Fatalf("read settings: %v", err)
		}

		var settings map[string]any
		if err := json.Unmarshal(data, &settings); err != nil {
			t.Fatalf("parse settings: %v", err)
		}

		hooks, ok := settings["hooks"].(map[string]any)
		if !ok {
			t.Fatal("settings missing hooks key")
		}

		// Check UserPromptSubmit is present (not UserPrompt)
		if _, ok := hooks["UserPromptSubmit"]; !ok {
			t.Error("missing UserPromptSubmit hook")
		}
		if _, ok := hooks["UserPrompt"]; ok {
			t.Error("should NOT have UserPrompt (old name)")
		}

		// Check SessionStart and SessionEnd are present
		if _, ok := hooks["SessionStart"]; !ok {
			t.Error("missing SessionStart hook")
		}
		if _, ok := hooks["SessionEnd"]; !ok {
			t.Error("missing SessionEnd hook")
		}

		// Verify matcher is empty string (not ".*")
		uph := hooks["UserPromptSubmit"].([]any)
		entry := uph[0].(map[string]any)
		if matcher, _ := entry["matcher"].(string); matcher != "" {
			t.Errorf("matcher = %q, want empty string", matcher)
		}
	})

	t.Run("preserves existing settings", func(t *testing.T) {
		dir := t.TempDir()
		claudeDir := filepath.Join(dir, ".claude")
		os.MkdirAll(claudeDir, 0755)

		existing := `{"permissions":{"allow":["Read"]}}`
		os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(existing), 0644)

		if err := Install(dir, false); err != nil {
			t.Fatalf("Install: %v", err)
		}

		data, _ := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
		var settings map[string]any
		json.Unmarshal(data, &settings)

		// Original settings preserved
		if _, ok := settings["permissions"]; !ok {
			t.Error("existing permissions key was lost")
		}
		// New hooks added
		if _, ok := settings["hooks"]; !ok {
			t.Error("hooks not added")
		}
	})
}

func TestEnsureGitignore(t *testing.T) {
	t.Run("creates gitignore if missing", func(t *testing.T) {
		dir := t.TempDir()
		if err := ensureGitignore(dir); err != nil {
			t.Fatalf("ensureGitignore: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
		if err != nil {
			t.Fatalf("read gitignore: %v", err)
		}
		if !strings.Contains(string(data), ".restruct/") {
			t.Error("gitignore missing .restruct/ entry")
		}
	})

	t.Run("appends to existing gitignore", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules/\n"), 0644)

		if err := ensureGitignore(dir); err != nil {
			t.Fatalf("ensureGitignore: %v", err)
		}

		data, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
		content := string(data)
		if !strings.Contains(content, "node_modules/") {
			t.Error("existing entries lost")
		}
		if !strings.Contains(content, ".restruct/") {
			t.Error("missing .restruct/ entry")
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		dir := t.TempDir()
		ensureGitignore(dir)
		ensureGitignore(dir)

		data, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
		count := strings.Count(string(data), ".restruct/")
		if count != 1 {
			t.Errorf(".restruct/ appears %d times, want 1", count)
		}
	})
}
