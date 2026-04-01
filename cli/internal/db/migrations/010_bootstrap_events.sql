-- Bootstrap events track project context indexing at session start.
CREATE TABLE IF NOT EXISTS bootstrap_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT REFERENCES sessions(id),
    project_path TEXT NOT NULL,
    files_discovered INTEGER NOT NULL DEFAULT 0,
    files_processed INTEGER NOT NULL DEFAULT 0,
    total_rules INTEGER NOT NULL DEFAULT 0,
    classify_status TEXT NOT NULL DEFAULT 'pending',
    duration_us INTEGER,
    classify_duration_us INTEGER,
    error_message TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_bootstrap_session ON bootstrap_events(session_id);
CREATE INDEX IF NOT EXISTS idx_bootstrap_session_time ON bootstrap_events(session_id, created_at DESC);

-- Context selections track which deep-context documents the LLM selected per refinement.
CREATE TABLE IF NOT EXISTS context_selections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    refinement_id INTEGER REFERENCES refinements(id),
    doc_source TEXT NOT NULL,
    doc_hash TEXT NOT NULL,
    rules_selected INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_context_sel_refinement ON context_selections(refinement_id);
