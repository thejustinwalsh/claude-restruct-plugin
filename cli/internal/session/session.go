package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	restructDir = ".restruct"
	sessionsDir = "sessions"
)

// Session represents an active Claude Code session tracked locally in
// .restruct/sessions/<session_id>.json for fast per-project lookups.
type Session struct {
	SessionID       string    `json:"session_id"`
	ProjectPath     string    `json:"project_path"`
	TranscriptPath  string    `json:"transcript_path"`
	StartedAt       time.Time `json:"started_at"`
	LastRefinementAt time.Time `json:"last_refinement_at,omitempty"`
	RefinementCount int       `json:"refinement_count"`
}

// Manager handles per-project session state in .restruct/sessions/.
type Manager struct {
	projectDir string
}

// NewManager creates a session manager for the given project directory.
func NewManager(projectDir string) *Manager {
	return &Manager{projectDir: projectDir}
}

// sessionsPath returns the path to the sessions directory.
func (m *Manager) sessionsPath() string {
	return filepath.Join(m.projectDir, restructDir, sessionsDir)
}

// sessionFilePath returns the path to a specific session file.
func (m *Manager) sessionFilePath(sessionID string) string {
	return filepath.Join(m.sessionsPath(), sessionID+".json")
}

// EnsureDirs creates the .restruct/sessions/ directory structure if it doesn't exist.
func (m *Manager) EnsureDirs() error {
	return os.MkdirAll(m.sessionsPath(), 0755)
}

// Get loads an existing session by ID. Returns nil if not found.
func (m *Manager) Get(sessionID string) (*Session, error) {
	data, err := os.ReadFile(m.sessionFilePath(sessionID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session file: %w", err)
	}

	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse session file: %w", err)
	}
	return &s, nil
}

// Start creates a new session file. If one already exists for this session ID,
// it is returned unchanged.
func (m *Manager) Start(sessionID, projectPath, transcriptPath string) (*Session, error) {
	if err := m.EnsureDirs(); err != nil {
		return nil, err
	}

	// Check if session already exists
	existing, err := m.Get(sessionID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	s := &Session{
		SessionID:      sessionID,
		ProjectPath:    projectPath,
		TranscriptPath: transcriptPath,
		StartedAt:      time.Now().UTC(),
	}
	return s, m.save(s)
}

// RecordRefinement updates the session's refinement count and timestamp.
func (m *Manager) RecordRefinement(sessionID string) (*Session, error) {
	s, err := m.Get(sessionID)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	s.RefinementCount++
	s.LastRefinementAt = time.Now().UTC()
	return s, m.save(s)
}

// End removes the session file (session is over).
func (m *Manager) End(sessionID string) error {
	path := m.sessionFilePath(sessionID)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil // already gone
	}
	return err
}

// ListActive returns all session files in the project's .restruct/sessions/ directory.
func (m *Manager) ListActive() ([]*Session, error) {
	entries, err := os.ReadDir(m.sessionsPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var sessions []*Session
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.sessionsPath(), entry.Name()))
		if err != nil {
			continue
		}
		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		sessions = append(sessions, &s)
	}
	return sessions, nil
}

// CleanStale removes session files older than the given duration.
func (m *Manager) CleanStale(maxAge time.Duration) (int, error) {
	sessions, err := m.ListActive()
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().UTC().Add(-maxAge)
	removed := 0
	for _, s := range sessions {
		lastActivity := s.LastRefinementAt
		if lastActivity.IsZero() {
			lastActivity = s.StartedAt
		}
		if lastActivity.Before(cutoff) {
			if err := m.End(s.SessionID); err == nil {
				removed++
			}
		}
	}
	return removed, nil
}

func (m *Manager) save(s *Session) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	return os.WriteFile(m.sessionFilePath(s.SessionID), data, 0644)
}
