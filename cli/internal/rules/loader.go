package rules

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Loader reads and concatenates project rule files.
type Loader struct {
	SearchPaths []string
	ProjectDir  string
}

func NewLoader(projectDir string, files []string) *Loader {
	return &Loader{
		SearchPaths: files,
		ProjectDir:  projectDir,
	}
}

// Load reads all existing rule files and returns their concatenated content.
func (l *Loader) Load() (string, error) {
	var parts []string
	for _, f := range l.SearchPaths {
		path := filepath.Join(l.ProjectDir, f)
		data, err := os.ReadFile(path)
		if err != nil {
			continue // File doesn't exist, skip
		}
		parts = append(parts, fmt.Sprintf("## Rules from %s\n%s", f, string(data)))
	}
	return strings.Join(parts, "\n\n"), nil
}

// Hash returns a SHA256 hash of the current rules content for cache keying.
func (l *Loader) Hash() (string, error) {
	content, err := l.Load()
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h), nil
}
