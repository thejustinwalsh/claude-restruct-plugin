package db

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testDB creates an in-memory SQLite database for testing.
func testDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	d, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestOpen_RunsMigrations(t *testing.T) {
	d := testDB(t)

	// Verify migrations table exists and has entries
	var count int
	err := d.pool.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("expected migrations to be applied")
	}
}

func TestOpen_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "test.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d.Close()

	if _, err := os.Stat(filepath.Dir(path)); os.IsNotExist(err) {
		t.Fatal("expected nested directory to be created")
	}
}

func TestOpen_IdempotentMigrations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	// Open twice — migrations should not fail on second open
	d1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d1.Close()

	d2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open should succeed: %v", err)
	}
	d2.Close()
}

// --- Session operations ---

func TestUpsertSession_Insert(t *testing.T) {
	d := testDB(t)

	s := &Session{
		ID:          "sess-1",
		ProjectPath: "/tmp/project",
		StartedAt:   time.Now().UTC().Truncate(time.Second),
		Status:      "active",
	}
	if err := d.UpsertSession(s); err != nil {
		t.Fatal(err)
	}

	got, err := d.GetSession("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected session, got nil")
	}
	if got.ProjectPath != "/tmp/project" {
		t.Errorf("project_path = %q, want /tmp/project", got.ProjectPath)
	}
	if got.Status != "active" {
		t.Errorf("status = %q, want active", got.Status)
	}
}

func TestUpsertSession_Upsert(t *testing.T) {
	d := testDB(t)

	s := &Session{
		ID:          "sess-1",
		ProjectPath: "/tmp/project",
		StartedAt:   time.Now().UTC(),
		Status:      "active",
	}
	d.UpsertSession(s)

	// Upsert again — should not error
	s2 := &Session{
		ID:          "sess-1",
		ProjectPath: "/tmp/project",
		StartedAt:   time.Now().UTC(),
		Status:      "active",
	}
	if err := d.UpsertSession(s2); err != nil {
		t.Fatalf("upsert should not error: %v", err)
	}
}

func TestEndSession(t *testing.T) {
	d := testDB(t)

	d.UpsertSession(&Session{
		ID:          "sess-end",
		ProjectPath: "/tmp",
		StartedAt:   time.Now().UTC(),
		Status:      "active",
	})

	if err := d.EndSession("sess-end"); err != nil {
		t.Fatal(err)
	}

	got, _ := d.GetSession("sess-end")
	if got.Status != "ended" {
		t.Errorf("status = %q, want ended", got.Status)
	}
	if got.EndedAt == nil {
		t.Error("expected ended_at to be set")
	}
}

func TestGetSession_NotFound(t *testing.T) {
	d := testDB(t)
	got, err := d.GetSession("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil for nonexistent session, got %+v", got)
	}
}

func TestListSessions(t *testing.T) {
	d := testDB(t)

	for i := 0; i < 5; i++ {
		d.UpsertSession(&Session{
			ID:          fmt.Sprintf("sess-%d", i),
			ProjectPath: "/tmp",
			StartedAt:   time.Now().UTC().Add(time.Duration(i) * time.Minute),
			Status:      "active",
		})
	}

	sessions, err := d.ListSessions(3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 3 {
		t.Errorf("got %d sessions, want 3", len(sessions))
	}

	// Offset
	sessions2, _ := d.ListSessions(10, 3)
	if len(sessions2) != 2 {
		t.Errorf("got %d sessions with offset 3, want 2", len(sessions2))
	}
}

// --- Refinement operations ---

func TestInsertRefinement(t *testing.T) {
	d := testDB(t)

	d.UpsertSession(&Session{
		ID: "sess-r", ProjectPath: "/tmp", StartedAt: time.Now().UTC(), Status: "active",
	})

	refined := "structured output"
	id, err := d.InsertRefinement(&Refinement{
		SessionID:     "sess-r",
		ProjectPath:   "/tmp",
		RawPrompt:     "fix the bug",
		RefinedPrompt: &refined,
		Model:         "qwen2.5-coder:14b",
		LatencyMs:     1500,
		Status:        "complete",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Errorf("expected positive id, got %d", id)
	}

	got, err := d.GetRefinement(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.RawPrompt != "fix the bug" {
		t.Errorf("raw_prompt = %q", got.RawPrompt)
	}
	if got.RefinedPrompt == nil || *got.RefinedPrompt != "structured output" {
		t.Errorf("refined_prompt = %v", got.RefinedPrompt)
	}
}

func TestInsertRefinement_DefaultStatus(t *testing.T) {
	d := testDB(t)

	d.UpsertSession(&Session{
		ID: "sess-ds", ProjectPath: "/tmp", StartedAt: time.Now().UTC(), Status: "active",
	})

	// InsertRefinement defaults empty status to "complete" in Go code
	id, _ := d.InsertRefinement(&Refinement{
		SessionID:   "sess-ds",
		ProjectPath: "/tmp",
		RawPrompt:   "test",
		Status:      "", // Go code defaults this to "complete"
	})

	// The SQL column default is also "complete", so either way it should be set
	var status string
	d.pool.QueryRow("SELECT status FROM refinements WHERE id = ?", id).Scan(&status)
	if status != "complete" {
		t.Errorf("default status = %q, want complete", status)
	}
}

func TestGetRefinement_NotFound(t *testing.T) {
	d := testDB(t)
	got, err := d.GetRefinement(999)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent refinement")
	}
}

func TestListRefinements(t *testing.T) {
	d := testDB(t)

	d.UpsertSession(&Session{
		ID: "sess-lr", ProjectPath: "/tmp", StartedAt: time.Now().UTC(), Status: "active",
	})

	for i := 0; i < 5; i++ {
		d.InsertRefinement(&Refinement{
			SessionID:   "sess-lr",
			ProjectPath: "/tmp",
			RawPrompt:   fmt.Sprintf("prompt %d", i),
			Status:      "complete",
		})
	}

	refs, err := d.ListRefinements(3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 3 {
		t.Errorf("got %d, want 3", len(refs))
	}
}

func TestGetRefinementsSince(t *testing.T) {
	d := testDB(t)

	d.UpsertSession(&Session{
		ID: "sess-since", ProjectPath: "/tmp", StartedAt: time.Now().UTC(), Status: "active",
	})

	var ids []int64
	for i := 0; i < 5; i++ {
		id, _ := d.InsertRefinement(&Refinement{
			SessionID:   "sess-since",
			ProjectPath: "/tmp",
			RawPrompt:   fmt.Sprintf("prompt %d", i),
			Status:      "complete",
		})
		ids = append(ids, id)
	}

	// Get refinements since the 3rd one
	refs, err := d.GetRefinementsSince(ids[2], 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Errorf("got %d refinements since id %d, want 2", len(refs), ids[2])
	}
}

func TestGetRefinementsForSession(t *testing.T) {
	d := testDB(t)

	d.UpsertSession(&Session{ID: "sess-a", ProjectPath: "/tmp", StartedAt: time.Now().UTC(), Status: "active"})
	d.UpsertSession(&Session{ID: "sess-b", ProjectPath: "/tmp", StartedAt: time.Now().UTC(), Status: "active"})

	d.InsertRefinement(&Refinement{SessionID: "sess-a", ProjectPath: "/tmp", RawPrompt: "a1", Status: "complete"})
	d.InsertRefinement(&Refinement{SessionID: "sess-a", ProjectPath: "/tmp", RawPrompt: "a2", Status: "complete"})
	d.InsertRefinement(&Refinement{SessionID: "sess-b", ProjectPath: "/tmp", RawPrompt: "b1", Status: "complete"})

	refs, err := d.GetRefinementsForSession("sess-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Errorf("got %d, want 2", len(refs))
	}
}

func TestUpdateRefinement(t *testing.T) {
	d := testDB(t)

	d.UpsertSession(&Session{ID: "sess-up", ProjectPath: "/tmp", StartedAt: time.Now().UTC(), Status: "active"})

	id, _ := d.InsertRefinement(&Refinement{
		SessionID:   "sess-up",
		ProjectPath: "/tmp",
		RawPrompt:   "test",
		Status:      "streaming",
	})

	refined := "final output"
	valid := true
	err := d.UpdateRefinement(id, &Refinement{
		RefinedPrompt: &refined,
		Model:         "qwen2.5-coder:14b",
		LatencyMs:     5000,
		OutputValid:   &valid,
		Status:        "complete",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, _ := d.GetRefinement(id)
	if got.RefinedPrompt == nil || *got.RefinedPrompt != "final output" {
		t.Errorf("refined_prompt after update = %v", got.RefinedPrompt)
	}
	if got.LatencyMs != 5000 {
		t.Errorf("latency_ms = %d, want 5000", got.LatencyMs)
	}
}

// --- Pipeline event operations ---

func TestInsertAndGetPipelineEvents(t *testing.T) {
	d := testDB(t)

	d.UpsertSession(&Session{ID: "sess-pe", ProjectPath: "/tmp", StartedAt: time.Now().UTC(), Status: "active"})
	refID, _ := d.InsertRefinement(&Refinement{
		SessionID: "sess-pe", ProjectPath: "/tmp", RawPrompt: "test", Status: "complete",
	})

	stages := []string{"rules_load", "cache_check", "ollama_inference", "validation"}
	for i, stage := range stages {
		d.InsertPipelineEvent(&PipelineEvent{
			RefinementID: refID,
			Stage:        stage,
			DurationUs:   int64((i + 1) * 100000),
			Success:      true,
		})
	}

	events, err := d.GetPipelineEvents(refID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Errorf("got %d events, want 4", len(events))
	}
	if events[0].Stage != "rules_load" {
		t.Errorf("first stage = %q, want rules_load", events[0].Stage)
	}
}

// --- Metrics ---

func TestGetMetrics(t *testing.T) {
	d := testDB(t)

	d.UpsertSession(&Session{ID: "s1", ProjectPath: "/tmp", StartedAt: time.Now().UTC(), Status: "active"})
	d.UpsertSession(&Session{ID: "s2", ProjectPath: "/tmp", StartedAt: time.Now().UTC(), Status: "ended"})

	d.InsertRefinement(&Refinement{SessionID: "s1", ProjectPath: "/tmp", RawPrompt: "p1", LatencyMs: 1000, Status: "complete"})
	d.InsertRefinement(&Refinement{SessionID: "s1", ProjectPath: "/tmp", RawPrompt: "p2", LatencyMs: 2000, CacheHit: true, Status: "complete"})
	d.InsertRefinement(&Refinement{SessionID: "s1", ProjectPath: "/tmp", RawPrompt: "p3", Passthrough: true, Status: "complete"})

	m, err := d.GetMetrics()
	if err != nil {
		t.Fatal(err)
	}

	if m.TotalSessions != 2 {
		t.Errorf("total_sessions = %d, want 2", m.TotalSessions)
	}
	if m.ActiveSessions != 1 {
		t.Errorf("active_sessions = %d, want 1", m.ActiveSessions)
	}
	if m.TotalRefinements != 3 {
		t.Errorf("total_refinements = %d, want 3", m.TotalRefinements)
	}
	if m.CacheHits != 1 {
		t.Errorf("cache_hits = %d, want 1", m.CacheHits)
	}
	if m.Passthroughs != 1 {
		t.Errorf("passthroughs = %d, want 1", m.Passthroughs)
	}
	// Average of (1000, 2000) = 1500 (0 latency excluded by WHERE latency_ms > 0)
	if m.AvgLatencyMs < 1400 || m.AvgLatencyMs > 1600 {
		t.Errorf("avg_latency_ms = %f, want ~1500", m.AvgLatencyMs)
	}
}

func TestGetMetrics_Empty(t *testing.T) {
	d := testDB(t)

	m, err := d.GetMetrics()
	if err != nil {
		t.Fatal(err)
	}
	if m.TotalRefinements != 0 {
		t.Errorf("expected 0 refinements, got %d", m.TotalRefinements)
	}
	if m.CacheHitRate != 0 {
		t.Errorf("expected 0 cache hit rate, got %f", m.CacheHitRate)
	}
}

// --- Stats ---

func TestGetRefinementStats(t *testing.T) {
	d := testDB(t)

	d.UpsertSession(&Session{ID: "s-stats", ProjectPath: "/tmp", StartedAt: time.Now().UTC(), Status: "active"})
	d.InsertRefinement(&Refinement{SessionID: "s-stats", ProjectPath: "/tmp", RawPrompt: "p", LatencyMs: 500, Model: "test", Status: "complete"})

	stats, err := d.GetRefinementStats(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Errorf("got %d stats, want 1", len(stats))
	}
}

func TestGetDailyCounts(t *testing.T) {
	d := testDB(t)

	d.UpsertSession(&Session{ID: "s-daily", ProjectPath: "/tmp", StartedAt: time.Now().UTC(), Status: "active"})
	d.InsertRefinement(&Refinement{SessionID: "s-daily", ProjectPath: "/tmp", RawPrompt: "p1", Status: "complete"})
	d.InsertRefinement(&Refinement{SessionID: "s-daily", ProjectPath: "/tmp", RawPrompt: "p2", Status: "complete"})

	counts, err := d.GetDailyCounts(30)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) == 0 {
		t.Fatal("expected at least one daily count")
	}
	if counts[0].Count != 2 {
		t.Errorf("today's count = %d, want 2", counts[0].Count)
	}
}

// --- FailStalePending ---

func TestFailStalePending(t *testing.T) {
	d := testDB(t)

	// Create a session for FK constraint
	d.UpsertSession(&Session{ID: "s1", ProjectPath: "/p", Status: "active"})

	// Insert a refinement that will look "old" (pending, created 10 min ago)
	_, err := d.Pool().Exec(
		`INSERT INTO refinements (session_id, project_path, raw_prompt, model, status, created_at)
		 VALUES ('s1', '/p', 'old pending', 'model', 'pending', datetime('now', '-10 minutes'))`)
	if err != nil {
		t.Fatal(err)
	}

	// Insert a recent pending refinement (should NOT be pruned)
	_, err = d.Pool().Exec(
		`INSERT INTO refinements (session_id, project_path, raw_prompt, model, status, created_at)
		 VALUES ('s1', '/p', 'fresh pending', 'model', 'pending', datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}

	// Insert a completed refinement (should NOT be touched)
	_, err = d.Pool().Exec(
		`INSERT INTO refinements (session_id, project_path, raw_prompt, model, status, created_at)
		 VALUES ('s1', '/p', 'done', 'model', 'complete', datetime('now', '-10 minutes'))`)
	if err != nil {
		t.Fatal(err)
	}

	n, err := d.FailStalePending(5 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expected 1 pruned, got %d", n)
	}

	// Verify the old one is now failed
	var status string
	d.Pool().QueryRow("SELECT status FROM refinements WHERE raw_prompt = 'old pending'").Scan(&status)
	if status != "failed" {
		t.Errorf("old pending should be 'failed', got %q", status)
	}

	// Verify the fresh one is still pending
	d.Pool().QueryRow("SELECT status FROM refinements WHERE raw_prompt = 'fresh pending'").Scan(&status)
	if status != "pending" {
		t.Errorf("fresh pending should still be 'pending', got %q", status)
	}

	// Verify the complete one is unchanged
	d.Pool().QueryRow("SELECT status FROM refinements WHERE raw_prompt = 'done'").Scan(&status)
	if status != "complete" {
		t.Errorf("complete should still be 'complete', got %q", status)
	}
}

// --- DefaultPath ---

func TestDefaultPath_UsesPluginData(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", "/custom/data")
	got := DefaultPath()
	want := "/custom/data/restruct.db"
	if got != want {
		t.Errorf("DefaultPath = %q, want %q", got, want)
	}
}

func TestDefaultPath_Fallback(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", "")
	got := DefaultPath()
	if !filepath.IsAbs(got) {
		t.Errorf("DefaultPath should be absolute, got %q", got)
	}
	if filepath.Base(got) != "restruct.db" {
		t.Errorf("DefaultPath should end with restruct.db, got %q", got)
	}
}
