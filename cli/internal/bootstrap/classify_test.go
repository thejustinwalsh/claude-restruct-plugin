package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tjw/restruct/internal/prompt"
)

// mockChat returns a fixed JSON response for testing.
func mockChat(response string) ChatFunc {
	return func(ctx context.Context, system, user string, temperature float32, maxTokens int) (string, error) {
		return response, nil
	}
}

// mockChatError returns an error for testing fallback behavior.
func mockChatError() ChatFunc {
	return func(ctx context.Context, system, user string, temperature float32, maxTokens int) (string, error) {
		return "", fmt.Errorf("connection refused")
	}
}

func TestClassifyOne_Success(t *testing.T) {
	linksDir := filepath.Join(t.TempDir(), "links")
	os.MkdirAll(linksDir, 0755)

	chat := mockChat(`{"summary": "Web frontend: React 19 patterns and Tailwind", "keywords": ["react", "tailwind", "vite", "typescript"], "scope": "directory-specific"}`)
	classifier := NewClassifier(chat, linksDir, 0.3, 512)

	doc := &Document{
		Source:  "web/CLAUDE.md",
		AbsPath: "/project/web/CLAUDE.md",
		Content: "# Web Dashboard\n## Stack\n- React 19\n- Tailwind\n",
		Rules:   &prompt.ParsedRules{},
	}

	result, err := classifier.ClassifyOne(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}

	if result.Summary != "Web frontend: React 19 patterns and Tailwind" {
		t.Errorf("summary = %q, want specific summary", result.Summary)
	}
	if len(result.Keywords) != 4 {
		t.Errorf("keywords = %v, want 4 items", result.Keywords)
	}
	if result.Scope != "directory-specific" {
		t.Errorf("scope = %q, want directory-specific", result.Scope)
	}
}

func TestClassifyOne_LLMError(t *testing.T) {
	linksDir := filepath.Join(t.TempDir(), "links")
	classifier := NewClassifier(mockChatError(), linksDir, 0.3, 512)

	doc := &Document{
		Source:  "CLAUDE.md",
		Content: "# Rules\n- rule1\n",
		Rules:   &prompt.ParsedRules{},
	}

	_, err := classifier.ClassifyOne(context.Background(), doc)
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
}

func TestClassifyAsync_WritessentinelAndUpdatesMap(t *testing.T) {
	linksDir := filepath.Join(t.TempDir(), "links")
	os.MkdirAll(linksDir, 0755)

	chat := mockChat(`{"summary": "Root project rules", "keywords": ["go", "typescript"], "scope": "global"}`)
	classifier := NewClassifier(chat, linksDir, 0.3, 512)

	docs := []*Document{
		{
			Source:     "CLAUDE.md",
			AbsPath:   filepath.Join(t.TempDir(), "CLAUDE.md"),
			Hash:       "test1234",
			Content:    "# Root\n## Constraints\n- rule1\n",
			Keywords:   []string{"original"},
			Categories: []string{"constraints"},
			Summary:    "original summary",
			RuleCount:  1,
			Generated:  time.Now(),
			Rules:      &prompt.ParsedRules{ConstraintRules: []string{"rule1"}},
		},
	}

	// Write initial document
	writeFile(t, docs[0].AbsPath, "# Root\n## Constraints\n- rule1\n")

	ctx := context.Background()
	done := classifier.ClassifyAsync(ctx, docs)
	<-done

	// Check sentinel exists
	if !IsClassified(linksDir) {
		t.Error("sentinel file not created")
	}

	// Check document was enriched
	if docs[0].Summary != "Root project rules" {
		t.Errorf("summary not enriched: %q", docs[0].Summary)
	}

	// Check keywords were merged (LLM first, then structural)
	found := false
	for _, kw := range docs[0].Keywords {
		if kw == "go" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("LLM keywords not merged: %v", docs[0].Keywords)
	}

	// Check index.json was updated
	pm, err := LoadMap(linksDir)
	if err != nil {
		t.Fatal(err)
	}
	if pm == nil {
		t.Fatal("project map not written")
	}
	if pm.Files[0].Summary != "Root project rules" {
		t.Errorf("map not updated with enriched summary: %q", pm.Files[0].Summary)
	}
}

func TestClassifyAsync_ContextCancellation(t *testing.T) {
	linksDir := filepath.Join(t.TempDir(), "links")
	os.MkdirAll(linksDir, 0755)

	// Slow chat that respects context cancellation
	chat := func(ctx context.Context, system, user string, temp float32, max int) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Second):
			return `{"summary": "late", "keywords": [], "scope": "global"}`, nil
		}
	}
	classifier := NewClassifier(chat, linksDir, 0.3, 512)

	docs := []*Document{
		{Source: "CLAUDE.md", Content: "# R\n- r\n", Rules: &prompt.ParsedRules{}, Keywords: []string{}, Categories: []string{}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := classifier.ClassifyAsync(ctx, docs)
	<-done

	// Should NOT write sentinel on cancellation
	if IsClassified(linksDir) {
		t.Error("sentinel should not exist after cancellation")
	}
}

func TestParseClassifyResult_ValidJSON(t *testing.T) {
	result, err := parseClassifyResult(`{"summary": "test", "keywords": ["a", "b"], "scope": "global"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "test" {
		t.Errorf("summary = %q", result.Summary)
	}
	if len(result.Keywords) != 2 {
		t.Errorf("keywords = %v", result.Keywords)
	}
}

func TestParseClassifyResult_MarkdownFences(t *testing.T) {
	input := "```json\n{\"summary\": \"fenced\", \"keywords\": [], \"scope\": \"global\"}\n```"
	result, err := parseClassifyResult(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "fenced" {
		t.Errorf("summary = %q, want fenced", result.Summary)
	}
}

func TestParseClassifyResult_ExtraText(t *testing.T) {
	input := "Here is the classification:\n{\"summary\": \"with extra\", \"keywords\": [], \"scope\": \"global\"}\nDone."
	result, err := parseClassifyResult(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "with extra" {
		t.Errorf("summary = %q", result.Summary)
	}
}

func TestParseClassifyResult_Invalid(t *testing.T) {
	_, err := parseClassifyResult("not json at all")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestMergeKeywords(t *testing.T) {
	structural := []string{"go", "typescript", "pnpm"}
	llm := []string{"React", "typescript", "Vite"} // note: typescript overlaps

	merged := mergeKeywords(structural, llm)

	// 3 LLM (react, typescript, vite) + 2 structural (go, pnpm) - typescript deduped = 5
	if len(merged) != 5 {
		t.Errorf("expected 5 unique keywords, got %d: %v", len(merged), merged)
	}

	// LLM keywords should come first
	if merged[0] != "react" {
		t.Errorf("first keyword should be from LLM, got %q", merged[0])
	}

	// No duplicates
	seen := make(map[string]bool)
	for _, kw := range merged {
		if seen[kw] {
			t.Errorf("duplicate keyword: %q", kw)
		}
		seen[kw] = true
	}
}

func TestIsClassified(t *testing.T) {
	linksDir := filepath.Join(t.TempDir(), "links")
	os.MkdirAll(linksDir, 0755)

	if IsClassified(linksDir) {
		t.Error("should not be classified before sentinel")
	}

	os.WriteFile(filepath.Join(linksDir, classifySentinel), []byte("1"), 0644)

	if !IsClassified(linksDir) {
		t.Error("should be classified after sentinel")
	}

	ClearClassified(linksDir)

	if IsClassified(linksDir) {
		t.Error("should not be classified after clear")
	}
}
