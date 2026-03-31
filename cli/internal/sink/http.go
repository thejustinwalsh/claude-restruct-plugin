package sink

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

// HttpTokenSink implements ollama.TokenSink by buffering tokens and
// sending them to the restruct server via a background goroutine.
// The LLM stream is never blocked by HTTP calls — tokens are written
// to a channel and drained asynchronously.
type HttpTokenSink struct {
	baseURL      string
	refinementID int64
	sessionID    string
	client       *http.Client
	disabled     atomic.Bool

	// Async send queue — all HTTP POSTs go through here
	sendCh chan sendMsg
	done   chan struct{} // closed when background goroutine exits

	// Batching state (only accessed by the drain goroutine)
	buf      bytes.Buffer
	seqStart int
	seqEnd   int
	timer    *time.Timer
}

type sendMsg struct {
	path    string
	payload interface{}
}

// sentinel payloads for control messages
type tokenMsg struct {
	content string
}
type doneMsg struct{}
type errorMsg struct {
	err error
}

const (
	batchWindow  = 50 * time.Millisecond
	batchMaxSize = 20
	httpTimeout  = 2 * time.Second
	sendQueueCap = 256
)

// NewHttpTokenSink creates a sink that streams tokens to the server.
// Returns nil if serverURL is empty.
func NewHttpTokenSink(serverURL string, refinementID int64, sessionID string) *HttpTokenSink {
	if serverURL == "" {
		return nil
	}
	s := &HttpTokenSink{
		baseURL:      serverURL,
		refinementID: refinementID,
		sessionID:    sessionID,
		client:       &http.Client{Timeout: httpTimeout},
		sendCh:       make(chan sendMsg, sendQueueCap),
		done:         make(chan struct{}),
	}
	go s.drain()
	return s
}

// Start notifies the server that a new refinement stream is beginning.
func (s *HttpTokenSink) Start(rawPrompt, model string) {
	if s == nil {
		return
	}
	s.enqueue("/api/stream/start", map[string]interface{}{
		"refinement_id": s.refinementID,
		"session_id":    s.sessionID,
		"raw_prompt":    rawPrompt,
		"model":         model,
	})
}

// SendInput broadcasts the LLM input prompt (step 2 in data flow).
// Called after the prompt is built but before inference starts.
func (s *HttpTokenSink) SendInput(inputPrompt string) {
	if s == nil {
		return
	}
	s.enqueue("/api/stream/input", map[string]interface{}{
		"refinement_id": s.refinementID,
		"input_prompt":  inputPrompt,
	})
}

// SendComplete broadcasts the final composed context and pipeline timings
// (step 4 in data flow). Called after all DB writes are done.
func (s *HttpTokenSink) SendComplete(refinedPrompt string, llmOutput string, latencyMs int64, timings []map[string]interface{}) {
	if s == nil {
		return
	}
	s.enqueue("/api/stream/complete", map[string]interface{}{
		"refinement_id":  s.refinementID,
		"refined_prompt": refinedPrompt,
		"llm_output":     llmOutput,
		"latency_ms":     latencyMs,
		"timings":        timings,
	})
}

// OnToken receives a single token from the Ollama stream.
// Never blocks — writes to the send channel for async batching.
func (s *HttpTokenSink) OnToken(content string) {
	if s == nil || s.disabled.Load() {
		return
	}
	// Non-blocking send; drop tokens if queue is full rather than blocking LLM
	select {
	case s.sendCh <- sendMsg{path: "token", payload: tokenMsg{content: content}}:
	default:
		slog.Debug("stream sink: send queue full, dropping token")
	}
}

// OnDone signals stream completion.
func (s *HttpTokenSink) OnDone() {
	if s == nil || s.disabled.Load() {
		return
	}
	select {
	case s.sendCh <- sendMsg{path: "done", payload: doneMsg{}}:
	default:
	}
}

// OnError signals a stream error.
func (s *HttpTokenSink) OnError(err error) {
	if s == nil || s.disabled.Load() {
		return
	}
	select {
	case s.sendCh <- sendMsg{path: "error", payload: errorMsg{err: err}}:
	default:
	}
}

// Close waits for the background goroutine to finish sending queued
// messages. Call after OnDone/OnError to ensure delivery. Safe to skip
// if you don't need delivery guarantees.
func (s *HttpTokenSink) Close() {
	if s == nil {
		return
	}
	close(s.sendCh)
	<-s.done
}

// drain runs in a background goroutine, processing all send messages.
// Tokens are batched; control messages (start/done/error) are sent immediately.
func (s *HttpTokenSink) drain() {
	defer close(s.done)

	for msg := range s.sendCh {
		if s.disabled.Load() {
			continue
		}

		switch v := msg.payload.(type) {
		case tokenMsg:
			s.bufferToken(v.content)
		case doneMsg:
			s.flushBatch()
			s.post("/api/stream/done", map[string]interface{}{
				"refinement_id": s.refinementID,
			})
		case errorMsg:
			s.flushBatch()
			s.post("/api/stream/error", map[string]interface{}{
				"refinement_id": s.refinementID,
				"error":         v.err.Error(),
			})
		default:
			if msg.path == "flush" {
				s.flushBatch()
			} else {
				// Direct send (e.g., stream/start)
				s.post(msg.path, msg.payload)
			}
		}
	}

	// Channel closed — flush anything remaining
	if s.timer != nil {
		s.timer.Stop()
	}
	s.flushBatch()
}

func (s *HttpTokenSink) bufferToken(content string) {
	if s.buf.Len() == 0 {
		s.seqStart = s.seqEnd
	}
	s.buf.WriteString(content)
	s.seqEnd++

	if s.seqEnd-s.seqStart >= batchMaxSize {
		s.flushBatch()
		return
	}

	if s.timer == nil {
		s.timer = time.AfterFunc(batchWindow, func() {
			// Timer fired — send a flush signal through the channel
			select {
			case s.sendCh <- sendMsg{path: "flush"}:
			default:
			}
		})
	}
}

func (s *HttpTokenSink) flushBatch() {
	if s.buf.Len() == 0 {
		return
	}
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}

	s.post("/api/stream/token", map[string]interface{}{
		"refinement_id": s.refinementID,
		"tokens":        s.buf.String(),
		"seq_start":     s.seqStart,
		"seq_end":       s.seqEnd,
	})
	s.buf.Reset()
	s.seqStart = s.seqEnd
}

func (s *HttpTokenSink) enqueue(path string, payload interface{}) {
	select {
	case s.sendCh <- sendMsg{path: path, payload: payload}:
	default:
		slog.Debug("stream sink: send queue full, dropping message", "path", path)
	}
}

func (s *HttpTokenSink) post(path string, payload interface{}) {
	if s.disabled.Load() {
		return
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	for attempt := 0; attempt < 3; attempt++ {
		resp, err := s.client.Post(s.baseURL+path, "application/json", bytes.NewReader(body))
		if err != nil {
			if attempt < 2 {
				time.Sleep(time.Duration(50*(attempt+1)) * time.Millisecond)
				continue
			}
			slog.Debug("stream sink: server unavailable after retries, disabling", "error", err)
			s.disabled.Store(true)
			return
		}
		resp.Body.Close()
		return
	}
}
