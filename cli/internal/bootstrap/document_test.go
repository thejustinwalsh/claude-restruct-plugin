package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateDocument_AllCategories(t *testing.T) {
	root := t.TempDir()
	content := `# Restruct

## Build & Test
- pnpm dev — starts xmake watch
- pnpm build — release build

## Code Style
- TypeScript: no as type assertions
- Go: pure Go only, no CGO

## Constraints
- CLI writes to SQLite
- All inference is local via Ollama

## Workflow
- Conventional commits
- If you added new behavior, add tests

## Do NOT
- Do not use as in TypeScript
- Do not add CGO dependencies
`
	path := filepath.Join(root, "CLAUDE.md")
	writeFile(t, path, content)

	file := DiscoveredFile{
		AbsPath: path,
		RelPath: "CLAUDE.md",
		Size:    int64(len(content)),
		ModTime: time.Now(),
	}

	doc, err := GenerateDocument(file)
	if err != nil {
		t.Fatal(err)
	}

	// Check all categories present
	catSet := make(map[string]bool)
	for _, c := range doc.Categories {
		catSet[c] = true
	}
	for _, expected := range []string{"context", "constraints", "workflow", "anti-patterns"} {
		if !catSet[expected] {
			t.Errorf("missing category %q, got %v", expected, doc.Categories)
		}
	}

	// Check rule count
	if doc.RuleCount < 8 {
		t.Errorf("expected at least 8 rules, got %d", doc.RuleCount)
	}

	// Check hash is 8 chars
	if len(doc.Hash) != 8 {
		t.Errorf("expected 8-char hash, got %q", doc.Hash)
	}

	// Check keywords extracted
	if len(doc.Keywords) == 0 {
		t.Error("expected keywords to be extracted")
	}

	// Check summary
	if doc.Summary == "" {
		t.Error("expected non-empty summary")
	}
}

func TestGenerateDocument_Keywords(t *testing.T) {
	root := t.TempDir()
	content := `# Web Dashboard

## Stack
- React 19 + TypeScript + Vite
- Tailwind CSS for styling
- Zustand for state management

## Do NOT
- Do not use React.FC
`
	path := filepath.Join(root, "CLAUDE.md")
	writeFile(t, path, content)

	file := DiscoveredFile{
		AbsPath: path,
		RelPath: "web/CLAUDE.md",
		Size:    int64(len(content)),
		ModTime: time.Now(),
	}

	doc, err := GenerateDocument(file)
	if err != nil {
		t.Fatal(err)
	}

	// Should contain some tech keywords
	kwStr := strings.Join(doc.Keywords, " ")
	if !strings.Contains(kwStr, "react") && !strings.Contains(kwStr, "typescript") {
		t.Errorf("expected React/TypeScript in keywords, got %v", doc.Keywords)
	}
}

func TestWriteAndReadDocument(t *testing.T) {
	root := t.TempDir()
	linksDir := filepath.Join(root, "links")

	content := "# Test\n## Constraints\n- rule1\n- rule2\n"
	srcPath := filepath.Join(root, "CLAUDE.md")
	writeFile(t, srcPath, content)

	file := DiscoveredFile{
		AbsPath: srcPath,
		RelPath: "CLAUDE.md",
		Size:    int64(len(content)),
		ModTime: time.Now(),
	}

	doc, err := GenerateDocument(file)
	if err != nil {
		t.Fatal(err)
	}

	if err := WriteDocument(doc, linksDir); err != nil {
		t.Fatal(err)
	}

	// Verify file exists
	expectedPath := filepath.Join(linksDir, doc.Hash+".md")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("document file not created: %v", err)
	}

	// Read and verify content
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatal(err)
	}
	written := string(data)

	if !strings.Contains(written, "---\n") {
		t.Error("missing YAML frontmatter delimiter")
	}
	if !strings.Contains(written, "source: CLAUDE.md") {
		t.Error("missing source in frontmatter")
	}
	if !strings.Contains(written, "## Constraints") {
		t.Error("missing constraints section")
	}
}

func TestExtractSummary(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			"h1 header",
			"# Web Dashboard\n## Stack\n- React\n",
			"Web Dashboard",
		},
		{
			"section headers only",
			"## Stack\n- React\n## Constraints\n- rule\n",
			"Stack, Constraints",
		},
		{
			"fallback to relPath",
			"- just a rule\n",
			"test.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSummary(tt.content, "test.md")
			if got != tt.want {
				t.Errorf("extractSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsStopword(t *testing.T) {
	if !isStopword("the") {
		t.Error("'the' should be a stopword")
	}
	if !isStopword("ab") {
		t.Error("2-char word should be filtered")
	}
	if isStopword("typescript") {
		t.Error("'typescript' should not be a stopword")
	}
}
