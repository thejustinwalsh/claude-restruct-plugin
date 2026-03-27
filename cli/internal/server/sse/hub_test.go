package sse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHub_SubscribeUnsubscribe(t *testing.T) {
	h := NewHub()

	ch := h.Subscribe()
	if h.ClientCount() != 1 {
		t.Errorf("client count = %d, want 1", h.ClientCount())
	}

	h.Unsubscribe(ch)
	if h.ClientCount() != 0 {
		t.Errorf("client count after unsubscribe = %d, want 0", h.ClientCount())
	}
}

func TestHub_Broadcast(t *testing.T) {
	h := NewHub()

	ch1 := h.Subscribe()
	ch2 := h.Subscribe()
	defer h.Unsubscribe(ch1)
	defer h.Unsubscribe(ch2)

	h.Broadcast(Event{Type: "test", Data: "hello"})

	select {
	case evt := <-ch1:
		if evt.Type != "test" {
			t.Errorf("ch1 type = %q", evt.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("ch1 timeout")
	}

	select {
	case evt := <-ch2:
		if evt.Type != "test" {
			t.Errorf("ch2 type = %q", evt.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("ch2 timeout")
	}
}

func TestHub_BroadcastDropsSlowClient(t *testing.T) {
	h := NewHub()

	ch := h.Subscribe()
	defer h.Unsubscribe(ch)

	// Fill the buffer (capacity 64)
	for i := 0; i < 70; i++ {
		h.Broadcast(Event{Type: "flood", Data: i})
	}

	// Should not panic or block; some events dropped
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count > 64 {
		t.Errorf("received %d events, expected at most 64 (buffer size)", count)
	}
}

func TestHub_MultipleSubscribers(t *testing.T) {
	h := NewHub()

	channels := make([]chan Event, 5)
	for i := range channels {
		channels[i] = h.Subscribe()
	}

	if h.ClientCount() != 5 {
		t.Errorf("client count = %d, want 5", h.ClientCount())
	}

	for _, ch := range channels {
		h.Unsubscribe(ch)
	}

	if h.ClientCount() != 0 {
		t.Errorf("client count after all unsubscribed = %d", h.ClientCount())
	}
}

func TestHub_ServeHTTP(t *testing.T) {
	h := NewHub()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/api/events", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(w, req)
		close(done)
	}()

	// Give it a moment to start, then broadcast an event
	time.Sleep(50 * time.Millisecond)
	h.Broadcast(Event{Type: "test:event", Data: map[string]string{"key": "val"}})
	time.Sleep(50 * time.Millisecond)

	cancel()
	<-done

	body := w.Body.String()
	if !strings.Contains(body, "event: connected") {
		t.Error("missing connected event")
	}
	if !strings.Contains(body, "event: test:event") {
		t.Error("missing broadcast event")
	}

	ct := w.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
}

func TestHub_ServeHTTP_NonFlusher(t *testing.T) {
	h := NewHub()
	req := httptest.NewRequest("GET", "/api/events", nil)
	w := &nonFlushWriter{rec: httptest.NewRecorder()}

	h.ServeHTTP(w, req)
	if w.rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for non-flusher", w.rec.Code)
	}
}

// nonFlushWriter wraps ResponseRecorder but doesn't implement http.Flusher
type nonFlushWriter struct {
	rec *httptest.ResponseRecorder
}

func (w *nonFlushWriter) Header() http.Header        { return w.rec.Header() }
func (w *nonFlushWriter) Write(b []byte) (int, error) { return w.rec.Write(b) }
func (w *nonFlushWriter) WriteHeader(code int)         { w.rec.WriteHeader(code) }
