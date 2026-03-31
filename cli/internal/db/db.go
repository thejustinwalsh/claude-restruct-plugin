package db

import (
	"database/sql"
	"embed"
	"fmt"
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
			ended_at = COALESCE(excluded.ended_at, sessions.ended_at),
			status = COALESCE(excluded.status, sessions.status)`,
		s.ID, s.ProjectPath, s.StartedAt, s.TranscriptPath, s.Status,
	)
	return err
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
	_, err := d.pool.Exec(`
		INSERT INTO verification_events (session_id, refinement_id, scope, hook_event, event_type, file_count, duration_us, cwd_input, project_dir, changed_files, checks_run, result)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.SessionID, e.RefinementID, e.Scope, e.HookEvent, e.EventType, e.FileCount, e.DurationUs,
		e.CwdInput, e.ProjectDir, e.ChangedFiles, e.ChecksRun, e.Result,
	)
	return err
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
