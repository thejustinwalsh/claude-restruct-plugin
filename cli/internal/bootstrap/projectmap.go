package bootstrap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProjectMap is the master index stored as .restruct/links/index.json.
type ProjectMap struct {
	Version    int        `json:"version"`
	Generated  time.Time  `json:"generated"`
	Files      []MapEntry `json:"files"`
	TotalRules int        `json:"total_rules"`
	Partial    bool       `json:"partial,omitempty"` // true if bootstrap was cut short by timeout
}

// MapEntry represents one discovered rule file in the project map.
type MapEntry struct {
	Source     string   `json:"source"`
	AbsPath   string   `json:"path"`
	Hash      string   `json:"hash"`
	Keywords  []string `json:"keywords"`
	Categories []string `json:"categories"`
	Summary   string   `json:"summary"`
	RuleCount int      `json:"rule_count"`
}

// BuildMap aggregates documents into a ProjectMap.
func BuildMap(docs []*Document) *ProjectMap {
	pm := &ProjectMap{
		Version:   1,
		Generated: time.Now().UTC(),
		Files:     make([]MapEntry, 0, len(docs)),
	}

	for _, doc := range docs {
		pm.Files = append(pm.Files, MapEntry{
			Source:     doc.Source,
			AbsPath:   doc.AbsPath,
			Hash:      doc.Hash,
			Keywords:  doc.Keywords,
			Categories: doc.Categories,
			Summary:   doc.Summary,
			RuleCount: doc.RuleCount,
		})
		pm.TotalRules += doc.RuleCount
	}

	return pm
}

// WriteMap writes the project map to .restruct/links/index.json.
func WriteMap(pm *ProjectMap, linksDir string) error {
	if err := os.MkdirAll(linksDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(pm, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal project map: %w", err)
	}

	path := filepath.Join(linksDir, "index.json")
	return os.WriteFile(path, data, 0644)
}

// LoadMap reads the project map from .restruct/links/index.json.
// Returns nil, nil if no index exists.
func LoadMap(linksDir string) (*ProjectMap, error) {
	path := filepath.Join(linksDir, "index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read project map: %w", err)
	}

	var pm ProjectMap
	if err := json.Unmarshal(data, &pm); err != nil {
		return nil, fmt.Errorf("parse project map: %w", err)
	}
	return &pm, nil
}

// ReindexStale re-processes any source files that are newer than the index.
// Called lazily during refine when FileChanged hooks aren't available.
// Updates documents in-place and rewrites the project map.
func ReindexStale(pm *ProjectMap, linksDir string, ruleFiles []string) {
	if pm == nil {
		return
	}

	indexPath := filepath.Join(linksDir, "index.json")
	indexInfo, err := os.Stat(indexPath)
	if err != nil {
		return
	}
	indexMtime := indexInfo.ModTime()

	changed := false
	for i, entry := range pm.Files {
		info, err := os.Stat(entry.AbsPath)
		if err != nil {
			// File deleted — remove from map
			pm.Files = append(pm.Files[:i], pm.Files[i+1:]...)
			os.Remove(filepath.Join(linksDir, entry.Hash+".md"))
			changed = true
			continue
		}
		if !info.ModTime().After(indexMtime) {
			continue
		}

		// Re-generate the document
		file := DiscoveredFile{
			AbsPath: entry.AbsPath,
			RelPath: entry.Source,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		doc, err := GenerateDocument(file)
		if err != nil {
			continue
		}
		WriteDocument(doc, linksDir)

		// Update map entry
		pm.Files[i] = MapEntry{
			Source:     doc.Source,
			AbsPath:   doc.AbsPath,
			Hash:      doc.Hash,
			Keywords:  doc.Keywords,
			Categories: doc.Categories,
			Summary:   doc.Summary,
			RuleCount: doc.RuleCount,
		}
		changed = true
	}

	if changed {
		// Recompute totals
		pm.TotalRules = 0
		for _, f := range pm.Files {
			pm.TotalRules += f.RuleCount
		}
		pm.Generated = time.Now().UTC()
		WriteMap(pm, linksDir)
		ClearClassified(linksDir) // stale classification after rule change
	}
}

// LinksDir returns the path to the .restruct/links/ directory for a project.
func LinksDir(projectDir string) string {
	return filepath.Join(projectDir, ".restruct", "links")
}

// FormatForClaude returns the project map as formatted markdown suitable
// for SessionStart additionalContext injection. Claude sees this as project
// awareness — which rule documents exist and what they cover.
func (pm *ProjectMap) FormatForClaude() string {
	if len(pm.Files) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("[Restruct project context index — %d rule documents, %d rules total]\n\n",
		len(pm.Files), pm.TotalRules))
	b.WriteString("Available project rule documents:\n")

	for i, f := range pm.Files {
		cats := strings.Join(f.Categories, ", ")
		b.WriteString(fmt.Sprintf("%d. %s — %s [%d rules: %s]\n",
			i, f.Source, f.Summary, f.RuleCount, cats))
	}

	// Aggregate keywords across all documents
	allKeywords := make(map[string]bool)
	for _, f := range pm.Files {
		for _, kw := range f.Keywords {
			allKeywords[kw] = true
		}
	}
	var kwList []string
	for kw := range allKeywords {
		kwList = append(kwList, kw)
	}
	if len(kwList) > 20 {
		kwList = kwList[:20]
	}

	if len(kwList) > 0 {
		b.WriteString(fmt.Sprintf("\nKeywords across all documents: %s\n",
			strings.Join(kwList, ", ")))
	}

	return b.String()
}

// FormatForLLM returns a compact version of the project map for the local
// LLM's user message during refinement. The LLM uses this to select which
// documents are relevant to the current request.
func (pm *ProjectMap) FormatForLLM() string {
	if len(pm.Files) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Project Document Map\n")

	for i, f := range pm.Files {
		b.WriteString(fmt.Sprintf("%d. %s — %s [%d rules]\n",
			i, f.Source, f.Summary, f.RuleCount))
	}

	b.WriteString("\nSelect relevant documents by index in the \"relevant_docs\" field.\n")
	return b.String()
}
