package cache

import (
	"os"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir, _ := os.MkdirTemp("", "restruct-cache-test")
	defer os.RemoveAll(dir)

	s := NewStore(dir, true)

	// Miss
	if _, ok := s.Get("prompt", "hash"); ok {
		t.Fatal("expected cache miss")
	}

	// Put + Hit
	if err := s.Put("prompt", "hash", "refined"); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, ok := s.Get("prompt", "hash")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got != "refined" {
		t.Fatalf("got %q, want %q", got, "refined")
	}

	// Stats
	entries, size, err := s.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if entries != 1 {
		t.Fatalf("entries: got %d, want 1", entries)
	}
	if size == 0 {
		t.Fatal("expected non-zero size")
	}
}

func TestStoreDisabled(t *testing.T) {
	s := NewStore("/tmp/unused", false)
	if _, ok := s.Get("prompt", "hash"); ok {
		t.Fatal("disabled store should always miss")
	}
	if err := s.Put("prompt", "hash", "refined"); err != nil {
		t.Fatalf("put on disabled store: %v", err)
	}
}
