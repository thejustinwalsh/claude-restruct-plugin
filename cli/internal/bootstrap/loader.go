package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tjw/restruct/internal/prompt"
)

// MapLoader loads deep-context documents selected by the LLM during refinement.
// Falls back to nil when no project map exists (triggering flat rules loading).
type MapLoader struct {
	projectMap *ProjectMap
	linksDir   string
}

// NewMapLoader creates a loader from the project map at the given links directory.
// Returns nil if no index.json exists or is unreadable.
func NewMapLoader(linksDir string) *MapLoader {
	pm, err := LoadMap(linksDir)
	if err != nil || pm == nil || len(pm.Files) == 0 {
		return nil
	}
	return &MapLoader{
		projectMap: pm,
		linksDir:   linksDir,
	}
}

// Map returns the loaded project map.
func (l *MapLoader) Map() *ProjectMap {
	return l.projectMap
}

// IsStale returns true if any source file has been modified since the index was generated.
func (l *MapLoader) IsStale() bool {
	indexPath := filepath.Join(l.linksDir, "index.json")
	indexInfo, err := os.Stat(indexPath)
	if err != nil {
		return true
	}
	indexMtime := indexInfo.ModTime()

	for _, f := range l.projectMap.Files {
		info, err := os.Stat(f.AbsPath)
		if err != nil {
			continue // file may have been deleted; FileChanged will handle it
		}
		if info.ModTime().After(indexMtime) {
			return true
		}
	}
	return false
}

// StaleFiles returns the list of source files modified after the index.
func (l *MapLoader) StaleFiles() []string {
	indexPath := filepath.Join(l.linksDir, "index.json")
	indexInfo, err := os.Stat(indexPath)
	if err != nil {
		return nil
	}
	indexMtime := indexInfo.ModTime()

	var stale []string
	for _, f := range l.projectMap.Files {
		info, err := os.Stat(f.AbsPath)
		if err != nil {
			continue
		}
		if info.ModTime().After(indexMtime) {
			stale = append(stale, f.Source)
		}
	}
	return stale
}

// FormatMapForLLM returns the project map formatted for the local LLM's user message.
func (l *MapLoader) FormatMapForLLM() string {
	if l.projectMap == nil {
		return ""
	}
	return l.projectMap.FormatForLLM()
}

// LoadSelected reads the specified documents by index and returns their
// combined rules content with source attribution, plus merged ParsedRules.
// Invalid indices are silently skipped.
func (l *MapLoader) LoadSelected(docIndices []int) (string, map[string]*prompt.ParsedRules, error) {
	if len(docIndices) == 0 {
		return "", nil, nil
	}

	var contentParts []string
	scopedRules := make(map[string]*prompt.ParsedRules)

	for _, idx := range docIndices {
		if idx < 0 || idx >= len(l.projectMap.Files) {
			continue
		}
		entry := l.projectMap.Files[idx]

		// Read the original source file (preserves bullet format for ParseRules).
		// Fall back to link document if source is unavailable.
		data, err := os.ReadFile(entry.AbsPath)
		if err != nil {
			docPath := filepath.Join(l.linksDir, entry.Hash+".md")
			data, err = os.ReadFile(docPath)
			if err != nil {
				continue
			}
		}

		content := string(data)

		// Strip YAML frontmatter if present (from .restruct/links/ files)
		if strings.HasPrefix(content, "---\n") {
			if end := strings.Index(content[4:], "---\n"); end >= 0 {
				content = content[end+8:] // skip past closing ---\n
			}
		}

		contentParts = append(contentParts, fmt.Sprintf("## Rules from %s\n%s", entry.Source, content))

		// Parse rules for this document
		parsed := prompt.ParseRules(content)
		scopedRules[entry.Source] = parsed
	}

	return strings.Join(contentParts, "\n\n"), scopedRules, nil
}

// SelectedSources returns the source paths for the given document indices.
func (l *MapLoader) SelectedSources(docIndices []int) []string {
	var sources []string
	for _, idx := range docIndices {
		if idx >= 0 && idx < len(l.projectMap.Files) {
			sources = append(sources, l.projectMap.Files[idx].Source)
		}
	}
	return sources
}
