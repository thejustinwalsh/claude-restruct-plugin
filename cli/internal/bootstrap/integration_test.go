package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestIntegration_FullBootstrapFlow tests the complete bootstrap pipeline:
// discover → generate → build map → classify → verify scoped rules.
func TestIntegration_FullBootstrapFlow(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)

	// Create rule files at different depths
	writeFile(t, filepath.Join(root, "CLAUDE.md"), `# Restruct

## Build & Test
- pnpm dev — starts dev server
- pnpm build — release build

## Constraints
- CLI writes to SQLite
- All inference is local via Ollama

## Workflow
- Conventional commits

## Do NOT
- Do not use CGO
`)

	os.MkdirAll(filepath.Join(root, "web"), 0755)
	writeFile(t, filepath.Join(root, "web", "CLAUDE.md"), `# Web Dashboard

## Stack
- React 19 + TypeScript
- Tailwind CSS

## Do NOT
- Do not use React.FC
- Do not use as assertions
`)

	os.MkdirAll(filepath.Join(root, "cli"), 0755)
	writeFile(t, filepath.Join(root, "cli", "CLAUDE.md"), `# CLI

## Code Style
- Pure Go, no CGO
- Use cobra for commands

## Constraints
- Performance is critical
`)

	// Phase 1: Discover
	result, err := Discover(root, []string{"CLAUDE.md"}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(result.Files))
	}

	// Phase 2: Generate documents
	linksDir := filepath.Join(root, ".restruct", "links")
	var docs []*Document
	for _, file := range result.Files {
		doc, err := GenerateDocument(file)
		if err != nil {
			t.Fatalf("generate %s: %v", file.RelPath, err)
		}
		if err := WriteDocument(doc, linksDir); err != nil {
			t.Fatalf("write %s: %v", file.RelPath, err)
		}
		docs = append(docs, doc)
	}

	// Phase 3: Build map
	pm := BuildMap(docs)
	if err := WriteMap(pm, linksDir); err != nil {
		t.Fatal(err)
	}

	if pm.TotalRules == 0 {
		t.Error("expected non-zero total rules")
	}
	if len(pm.Files) != 3 {
		t.Fatalf("expected 3 files in map, got %d", len(pm.Files))
	}

	// Phase 4: Load map via MapLoader
	ml := NewMapLoader(linksDir)
	if ml == nil {
		t.Fatal("expected non-nil MapLoader")
	}

	// Verify FormatForLLM includes all 3 documents
	llmMap := ml.FormatMapForLLM()
	if !strings.Contains(llmMap, "CLAUDE.md") {
		t.Error("LLM map missing root doc")
	}
	if !strings.Contains(llmMap, "web/CLAUDE.md") {
		t.Error("LLM map missing web doc")
	}
	if !strings.Contains(llmMap, "cli/CLAUDE.md") {
		t.Error("LLM map missing cli doc")
	}

	// Phase 5: Find web doc index and simulate LLM selecting it
	webIdx := -1
	for i, f := range pm.Files {
		if f.Source == "web/CLAUDE.md" {
			webIdx = i
			break
		}
	}
	if webIdx < 0 {
		t.Fatal("web/CLAUDE.md not found in project map")
	}

	content, scopedRules, err := ml.LoadSelected([]int{webIdx})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "web/CLAUDE.md") {
		t.Errorf("expected web rules, got: %s", content[:min(len(content), 100)])
	}

	webRules, ok := scopedRules["web/CLAUDE.md"]
	if !ok {
		t.Fatal("missing web/CLAUDE.md in scoped rules")
	}
	if len(webRules.AntiPatterns) == 0 {
		t.Error("expected web anti-patterns (Do not use React.FC)")
	}

	// Root rules should NOT be in scoped rules
	if _, ok := scopedRules["CLAUDE.md"]; ok {
		t.Error("root CLAUDE.md should not be in scoped rules when only web selected")
	}

	// Phase 6: Verify staleness detection
	if ml.IsStale() {
		t.Error("map should not be stale immediately after creation")
	}

	// Touch a source file to make it stale
	time.Sleep(10 * time.Millisecond) // ensure mtime differs
	writeFile(t, filepath.Join(root, "web", "CLAUDE.md"), `# Web Dashboard (updated)
## Stack
- React 19 + TypeScript + Vite
`)

	if !ml.IsStale() {
		t.Error("map should be stale after source file modification")
	}
	stale := ml.StaleFiles()
	if len(stale) != 1 || stale[0] != "web/CLAUDE.md" {
		t.Errorf("expected web/CLAUDE.md stale, got %v", stale)
	}
}

// TestIntegration_ClassifyMock tests the classify → enrich → update flow with a mock LLM.
func TestIntegration_ClassifyMock(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "CLAUDE.md"), "# Root\n## Constraints\n- rule1\n")

	linksDir := filepath.Join(root, ".restruct", "links")

	file := DiscoveredFile{
		AbsPath: filepath.Join(root, "CLAUDE.md"),
		RelPath: "CLAUDE.md",
		ModTime: time.Now(),
	}
	doc, _ := GenerateDocument(file)
	WriteDocument(doc, linksDir)
	docs := []*Document{doc}

	// Before classification
	origSummary := doc.Summary

	// Classify with mock LLM
	chat := func(ctx context.Context, system, user string, temp float32, max int) (string, error) {
		return `{"summary": "Root project configuration and build rules", "keywords": ["go", "sqlite", "ollama"], "scope": "global"}`, nil
	}
	classifier := NewClassifier(chat, linksDir, 0.3, 512)
	<-classifier.ClassifyAsync(context.Background(), docs)

	// After classification
	if doc.Summary == origSummary {
		t.Error("summary should have been enriched by classification")
	}
	if doc.Summary != "Root project configuration and build rules" {
		t.Errorf("unexpected summary: %q", doc.Summary)
	}

	// Verify sentinel
	if !IsClassified(linksDir) {
		t.Error("sentinel should exist after successful classification")
	}

	// Verify index.json was updated with enriched data
	pm, err := LoadMap(linksDir)
	if err != nil {
		t.Fatal(err)
	}
	if pm.Files[0].Summary != "Root project configuration and build rules" {
		t.Errorf("index.json not updated: %q", pm.Files[0].Summary)
	}
}

// TestIntegration_FallbackToFlatLoading tests that when no index.json exists,
// the system gracefully falls back (MapLoader returns nil).
func TestIntegration_FallbackToFlatLoading(t *testing.T) {
	ml := NewMapLoader(filepath.Join(t.TempDir(), "nonexistent"))
	if ml != nil {
		t.Error("expected nil MapLoader for nonexistent links dir")
	}
	// The pipeline would fall back to flat rules loading when ml is nil
}
