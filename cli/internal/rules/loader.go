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
// Searches the project directory and walks up to the git root to find rules.
func (l *Loader) Load() (string, error) {
	dirs := l.searchDirs()
	var parts []string
	seen := make(map[string]bool)

	for _, dir := range dirs {
		for _, f := range l.SearchPaths {
			path := filepath.Join(dir, f)
			abs, err := filepath.Abs(path)
			if err != nil {
				continue
			}
			if seen[abs] {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			seen[abs] = true
			parts = append(parts, fmt.Sprintf("## Rules from %s\n%s", f, string(data)))
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// searchDirs returns directories to search for rules files, starting from
// ProjectDir and walking up to the git root (or filesystem root).
func (l *Loader) searchDirs() []string {
	dirs := []string{l.ProjectDir}
	dir := l.ProjectDir
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dirs = append(dirs, parent)
		// Stop at git root
		if _, err := os.Stat(filepath.Join(parent, ".git")); err == nil {
			break
		}
		dir = parent
	}
	return dirs
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
