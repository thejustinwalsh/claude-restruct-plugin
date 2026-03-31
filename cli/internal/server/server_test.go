package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tjw/restruct/internal/db"
)

func testServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	s := New(d, "0", true, nil, "test")
	return s, d
}

func doReq(t *testing.T, s *Server, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	return w
}

func TestHealthEndpoint(t *testing.T) {
	s, _ := testServer(t)
	w := doReq(t, s, "GET", "/api/health", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %v, want ok", resp["status"])
	}
}

func TestMetricsEndpoint_Empty(t *testing.T) {
	s, _ := testServer(t)
	w := doReq(t, s, "GET", "/api/metrics", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var m db.Metrics
	json.NewDecoder(w.Body).Decode(&m)
	if m.TotalRefinements != 0 {
		t.Errorf("total_refinements = %d, want 0", m.TotalRefinements)
	}
}

func TestSessionsEndpoint(t *testing.T) {
	s, d := testServer(t)

	d.UpsertSession(&db.Session{
		ID: "sess-1", ProjectPath: "/tmp", StartedAt: timeNow(), Status: "active",
	})

	w := doReq(t, s, "GET", "/api/sessions", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var sessions []db.Session
	json.NewDecoder(w.Body).Decode(&sessions)
	if len(sessions) != 1 {
		t.Errorf("got %d sessions, want 1", len(sessions))
	}
}

func TestGetSessionEndpoint(t *testing.T) {
	s, d := testServer(t)

	d.UpsertSession(&db.Session{
		ID: "sess-get", ProjectPath: "/project", StartedAt: timeNow(), Status: "active",
	})

	w := doReq(t, s, "GET", "/api/sessions/sess-get", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var sess db.Session
	json.NewDecoder(w.Body).Decode(&sess)
	if sess.ID != "sess-get" {
		t.Errorf("id = %q", sess.ID)
	}
}

func TestGetSession_NotFound(t *testing.T) {
	s, _ := testServer(t)
	w := doReq(t, s, "GET", "/api/sessions/nonexistent", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestRefinementsEndpoint(t *testing.T) {
	s, d := testServer(t)

	d.UpsertSession(&db.Session{
		ID: "sess-ref", ProjectPath: "/tmp", StartedAt: timeNow(), Status: "active",
	})
	d.InsertRefinement(&db.Refinement{
		SessionID: "sess-ref", ProjectPath: "/tmp", RawPrompt: "test", Status: "complete",
	})

	w := doReq(t, s, "GET", "/api/refinements", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var refs []db.Refinement
	json.NewDecoder(w.Body).Decode(&refs)
	if len(refs) != 1 {
		t.Errorf("got %d refinements, want 1", len(refs))
	}
}

func TestGetRefinementEndpoint(t *testing.T) {
	s, d := testServer(t)

	d.UpsertSession(&db.Session{
		ID: "sess-gref", ProjectPath: "/tmp", StartedAt: timeNow(), Status: "active",
	})
	id, _ := d.InsertRefinement(&db.Refinement{
		SessionID: "sess-gref", ProjectPath: "/tmp", RawPrompt: "test prompt", Status: "complete",
	})

	w := doReq(t, s, "GET", "/api/refinements/"+itoa(id), "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var resp map[string]json.RawMessage
	json.NewDecoder(w.Body).Decode(&resp)
	if _, ok := resp["refinement"]; !ok {
		t.Error("response missing 'refinement' key")
	}
	if _, ok := resp["events"]; !ok {
		t.Error("response missing 'events' key")
	}
}

func TestGetRefinement_NotFound(t *testing.T) {
	s, _ := testServer(t)
	w := doReq(t, s, "GET", "/api/refinements/99999", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestGetRefinement_InvalidID(t *testing.T) {
	s, _ := testServer(t)
	w := doReq(t, s, "GET", "/api/refinements/abc", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSessionRefinementsEndpoint(t *testing.T) {
	s, d := testServer(t)

	d.UpsertSession(&db.Session{
		ID: "sess-sr", ProjectPath: "/tmp", StartedAt: timeNow(), Status: "active",
	})
	d.InsertRefinement(&db.Refinement{SessionID: "sess-sr", ProjectPath: "/tmp", RawPrompt: "p1", Status: "complete"})
	d.InsertRefinement(&db.Refinement{SessionID: "sess-sr", ProjectPath: "/tmp", RawPrompt: "p2", Status: "complete"})

	w := doReq(t, s, "GET", "/api/sessions/sess-sr/refinements", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var refs []db.Refinement
	json.NewDecoder(w.Body).Decode(&refs)
	if len(refs) != 2 {
		t.Errorf("got %d, want 2", len(refs))
	}
}

func TestRefinementEventsEndpoint(t *testing.T) {
	s, d := testServer(t)

	d.UpsertSession(&db.Session{ID: "sess-ev", ProjectPath: "/tmp", StartedAt: timeNow(), Status: "active"})
	id, _ := d.InsertRefinement(&db.Refinement{
		SessionID: "sess-ev", ProjectPath: "/tmp", RawPrompt: "test", Status: "complete",
	})
	d.InsertPipelineEvent(&db.PipelineEvent{
		RefinementID: id, Stage: "rules_load", DurationUs: 50000, Success: true,
	})

	w := doReq(t, s, "GET", "/api/refinements/"+itoa(id)+"/events", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var events []db.PipelineEvent
	json.NewDecoder(w.Body).Decode(&events)
	if len(events) != 1 {
		t.Errorf("got %d events, want 1", len(events))
	}
}

func TestStatsEndpoint(t *testing.T) {
	s, _ := testServer(t)
	w := doReq(t, s, "GET", "/api/stats", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var resp map[string]json.RawMessage
	json.NewDecoder(w.Body).Decode(&resp)
	for _, key := range []string{"refinements", "pipeline", "daily", "sessions"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("response missing %q key", key)
		}
	}
}

// --- Stream endpoints ---

func TestStreamStart(t *testing.T) {
	s, _ := testServer(t)
	w := doReq(t, s, "POST", "/api/stream/start",
		`{"refinement_id":1,"session_id":"s1","raw_prompt":"test","model":"qwen"}`)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

func TestStreamToken(t *testing.T) {
	s, _ := testServer(t)
	w := doReq(t, s, "POST", "/api/stream/token",
		`{"refinement_id":1,"tokens":"hello","seq_start":0,"seq_end":1}`)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

func TestStreamDone(t *testing.T) {
	s, _ := testServer(t)
	w := doReq(t, s, "POST", "/api/stream/done", `{"refinement_id":1}`)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

func TestStreamError(t *testing.T) {
	s, _ := testServer(t)
	w := doReq(t, s, "POST", "/api/stream/error",
		`{"refinement_id":1,"error":"timeout"}`)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

func TestStreamStart_InvalidJSON(t *testing.T) {
	s, _ := testServer(t)
	w := doReq(t, s, "POST", "/api/stream/start", `{invalid`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// --- Empty list endpoints return [] not null ---

func TestEmptyListsReturnArray(t *testing.T) {
	s, _ := testServer(t)

	for _, path := range []string{
		"/api/sessions",
		"/api/refinements",
		"/api/sessions/nonexistent/refinements",
		"/api/refinements/99999/events",
	} {
		w := doReq(t, s, "GET", path, "")
		// Sessions/refinements for nonexistent session should be 200 with []
		if w.Code == http.StatusOK {
			body := strings.TrimSpace(w.Body.String())
			if !strings.HasPrefix(body, "[") {
				t.Errorf("%s: expected array, got %q", path, body)
			}
		}
	}
}
