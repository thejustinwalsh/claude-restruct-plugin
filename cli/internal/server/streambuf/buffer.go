// Package streambuf provides an in-memory buffer for active refinement streams.
// The server writes tokens here as they arrive from the CLI, and new SSE clients
// can catch up by reading the accumulated buffer.
package streambuf

import (
	"strings"
	"sync"
	"time"
)

// ActiveStream represents the current state of one refinement's token stream.
type ActiveStream struct {
	RefinementID int64     `json:"refinement_id"`
	SessionID    string    `json:"session_id"`
	RawPrompt    string    `json:"raw_prompt"`
	Model        string    `json:"model"`
	Text         string    `json:"text"`
	SeqEnd       int       `json:"seq_end"`
	IsStreaming  bool      `json:"is_streaming"`
	Error        string    `json:"error,omitempty"`
	StartedAt    time.Time `json:"started_at"`
}

// stream is the internal mutable representation.
type stream struct {
	refinementID int64
	sessionID    string
	rawPrompt    string
	model        string
	text         strings.Builder
	seqEnd       int
	isStreaming   bool
	err          string
	startedAt    time.Time
}

func (s *stream) snapshot() ActiveStream {
	return ActiveStream{
		RefinementID: s.refinementID,
		SessionID:    s.sessionID,
		RawPrompt:    s.rawPrompt,
		Model:        s.model,
		Text:         s.text.String(),
		SeqEnd:       s.seqEnd,
		IsStreaming:  s.isStreaming,
		Error:        s.err,
		StartedAt:    s.startedAt,
	}
}

// Buffer holds active streams keyed by refinement ID.
type Buffer struct {
	mu      sync.RWMutex
	streams map[int64]*stream
	ttl     time.Duration
}

// New creates a Buffer with the given TTL for stale stream pruning.
func New(ttl time.Duration) *Buffer {
	return &Buffer{
		streams: make(map[int64]*stream),
		ttl:     ttl,
	}
}

// Start registers a new active stream.
func (b *Buffer) Start(refinementID int64, sessionID, rawPrompt, model string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.streams[refinementID] = &stream{
		refinementID: refinementID,
		sessionID:    sessionID,
		rawPrompt:    rawPrompt,
		model:        model,
		isStreaming:   true,
		startedAt:    time.Now(),
	}
}

// Append adds token text to an active stream.
func (b *Buffer) Append(refinementID int64, tokens string, seqEnd int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.streams[refinementID]
	if !ok {
		return
	}
	s.text.WriteString(tokens)
	s.seqEnd = seqEnd
}

// End marks a stream as complete (no longer streaming).
func (b *Buffer) End(refinementID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.streams[refinementID]
	if !ok {
		return
	}
	s.isStreaming = false
}

// SetError marks a stream as errored.
func (b *Buffer) SetError(refinementID int64, errMsg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.streams[refinementID]
	if !ok {
		return
	}
	s.isStreaming = false
	s.err = errMsg
}

// Get returns a snapshot of one stream. Returns false if not found.
func (b *Buffer) Get(refinementID int64) (ActiveStream, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	s, ok := b.streams[refinementID]
	if !ok {
		return ActiveStream{}, false
	}
	return s.snapshot(), true
}

// Active returns snapshots of all currently streaming entries.
func (b *Buffer) Active() []ActiveStream {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var out []ActiveStream
	for _, s := range b.streams {
		if s.isStreaming {
			out = append(out, s.snapshot())
		}
	}
	return out
}

// Prune removes streams that have exceeded the TTL.
func (b *Buffer) Prune() {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for id, s := range b.streams {
		if now.Sub(s.startedAt) > b.ttl {
			delete(b.streams, id)
		}
	}
}
