package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/tjw/restruct/internal/db"
	"github.com/tjw/restruct/internal/server/sse"
)

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version":  s.version,
		"mode":     db.BuildMode,
		"db_path":  db.DefaultPath(),
		"plugin_id": db.PluginID(),
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	metrics, _ := s.db.GetMetrics()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "ok",
		"sse_clients": s.hub.ClientCount(),
		"metrics":     metrics,
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	metrics, err := s.db.GetMetrics()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	sessions, err := s.db.ListSessions(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sessions == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, err := s.db.GetSession(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handleSessionStats(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	metrics, err := s.db.GetSessionMetrics(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (s *Server) handleSessionRefinements(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	refs, err := s.db.GetRefinementsForSession(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if refs == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	writeJSON(w, http.StatusOK, refs)
}

func (s *Server) handleListRefinements(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	refs, err := s.db.ListRefinements(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if refs == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	writeJSON(w, http.StatusOK, refs)
}

func (s *Server) handleGetRefinement(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid refinement id")
		return
	}

	ref, err := s.db.GetRefinement(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ref == nil {
		writeError(w, http.StatusNotFound, "refinement not found")
		return
	}

	// Include pipeline events and verification events
	events, _ := s.db.GetPipelineEvents(id)
	verifications, _ := s.db.GetVerificationEventsForRefinement(id)
	if verifications == nil {
		verifications = []db.VerificationEvent{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"refinement":    ref,
		"events":        events,
		"verifications": verifications,
	})
}

func (s *Server) handleRefinementEvents(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid refinement id")
		return
	}

	events, err := s.db.GetPipelineEvents(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if events == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	writeJSON(w, http.StatusOK, events)
}

// --- Stats endpoints (for charts) ---

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	refinements, _ := s.db.GetRefinementStats(limit)
	pipeline, _ := s.db.GetPipelineBreakdown(limit)
	daily, _ := s.db.GetDailyCounts(30)
	sessions, _ := s.db.GetSessionStats(50)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"refinements": refinements,
		"pipeline":    pipeline,
		"daily":       daily,
		"sessions":    sessions,
	})
}

// --- Stream endpoints (CLI → Server → SSE clients) ---

func (s *Server) handleStreamActive(w http.ResponseWriter, r *http.Request) {
	active := s.streamBuf.Active()
	if active == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	writeJSON(w, http.StatusOK, active)
}

func (s *Server) handleStreamBuffer(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	stream, ok := s.streamBuf.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "no active stream for this refinement")
		return
	}
	writeJSON(w, http.StatusOK, stream)
}

func (s *Server) handleStreamInput(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		RefinementID int64  `json:"refinement_id"`
		InputPrompt  string `json:"input_prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.hub.Broadcast(sse.Event{Type: "refinement:input", Data: payload})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStreamComplete(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		RefinementID  int64                    `json:"refinement_id"`
		RefinedPrompt string                   `json:"refined_prompt"`
		LLMOutput     string                   `json:"llm_output"`
		LatencyMs     int64                    `json:"latency_ms"`
		Timings       []map[string]interface{} `json:"timings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.hub.Broadcast(sse.Event{Type: "refinement:complete", Data: payload})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStreamStart(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		RefinementID int64  `json:"refinement_id"`
		SessionID    string `json:"session_id"`
		RawPrompt    string `json:"raw_prompt"`
		Model        string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.streamBuf.Start(payload.RefinementID, payload.SessionID, payload.RawPrompt, payload.Model)
	s.hub.Broadcast(sse.Event{Type: "refinement:stream-start", Data: payload})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStreamToken(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		RefinementID int64  `json:"refinement_id"`
		Tokens       string `json:"tokens"`
		SeqStart     int    `json:"seq_start"`
		SeqEnd       int    `json:"seq_end"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.streamBuf.Append(payload.RefinementID, payload.Tokens, payload.SeqEnd)
	s.hub.Broadcast(sse.Event{Type: "refinement:streaming", Data: payload})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStreamDone(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		RefinementID int64 `json:"refinement_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.streamBuf.End(payload.RefinementID)
	s.hub.Broadcast(sse.Event{Type: "refinement:stream-end", Data: payload})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStreamError(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		RefinementID int64  `json:"refinement_id"`
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.streamBuf.SetError(payload.RefinementID, payload.Error)
	s.hub.Broadcast(sse.Event{Type: "refinement:stream-error", Data: payload})
	w.WriteHeader(http.StatusNoContent)
}

// --- Tool decision endpoints ---

func (s *Server) handleListToolDecisions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	decisions, err := s.db.ListToolDecisions(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if decisions == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	writeJSON(w, http.StatusOK, decisions)
}

func (s *Server) handleToolDecisionStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.GetToolDecisionStats("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleSessionToolDecisions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	decisions, err := s.db.ListToolDecisionsBySession(id, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if decisions == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	writeJSON(w, http.StatusOK, decisions)
}

func (s *Server) handleSessionVerifications(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 500 {
		limit = 200
	}

	events, err := s.db.GetVerificationEventsForSession(id, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if events == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleSessionTimeline(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 1000 {
		limit = 200
	}

	events, err := s.db.GetTimelineEvents(id, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if events == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleVerificationEvent(w http.ResponseWriter, r *http.Request) {
	var payload db.VerificationEvent
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.hub.Broadcast(sse.Event{Type: "verification:new", Data: payload})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleToolDecisionEvent(w http.ResponseWriter, r *http.Request) {
	var payload db.ToolDecision
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.hub.Broadcast(sse.Event{Type: "tool-decision:new", Data: payload})
	w.WriteHeader(http.StatusNoContent)
}
