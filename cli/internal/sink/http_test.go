package sink

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestNewHttpTokenSink_NilOnEmptyURL(t *testing.T) {
	s := NewHttpTokenSink("", 1, "sess-1")
	if s != nil {
		t.Error("expected nil for empty URL")
	}
}

func TestHttpTokenSink_NilSafe(t *testing.T) {
	// All methods should be safe to call on nil
	var s *HttpTokenSink
	s.Start("prompt", "model") // should not panic
	s.OnToken("hello")
	s.OnDone()
	s.OnError(nil)
}

type capturedReq struct {
	path string
	body map[string]interface{}
}

func testServerCapture(t *testing.T) (*httptest.Server, *[]capturedReq, *sync.Mutex) {
	t.Helper()
	var reqs []capturedReq
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		reqs = append(reqs, capturedReq{path: r.URL.Path, body: body})
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(ts.Close)
	return ts, &reqs, &mu
}

func TestHttpTokenSink_Start(t *testing.T) {
	ts, reqs, mu := testServerCapture(t)
	s := NewHttpTokenSink(ts.URL, 42, "sess-1")

	s.Start("fix the bug", "qwen2.5")

	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()

	if len(*reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(*reqs))
	}
	if (*reqs)[0].path != "/api/stream/start" {
		t.Errorf("path = %q", (*reqs)[0].path)
	}
	if (*reqs)[0].body["raw_prompt"] != "fix the bug" {
		t.Errorf("raw_prompt = %v", (*reqs)[0].body["raw_prompt"])
	}
}

func TestHttpTokenSink_OnDone(t *testing.T) {
	ts, reqs, mu := testServerCapture(t)
	s := NewHttpTokenSink(ts.URL, 1, "sess-1")

	s.OnDone()

	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()

	found := false
	for _, r := range *reqs {
		if r.path == "/api/stream/done" {
			found = true
		}
	}
	if !found {
		t.Error("expected /api/stream/done request")
	}
}

func TestHttpTokenSink_OnError(t *testing.T) {
	ts, reqs, mu := testServerCapture(t)
	s := NewHttpTokenSink(ts.URL, 1, "sess-1")

	s.OnError(http.ErrAbortHandler)

	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()

	found := false
	for _, r := range *reqs {
		if r.path == "/api/stream/error" {
			found = true
		}
	}
	if !found {
		t.Error("expected /api/stream/error request")
	}
}

func TestHttpTokenSink_Batching(t *testing.T) {
	ts, reqs, mu := testServerCapture(t)
	s := NewHttpTokenSink(ts.URL, 1, "sess-1")

	// Send 3 tokens quickly — should batch within the window
	s.OnToken("hello")
	s.OnToken(" ")
	s.OnToken("world")

	// Wait for batch window + flush
	time.Sleep(200 * time.Millisecond)

	s.OnDone()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Should have token batch(es) + done
	var tokenReqs []capturedReq
	for _, r := range *reqs {
		if r.path == "/api/stream/token" {
			tokenReqs = append(tokenReqs, r)
		}
	}
	if len(tokenReqs) == 0 {
		t.Fatal("expected at least one token batch request")
	}

	// Verify tokens were concatenated
	allTokens := ""
	for _, r := range tokenReqs {
		if tokens, ok := r.body["tokens"].(string); ok {
			allTokens += tokens
		}
	}
	if allTokens != "hello world" {
		t.Errorf("concatenated tokens = %q, want %q", allTokens, "hello world")
	}
}

func TestHttpTokenSink_LargeBatchFlush(t *testing.T) {
	ts, reqs, mu := testServerCapture(t)
	s := NewHttpTokenSink(ts.URL, 1, "sess-1")

	// Send more than batchMaxSize tokens
	for i := 0; i < 25; i++ {
		s.OnToken("x")
	}

	time.Sleep(200 * time.Millisecond)
	s.OnDone()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	var tokenReqs int
	for _, r := range *reqs {
		if r.path == "/api/stream/token" {
			tokenReqs++
		}
	}
	// 25 tokens with batchMaxSize=20 should produce at least 2 batches
	if tokenReqs < 2 {
		t.Errorf("got %d token batches, expected at least 2 for 25 tokens", tokenReqs)
	}
}

func TestHttpTokenSink_DisablesOnServerDown(t *testing.T) {
	// Point to a server that's already closed
	s := NewHttpTokenSink("http://127.0.0.1:1", 1, "sess-1")

	s.Start("test", "model")
	time.Sleep(50 * time.Millisecond)

	if !s.disabled.Load() {
		t.Error("expected sink to disable after connection failure")
	}

	// Subsequent calls should be no-ops (not panic)
	s.OnToken("hello")
	s.OnDone()
}
