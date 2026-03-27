package prompt

import (
	_ "embed"
	"log/slog"
	"os"
	"path/filepath"
)

//go:embed system_prompt.tmpl
var defaultSystemPrompt string

// SystemPrompt returns the active system prompt, checking for user
// overrides before falling back to the embedded default.
// Search order:
//  1. .restruct/system_prompt.tmpl (project-local, relative to cwd)
//  2. ~/.config/restruct/system_prompt.tmpl (user-global)
//  3. Embedded default
func SystemPrompt() string {
	return LoadSystemPrompt("")
}

// LoadSystemPrompt loads the system prompt with the given working directory
// for project-local override resolution. Empty cwd uses current directory.
func LoadSystemPrompt(cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	// 1. Project-local override
	projectPath := filepath.Join(cwd, ".restruct", "system_prompt.tmpl")
	if content, err := os.ReadFile(projectPath); err == nil {
		slog.Debug("using project-local system prompt", "path", projectPath)
		return string(content)
	}

	// 2. User-global override
	home, err := os.UserHomeDir()
	if err == nil {
		globalPath := filepath.Join(home, ".config", "restruct", "system_prompt.tmpl")
		if content, err := os.ReadFile(globalPath); err == nil {
			slog.Debug("using user-global system prompt", "path", globalPath)
			return string(content)
		}
	}

	// 3. Embedded default
	return defaultSystemPrompt
}
