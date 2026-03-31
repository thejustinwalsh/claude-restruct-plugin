package sse

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Event represents a server-sent event.
type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Hub manages SSE client connections and broadcasts events.
type Hub struct {
	mu      sync.RWMutex
	clients map[chan Event]struct{}
}

// NewHub creates a new SSE hub.
func NewHub() *Hub {
	return &Hub{
		clients: make(map[chan Event]struct{}),
	}
}

// Subscribe adds a new client and returns its event channel.
func (h *Hub) Subscribe() chan Event {
	ch := make(chan Event, 64)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	slog.Debug("sse client connected", "total", h.ClientCount())
	return ch
}

// Unsubscribe removes a client.
func (h *Hub) Unsubscribe(ch chan Event) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
	slog.Debug("sse client disconnected", "total", h.ClientCount())
}

// Broadcast sends an event to all connected clients.
func (h *Hub) Broadcast(evt Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- evt:
		default:
			// Client buffer full, skip
			slog.Debug("dropping sse event for slow client")
		}
	}
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// ServeHTTP handles SSE connections with no init events.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.ServeHTTPWithInit(w, r, nil)
}

// ServeHTTPWithInit handles SSE connections, sending initEvents immediately
// after the "connected" event so new clients can catch up on active state.
func (h *Hub) ServeHTTPWithInit(w http.ResponseWriter, r *http.Request, initEvents []Event) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := h.Subscribe()
	defer h.Unsubscribe(ch)

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {\"clients\":%d}\n\n", h.ClientCount())
	flusher.Flush()

	// Replay init events for catch-up
	for _, evt := range initEvents {
		data, err := json.Marshal(evt.Data)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
	}
	if len(initEvents) > 0 {
		flusher.Flush()
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			// Keep connection alive through proxies and detect stale clients.
			// Comments (: prefix) are consumed by EventSource but don't fire JS events,
			// so we send an actual event for client-side staleness detection.
			fmt.Fprintf(w, ": keepalive\nevent: heartbeat\ndata: {}\n\n")
			flusher.Flush()
		case evt := <-ch:
			data, err := json.Marshal(evt.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
			flusher.Flush()
		}
	}
}
