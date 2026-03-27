package db

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
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

// DefaultPath returns the default database path based on CLAUDE_PLUGIN_DATA or ~/.local/share/restruct.
func DefaultPath() string {
	if dir := os.Getenv("CLAUDE_PLUGIN_DATA"); dir != "" {
		return filepath.Join(dir, "restruct.db")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "restruct", "restruct.db")
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
	Model         string    `json:"model,omitempty"`
	Temperature   float64   `json:"temperature,omitempty"`
	LatencyMs     int64     `json:"latency_ms"`
	CacheHit      bool      `json:"cache_hit"`
	Passthrough   bool      `json:"passthrough"`
	OutputValid   *bool     `json:"output_valid,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type PipelineEvent struct {
	ID           int64     `json:"id"`
	RefinementID int64     `json:"refinement_id"`
	Stage        string    `json:"stage"`
	DurationMs   int64     `json:"duration_ms"`
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
	res, err := d.pool.Exec(`
		INSERT INTO refinements (session_id, project_path, raw_prompt, refined_prompt, model, temperature, latency_ms, cache_hit, passthrough, output_valid)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.SessionID, r.ProjectPath, r.RawPrompt, r.RefinedPrompt, r.Model, r.Temperature, r.LatencyMs, r.CacheHit, r.Passthrough, r.OutputValid,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListRefinements(limit, offset int) ([]Refinement, error) {
	rows, err := d.pool.Query(`
		SELECT id, session_id, project_path, raw_prompt, refined_prompt, model, temperature, latency_ms, cache_hit, passthrough, output_valid, created_at
		FROM refinements ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []Refinement
	for rows.Next() {
		var r Refinement
		if err := rows.Scan(&r.ID, &r.SessionID, &r.ProjectPath, &r.RawPrompt, &r.RefinedPrompt, &r.Model, &r.Temperature, &r.LatencyMs, &r.CacheHit, &r.Passthrough, &r.OutputValid, &r.CreatedAt); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

func (d *DB) GetRefinement(id int64) (*Refinement, error) {
	var r Refinement
	err := d.pool.QueryRow(`
		SELECT id, session_id, project_path, raw_prompt, refined_prompt, model, temperature, latency_ms, cache_hit, passthrough, output_valid, created_at
		FROM refinements WHERE id = ?`, id).Scan(&r.ID, &r.SessionID, &r.ProjectPath, &r.RawPrompt, &r.RefinedPrompt, &r.Model, &r.Temperature, &r.LatencyMs, &r.CacheHit, &r.Passthrough, &r.OutputValid, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &r, err
}

func (d *DB) GetRefinementsSince(lastID int64, limit int) ([]Refinement, error) {
	rows, err := d.pool.Query(`
		SELECT id, session_id, project_path, raw_prompt, refined_prompt, model, temperature, latency_ms, cache_hit, passthrough, output_valid, created_at
		FROM refinements WHERE id > ? ORDER BY id ASC LIMIT ?`, lastID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []Refinement
	for rows.Next() {
		var r Refinement
		if err := rows.Scan(&r.ID, &r.SessionID, &r.ProjectPath, &r.RawPrompt, &r.RefinedPrompt, &r.Model, &r.Temperature, &r.LatencyMs, &r.CacheHit, &r.Passthrough, &r.OutputValid, &r.CreatedAt); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

func (d *DB) GetRefinementsForSession(sessionID string) ([]Refinement, error) {
	rows, err := d.pool.Query(`
		SELECT id, session_id, project_path, raw_prompt, refined_prompt, model, temperature, latency_ms, cache_hit, passthrough, output_valid, created_at
		FROM refinements WHERE session_id = ? ORDER BY created_at ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []Refinement
	for rows.Next() {
		var r Refinement
		if err := rows.Scan(&r.ID, &r.SessionID, &r.ProjectPath, &r.RawPrompt, &r.RefinedPrompt, &r.Model, &r.Temperature, &r.LatencyMs, &r.CacheHit, &r.Passthrough, &r.OutputValid, &r.CreatedAt); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

// --- Pipeline event operations ---

func (d *DB) InsertPipelineEvent(e *PipelineEvent) error {
	_, err := d.pool.Exec(`
		INSERT INTO pipeline_events (refinement_id, stage, duration_ms, success, metadata)
		VALUES (?, ?, ?, ?, ?)`,
		e.RefinementID, e.Stage, e.DurationMs, e.Success, e.Metadata,
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
		if err := rows.Scan(&e.ID, &e.RefinementID, &e.Stage, &e.DurationMs, &e.Success, &e.Metadata, &e.CreatedAt); err != nil {
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
