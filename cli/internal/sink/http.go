package sink

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// HttpTokenSink implements ollama.TokenSink by POSTing token batches
// to the restruct server for live SSE streaming to the dashboard.
// All operations are best-effort — failures are logged and ignored.
type HttpTokenSink struct {
	baseURL      string
	refinementID int64
	sessionID    string
	client       *http.Client
	disabled     atomic.Bool

	// Batching: accumulate tokens and flush periodically
	mu       sync.Mutex
	buf      bytes.Buffer
	seqStart int
	seqEnd   int
	timer    *time.Timer
}

const (
	batchWindow  = 50 * time.Millisecond
	batchMaxSize = 20
	httpTimeout  = 2 * time.Second
)

// NewHttpTokenSink creates a sink that streams tokens to the server.
// Returns nil if serverURL is empty.
func NewHttpTokenSink(serverURL string, refinementID int64, sessionID string) *HttpTokenSink {
	if serverURL == "" {
		return nil
	}
	return &HttpTokenSink{
		baseURL:      serverURL,
		refinementID: refinementID,
		sessionID:    sessionID,
		client:       &http.Client{Timeout: httpTimeout},
	}
}

// Start notifies the server that a new refinement stream is beginning.
func (s *HttpTokenSink) Start(rawPrompt, model string) {
	if s == nil {
		return
	}
	s.post("/api/stream/start", map[string]interface{}{
		"refinement_id": s.refinementID,
		"session_id":    s.sessionID,
		"raw_prompt":    rawPrompt,
		"model":         model,
	})
}

// OnToken receives a single token from the Ollama stream.
func (s *HttpTokenSink) OnToken(content string) {
	if s == nil || s.disabled.Load() {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.buf.Len() == 0 {
		s.seqStart = s.seqEnd
	}
	s.buf.WriteString(content)
	s.seqEnd++

	if s.seqEnd-s.seqStart >= batchMaxSize {
		s.flushLocked()
		return
	}

	if s.timer == nil {
		s.timer = time.AfterFunc(batchWindow, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.flushLocked()
		})
	}
}

// OnDone signals stream completion.
func (s *HttpTokenSink) OnDone() {
	if s == nil || s.disabled.Load() {
		return
	}

	// Flush any remaining tokens
	s.mu.Lock()
	s.flushLocked()
	s.mu.Unlock()

	s.post("/api/stream/done", map[string]interface{}{
		"refinement_id": s.refinementID,
	})
}

// OnError signals a stream error.
func (s *HttpTokenSink) OnError(err error) {
	if s == nil || s.disabled.Load() {
		return
	}

	s.post("/api/stream/error", map[string]interface{}{
		"refinement_id": s.refinementID,
		"error":         err.Error(),
	})
}

func (s *HttpTokenSink) flushLocked() {
	if s.buf.Len() == 0 {
		return
	}
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}

	tokens := s.buf.String()
	seqStart := s.seqStart
	seqEnd := s.seqEnd
	s.buf.Reset()
	s.seqStart = s.seqEnd

	// Send in background to avoid blocking the LLM stream
	go s.post("/api/stream/token", map[string]interface{}{
		"refinement_id": s.refinementID,
		"tokens":        tokens,
		"seq_start":     seqStart,
		"seq_end":       seqEnd,
	})
}

func (s *HttpTokenSink) post(path string, payload interface{}) {
	if s.disabled.Load() {
		return
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	resp, err := s.client.Post(s.baseURL+path, "application/json", bytes.NewReader(body))
	if err != nil {
		slog.Debug("stream sink: server unavailable, disabling", "error", err)
		s.disabled.Store(true)
		return
	}
	resp.Body.Close()
}
