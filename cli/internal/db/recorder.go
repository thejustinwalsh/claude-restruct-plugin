package db

import (
	"log/slog"
	"time"
)

// Recorder writes refinement telemetry to SQLite.
// Used by the pipeline to record every step.
type Recorder struct {
	db *DB
}

// NewRecorder creates a Recorder backed by the given database.
func NewRecorder(database *DB) *Recorder {
	return &Recorder{db: database}
}

// RecordSession ensures a session record exists.
func (r *Recorder) RecordSession(sessionID, projectPath, transcriptPath string) {
	if sessionID == "" {
		return
	}
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

// RecordPipelineEvent writes a pipeline stage timing record.
func (r *Recorder) RecordPipelineEvent(refinementID int64, stage string, durationMs int64, success bool, metadata string) {
	if refinementID == 0 {
		return
	}
	err := r.db.InsertPipelineEvent(&PipelineEvent{
		RefinementID: refinementID,
		Stage:        stage,
		DurationMs:   durationMs,
		Success:      success,
		Metadata:     metadata,
	})
	if err != nil {
		slog.Warn("failed to record pipeline event", "error", err, "stage", stage)
	}
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
