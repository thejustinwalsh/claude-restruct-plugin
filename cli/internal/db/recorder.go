package db

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Recorder writes telemetry to SQLite and broadcasts events to the
// restruct server for real-time SSE delivery. Every Record* method
// writes to DB first, then broadcasts — callers never need to think
// about event delivery.
type Recorder struct {
	db        *DB
	serverURL string // empty = no broadcasting (tests, offline)
}

// NewRecorder creates a Recorder backed by the given database.
// If serverURL is non-empty, verification events are broadcast
// to the server for SSE delivery after each DB write.
func NewRecorder(database *DB, serverURL string) *Recorder {
	return &Recorder{db: database, serverURL: serverURL}
}

// RecordSession ensures a session record exists.
// Uses purgatory session if sessionID is empty.
func (r *Recorder) RecordSession(sessionID, projectPath, transcriptPath string) {
	sessionID = ResolveSessionID(sessionID)
	err := r.db.UpsertSession(&Session{
		ID:             sessionID,
		ProjectPath:    projectPath,
		TranscriptPath: transcriptPath,
		StartedAt:      time.Now().UTC(),
		Status:         "active",
	})
	if err != nil {
		slog.Warn("failed to record session", "error", err)
	}
}

// RecordRefinement writes a refinement record and returns the ID.
func (r *Recorder) RecordRefinement(ref *Refinement) int64 {
	id, err := r.db.InsertRefinement(ref)
	if err != nil {
		slog.Warn("failed to record refinement", "error", err)
		return 0
	}
	return id
}

// RecordPendingRefinement creates a refinement with status "pending" before the LLM call.
// Returns the refinement ID for use with streaming and later completion.
func (r *Recorder) RecordPendingRefinement(ref *Refinement) int64 {
	ref.Status = "pending"
	return r.RecordRefinement(ref)
}

// CompleteRefinement updates a pending refinement with the final results.
func (r *Recorder) CompleteRefinement(id int64, ref *Refinement) {
	if id == 0 {
		return
	}
	ref.Status = "complete"
	if err := r.db.UpdateRefinement(id, ref); err != nil {
		slog.Warn("failed to complete refinement", "error", err, "id", id)
	}
}

// RecordPipelineEvent writes a pipeline stage timing record.
func (r *Recorder) RecordPipelineEvent(refinementID int64, stage string, durationUs int64, success bool, metadata string) {
	if refinementID == 0 {
		return
	}
	err := r.db.InsertPipelineEvent(&PipelineEvent{
		RefinementID: refinementID,
		Stage:        stage,
		DurationUs:   durationUs,
		Success:      success,
		Metadata:     metadata,
	})
	if err != nil {
		slog.Warn("failed to record pipeline event", "error", err, "stage", stage)
	}
}

// RecordSnapshot records a snapshot verification event and broadcasts it.
func (r *Recorder) RecordSnapshot(sessionID string, refinementID int64, scope, hookEvent, cwdInput, projectDir string, fileCount int, durationUs int64) {
	if sessionID == "" {
		return
	}
	var refID *int64
	if refinementID > 0 {
		refID = &refinementID
	}
	e := &VerificationEvent{
		SessionID:    sessionID,
		RefinementID: refID,
		Scope:        scope,
		HookEvent:    hookEvent,
		EventType:    "snapshot",
		FileCount:    &fileCount,
		DurationUs:   &durationUs,
		CwdInput:     cwdInput,
		ProjectDir:   projectDir,
	}
	if err := r.db.InsertVerificationEvent(e); err != nil {
		slog.Warn("failed to record snapshot event", "error", err)
		return
	}
	r.broadcastVerification(e)
}

// RecordVerification records a verify event and broadcasts it.
func (r *Recorder) RecordVerification(sessionID string, refinementID int64, scope, hookEvent, cwdInput, projectDir, changedFilesJSON, checksRunJSON, result string, durationUs int64) {
	if sessionID == "" {
		return
	}
	var refID *int64
	if refinementID > 0 {
		refID = &refinementID
	}
	e := &VerificationEvent{
		SessionID:    sessionID,
		RefinementID: refID,
		Scope:        scope,
		HookEvent:    hookEvent,
		EventType:    "verify",
		DurationUs:   &durationUs,
		CwdInput:     cwdInput,
		ProjectDir:   projectDir,
		Result:       &result,
	}
	if changedFilesJSON != "" {
		e.ChangedFiles = &changedFilesJSON
	}
	if checksRunJSON != "" {
		e.ChecksRun = &checksRunJSON
	}
	if err := r.db.InsertVerificationEvent(e); err != nil {
		slog.Warn("failed to record verification event", "error", err)
		return
	}
	r.broadcastVerification(e)
}

// EnsureSession guarantees the session exists and is active.
// Call this from any hook handler that receives a session_id.
// Returns the resolved session ID (may be purgatory if input was empty).
func (r *Recorder) EnsureSession(sessionID, projectPath, transcriptPath string) string {
	return r.db.EnsureSession(sessionID, projectPath, transcriptPath)
}

// EndSession marks a session as ended.
func (r *Recorder) EndSession(sessionID string) {
	if sessionID == "" {
		return
	}
	if err := r.db.EndSession(sessionID); err != nil {
		slog.Warn("failed to end session", "error", err)
	}
}

// RecordToolDecision records a PreToolUse permission decision.
func (r *Recorder) RecordToolDecision(td *ToolDecision) int64 {
	id, err := r.db.InsertToolDecision(td)
	if err != nil {
		slog.Warn("failed to record tool decision", "error", err)
		return 0
	}
	return id
}

// UpdateToolOutcome updates a pending tool decision with the execution outcome.
func (r *Recorder) UpdateToolOutcome(toolUseID, outcome string, durationMs *int64) {
	if toolUseID == "" {
		return
	}
	if err := r.db.UpdateToolOutcome(toolUseID, outcome, durationMs); err != nil {
		slog.Warn("failed to update tool outcome", "error", err, "tool_use_id", toolUseID)
	}
}

// broadcastVerification POSTs a verification event to the server for SSE delivery.
// Best-effort: failures are logged, never block the caller.
func (r *Recorder) broadcastVerification(e *VerificationEvent) {
	if r.serverURL == "" {
		return
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Post(r.serverURL+"/api/verification", "application/json", bytes.NewReader(data))
	if err != nil {
		slog.Debug("broadcast verification: post error", "error", err)
		return
	}
	resp.Body.Close()
}
