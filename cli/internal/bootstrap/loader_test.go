package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewMapLoader_NoIndex(t *testing.T) {
	ml := NewMapLoader(filepath.Join(t.TempDir(), "nonexistent"))
	if ml != nil {
		t.Error("expected nil for nonexistent dir")
	}
}

func TestNewMapLoader_EmptyIndex(t *testing.T) {
	linksDir := filepath.Join(t.TempDir(), "links")
	pm := &ProjectMap{Version: 1, Files: []MapEntry{}}
	WriteMap(pm, linksDir)

	ml := NewMapLoader(linksDir)
	if ml != nil {
		t.Error("expected nil for empty index")
	}
}

func TestNewMapLoader_ValidIndex(t *testing.T) {
	linksDir := setupTestLinks(t)

	ml := NewMapLoader(linksDir)
	if ml == nil {
		t.Fatal("expected non-nil MapLoader")
	}
	if len(ml.Map().Files) != 2 {
		t.Errorf("expected 2 files in map, got %d", len(ml.Map().Files))
	}
}

func TestFormatMapForLLM(t *testing.T) {
	linksDir := setupTestLinks(t)
	ml := NewMapLoader(linksDir)
	if ml == nil {
		t.Fatal("expected non-nil MapLoader")
	}

	formatted := ml.FormatMapForLLM()
	if !strings.Contains(formatted, "Project Document Map") {
		t.Errorf("missing header in formatted map: %s", formatted)
	}
	if !strings.Contains(formatted, "CLAUDE.md") {
		t.Errorf("missing root doc in map: %s", formatted)
	}
	if !strings.Contains(formatted, "web/CLAUDE.md") {
		t.Errorf("missing web doc in map: %s", formatted)
	}
}

func TestLoadSelected_ValidIndices(t *testing.T) {
	linksDir := setupTestLinks(t)
	ml := NewMapLoader(linksDir)
	if ml == nil {
		t.Fatal("expected non-nil MapLoader")
	}

	// Select only the web doc (index 1)
	content, scopedRules, err := ml.LoadSelected([]int{1})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(content, "web/CLAUDE.md") {
		t.Errorf("expected web rules in content, got: %s", content[:min(len(content), 200)])
	}

	if _, ok := scopedRules["web/CLAUDE.md"]; !ok {
		t.Error("expected web/CLAUDE.md in scoped rules")
	}

	// Should NOT contain root rules
	if _, ok := scopedRules["CLAUDE.md"]; ok {
		t.Error("should not contain root CLAUDE.md when only web selected")
	}
}

func TestLoadSelected_InvalidIndices(t *testing.T) {
	linksDir := setupTestLinks(t)
	ml := NewMapLoader(linksDir)

	// Invalid indices are silently skipped
	content, scopedRules, err := ml.LoadSelected([]int{-1, 99})
	if err != nil {
		t.Fatal(err)
	}
	if content != "" {
		t.Errorf("expected empty content for invalid indices, got: %s", content)
	}
	if len(scopedRules) != 0 {
		t.Errorf("expected no scoped rules for invalid indices, got %d", len(scopedRules))
	}
}

func TestLoadSelected_EmptyIndices(t *testing.T) {
	linksDir := setupTestLinks(t)
	ml := NewMapLoader(linksDir)

	content, scopedRules, err := ml.LoadSelected([]int{})
	if err != nil {
		t.Fatal(err)
	}
	if content != "" {
		t.Error("expected empty content for empty indices")
	}
	if scopedRules != nil {
		t.Error("expected nil scoped rules for empty indices")
	}
}

func TestSelectedSources(t *testing.T) {
	linksDir := setupTestLinks(t)
	ml := NewMapLoader(linksDir)

	sources := ml.SelectedSources([]int{0, 1})
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
	if sources[0] != "CLAUDE.md" {
		t.Errorf("expected CLAUDE.md, got %s", sources[0])
	}
	if sources[1] != "web/CLAUDE.md" {
		t.Errorf("expected web/CLAUDE.md, got %s", sources[1])
	}
}

func TestLoadSelected_FallsBackToSource(t *testing.T) {
	// Create a project map but DON'T write the link document files.
	// LoadSelected should fall back to reading the source file.
	root := t.TempDir()
	linksDir := filepath.Join(root, "links")
	os.MkdirAll(linksDir, 0755)

	srcPath := filepath.Join(root, "CLAUDE.md")
	writeFile(t, srcPath, "## Constraints\n- rule from source\n")

	pm := &ProjectMap{
		Version:    1,
		Generated:  time.Now(),
		TotalRules: 1,
		Files: []MapEntry{
			{Source: "CLAUDE.md", AbsPath: srcPath, Hash: "nosuchfile", RuleCount: 1, Summary: "test"},
		},
	}
	WriteMap(pm, linksDir)

	ml := NewMapLoader(linksDir)
	if ml == nil {
		t.Fatal("expected non-nil MapLoader")
	}

	content, scopedRules, err := ml.LoadSelected([]int{0})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "rule from source") {
		t.Errorf("expected fallback to source file, got: %s", content)
	}
	if pr, ok := scopedRules["CLAUDE.md"]; !ok || len(pr.ConstraintRules) == 0 {
		t.Error("expected parsed constraint rules from source")
	}
}

// --- helpers ---

func setupTestLinks(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	linksDir := filepath.Join(root, "links")

	// Create source files
	rootContent := "# Root\n## Constraints\n- root constraint\n## Do NOT\n- root anti\n"
	webContent := "# Web\n## Constraints\n- web constraint\n## Workflow\n- web workflow\n"

	rootPath := filepath.Join(root, "CLAUDE.md")
	webPath := filepath.Join(root, "web", "CLAUDE.md")
	writeFile(t, rootPath, rootContent)
	writeFile(t, webPath, webContent)

	// Generate documents
	rootDoc, _ := GenerateDocument(DiscoveredFile{AbsPath: rootPath, RelPath: "CLAUDE.md", ModTime: time.Now()})
	webDoc, _ := GenerateDocument(DiscoveredFile{AbsPath: webPath, RelPath: "web/CLAUDE.md", ModTime: time.Now()})

	// Write link documents
	WriteDocument(rootDoc, linksDir)
	WriteDocument(webDoc, linksDir)

	// Build and write map
	pm := BuildMap([]*Document{rootDoc, webDoc})
	WriteMap(pm, linksDir)

	return linksDir
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
