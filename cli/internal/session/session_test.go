package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionLifecycle(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	// Start a session
	s, err := mgr.Start("sess-1", "/home/dev/project", "/tmp/transcript.jsonl")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if s.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", s.SessionID, "sess-1")
	}
	if s.RefinementCount != 0 {
		t.Errorf("RefinementCount = %d, want 0", s.RefinementCount)
	}

	// Start again is idempotent
	s2, err := mgr.Start("sess-1", "/home/dev/project", "/tmp/transcript.jsonl")
	if err != nil {
		t.Fatalf("Start (idempotent): %v", err)
	}
	if s2.SessionID != "sess-1" {
		t.Error("idempotent Start returned different session")
	}

	// Record refinement
	s3, err := mgr.RecordRefinement("sess-1")
	if err != nil {
		t.Fatalf("RecordRefinement: %v", err)
	}
	if s3.RefinementCount != 1 {
		t.Errorf("RefinementCount = %d, want 1", s3.RefinementCount)
	}
	if s3.LastRefinementAt.IsZero() {
		t.Error("LastRefinementAt should be set")
	}

	// Get
	s4, err := mgr.Get("sess-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if s4.RefinementCount != 1 {
		t.Errorf("persisted RefinementCount = %d, want 1", s4.RefinementCount)
	}

	// Get nonexistent
	s5, err := mgr.Get("nonexistent")
	if err != nil {
		t.Fatalf("Get nonexistent: %v", err)
	}
	if s5 != nil {
		t.Error("expected nil for nonexistent session")
	}

	// Record refinement on nonexistent session
	_, err = mgr.RecordRefinement("nonexistent")
	if err == nil {
		t.Error("expected error recording refinement for nonexistent session")
	}

	// List active
	sessions, err := mgr.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("ListActive count = %d, want 1", len(sessions))
	}

	// End
	if err := mgr.End("sess-1"); err != nil {
		t.Fatalf("End: %v", err)
	}
	s6, err := mgr.Get("sess-1")
	if err != nil {
		t.Fatalf("Get after End: %v", err)
	}
	if s6 != nil {
		t.Error("session should be nil after End")
	}

	// End idempotent
	if err := mgr.End("sess-1"); err != nil {
		t.Fatalf("End (idempotent): %v", err)
	}
}

func TestCleanStale(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	// Create a session with old timestamp
	s, err := mgr.Start("old-sess", "/proj", "/tmp/t.jsonl")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.StartedAt = time.Now().UTC().Add(-48 * time.Hour)
	if err := mgr.save(s); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Create a fresh session
	_, err = mgr.Start("new-sess", "/proj", "/tmp/t.jsonl")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	removed, err := mgr.CleanStale(24 * time.Hour)
	if err != nil {
		t.Fatalf("CleanStale: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}

	sessions, _ := mgr.ListActive()
	if len(sessions) != 1 {
		t.Errorf("active sessions = %d, want 1", len(sessions))
	}
	if sessions[0].SessionID != "new-sess" {
		t.Errorf("remaining session = %q, want %q", sessions[0].SessionID, "new-sess")
	}
}

func TestEnsureDirs(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	if err := mgr.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	expected := filepath.Join(dir, ".restruct", "sessions")
	info, err := os.Stat(expected)
	if err != nil {
		t.Fatalf("stat sessions dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("sessions path should be a directory")
	}
}
