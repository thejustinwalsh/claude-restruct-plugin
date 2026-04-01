package db

import (
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps a SQLite database with WAL mode for concurrent CLI + server access.
type DB struct {
	pool *sql.DB
	path string
}

// Open opens (or creates) the SQLite database at the given path.
// Enables WAL mode and runs migrations.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	pool, err := sql.Open("sqlite", path+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Verify connection
	if err := pool.Ping(); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	d := &DB{pool: pool, path: path}
	if err := d.migrate(); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return d, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.pool.Close()
}

// Pool returns the underlying sql.DB for direct queries.
func (d *DB) Pool() *sql.DB {
	return d.pool
}

func (d *DB) migrate() error {
	// Create migrations tracking table
	_, err := d.pool.Exec(`CREATE TABLE IF NOT EXISTS _migrations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		filename TEXT NOT NULL UNIQUE,
		applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		// Check if already applied
		var count int
		d.pool.QueryRow("SELECT COUNT(*) FROM _migrations WHERE filename = ?", name).Scan(&count)
		if count > 0 {
			continue
		}

		content, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		if _, err := d.pool.Exec(string(content)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}

		if _, err := d.pool.Exec("INSERT INTO _migrations (filename) VALUES (?)", name); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}

	return nil
}

// DataDir returns the plugin data directory.
// Uses CLAUDE_PLUGIN_DATA when set (production), otherwise computes the
// same path Claude Code would resolve to, so dev and prod share one dir.
// pluginID is defined in pluginid_debug.go / pluginid_release.go via build tags.
func DataDir() string {
	dir := os.Getenv("CLAUDE_PLUGIN_DATA")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".claude", "plugins", "data", pluginID)
	}
	return dir
}

// PluginID returns the plugin identifier used for data directory resolution.
func PluginID() string {
	return pluginID
}

// DefaultPath returns the database path.
func DefaultPath() string {
	return filepath.Join(DataDir(), "restruct.db")
}

// --- Data types ---

type Session struct {
	ID             string     `json:"id"`
	ProjectPath    string     `json:"project_path"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	TranscriptPath string     `json:"transcript_path,omitempty"`
	Status         string     `json:"status"`
}

type Refinement struct {
	ID            int64     `json:"id"`
	SessionID     string    `json:"session_id,omitempty"`
	ProjectPath   string    `json:"project_path"`
	RawPrompt     string    `json:"raw_prompt"`
	RefinedPrompt *string   `json:"refined_prompt,omitempty"`
	InputPrompt   *string   `json:"input_prompt,omitempty"`
	LLMOutput     *string   `json:"llm_output,omitempty"`
	Model         string    `json:"model,omitempty"`
	Temperature   float64   `json:"temperature,omitempty"`
	LatencyMs     int64     `json:"latency_ms"`
	CacheHit      bool      `json:"cache_hit"`
	Passthrough   bool      `json:"passthrough"`
	OutputValid   *bool     `json:"output_valid,omitempty"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

// PipelineEvent stores stage timing data. The duration_ms column in SQLite
// now stores microseconds (renamed from milliseconds for higher resolution).
// The JSON field is duration_us for clarity.
type PipelineEvent struct {
	ID           int64     `json:"id"`
	RefinementID int64     `json:"refinement_id"`
	Stage        string    `json:"stage"`
	DurationUs   int64     `json:"duration_us"`
	Success      bool      `json:"success"`
	Metadata     string    `json:"metadata,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// --- Session operations ---

func (d *DB) UpsertSession(s *Session) error {
	_, err := d.pool.Exec(`
		INSERT INTO sessions (id, project_path, started_at, transcript_path, status)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			ended_at = NULL,
			status = 'active',
			transcript_path = COALESCE(excluded.transcript_path, sessions.transcript_path)`,
		s.ID, s.ProjectPath, s.StartedAt, s.TranscriptPath, s.Status,
	)
	return err
}

// PurgatorySessionID is used when no session_id is provided by Claude Code.
// Data still flows somewhere rather than being silently dropped. Hooks that
// receive a real session_id later will migrate data via standard upsert.
const PurgatorySessionID = "purgatory"

// ResolveSessionID returns the provided session ID, or the purgatory ID if empty.
// Call this in every hook handler to guarantee data is never silently dropped.
func ResolveSessionID(sessionID string) string {
	if sessionID == "" {
		slog.Warn("session_id missing from hook input, using purgatory session")
		return PurgatorySessionID
	}
	return sessionID
}

// EnsureSession guarantees a session row exists and is active.
// Any hook that receives a session_id calls this to auto-heal
// sessions that were ended or never recorded (e.g., resumed sessions).
// If sessionID is empty, uses PurgatorySessionID.
func (d *DB) EnsureSession(sessionID, projectPath, transcriptPath string) string {
	sessionID = ResolveSessionID(sessionID)
	_, err := d.pool.Exec(`
		INSERT INTO sessions (id, project_path, started_at, status, transcript_path)
		VALUES (?, ?, ?, 'active', ?)
		ON CONFLICT(id) DO UPDATE SET
			ended_at = NULL,
			status = 'active',
			project_path = COALESCE(NULLIF(excluded.project_path, ''), sessions.project_path),
			transcript_path = COALESCE(NULLIF(excluded.transcript_path, ''), sessions.transcript_path)`,
		sessionID, projectPath, time.Now().UTC(), transcriptPath,
	)
	if err != nil {
		slog.Warn("ensure session failed", "error", err, "session_id", sessionID)
	}
	return sessionID
}

func (d *DB) EndSession(id string) error {
	_, err := d.pool.Exec("UPDATE sessions SET ended_at = ?, status = 'ended' WHERE id = ?", time.Now().UTC(), id)
	return err
}

func (d *DB) ListSessions(limit, offset int) ([]Session, error) {
	rows, err := d.pool.Query(`
		SELECT id, project_path, started_at, ended_at, transcript_path, status
		FROM sessions ORDER BY started_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.ProjectPath, &s.StartedAt, &s.EndedAt, &s.TranscriptPath, &s.Status); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func (d *DB) GetSession(id string) (*Session, error) {
	var s Session
	err := d.pool.QueryRow(`
		SELECT id, project_path, started_at, ended_at, transcript_path, status
		FROM sessions WHERE id = ?`, id).Scan(&s.ID, &s.ProjectPath, &s.StartedAt, &s.EndedAt, &s.TranscriptPath, &s.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &s, err
}

// --- Refinement operations ---

func (d *DB) InsertRefinement(r *Refinement) (int64, error) {
	status := r.Status
	if status == "" {
		status = "complete"
	}
	res, err := d.pool.Exec(`
		INSERT INTO refinements (session_id, project_path, raw_prompt, refined_prompt, input_prompt, llm_output, model, temperature, latency_ms, cache_hit, passthrough, output_valid, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.SessionID, r.ProjectPath, r.RawPrompt, r.RefinedPrompt, r.InputPrompt, r.LLMOutput, r.Model, r.Temperature, r.LatencyMs, r.CacheHit, r.Passthrough, r.OutputValid, status,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateRefinement(id int64, r *Refinement) error {
	status := r.Status
	if status == "" {
		status = "complete"
	}
	_, err := d.pool.Exec(`
		UPDATE refinements SET
			refined_prompt = ?, input_prompt = COALESCE(?, input_prompt), llm_output = COALESCE(?, llm_output),
			model = ?, temperature = ?, latency_ms = ?,
			cache_hit = ?, passthrough = ?, output_valid = ?, status = ?
		WHERE id = ?`,
		r.RefinedPrompt, r.InputPrompt, r.LLMOutput, r.Model, r.Temperature, r.LatencyMs,
		r.CacheHit, r.Passthrough, r.OutputValid, status, id,
	)
	return err
}

func (d *DB) ListRefinements(limit, offset int) ([]Refinement, error) {
	rows, err := d.pool.Query(`
		SELECT id, session_id, project_path, raw_prompt, refined_prompt, input_prompt, llm_output, model, temperature, latency_ms, cache_hit, passthrough, output_valid, status, created_at
		FROM refinements ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []Refinement
	for rows.Next() {
		var r Refinement
		if err := rows.Scan(&r.ID, &r.SessionID, &r.ProjectPath, &r.RawPrompt, &r.RefinedPrompt, &r.InputPrompt, &r.LLMOutput, &r.Model, &r.Temperature, &r.LatencyMs, &r.CacheHit, &r.Passthrough, &r.OutputValid, &r.Status, &r.CreatedAt); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

func (d *DB) GetRefinement(id int64) (*Refinement, error) {
	var r Refinement
	err := d.pool.QueryRow(`
		SELECT id, session_id, project_path, raw_prompt, refined_prompt, input_prompt, llm_output, model, temperature, latency_ms, cache_hit, passthrough, output_valid, status, created_at
		FROM refinements WHERE id = ?`, id).Scan(&r.ID, &r.SessionID, &r.ProjectPath, &r.RawPrompt, &r.RefinedPrompt, &r.InputPrompt, &r.LLMOutput, &r.Model, &r.Temperature, &r.LatencyMs, &r.CacheHit, &r.Passthrough, &r.OutputValid, &r.Status, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &r, err
}

func (d *DB) GetRefinementsSince(lastID int64, limit int) ([]Refinement, error) {
	rows, err := d.pool.Query(`
		SELECT id, session_id, project_path, raw_prompt, refined_prompt, input_prompt, llm_output, model, temperature, latency_ms, cache_hit, passthrough, output_valid, status, created_at
		FROM refinements WHERE id > ? AND status IN ('complete', 'failed') ORDER BY id ASC LIMIT ?`, lastID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []Refinement
	for rows.Next() {
		var r Refinement
		if err := rows.Scan(&r.ID, &r.SessionID, &r.ProjectPath, &r.RawPrompt, &r.RefinedPrompt, &r.InputPrompt, &r.LLMOutput, &r.Model, &r.Temperature, &r.LatencyMs, &r.CacheHit, &r.Passthrough, &r.OutputValid, &r.Status, &r.CreatedAt); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

// FailStalePending marks pending refinements older than maxAge as failed.
// This cleans up refinements where the CLI crashed before completing them.
func (d *DB) FailStalePending(maxAge time.Duration) (int64, error) {
	// Use the same format as SQLite's CURRENT_TIMESTAMP (no T, no Z)
	cutoff := time.Now().Add(-maxAge).UTC().Format("2006-01-02 15:04:05")
	result, err := d.pool.Exec(
		`UPDATE refinements SET status = 'failed' WHERE status = 'pending' AND created_at < ?`,
		cutoff,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (d *DB) GetRefinementsForSession(sessionID string) ([]Refinement, error) {
	rows, err := d.pool.Query(`
		SELECT id, session_id, project_path, raw_prompt, refined_prompt, input_prompt, llm_output, model, temperature, latency_ms, cache_hit, passthrough, output_valid, status, created_at
		FROM refinements WHERE session_id = ? ORDER BY created_at ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []Refinement
	for rows.Next() {
		var r Refinement
		if err := rows.Scan(&r.ID, &r.SessionID, &r.ProjectPath, &r.RawPrompt, &r.RefinedPrompt, &r.InputPrompt, &r.LLMOutput, &r.Model, &r.Temperature, &r.LatencyMs, &r.CacheHit, &r.Passthrough, &r.OutputValid, &r.Status, &r.CreatedAt); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

// --- Session context for pipeline ---

// SessionClip is a compact summary of a recent refinement for feeding back
// into the local LLM as conversation context.
type SessionClip struct {
	Intent    string `json:"intent"`
	RawPrompt string `json:"raw_prompt"`
	AgoSec    int64  `json:"ago_sec"`
}

// GetRecentIntents returns the last N completed refinements for a session,
// ordered newest-first. Only returns refinements that have a refined_prompt.
func (d *DB) GetRecentIntents(sessionID string, limit int) ([]SessionClip, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := d.pool.Query(`
		SELECT raw_prompt, refined_prompt,
		       CAST((JULIANDAY('now') - JULIANDAY(created_at)) * 86400 AS INTEGER) as ago_sec
		FROM refinements
		WHERE session_id = ? AND status = 'complete' AND refined_prompt IS NOT NULL AND passthrough = FALSE
		ORDER BY created_at DESC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clips []SessionClip
	for rows.Next() {
		var raw, refined string
		var ago int64
		if err := rows.Scan(&raw, &refined, &ago); err != nil {
			return nil, err
		}
		clips = append(clips, SessionClip{
			Intent:    extractIntent(refined),
			RawPrompt: raw,
			AgoSec:    ago,
		})
	}
	return clips, rows.Err()
}

// extractIntent pulls the text from <intent>...</intent> in a refined prompt.
// Returns the raw prompt's first 100 chars as fallback if no intent tag found.
func extractIntent(refined string) string {
	const open = "<intent>"
	const close = "</intent>"
	start := strings.Index(refined, open)
	if start == -1 {
		return ""
	}
	start += len(open)
	end := strings.Index(refined[start:], close)
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(refined[start : start+end])
}

// --- Pipeline event operations ---

func (d *DB) InsertPipelineEvent(e *PipelineEvent) error {
	_, err := d.pool.Exec(`
		INSERT INTO pipeline_events (refinement_id, stage, duration_ms, success, metadata)
		VALUES (?, ?, ?, ?, ?)`,
		e.RefinementID, e.Stage, e.DurationUs, e.Success, e.Metadata,
	)
	return err
}

func (d *DB) GetPipelineEvents(refinementID int64) ([]PipelineEvent, error) {
	rows, err := d.pool.Query(`
		SELECT id, refinement_id, stage, duration_ms, success, metadata, created_at
		FROM pipeline_events WHERE refinement_id = ? ORDER BY id ASC`, refinementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []PipelineEvent
	for rows.Next() {
		var e PipelineEvent
		if err := rows.Scan(&e.ID, &e.RefinementID, &e.Stage, &e.DurationUs, &e.Success, &e.Metadata, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// --- Metrics ---

type Metrics struct {
	TotalSessions    int     `json:"total_sessions"`
	ActiveSessions   int     `json:"active_sessions"`
	TotalRefinements int     `json:"total_refinements"`
	CacheHits        int     `json:"cache_hits"`
	CacheHitRate     float64 `json:"cache_hit_rate"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
	Passthroughs     int     `json:"passthroughs"`
}

// --- Stats (for charts) ---

type RefinementStat struct {
	ID          int64   `json:"id"`
	CreatedAt   string  `json:"created_at"`
	LatencyMs   int64   `json:"latency_ms"`
	CacheHit    bool    `json:"cache_hit"`
	Passthrough bool    `json:"passthrough"`
	PromptWords int     `json:"prompt_words"`
	Model      string  `json:"model"`
}

type PipelineBreakdown struct {
	RefinementID int64  `json:"refinement_id"`
	CreatedAt    string `json:"created_at"`
	Stage        string `json:"stage"`
	DurationUs   int64  `json:"duration_us"`
}

type DailyCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type SessionStat struct {
	ID              string  `json:"id"`
	DurationMinutes float64 `json:"duration_minutes"`
	Refinements     int     `json:"refinements"`
}

func (d *DB) GetRefinementStats(limit int) ([]RefinementStat, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.pool.Query(`
		SELECT id, created_at, latency_ms, cache_hit, passthrough, model,
		       length(raw_prompt) - length(replace(raw_prompt, ' ', '')) + 1
		FROM refinements WHERE status = 'complete'
		ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var stats []RefinementStat
	for rows.Next() {
		var s RefinementStat
		if err := rows.Scan(&s.ID, &s.CreatedAt, &s.LatencyMs, &s.CacheHit, &s.Passthrough, &s.Model, &s.PromptWords); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

func (d *DB) GetPipelineBreakdown(limit int) ([]PipelineBreakdown, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.pool.Query(`
		SELECT pe.refinement_id, r.created_at, pe.stage, pe.duration_ms
		FROM pipeline_events pe
		JOIN refinements r ON r.id = pe.refinement_id
		WHERE r.status = 'complete'
		ORDER BY r.created_at DESC, pe.id ASC
		LIMIT ?`, limit*10)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var breakdown []PipelineBreakdown
	for rows.Next() {
		var b PipelineBreakdown
		if err := rows.Scan(&b.RefinementID, &b.CreatedAt, &b.Stage, &b.DurationUs); err != nil {
			return nil, err
		}
		breakdown = append(breakdown, b)
	}
	return breakdown, rows.Err()
}

func (d *DB) GetDailyCounts(days int) ([]DailyCount, error) {
	if days <= 0 {
		days = 30
	}
	rows, err := d.pool.Query(`
		SELECT DATE(created_at) as date, COUNT(*) as count
		FROM refinements WHERE status = 'complete'
		GROUP BY DATE(created_at)
		ORDER BY date DESC LIMIT ?`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var counts []DailyCount
	for rows.Next() {
		var c DailyCount
		if err := rows.Scan(&c.Date, &c.Count); err != nil {
			return nil, err
		}
		counts = append(counts, c)
	}
	return counts, rows.Err()
}

func (d *DB) GetSessionStats(limit int) ([]SessionStat, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.pool.Query(`
		SELECT s.id,
			COALESCE((JULIANDAY(s.ended_at) - JULIANDAY(s.started_at)) * 24 * 60, 0) as duration_minutes,
			COUNT(r.id) as refinements
		FROM sessions s
		LEFT JOIN refinements r ON r.session_id = s.id AND r.status = 'complete'
		WHERE s.ended_at IS NOT NULL
		GROUP BY s.id
		ORDER BY s.started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var stats []SessionStat
	for rows.Next() {
		var s SessionStat
		if err := rows.Scan(&s.ID, &s.DurationMinutes, &s.Refinements); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// --- Session metrics ---

type SessionMetrics struct {
	TotalRefinements      int     `json:"total_refinements"`
	Passthroughs          int     `json:"passthroughs"`
	CacheHits             int     `json:"cache_hits"`
	AvgLatencyMs          float64 `json:"avg_latency_ms"`
	TotalVerifications    int     `json:"total_verifications"`
	VerificationPasses    int     `json:"verification_passes"`
	VerificationFailures  int     `json:"verification_failures"`
	DurationMinutes       float64 `json:"duration_minutes"`
}

func (d *DB) GetSessionMetrics(sessionID string) (*SessionMetrics, error) {
	m := &SessionMetrics{}

	d.pool.QueryRow(`SELECT COUNT(*) FROM refinements WHERE session_id = ?`, sessionID).Scan(&m.TotalRefinements)
	d.pool.QueryRow(`SELECT COUNT(*) FROM refinements WHERE session_id = ? AND passthrough = TRUE`, sessionID).Scan(&m.Passthroughs)
	d.pool.QueryRow(`SELECT COUNT(*) FROM refinements WHERE session_id = ? AND cache_hit = TRUE`, sessionID).Scan(&m.CacheHits)
	d.pool.QueryRow(`SELECT COALESCE(AVG(latency_ms), 0) FROM refinements WHERE session_id = ? AND latency_ms > 0`, sessionID).Scan(&m.AvgLatencyMs)

	d.pool.QueryRow(`SELECT COUNT(*) FROM verification_events WHERE session_id = ? AND event_type = 'verify'`, sessionID).Scan(&m.TotalVerifications)
	d.pool.QueryRow(`SELECT COUNT(*) FROM verification_events WHERE session_id = ? AND event_type = 'verify' AND result = 'pass'`, sessionID).Scan(&m.VerificationPasses)
	d.pool.QueryRow(`SELECT COUNT(*) FROM verification_events WHERE session_id = ? AND event_type = 'verify' AND result = 'fail'`, sessionID).Scan(&m.VerificationFailures)

	d.pool.QueryRow(`
		SELECT COALESCE((JULIANDAY(COALESCE(ended_at, CURRENT_TIMESTAMP)) - JULIANDAY(started_at)) * 24 * 60, 0)
		FROM sessions WHERE id = ?`, sessionID).Scan(&m.DurationMinutes)

	return m, nil
}

// --- Verification event operations ---

type VerificationEvent struct {
	ID           int64     `json:"id"`
	SessionID    string    `json:"session_id"`
	RefinementID *int64    `json:"refinement_id,omitempty"`
	Scope        string    `json:"scope"`
	HookEvent    string    `json:"hook_event"`
	EventType    string    `json:"event_type"`
	FileCount    *int      `json:"file_count,omitempty"`
	DurationUs   *int64    `json:"duration_us,omitempty"`
	CwdInput     string    `json:"cwd_input"`
	ProjectDir   string    `json:"project_dir"`
	ChangedFiles *string   `json:"changed_files,omitempty"`
	ChecksRun    *string   `json:"checks_run,omitempty"`
	Result       *string   `json:"result,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func (d *DB) InsertVerificationEvent(e *VerificationEvent) error {
	res, err := d.pool.Exec(`
		INSERT INTO verification_events (session_id, refinement_id, scope, hook_event, event_type, file_count, duration_us, cwd_input, project_dir, changed_files, checks_run, result)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.SessionID, e.RefinementID, e.Scope, e.HookEvent, e.EventType, e.FileCount, e.DurationUs,
		e.CwdInput, e.ProjectDir, e.ChangedFiles, e.ChecksRun, e.Result,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	e.ID = id
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	return nil
}

// LatestRefinementID returns the most recent refinement ID for a session.
// Returns 0 if no refinements exist. Used to link verification events
// to the refinement that triggered them.
func (d *DB) LatestRefinementID(sessionID string) int64 {
	var id int64
	d.pool.QueryRow(
		"SELECT COALESCE(MAX(id), 0) FROM refinements WHERE session_id = ?",
		sessionID,
	).Scan(&id)
	return id
}

// GetVerificationEventsForSession returns all verification events for a session.
func (d *DB) GetVerificationEventsForSession(sessionID string, limit, offset int) ([]VerificationEvent, error) {
	rows, err := d.pool.Query(`
		SELECT id, session_id, refinement_id, scope, hook_event, event_type, file_count, duration_us, cwd_input, project_dir, changed_files, checks_run, result, created_at
		FROM verification_events WHERE session_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`, sessionID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []VerificationEvent
	for rows.Next() {
		var e VerificationEvent
		if err := rows.Scan(&e.ID, &e.SessionID, &e.RefinementID, &e.Scope, &e.HookEvent, &e.EventType,
			&e.FileCount, &e.DurationUs, &e.CwdInput, &e.ProjectDir,
			&e.ChangedFiles, &e.ChecksRun, &e.Result, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// GetVerificationEventsForRefinement returns verification events linked to
// a specific refinement. This scopes events to the prompt cycle that
// produced this refinement (UserPromptSubmit → Stop).
func (d *DB) GetVerificationEventsForRefinement(refinementID int64) ([]VerificationEvent, error) {
	rows, err := d.pool.Query(`
		SELECT id, session_id, refinement_id, scope, hook_event, event_type, file_count, duration_us, cwd_input, project_dir, changed_files, checks_run, result, created_at
		FROM verification_events WHERE refinement_id = ? ORDER BY created_at ASC`, refinementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []VerificationEvent
	for rows.Next() {
		var e VerificationEvent
		if err := rows.Scan(&e.ID, &e.SessionID, &e.RefinementID, &e.Scope, &e.HookEvent, &e.EventType,
			&e.FileCount, &e.DurationUs, &e.CwdInput, &e.ProjectDir,
			&e.ChangedFiles, &e.ChecksRun, &e.Result, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (d *DB) GetMetrics() (*Metrics, error) {
	m := &Metrics{}
	d.pool.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&m.TotalSessions)
	d.pool.QueryRow("SELECT COUNT(*) FROM sessions WHERE status = 'active'").Scan(&m.ActiveSessions)
	d.pool.QueryRow("SELECT COUNT(*) FROM refinements").Scan(&m.TotalRefinements)
	d.pool.QueryRow("SELECT COUNT(*) FROM refinements WHERE cache_hit = TRUE").Scan(&m.CacheHits)
	d.pool.QueryRow("SELECT COUNT(*) FROM refinements WHERE passthrough = TRUE").Scan(&m.Passthroughs)
	d.pool.QueryRow("SELECT COALESCE(AVG(latency_ms), 0) FROM refinements WHERE latency_ms > 0").Scan(&m.AvgLatencyMs)
	if m.TotalRefinements > 0 {
		m.CacheHitRate = float64(m.CacheHits) / float64(m.TotalRefinements)
	}
	return m, nil
}

// --- Tool decision operations ---

// ToolDecision records a tool permission decision and its outcome.
type ToolDecision struct {
	ID               int64      `json:"id"`
	SessionID        string     `json:"session_id"`
	ProjectPath      string     `json:"project_path"`
	ToolName         string     `json:"tool_name"`
	ToolInputSummary string     `json:"tool_input_summary,omitempty"`
	ToolUseID        string     `json:"tool_use_id,omitempty"`
	HookDecision     *string    `json:"hook_decision,omitempty"`
	HookTier         *int       `json:"hook_tier,omitempty"`
	HookReason       *string    `json:"hook_reason,omitempty"`
	HookDurationUs   *int64     `json:"hook_duration_us,omitempty"`
	Outcome          string     `json:"outcome"`
	ToolDurationMs   *int64     `json:"tool_duration_ms,omitempty"`
	Reviewed         bool       `json:"reviewed"`
	ReviewedAt       *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

// InsertToolDecision records a PreToolUse hook decision. Outcome starts as "pending".
func (d *DB) InsertToolDecision(td *ToolDecision) (int64, error) {
	res, err := d.pool.Exec(`
		INSERT INTO tool_decisions (session_id, project_path, tool_name, tool_input_summary, tool_use_id, hook_decision, hook_tier, hook_reason, hook_duration_us, outcome)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		td.SessionID, td.ProjectPath, td.ToolName, td.ToolInputSummary, td.ToolUseID,
		td.HookDecision, td.HookTier, td.HookReason, td.HookDurationUs, "pending",
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateToolOutcome sets the outcome for a tool decision identified by tool_use_id.
func (d *DB) UpdateToolOutcome(toolUseID, outcome string, durationMs *int64) error {
	_, err := d.pool.Exec(`
		UPDATE tool_decisions SET outcome = ?, tool_duration_ms = ?
		WHERE tool_use_id = ? AND outcome = 'pending'`,
		outcome, durationMs, toolUseID,
	)
	return err
}

// GetToolDecisionByUseID returns a single tool decision by its tool_use_id.
func (d *DB) GetToolDecisionByUseID(toolUseID string) (*ToolDecision, error) {
	var td ToolDecision
	err := d.pool.QueryRow(`
		SELECT id, session_id, project_path, tool_name, tool_input_summary, tool_use_id,
		       hook_decision, hook_tier, hook_reason, hook_duration_us,
		       outcome, tool_duration_ms, reviewed, reviewed_at, created_at
		FROM tool_decisions WHERE tool_use_id = ?`, toolUseID,
	).Scan(&td.ID, &td.SessionID, &td.ProjectPath, &td.ToolName, &td.ToolInputSummary, &td.ToolUseID,
		&td.HookDecision, &td.HookTier, &td.HookReason, &td.HookDurationUs,
		&td.Outcome, &td.ToolDurationMs, &td.Reviewed, &td.ReviewedAt, &td.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &td, nil
}

// GetUnreviewedDecisions returns tool decisions for a project that haven't been reviewed,
// where the hook passed through but the user approved (candidates for auto-approval rules).
func (d *DB) GetUnreviewedDecisions(projectPath string, limit int) ([]ToolDecision, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := d.pool.Query(`
		SELECT id, session_id, project_path, tool_name, tool_input_summary, tool_use_id,
			hook_decision, hook_tier, hook_reason, hook_duration_us,
			outcome, tool_duration_ms, reviewed, reviewed_at, created_at
		FROM tool_decisions
		WHERE project_path = ? AND reviewed = FALSE AND hook_decision = 'passthrough' AND outcome = 'executed'
		ORDER BY created_at DESC LIMIT ?`, projectPath, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []ToolDecision
	for rows.Next() {
		var td ToolDecision
		if err := rows.Scan(&td.ID, &td.SessionID, &td.ProjectPath, &td.ToolName,
			&td.ToolInputSummary, &td.ToolUseID, &td.HookDecision, &td.HookTier,
			&td.HookReason, &td.HookDurationUs, &td.Outcome, &td.ToolDurationMs,
			&td.Reviewed, &td.ReviewedAt, &td.CreatedAt); err != nil {
			return nil, err
		}
		decisions = append(decisions, td)
	}
	return decisions, rows.Err()
}

// MarkDecisionsReviewed marks a batch of tool decisions as reviewed.
func (d *DB) MarkDecisionsReviewed(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := d.pool.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("UPDATE tool_decisions SET reviewed = TRUE, reviewed_at = CURRENT_TIMESTAMP WHERE id = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, id := range ids {
		if _, err := stmt.Exec(id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetToolDecisionStats returns aggregate statistics for the dashboard.
type ToolDecisionStats struct {
	TotalDecisions     int     `json:"total_decisions"`
	AutoApproved       int     `json:"auto_approved"`
	Denied             int     `json:"denied"`
	Passthrough        int     `json:"passthrough"`
	Executed           int     `json:"executed"`
	Pending            int     `json:"pending"`
	Failed             int     `json:"failed"`
	AvgHookDurationUs  float64 `json:"avg_hook_duration_us"`
	UnreviewedCount    int     `json:"unreviewed_count"`
}

func (d *DB) GetToolDecisionStats(sessionID string) (*ToolDecisionStats, error) {
	s := &ToolDecisionStats{}
	clause := ""
	var args []any
	if sessionID != "" {
		clause = " WHERE session_id = ?"
		args = append(args, sessionID)
	}
	d.pool.QueryRow("SELECT COUNT(*) FROM tool_decisions"+clause, args...).Scan(&s.TotalDecisions)
	d.pool.QueryRow("SELECT COUNT(*) FROM tool_decisions"+clause+" AND hook_decision = 'allow'", args...).Scan(&s.AutoApproved)
	d.pool.QueryRow("SELECT COUNT(*) FROM tool_decisions"+clause+" AND hook_decision = 'deny'", args...).Scan(&s.Denied)
	d.pool.QueryRow("SELECT COUNT(*) FROM tool_decisions"+clause+" AND hook_decision = 'passthrough'", args...).Scan(&s.Passthrough)
	d.pool.QueryRow("SELECT COUNT(*) FROM tool_decisions"+clause+" AND outcome = 'executed'", args...).Scan(&s.Executed)
	d.pool.QueryRow("SELECT COUNT(*) FROM tool_decisions"+clause+" AND outcome = 'pending'", args...).Scan(&s.Pending)
	d.pool.QueryRow("SELECT COUNT(*) FROM tool_decisions"+clause+" AND outcome = 'failed'", args...).Scan(&s.Failed)
	d.pool.QueryRow("SELECT COALESCE(AVG(hook_duration_us), 0) FROM tool_decisions"+clause+" AND hook_duration_us > 0", args...).Scan(&s.AvgHookDurationUs)
	d.pool.QueryRow("SELECT COUNT(*) FROM tool_decisions WHERE reviewed = FALSE AND hook_decision = 'passthrough' AND outcome = 'executed'").Scan(&s.UnreviewedCount)
	return s, nil
}

// ListToolDecisions returns all tool decisions, newest first.
func (d *DB) ListToolDecisions(limit, offset int) ([]ToolDecision, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.pool.Query(`
		SELECT id, session_id, project_path, tool_name, tool_input_summary, tool_use_id,
			hook_decision, hook_tier, hook_reason, hook_duration_us,
			outcome, tool_duration_ms, reviewed, reviewed_at, created_at
		FROM tool_decisions
		ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []ToolDecision
	for rows.Next() {
		var td ToolDecision
		if err := rows.Scan(&td.ID, &td.SessionID, &td.ProjectPath, &td.ToolName,
			&td.ToolInputSummary, &td.ToolUseID, &td.HookDecision, &td.HookTier,
			&td.HookReason, &td.HookDurationUs, &td.Outcome, &td.ToolDurationMs,
			&td.Reviewed, &td.ReviewedAt, &td.CreatedAt); err != nil {
			return nil, err
		}
		decisions = append(decisions, td)
	}
	return decisions, rows.Err()
}

// ListToolDecisionsBySession returns tool decisions for a session, newest first.
func (d *DB) ListToolDecisionsBySession(sessionID string, limit, offset int) ([]ToolDecision, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.pool.Query(`
		SELECT id, session_id, project_path, tool_name, tool_input_summary, tool_use_id,
			hook_decision, hook_tier, hook_reason, hook_duration_us,
			outcome, tool_duration_ms, reviewed, reviewed_at, created_at
		FROM tool_decisions
		WHERE session_id = ?
		ORDER BY created_at DESC LIMIT ? OFFSET ?`, sessionID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []ToolDecision
	for rows.Next() {
		var td ToolDecision
		if err := rows.Scan(&td.ID, &td.SessionID, &td.ProjectPath, &td.ToolName,
			&td.ToolInputSummary, &td.ToolUseID, &td.HookDecision, &td.HookTier,
			&td.HookReason, &td.HookDurationUs, &td.Outcome, &td.ToolDurationMs,
			&td.Reviewed, &td.ReviewedAt, &td.CreatedAt); err != nil {
			return nil, err
		}
		decisions = append(decisions, td)
	}
	return decisions, rows.Err()
}

// TimelineEvent is a unified event from refinements, tool_decisions, or verification_events.
type TimelineEvent struct {
	ID        int64  `json:"id"`
	EventType string `json:"event_type"`
	Timestamp string `json:"timestamp"`
	Payload   string `json:"payload"`
}

// GetTimelineEvents returns a unified chronological list of refinements, tool_decisions,
// and verification_events for a session, ordered by created_at DESC.
func (d *DB) GetTimelineEvents(sessionID string, limit, offset int) ([]TimelineEvent, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.pool.Query(`
		SELECT id, event_type, timestamp, payload FROM (
			SELECT
				id,
				'refinement' AS event_type,
				created_at AS timestamp,
				json_object(
					'id', id,
					'session_id', session_id,
					'project_path', project_path,
					'raw_prompt', raw_prompt,
					'refined_prompt', refined_prompt,
					'model', model,
					'latency_ms', latency_ms,
					'cache_hit', cache_hit,
					'passthrough', passthrough,
					'status', status,
					'created_at', created_at
				) AS payload
			FROM refinements
			WHERE session_id = ?

			UNION ALL

			SELECT
				id,
				'tool_decision' AS event_type,
				created_at AS timestamp,
				json_object(
					'id', id,
					'session_id', session_id,
					'project_path', project_path,
					'tool_name', tool_name,
					'tool_input_summary', tool_input_summary,
					'tool_use_id', tool_use_id,
					'hook_decision', hook_decision,
					'hook_tier', hook_tier,
					'hook_reason', hook_reason,
					'hook_duration_us', hook_duration_us,
					'outcome', outcome,
					'tool_duration_ms', tool_duration_ms,
					'reviewed', reviewed,
					'created_at', created_at
				) AS payload
			FROM tool_decisions
			WHERE session_id = ?

			UNION ALL

			SELECT
				id,
				'verification' AS event_type,
				created_at AS timestamp,
				json_object(
					'id', id,
					'session_id', session_id,
					'refinement_id', refinement_id,
					'scope', scope,
					'hook_event', hook_event,
					'event_type', event_type,
					'file_count', file_count,
					'duration_us', duration_us,
					'changed_files', changed_files,
					'checks_run', checks_run,
					'result', result,
					'created_at', created_at
				) AS payload
			FROM verification_events
			WHERE session_id = ?

			UNION ALL

			SELECT
				id,
				'bootstrap' AS event_type,
				created_at AS timestamp,
				json_object(
					'id', id,
					'session_id', session_id,
					'project_path', project_path,
					'files_discovered', files_discovered,
					'files_processed', files_processed,
					'total_rules', total_rules,
					'classify_status', classify_status,
					'duration_us', duration_us,
					'classify_duration_us', classify_duration_us,
					'error_message', error_message,
					'created_at', created_at
				) AS payload
			FROM bootstrap_events
			WHERE session_id = ?
		)
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?`, sessionID, sessionID, sessionID, sessionID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []TimelineEvent
	for rows.Next() {
		var e TimelineEvent
		if err := rows.Scan(&e.ID, &e.EventType, &e.Timestamp, &e.Payload); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// --- Bootstrap events ---

// BootstrapEvent records a project context bootstrap at session start.
type BootstrapEvent struct {
	ID                 int64     `json:"id"`
	SessionID          string    `json:"session_id"`
	ProjectPath        string    `json:"project_path"`
	FilesDiscovered    int       `json:"files_discovered"`
	FilesProcessed     int       `json:"files_processed"`
	TotalRules         int       `json:"total_rules"`
	ClassifyStatus     string    `json:"classify_status"`
	DurationUs         int64     `json:"duration_us"`
	ClassifyDurationUs *int64    `json:"classify_duration_us,omitempty"`
	ErrorMessage       *string   `json:"error_message,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
}

func (d *DB) InsertBootstrapEvent(e *BootstrapEvent) (int64, error) {
	res, err := d.pool.Exec(`
		INSERT INTO bootstrap_events (session_id, project_path, files_discovered, files_processed, total_rules, classify_status, duration_us, error_message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.SessionID, e.ProjectPath, e.FilesDiscovered, e.FilesProcessed,
		e.TotalRules, e.ClassifyStatus, e.DurationUs, e.ErrorMessage,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	e.ID = id
	return id, nil
}

func (d *DB) UpdateBootstrapClassify(id int64, status string, durationUs int64, errMsg *string) error {
	_, err := d.pool.Exec(`
		UPDATE bootstrap_events SET classify_status = ?, classify_duration_us = ?, error_message = ?
		WHERE id = ?`,
		status, durationUs, errMsg, id,
	)
	return err
}

func (d *DB) GetBootstrapForSession(sessionID string) (*BootstrapEvent, error) {
	var e BootstrapEvent
	err := d.pool.QueryRow(`
		SELECT id, session_id, project_path, files_discovered, files_processed, total_rules,
			classify_status, duration_us, classify_duration_us, error_message, created_at
		FROM bootstrap_events WHERE session_id = ? ORDER BY created_at DESC LIMIT 1`, sessionID,
	).Scan(&e.ID, &e.SessionID, &e.ProjectPath, &e.FilesDiscovered, &e.FilesProcessed,
		&e.TotalRules, &e.ClassifyStatus, &e.DurationUs, &e.ClassifyDurationUs,
		&e.ErrorMessage, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// --- Context selections ---

// ContextSelection records which deep-context document the LLM selected during refinement.
type ContextSelection struct {
	ID            int64     `json:"id"`
	RefinementID  int64     `json:"refinement_id"`
	DocSource     string    `json:"doc_source"`
	DocHash       string    `json:"doc_hash"`
	RulesSelected int       `json:"rules_selected"`
	CreatedAt     time.Time `json:"created_at"`
}

func (d *DB) InsertContextSelections(refinementID int64, selections []ContextSelection) error {
	tx, err := d.pool.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO context_selections (refinement_id, doc_source, doc_hash, rules_selected)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, s := range selections {
		if _, err := stmt.Exec(refinementID, s.DocSource, s.DocHash, s.RulesSelected); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) GetContextSelections(refinementID int64) ([]ContextSelection, error) {
	rows, err := d.pool.Query(`
		SELECT id, refinement_id, doc_source, doc_hash, rules_selected, created_at
		FROM context_selections WHERE refinement_id = ?`, refinementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sels []ContextSelection
	for rows.Next() {
		var s ContextSelection
		if err := rows.Scan(&s.ID, &s.RefinementID, &s.DocSource, &s.DocHash, &s.RulesSelected, &s.CreatedAt); err != nil {
			return nil, err
		}
		sels = append(sels, s)
	}
	return sels, rows.Err()
}
