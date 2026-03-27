CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    project_path TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    ended_at DATETIME,
    transcript_path TEXT,
    status TEXT DEFAULT 'active'
);

CREATE TABLE IF NOT EXISTS refinements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT REFERENCES sessions(id),
    project_path TEXT NOT NULL,
    raw_prompt TEXT NOT NULL,
    refined_prompt TEXT,
    model TEXT,
    temperature REAL,
    latency_ms INTEGER,
    cache_hit BOOLEAN DEFAULT FALSE,
    passthrough BOOLEAN DEFAULT FALSE,
    output_valid BOOLEAN,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS pipeline_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    refinement_id INTEGER REFERENCES refinements(id),
    stage TEXT NOT NULL,
    duration_ms INTEGER,
    success BOOLEAN DEFAULT TRUE,
    metadata TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_refinements_session ON refinements(session_id);
CREATE INDEX IF NOT EXISTS idx_refinements_project ON refinements(project_path);
CREATE INDEX IF NOT EXISTS idx_refinements_created ON refinements(created_at);
CREATE INDEX IF NOT EXISTS idx_pipeline_events_refinement ON pipeline_events(refinement_id);
