package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Install adds restruct as a UserPromptSubmit hook in .claude/settings.json
// and optionally adds SessionStart/SessionEnd hooks for session tracking.
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

	// Build hook entries per Claude Code's hook contract:
	// - UserPromptSubmit: no matcher support (use empty string)
	// - type: "command" is the only supported type for UserPromptSubmit
	userPromptHook := map[string]any{
		"matcher": "",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": binaryPath + " refine",
			},
		},
	}

	sessionStartHook := map[string]any{
		"matcher": "",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": binaryPath + " session start",
			},
		},
	}

	sessionEndHook := map[string]any{
		"matcher": "",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": binaryPath + " session end",
			},
		},
	}

	// Merge into settings, preserving existing non-restruct hooks
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		hooks = make(map[string]any)
	}
	hooks["UserPromptSubmit"] = []any{userPromptHook}
	hooks["SessionStart"] = []any{sessionStartHook}
	hooks["SessionEnd"] = []any{sessionEndHook}
	settings["hooks"] = hooks

	// Write back
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, out, 0644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	// Add .restruct/ to .gitignore if installing at project level
	if !global {
		if err := ensureGitignore(projectDir); err != nil {
			fmt.Fprintf(os.Stderr, "restruct: warning: could not update .gitignore: %v\n", err)
		}
	}

	return nil
}

// ensureGitignore adds .restruct/ to the project's .gitignore if not already present.
func ensureGitignore(projectDir string) error {
	gitignorePath := filepath.Join(projectDir, ".gitignore")

	content, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	entry := ".restruct/"
	// Check if already present
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == entry {
			return nil // already there
		}
	}

	// Append
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// Add newline before entry if file doesn't end with one
	prefix := ""
	if len(content) > 0 && content[len(content)-1] != '\n' {
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + entry + "\n")
	return err
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
