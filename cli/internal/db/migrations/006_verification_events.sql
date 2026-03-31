CREATE TABLE IF NOT EXISTS verification_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    scope TEXT NOT NULL,
    hook_event TEXT NOT NULL,
    event_type TEXT NOT NULL,
    file_count INTEGER,
    duration_us INTEGER,
    cwd_input TEXT,
    project_dir TEXT,
    changed_files TEXT,
    checks_run TEXT,
    result TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_verification_session ON verification_events(session_id);
