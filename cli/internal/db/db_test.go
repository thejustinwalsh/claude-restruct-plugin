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

func TestResolveSessionID(t *testing.T) {
	if got := ResolveSessionID("abc-123"); got != "abc-123" {
		t.Errorf("ResolveSessionID('abc-123') = %q, want 'abc-123'", got)
	}
	if got := ResolveSessionID(""); got != PurgatorySessionID {
		t.Errorf("ResolveSessionID('') = %q, want %q", got, PurgatorySessionID)
	}
}

func TestEnsureSession_CreatesNew(t *testing.T) {
	d := testDB(t)

	sid := d.EnsureSession("new-sess", "/tmp/proj", "/tmp/transcript.jsonl")
	if sid != "new-sess" {
		t.Errorf("EnsureSession returned %q, want 'new-sess'", sid)
	}

	got, _ := d.GetSession("new-sess")
	if got == nil {
		t.Fatal("expected session to be created")
	}
	if got.Status != "active" {
		t.Errorf("status = %q, want 'active'", got.Status)
	}
}

func TestEnsureSession_RevivesEnded(t *testing.T) {
	d := testDB(t)

	// Create and end a session
	d.UpsertSession(&Session{
		ID: "ended-sess", ProjectPath: "/tmp", StartedAt: time.Now().UTC(), Status: "active",
	})
	d.EndSession("ended-sess")

	got, _ := d.GetSession("ended-sess")
	if got.Status != "ended" {
		t.Fatalf("precondition: status = %q, want 'ended'", got.Status)
	}

	// EnsureSession should revive it
	d.EnsureSession("ended-sess", "/tmp", "")

	got, _ = d.GetSession("ended-sess")
	if got.Status != "active" {
		t.Errorf("status after ensure = %q, want 'active'", got.Status)
	}
	if got.EndedAt != nil {
		t.Error("ended_at should be NULL after revive")
	}
}

func TestEnsureSession_Purgatory(t *testing.T) {
	d := testDB(t)

	sid := d.EnsureSession("", "/tmp/proj", "")
	if sid != PurgatorySessionID {
		t.Errorf("EnsureSession('') returned %q, want %q", sid, PurgatorySessionID)
	}

	got, _ := d.GetSession(PurgatorySessionID)
	if got == nil {
		t.Fatal("expected purgatory session to be created")
	}
	if got.Status != "active" {
		t.Errorf("purgatory status = %q, want 'active'", got.Status)
	}
}

func TestUpsertSession_ClearsEndedAt(t *testing.T) {
	d := testDB(t)

	// Create, end, then upsert again
	d.UpsertSession(&Session{
		ID: "reopen-sess", ProjectPath: "/tmp", StartedAt: time.Now().UTC(), Status: "active",
	})
	d.EndSession("reopen-sess")

	got, _ := d.GetSession("reopen-sess")
	if got.EndedAt == nil {
		t.Fatal("precondition: ended_at should be set")
	}

	// Upsert should clear ended_at and set active
	d.UpsertSession(&Session{
		ID: "reopen-sess", ProjectPath: "/tmp", StartedAt: time.Now().UTC(), Status: "active",
	})

	got, _ = d.GetSession("reopen-sess")
	if got.Status != "active" {
		t.Errorf("status = %q, want 'active'", got.Status)
	}
	if got.EndedAt != nil {
		t.Error("ended_at should be NULL after upsert")
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

// --- Verification events ---

func TestInsertAndGetVerificationEvents(t *testing.T) {
	d := testDB(t)

	// Create a session and refinement so we have a refinement_id to link to
	d.UpsertSession(&Session{ID: "sess-v", ProjectPath: "/project", StartedAt: time.Now().UTC(), Status: "active"})
	refID, _ := d.InsertRefinement(&Refinement{
		SessionID: "sess-v", ProjectPath: "/project", RawPrompt: "fix bug", Status: "complete",
	})

	fileCount := 42
	durationUs := int64(15000)
	changedFiles := `["main.go","lib/util.go"]`
	checksRun := `[{"name":"test","command":"go test","passed":true,"output":"ok","duration_ms":500}]`
	result := "pass"

	// Insert snapshot event linked to refinement
	err := d.InsertVerificationEvent(&VerificationEvent{
		SessionID:    "sess-v",
		RefinementID: &refID,
		Scope:        "prompt",
		HookEvent:    "UserPromptSubmit",
		EventType:    "snapshot",
		FileCount:    &fileCount,
		DurationUs:   &durationUs,
		CwdInput:     "/project/cli",
		ProjectDir:   "/project",
	})
	if err != nil {
		t.Fatalf("insert snapshot event: %v", err)
	}

	// Insert verify event linked to same refinement
	err = d.InsertVerificationEvent(&VerificationEvent{
		SessionID:    "sess-v",
		RefinementID: &refID,
		Scope:        "prompt",
		HookEvent:    "Stop",
		EventType:    "verify",
		DurationUs:   &durationUs,
		CwdInput:     "/project/cli",
		ProjectDir:   "/project",
		ChangedFiles: &changedFiles,
		ChecksRun:    &checksRun,
		Result:       &result,
	})
	if err != nil {
		t.Fatalf("insert verify event: %v", err)
	}

	// Query by refinement ID
	events, err := d.GetVerificationEventsForRefinement(refID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	if events[0].EventType != "snapshot" {
		t.Errorf("event[0].EventType = %q, want snapshot", events[0].EventType)
	}
	if events[0].RefinementID == nil || *events[0].RefinementID != refID {
		t.Errorf("event[0].RefinementID = %v, want %d", events[0].RefinementID, refID)
	}
	if events[0].FileCount == nil || *events[0].FileCount != 42 {
		t.Errorf("event[0].FileCount = %v, want 42", events[0].FileCount)
	}

	if events[1].EventType != "verify" {
		t.Errorf("event[1].EventType = %q, want verify", events[1].EventType)
	}
	if events[1].Result == nil || *events[1].Result != "pass" {
		t.Errorf("event[1].Result = %v, want pass", events[1].Result)
	}
}

func TestGetVerificationEvents_Empty(t *testing.T) {
	d := testDB(t)
	events, err := d.GetVerificationEventsForRefinement(9999)
	if err != nil {
		t.Fatal(err)
	}
	if events != nil {
		t.Errorf("expected nil for unknown refinement, got %v", events)
	}
}

func TestGetVerificationEvents_IsolatedByRefinement(t *testing.T) {
	d := testDB(t)

	d.UpsertSession(&Session{ID: "sess-iso", ProjectPath: "/p", StartedAt: time.Now().UTC(), Status: "active"})
	ref1, _ := d.InsertRefinement(&Refinement{SessionID: "sess-iso", ProjectPath: "/p", RawPrompt: "p1", Status: "complete"})
	ref2, _ := d.InsertRefinement(&Refinement{SessionID: "sess-iso", ProjectPath: "/p", RawPrompt: "p2", Status: "complete"})

	dur := int64(1000)
	// Event for refinement 1
	d.InsertVerificationEvent(&VerificationEvent{
		SessionID: "sess-iso", RefinementID: &ref1, Scope: "prompt",
		HookEvent: "Stop", EventType: "verify", DurationUs: &dur, Result: strPtr("pass"),
	})
	// Event for refinement 2
	d.InsertVerificationEvent(&VerificationEvent{
		SessionID: "sess-iso", RefinementID: &ref2, Scope: "prompt",
		HookEvent: "Stop", EventType: "verify", DurationUs: &dur, Result: strPtr("fail"),
	})

	events1, _ := d.GetVerificationEventsForRefinement(ref1)
	events2, _ := d.GetVerificationEventsForRefinement(ref2)

	if len(events1) != 1 {
		t.Fatalf("ref1: expected 1 event, got %d", len(events1))
	}
	if *events1[0].Result != "pass" {
		t.Errorf("ref1 event should be pass, got %q", *events1[0].Result)
	}

	if len(events2) != 1 {
		t.Fatalf("ref2: expected 1 event, got %d", len(events2))
	}
	if *events2[0].Result != "fail" {
		t.Errorf("ref2 event should be fail, got %q", *events2[0].Result)
	}
}

func TestLatestRefinementID(t *testing.T) {
	d := testDB(t)

	// No refinements yet
	if id := d.LatestRefinementID("sess-none"); id != 0 {
		t.Errorf("expected 0 for empty session, got %d", id)
	}

	d.UpsertSession(&Session{ID: "sess-lat", ProjectPath: "/p", StartedAt: time.Now().UTC(), Status: "active"})
	id1, _ := d.InsertRefinement(&Refinement{SessionID: "sess-lat", ProjectPath: "/p", RawPrompt: "p1", Status: "complete"})
	id2, _ := d.InsertRefinement(&Refinement{SessionID: "sess-lat", ProjectPath: "/p", RawPrompt: "p2", Status: "complete"})

	got := d.LatestRefinementID("sess-lat")
	if got != id2 {
		t.Errorf("expected %d (latest), got %d (first was %d)", id2, got, id1)
	}
}

// TestLatestRefinementID_MatchesAfterInsert verifies that a snapshot taken
// in the same process as a refinement insert will see the correct ID.
// This is the fix for the parallel hook race: refine creates the record,
// then calls takeSnapshot in the same process, so LatestRefinementID
// must return the just-created ID.
func TestLatestRefinementID_MatchesAfterInsert(t *testing.T) {
	d := testDB(t)
	d.UpsertSession(&Session{ID: "sess-race", ProjectPath: "/p", StartedAt: time.Now().UTC(), Status: "active"})

	// Simulate refine: insert pending refinement, then immediately query latest
	refID, err := d.InsertRefinement(&Refinement{
		SessionID: "sess-race", ProjectPath: "/p", RawPrompt: "prompt 1", Status: "pending",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := d.LatestRefinementID("sess-race")
	if got != refID {
		t.Errorf("LatestRefinementID after insert = %d, want %d", got, refID)
	}

	// Insert second refinement (simulates next prompt), verify latest advances
	refID2, _ := d.InsertRefinement(&Refinement{
		SessionID: "sess-race", ProjectPath: "/p", RawPrompt: "prompt 2", Status: "pending",
	})
	got2 := d.LatestRefinementID("sess-race")
	if got2 != refID2 {
		t.Errorf("LatestRefinementID after second insert = %d, want %d", got2, refID2)
	}
}

// TestLatestRefinementID_SessionIsolation ensures different sessions don't leak.
func TestLatestRefinementID_SessionIsolation(t *testing.T) {
	d := testDB(t)
	d.UpsertSession(&Session{ID: "s-a", ProjectPath: "/p", StartedAt: time.Now().UTC(), Status: "active"})
	d.UpsertSession(&Session{ID: "s-b", ProjectPath: "/p", StartedAt: time.Now().UTC(), Status: "active"})

	d.InsertRefinement(&Refinement{SessionID: "s-a", ProjectPath: "/p", RawPrompt: "a1", Status: "complete"})
	refB, _ := d.InsertRefinement(&Refinement{SessionID: "s-b", ProjectPath: "/p", RawPrompt: "b1", Status: "complete"})

	// Session b should only see its own refinement
	got := d.LatestRefinementID("s-b")
	if got != refB {
		t.Errorf("session b latest = %d, want %d (should not see session a)", got, refB)
	}

	// Nonexistent session returns 0
	if d.LatestRefinementID("s-none") != 0 {
		t.Error("nonexistent session should return 0")
	}
}

// TestVerificationEvent_NullRefinementID ensures events with no refinement_id
// are not returned when querying by refinement_id.
func TestVerificationEvent_NullRefinementID(t *testing.T) {
	d := testDB(t)
	d.UpsertSession(&Session{ID: "sess-null", ProjectPath: "/p", StartedAt: time.Now().UTC(), Status: "active"})
	refID, _ := d.InsertRefinement(&Refinement{SessionID: "sess-null", ProjectPath: "/p", RawPrompt: "p", Status: "complete"})

	dur := int64(100)

	// Insert event WITHOUT refinement_id (old data or edge case)
	d.InsertVerificationEvent(&VerificationEvent{
		SessionID: "sess-null", Scope: "prompt",
		HookEvent: "Stop", EventType: "verify", DurationUs: &dur, Result: strPtr("pass"),
		// RefinementID intentionally nil
	})

	// Insert event WITH refinement_id
	d.InsertVerificationEvent(&VerificationEvent{
		SessionID: "sess-null", RefinementID: &refID, Scope: "prompt",
		HookEvent: "Stop", EventType: "verify", DurationUs: &dur, Result: strPtr("pass"),
	})

	events, _ := d.GetVerificationEventsForRefinement(refID)
	if len(events) != 1 {
		t.Fatalf("expected 1 event (linked), got %d (null-linked event leaked)", len(events))
	}
}

// TestVerificationEvent_SnapshotBeforeVerifyOrdering ensures events come back
// in chronological order (snapshot first, then verify).
func TestVerificationEvent_SnapshotBeforeVerifyOrdering(t *testing.T) {
	d := testDB(t)
	d.UpsertSession(&Session{ID: "sess-ord", ProjectPath: "/p", StartedAt: time.Now().UTC(), Status: "active"})
	refID, _ := d.InsertRefinement(&Refinement{SessionID: "sess-ord", ProjectPath: "/p", RawPrompt: "p", Status: "complete"})

	dur := int64(100)

	// Insert verify first (id=1), then snapshot (id=2)
	// But created_at defaults to CURRENT_TIMESTAMP which should be same
	// The ORDER BY created_at ASC should still preserve insert order for same timestamp
	d.InsertVerificationEvent(&VerificationEvent{
		SessionID: "sess-ord", RefinementID: &refID, Scope: "prompt",
		HookEvent: "UserPromptSubmit", EventType: "snapshot", DurationUs: &dur,
	})
	d.InsertVerificationEvent(&VerificationEvent{
		SessionID: "sess-ord", RefinementID: &refID, Scope: "prompt",
		HookEvent: "Stop", EventType: "verify", DurationUs: &dur, Result: strPtr("pass"),
	})

	events, _ := d.GetVerificationEventsForRefinement(refID)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].EventType != "snapshot" {
		t.Error("first event should be snapshot (chronological order)")
	}
	if events[1].EventType != "verify" {
		t.Error("second event should be verify")
	}
}

// TestRecorderSnapshot_LinksRefinement tests the recorder convenience method.
func TestRecorderSnapshot_LinksRefinement(t *testing.T) {
	d := testDB(t)
	d.UpsertSession(&Session{ID: "sess-rec", ProjectPath: "/p", StartedAt: time.Now().UTC(), Status: "active"})
	refID, _ := d.InsertRefinement(&Refinement{SessionID: "sess-rec", ProjectPath: "/p", RawPrompt: "p", Status: "complete"})

	r := NewRecorder(d, "")
	r.RecordSnapshot("sess-rec", refID, "prompt", "UserPromptSubmit", "/p/cli", "/p", 50, 3000)

	events, _ := d.GetVerificationEventsForRefinement(refID)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].RefinementID == nil || *events[0].RefinementID != refID {
		t.Errorf("refinement_id = %v, want %d", events[0].RefinementID, refID)
	}
	if *events[0].FileCount != 50 {
		t.Errorf("file_count = %d, want 50", *events[0].FileCount)
	}
	if *events[0].DurationUs != 3000 {
		t.Errorf("duration_us = %d, want 3000", *events[0].DurationUs)
	}
}

// TestRecorderVerification_LinksRefinement tests the recorder convenience method.
func TestRecorderVerification_LinksRefinement(t *testing.T) {
	d := testDB(t)
	d.UpsertSession(&Session{ID: "sess-rv", ProjectPath: "/p", StartedAt: time.Now().UTC(), Status: "active"})
	refID, _ := d.InsertRefinement(&Refinement{SessionID: "sess-rv", ProjectPath: "/p", RawPrompt: "p", Status: "complete"})

	r := NewRecorder(d, "")
	r.RecordVerification("sess-rv", refID, "prompt", "Stop", "/p", "/p",
		`["main.go"]`,
		`[{"name":"test","command":"go test","passed":false,"output":"FAIL","duration_ms":100}]`,
		"fail", 5000)

	events, _ := d.GetVerificationEventsForRefinement(refID)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if *events[0].Result != "fail" {
		t.Errorf("result = %q, want fail", *events[0].Result)
	}
	if events[0].ChangedFiles == nil || *events[0].ChangedFiles != `["main.go"]` {
		t.Errorf("changed_files = %v", events[0].ChangedFiles)
	}
	if events[0].ChecksRun == nil {
		t.Error("checks_run should not be nil")
	}
}

// TestRecorderSnapshot_ZeroRefinementID stores NULL when refinement_id is 0.
func TestRecorderSnapshot_ZeroRefinementID(t *testing.T) {
	d := testDB(t)
	r := NewRecorder(d, "")
	r.RecordSnapshot("sess-zero", 0, "prompt", "UserPromptSubmit", "/p", "/p", 10, 100)

	// Should not be findable by refinement_id = 0
	events, _ := d.GetVerificationEventsForRefinement(0)
	if len(events) != 0 {
		t.Errorf("events with refinement_id=0 should not be returned, got %d", len(events))
	}
}

func strPtr(s string) *string { return &s }
