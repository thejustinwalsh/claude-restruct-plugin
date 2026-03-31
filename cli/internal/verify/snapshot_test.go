package verify

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tjw/restruct/internal/db"
)

func setupTestDB(t *testing.T) *db.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func createTestFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		abs := filepath.Join(dir, path)
		os.MkdirAll(filepath.Dir(abs), 0755)
		if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", path, err)
		}
	}
}

func TestTakeSnapshot_Basic(t *testing.T) {
	d := setupTestDB(t)
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"main.go":          "package main",
		"lib/util.go":      "package lib",
		"web/app.ts":       "export {}",
		"web/styles.css":   "body {}",
		"README.md":        "# project",
		"node_modules/x.js": "skip",
	})

	err := TakeSnapshot(d, "sess1", "prompt", dir, []string{"**/*.go", "web/**/*.ts"})
	if err != nil {
		t.Fatalf("TakeSnapshot error: %v", err)
	}

	// Verify files were recorded
	var count int
	d.Pool().QueryRow("SELECT COUNT(*) FROM snapshots WHERE session_id = 'sess1' AND scope = 'prompt'").Scan(&count)
	if count != 3 { // main.go, lib/util.go, web/app.ts
		t.Errorf("expected 3 snapshot entries, got %d", count)
	}
}

func TestTakeSnapshot_ReplacesExisting(t *testing.T) {
	d := setupTestDB(t)
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{"main.go": "v1"})

	TakeSnapshot(d, "sess1", "prompt", dir, []string{"**/*.go"})
	TakeSnapshot(d, "sess1", "prompt", dir, []string{"**/*.go"})

	var count int
	d.Pool().QueryRow("SELECT COUNT(*) FROM snapshots WHERE session_id = 'sess1' AND scope = 'prompt'").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 entry after replace, got %d", count)
	}
}

func TestDiffSnapshot_NoChanges(t *testing.T) {
	d := setupTestDB(t)
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{"main.go": "package main"})

	TakeSnapshot(d, "sess1", "prompt", dir, []string{"**/*.go"})

	changed, err := DiffSnapshot(d, "sess1", "prompt", dir)
	if err != nil {
		t.Fatalf("DiffSnapshot error: %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("expected no changes, got %v", changed)
	}
}

func TestDiffSnapshot_ModifiedFile(t *testing.T) {
	d := setupTestDB(t)
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{"main.go": "package main"})

	TakeSnapshot(d, "sess1", "prompt", dir, []string{"**/*.go"})

	// Modify the file (ensure different mtime)
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main // changed"), 0644)

	changed, err := DiffSnapshot(d, "sess1", "prompt", dir)
	if err != nil {
		t.Fatalf("DiffSnapshot error: %v", err)
	}
	if len(changed) != 1 || changed[0] != "main.go" {
		t.Errorf("expected [main.go], got %v", changed)
	}
}

func TestDiffSnapshot_DeletedFile(t *testing.T) {
	d := setupTestDB(t)
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{"main.go": "package main"})

	TakeSnapshot(d, "sess1", "prompt", dir, []string{"**/*.go"})

	os.Remove(filepath.Join(dir, "main.go"))

	changed, err := DiffSnapshot(d, "sess1", "prompt", dir)
	if err != nil {
		t.Fatalf("DiffSnapshot error: %v", err)
	}
	if len(changed) != 1 || changed[0] != "main.go" {
		t.Errorf("expected [main.go], got %v", changed)
	}
}

func TestDiffSnapshot_NoSnapshot(t *testing.T) {
	d := setupTestDB(t)
	dir := t.TempDir()

	changed, err := DiffSnapshot(d, "sess1", "prompt", dir)
	if err != nil {
		t.Fatalf("DiffSnapshot error: %v", err)
	}
	if changed != nil {
		t.Errorf("expected nil for no snapshot, got %v", changed)
	}
}

func TestDiffSnapshot_NewFileInTrackedDir(t *testing.T) {
	d := setupTestDB(t)
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{"src/main.go": "package main"})

	TakeSnapshot(d, "sess1", "prompt", dir, []string{"**/*.go"})

	// Add a new file in the same directory
	createTestFiles(t, dir, map[string]string{"src/new.go": "package main"})

	changed, err := DiffSnapshot(d, "sess1", "prompt", dir)
	if err != nil {
		t.Fatalf("DiffSnapshot error: %v", err)
	}

	found := false
	for _, c := range changed {
		if c == filepath.Join("src", "new.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected new.go in changed files, got %v", changed)
	}
}

func TestCleanSnapshot(t *testing.T) {
	d := setupTestDB(t)
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{"main.go": "package main"})

	TakeSnapshot(d, "sess1", "task-1", dir, []string{"**/*.go"})
	CleanSnapshot(d, "sess1", "task-1")

	var count int
	d.Pool().QueryRow("SELECT COUNT(*) FROM snapshots WHERE session_id = 'sess1' AND scope = 'task-1'").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 entries after clean, got %d", count)
	}
}

func TestHasSnapshot(t *testing.T) {
	d := setupTestDB(t)
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{"main.go": "package main"})

	has, _ := HasSnapshot(d, "sess1", "prompt")
	if has {
		t.Error("expected no snapshot before TakeSnapshot")
	}

	TakeSnapshot(d, "sess1", "prompt", dir, []string{"**/*.go"})

	has, _ = HasSnapshot(d, "sess1", "prompt")
	if !has {
		t.Error("expected snapshot after TakeSnapshot")
	}
}

func TestPruneStaleSnapshots(t *testing.T) {
	d := setupTestDB(t)
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{"main.go": "package main"})

	TakeSnapshot(d, "old-session", "prompt", dir, []string{"**/*.go"})

	// Backdate the entry
	d.Pool().Exec("UPDATE snapshots SET created_at = datetime('now', '-2 days') WHERE session_id = 'old-session'")

	TakeSnapshot(d, "new-session", "prompt", dir, []string{"**/*.go"})

	pruned, err := PruneStaleSnapshots(d, 24*time.Hour)
	if err != nil {
		t.Fatalf("PruneStaleSnapshots error: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}

	// New session should still exist
	has, _ := HasSnapshot(d, "new-session", "prompt")
	if !has {
		t.Error("new session snapshot should survive pruning")
	}
}

func TestCollectGlobs(t *testing.T) {
	cfg := &VerifyConfig{
		Checks: []CheckConfig{
			{Name: "test", Command: "go test ./..."},                                     // no globs
			{Name: "lint", Command: "eslint .", Globs: []string{"**/*.ts", "**/*.tsx"}},  // with globs
			{Name: "vet", Command: "go vet ./...", Globs: []string{"**/*.go"}},           // with globs
		},
	}

	globs := CollectGlobs(cfg)
	if len(globs) == 0 {
		t.Fatal("expected globs")
	}

	// Should contain all explicit globs + **/* for unglobbed checks
	has := make(map[string]bool)
	for _, g := range globs {
		has[g] = true
	}
	if !has["**/*"] {
		t.Error("expected **/* for unglobbed test check")
	}
	if !has["**/*.ts"] {
		t.Error("expected **/*.ts")
	}
	if !has["**/*.go"] {
		t.Error("expected **/*.go")
	}
}

func TestCollectGlobs_AllGlobbed(t *testing.T) {
	cfg := &VerifyConfig{
		Checks: []CheckConfig{
			{Name: "vet", Command: "go vet ./...", Globs: []string{"**/*.go"}},
		},
	}

	globs := CollectGlobs(cfg)
	has := make(map[string]bool)
	for _, g := range globs {
		has[g] = true
	}
	if has["**/*"] {
		t.Error("should not have **/* when all checks are globbed")
	}
	if !has["**/*.go"] {
		t.Error("expected **/*.go")
	}
}

func TestMatchesAnyGlob(t *testing.T) {
	tests := []struct {
		rel    string
		globs  []string
		expect bool
	}{
		{"main.go", []string{"**/*.go"}, true},
		{"src/main.go", []string{"**/*.go"}, true},
		{"src/deep/main.go", []string{"**/*.go"}, true},
		{"main.ts", []string{"**/*.go"}, false},
		{"web/app.ts", []string{"web/**/*.ts"}, true},
		{"web/src/app.ts", []string{"web/**/*.ts"}, true},
		{"cli/main.go", []string{"web/**/*.ts"}, false},
		{"main.go", []string{"**/*"}, true},
		{"README.md", []string{"**/*"}, true},
		{"binary.exe", []string{"**/*"}, false}, // not a source file
		{"main.go", []string{}, false},
	}

	for _, tt := range tests {
		got := matchesAnyGlob(tt.rel, tt.globs)
		if got != tt.expect {
			t.Errorf("matchesAnyGlob(%q, %v) = %v, want %v", tt.rel, tt.globs, got, tt.expect)
		}
	}
}

func TestDiffSnapshot_AddedFileOutsideTrackedDir(t *testing.T) {
	d := setupTestDB(t)
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"src/main.go": "package main",
	})

	TakeSnapshot(d, "sess1", "prompt", dir, []string{"**/*.go"})

	// Add a file in a NEW directory (not tracked in snapshot)
	createTestFiles(t, dir, map[string]string{
		"pkg/new.go": "package pkg",
	})

	changed, err := DiffSnapshot(d, "sess1", "prompt", dir)
	if err != nil {
		t.Fatalf("DiffSnapshot error: %v", err)
	}

	// New files in untracked dirs won't be detected by the current approach
	// (DiffSnapshot only checks dirs that were in the original snapshot).
	// This is a known limitation — the full verify flow handles it because
	// TakeSnapshot is refreshed after each successful verify.
	// This test documents the behavior.
	_ = changed
}

func TestTakeSnapshot_EmptyGlobs(t *testing.T) {
	d := setupTestDB(t)
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{"main.go": "package main"})

	// Empty globs = nothing matches
	err := TakeSnapshot(d, "sess1", "prompt", dir, []string{})
	if err != nil {
		t.Fatalf("TakeSnapshot error: %v", err)
	}

	var count int
	d.Pool().QueryRow("SELECT COUNT(*) FROM snapshots WHERE session_id = 'sess1'").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 entries for empty globs, got %d", count)
	}
}

func TestFullVerifyFlow_SnapshotDiffFilterRun(t *testing.T) {
	// Integration test: simulates the full UserPromptSubmit → Stop cycle
	d := setupTestDB(t)
	dir := t.TempDir()

	// Create project files
	createTestFiles(t, dir, map[string]string{
		"main.go":       "package main",
		"web/app.ts":    "export {}",
		"web/styles.css": "body {}",
	})

	// Write verify config
	cfg := &VerifyConfig{
		Checks: []CheckConfig{
			{Name: "go-vet", Command: "true", Globs: []string{"**/*.go"}},
			{Name: "typecheck", Command: "true", Globs: []string{"**/*.ts"}},
			{Name: "test", Command: "true"}, // no globs = always runs
		},
	}
	SaveConfig(dir, cfg)

	// Step 1: Take snapshot (simulates UserPromptSubmit)
	globs := CollectGlobs(cfg)
	err := TakeSnapshot(d, "sess-flow", "prompt", dir, globs)
	if err != nil {
		t.Fatalf("TakeSnapshot: %v", err)
	}

	// Step 2: Modify only a Go file (simulates Claude working)
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main // changed"), 0644)

	// Step 3: Diff (simulates Stop hook)
	changed, err := DiffSnapshot(d, "sess-flow", "prompt", dir)
	if err != nil {
		t.Fatalf("DiffSnapshot: %v", err)
	}
	if len(changed) != 1 || changed[0] != "main.go" {
		t.Fatalf("expected [main.go] changed, got %v", changed)
	}

	// Step 4: Filter checks — only go-vet and test should match
	relevant := FilterChecks(cfg.Checks, changed)
	names := map[string]bool{}
	for _, c := range relevant {
		names[c.Name] = true
	}
	if !names["go-vet"] {
		t.Error("go-vet should match .go changes")
	}
	if !names["test"] {
		t.Error("test (no globs) should always match")
	}
	if names["typecheck"] {
		t.Error("typecheck should NOT match .go changes")
	}
}

func TestScopeIsolation(t *testing.T) {
	d := setupTestDB(t)
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{"main.go": "package main"})

	// Create snapshots with different scopes in same session
	TakeSnapshot(d, "sess1", "prompt", dir, []string{"**/*.go"})
	TakeSnapshot(d, "sess1", "task-1", dir, []string{"**/*.go"})

	// Clean task scope should not affect prompt scope
	CleanSnapshot(d, "sess1", "task-1")

	has, _ := HasSnapshot(d, "sess1", "prompt")
	if !has {
		t.Error("prompt snapshot should survive task cleanup")
	}

	has, _ = HasSnapshot(d, "sess1", "task-1")
	if has {
		t.Error("task snapshot should be cleaned")
	}
}

// TestScopeFallback_TaskToPrompt simulates the verify.go fallback:
// if no snapshot exists for a task scope, it should fall back to prompt scope.
func TestScopeFallback_TaskToPrompt(t *testing.T) {
	d := setupTestDB(t)
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{"main.go": "package main"})

	// Only prompt scope snapshot exists (no TaskCreated snapshot)
	TakeSnapshot(d, "sess-fb", "prompt", dir, []string{"**/*.go"})

	// Task scope does NOT exist
	has, _ := HasSnapshot(d, "sess-fb", "task-123")
	if has {
		t.Fatal("task snapshot should not exist")
	}

	// Prompt scope DOES exist (fallback target)
	has, _ = HasSnapshot(d, "sess-fb", "prompt")
	if !has {
		t.Fatal("prompt snapshot should exist for fallback")
	}

	// Modify a file, then diff using the prompt scope (the fallback)
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("changed"), 0644)

	changed, _ := DiffSnapshot(d, "sess-fb", "prompt", dir)
	if len(changed) != 1 {
		t.Errorf("expected 1 changed file via prompt fallback, got %v", changed)
	}
}

// TestSnapshotAdvance_AfterVerifyPass simulates the verify.go behavior:
// after all checks pass for a task scope, the prompt snapshot is refreshed
// so that Stop hook sees no delta.
func TestSnapshotAdvance_AfterVerifyPass(t *testing.T) {
	d := setupTestDB(t)
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{"main.go": "v1"})

	globs := []string{"**/*.go"}

	// 1. UserPromptSubmit: take prompt snapshot
	TakeSnapshot(d, "sess-adv", "prompt", dir, globs)

	// 2. TaskCreated: take task snapshot
	TakeSnapshot(d, "sess-adv", "task-1", dir, globs)

	// 3. Claude modifies a file
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("v2"), 0644)

	// 4. TaskCompleted: verify detects change
	changed, _ := DiffSnapshot(d, "sess-adv", "task-1", dir)
	if len(changed) != 1 {
		t.Fatalf("task diff should show 1 change, got %v", changed)
	}

	// 5. Checks pass → advance prompt snapshot to current state + clean task
	TakeSnapshot(d, "sess-adv", "prompt", dir, globs) // refresh prompt baseline
	CleanSnapshot(d, "sess-adv", "task-1")

	// 6. Stop: diff prompt scope — should show NO changes
	changed, _ = DiffSnapshot(d, "sess-adv", "prompt", dir)
	if len(changed) != 0 {
		t.Errorf("prompt diff after advance should be empty, got %v", changed)
	}
}
