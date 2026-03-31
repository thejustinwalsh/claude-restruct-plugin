package streambuf

import (
	"sync"
	"testing"
	"time"
)

func TestLifecycle(t *testing.T) {
	b := New(5 * time.Minute)

	b.Start(1, "sess-1", "fix the bug", "qwen2.5")

	// Active should include it
	active := b.Active()
	if len(active) != 1 {
		t.Fatalf("expected 1 active, got %d", len(active))
	}
	if active[0].RefinementID != 1 || !active[0].IsStreaming {
		t.Fatal("unexpected active state")
	}

	// Append tokens
	b.Append(1, "hello ", 1)
	b.Append(1, "world", 2)

	got, ok := b.Get(1)
	if !ok {
		t.Fatal("expected to find stream")
	}
	if got.Text != "hello world" {
		t.Errorf("expected 'hello world', got %q", got.Text)
	}
	if got.SeqEnd != 2 {
		t.Errorf("expected SeqEnd=2, got %d", got.SeqEnd)
	}

	// End the stream
	b.End(1)

	got, ok = b.Get(1)
	if !ok {
		t.Fatal("expected to find completed stream")
	}
	if got.IsStreaming {
		t.Error("expected IsStreaming=false after End")
	}

	// Active should now be empty (End marks it non-streaming)
	if len(b.Active()) != 0 {
		t.Error("expected no active streams after End")
	}
}

func TestError(t *testing.T) {
	b := New(5 * time.Minute)
	b.Start(1, "sess-1", "prompt", "model")
	b.Append(1, "partial", 1)
	b.SetError(1, "model crashed")

	got, ok := b.Get(1)
	if !ok {
		t.Fatal("expected to find stream")
	}
	if got.IsStreaming {
		t.Error("expected IsStreaming=false after SetError")
	}
	if got.Error != "model crashed" {
		t.Errorf("expected error 'model crashed', got %q", got.Error)
	}
	if got.Text != "partial" {
		t.Errorf("expected text 'partial', got %q", got.Text)
	}
}

func TestPrune(t *testing.T) {
	b := New(50 * time.Millisecond)
	b.Start(1, "sess-1", "prompt", "model")
	b.Append(1, "hello", 1)

	// Should exist before TTL
	if _, ok := b.Get(1); !ok {
		t.Fatal("expected to find stream before TTL")
	}

	time.Sleep(100 * time.Millisecond)
	b.Prune()

	if _, ok := b.Get(1); ok {
		t.Fatal("expected stream to be pruned after TTL")
	}
}

func TestPruneKeepsRecent(t *testing.T) {
	b := New(1 * time.Second)
	b.Start(1, "sess-1", "old", "model")
	b.Start(2, "sess-2", "new", "model")

	// Only stream 1 should be pruned if it's old enough
	b.Prune()

	if _, ok := b.Get(1); !ok {
		t.Fatal("stream 1 should still exist (not yet expired)")
	}
	if _, ok := b.Get(2); !ok {
		t.Fatal("stream 2 should still exist")
	}
}

func TestGetNotFound(t *testing.T) {
	b := New(5 * time.Minute)
	if _, ok := b.Get(999); ok {
		t.Fatal("expected not found for nonexistent stream")
	}
}

func TestAppendToNonexistent(t *testing.T) {
	b := New(5 * time.Minute)
	// Should not panic
	b.Append(999, "tokens", 1)
	b.End(999)
	b.SetError(999, "err")
}

func TestConcurrentAccess(t *testing.T) {
	b := New(5 * time.Minute)
	b.Start(1, "sess-1", "prompt", "model")

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			b.Append(1, "tok", n)
			b.Get(1)
			b.Active()
		}(i)
	}
	wg.Wait()

	got, ok := b.Get(1)
	if !ok {
		t.Fatal("expected to find stream after concurrent access")
	}
	// 100 appends of "tok" = 300 chars
	if len(got.Text) != 300 {
		t.Errorf("expected 300 chars, got %d", len(got.Text))
	}
}

func TestSnapshotIsolation(t *testing.T) {
	b := New(5 * time.Minute)
	b.Start(1, "sess-1", "prompt", "model")
	b.Append(1, "before", 1)

	snap, _ := b.Get(1)

	// Mutate the buffer after snapshot
	b.Append(1, "after", 2)

	// Snapshot should be unchanged
	if snap.Text != "before" {
		t.Errorf("snapshot should be isolated, got %q", snap.Text)
	}
}
