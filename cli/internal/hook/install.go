package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Install adds restruct as a UserPrompt hook in .claude/settings.json.
func Install(projectDir string, global bool) error {
	var settingsPath string
	if global {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home dir: %w", err)
		}
		settingsPath = filepath.Join(home, ".claude", "settings.json")
	} else {
		settingsPath = filepath.Join(projectDir, ".claude", "settings.json")
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return fmt.Errorf("create settings dir: %w", err)
	}

	// Load existing settings or start fresh
	settings := make(map[string]any)
	data, err := os.ReadFile(settingsPath)
	if err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("parse existing settings: %w", err)
		}
	}

	// Resolve restruct binary path
	binaryPath, err := resolveRestructBinary()
	if err != nil {
		return fmt.Errorf("resolve binary: %w", err)
	}

	// Build the hook entry
	hookEntry := map[string]any{
		"matcher": ".*",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": binaryPath + " refine",
			},
		},
	}

	// Merge into settings
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		hooks = make(map[string]any)
	}
	hooks["UserPrompt"] = []any{hookEntry}
	settings["hooks"] = hooks

	// Write back
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, out, 0644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	return nil
}

func resolveRestructBinary() (string, error) {
	// Try to find the installed binary
	path, err := exec.LookPath("restruct")
	if err == nil {
		return path, nil
	}
	// Fall back to current executable
	return os.Executable()
}
