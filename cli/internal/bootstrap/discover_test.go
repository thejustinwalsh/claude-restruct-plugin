package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscover_MultiDepth(t *testing.T) {
	// Create a temp project with rule files at different depths
	root := t.TempDir()
	initGitRepo(t, root)

	// Root level
	writeFile(t, filepath.Join(root, "CLAUDE.md"), "# Root\n## Constraints\n- rule1\n")
	// Depth 1
	os.MkdirAll(filepath.Join(root, "web"), 0755)
	writeFile(t, filepath.Join(root, "web", "CLAUDE.md"), "# Web\n## Constraints\n- rule2\n")
	// Depth 2
	os.MkdirAll(filepath.Join(root, "cli", "internal"), 0755)
	writeFile(t, filepath.Join(root, "cli", "internal", "CLAUDE.md"), "# Deep\n- rule3\n")

	result, err := Discover(root, []string{"CLAUDE.md"}, 50)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(result.Files))
	}

	// Verify sorted by depth ascending
	if result.Files[0].Depth != 0 {
		t.Errorf("first file should be depth 0, got %d", result.Files[0].Depth)
	}
	if result.Files[1].Depth != 1 {
		t.Errorf("second file should be depth 1, got %d", result.Files[1].Depth)
	}
	if result.Files[2].Depth != 2 {
		t.Errorf("third file should be depth 2, got %d", result.Files[2].Depth)
	}
}

func TestDiscover_CapAtMax(t *testing.T) {
	root := t.TempDir()

	// Create 5 files but cap at 3
	for _, dir := range []string{"a", "b", "c", "d", "e"} {
		os.MkdirAll(filepath.Join(root, dir), 0755)
		writeFile(t, filepath.Join(root, dir, "CLAUDE.md"), "# "+dir+"\n- rule\n")
	}

	result, err := Discover(root, []string{"CLAUDE.md"}, 3)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Files) != 3 {
		t.Fatalf("expected 3 files (capped), got %d", len(result.Files))
	}
}

func TestDiscover_EmptyProject(t *testing.T) {
	root := t.TempDir()

	result, err := Discover(root, []string{"CLAUDE.md"}, 50)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(result.Files))
	}
}

func TestDiscover_MultipleFileNames(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "CLAUDE.md"), "# Rules\n- r1\n")
	writeFile(t, filepath.Join(root, "agents.md"), "# Agents\n- r2\n")

	result, err := Discover(root, []string{"CLAUDE.md", "agents.md"}, 50)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(result.Files))
	}
}

func TestPathDepth(t *testing.T) {
	tests := []struct {
		path  string
		depth int
	}{
		{"CLAUDE.md", 0},
		{"web/CLAUDE.md", 1},
		{"cli/internal/CLAUDE.md", 2},
		{".", 0},
		{"", 0},
	}
	for _, tt := range tests {
		got := pathDepth(tt.path)
		if got != tt.depth {
			t.Errorf("pathDepth(%q) = %d, want %d", tt.path, got, tt.depth)
		}
	}
}

// --- helpers ---

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
}
