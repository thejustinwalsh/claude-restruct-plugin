package bootstrap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tjw/restruct/internal/prompt"
)

func TestBuildMap(t *testing.T) {
	docs := []*Document{
		{
			Source:     "CLAUDE.md",
			AbsPath:   "/project/CLAUDE.md",
			Hash:      "a1b2c3d4",
			Keywords:  []string{"go", "typescript", "pnpm"},
			Categories: []string{"context", "constraints", "workflow"},
			RuleCount: 14,
			Summary:   "Root project rules",
			Rules:     &prompt.ParsedRules{},
		},
		{
			Source:     "web/CLAUDE.md",
			AbsPath:   "/project/web/CLAUDE.md",
			Hash:      "e5f6g7h8",
			Keywords:  []string{"react", "tailwind", "vite"},
			Categories: []string{"context", "constraints"},
			RuleCount: 8,
			Summary:   "Web frontend rules",
			Rules:     &prompt.ParsedRules{},
		},
	}

	pm := BuildMap(docs)

	if pm.Version != 1 {
		t.Errorf("version = %d, want 1", pm.Version)
	}
	if len(pm.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(pm.Files))
	}
	if pm.TotalRules != 22 {
		t.Errorf("total rules = %d, want 22", pm.TotalRules)
	}
	if pm.Files[0].Source != "CLAUDE.md" {
		t.Errorf("first file source = %q, want CLAUDE.md", pm.Files[0].Source)
	}
}

func TestWriteAndLoadMap(t *testing.T) {
	linksDir := filepath.Join(t.TempDir(), "links")

	pm := &ProjectMap{
		Version:    1,
		Generated:  time.Now().UTC(),
		TotalRules: 5,
		Files: []MapEntry{
			{
				Source:     "CLAUDE.md",
				AbsPath:   "/test/CLAUDE.md",
				Hash:      "abc12345",
				Keywords:  []string{"go"},
				Categories: []string{"context"},
				Summary:   "Test rules",
				RuleCount: 5,
			},
		},
	}

	if err := WriteMap(pm, linksDir); err != nil {
		t.Fatal(err)
	}

	// Verify JSON file exists
	path := filepath.Join(linksDir, "index.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("index.json not created: %v", err)
	}

	// Load it back
	loaded, err := LoadMap(linksDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("loaded map is nil")
	}
	if loaded.Version != 1 {
		t.Errorf("loaded version = %d, want 1", loaded.Version)
	}
	if len(loaded.Files) != 1 {
		t.Fatalf("loaded files = %d, want 1", len(loaded.Files))
	}
	if loaded.Files[0].Source != "CLAUDE.md" {
		t.Errorf("loaded source = %q, want CLAUDE.md", loaded.Files[0].Source)
	}
}

func TestLoadMap_NotExist(t *testing.T) {
	pm, err := LoadMap(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Fatal(err)
	}
	if pm != nil {
		t.Error("expected nil for non-existent dir")
	}
}

func TestFormatForClaude(t *testing.T) {
	pm := &ProjectMap{
		Version:    1,
		TotalRules: 22,
		Files: []MapEntry{
			{Source: "CLAUDE.md", Summary: "Root rules", RuleCount: 14, Categories: []string{"context", "constraints"}, Keywords: []string{"go", "pnpm"}},
			{Source: "web/CLAUDE.md", Summary: "Web rules", RuleCount: 8, Categories: []string{"context"}, Keywords: []string{"react"}},
		},
	}

	out := pm.FormatForClaude()
	if !strings.Contains(out, "2 rule documents") {
		t.Errorf("missing document count in output: %s", out)
	}
	if !strings.Contains(out, "22 rules total") {
		t.Errorf("missing total rules in output: %s", out)
	}
	if !strings.Contains(out, "0. CLAUDE.md") {
		t.Errorf("missing first document in output: %s", out)
	}
	if !strings.Contains(out, "1. web/CLAUDE.md") {
		t.Errorf("missing second document in output: %s", out)
	}
}

func TestFormatForLLM(t *testing.T) {
	pm := &ProjectMap{
		Files: []MapEntry{
			{Source: "CLAUDE.md", Summary: "Root rules", RuleCount: 14},
			{Source: "web/CLAUDE.md", Summary: "Web rules", RuleCount: 8},
		},
	}

	out := pm.FormatForLLM()
	if !strings.Contains(out, "## Project Document Map") {
		t.Errorf("missing header in output: %s", out)
	}
	if !strings.Contains(out, "relevant_docs") {
		t.Errorf("missing relevant_docs instruction: %s", out)
	}
}

func TestProjectMapJSON_RoundTrip(t *testing.T) {
	pm := &ProjectMap{
		Version:    1,
		Generated:  time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		TotalRules: 10,
		Files: []MapEntry{
			{
				Source:     "CLAUDE.md",
				AbsPath:   "/test/CLAUDE.md",
				Hash:      "abcd1234",
				Keywords:  []string{"go", "test"},
				Categories: []string{"context"},
				Summary:   "Test",
				RuleCount: 10,
			},
		},
	}

	data, err := json.Marshal(pm)
	if err != nil {
		t.Fatal(err)
	}

	var loaded ProjectMap
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}

	if loaded.Version != pm.Version {
		t.Errorf("version mismatch: %d vs %d", loaded.Version, pm.Version)
	}
	if loaded.TotalRules != pm.TotalRules {
		t.Errorf("total rules mismatch: %d vs %d", loaded.TotalRules, pm.TotalRules)
	}
	if len(loaded.Files) != 1 {
		t.Fatalf("files count mismatch: %d vs 1", len(loaded.Files))
	}
}
