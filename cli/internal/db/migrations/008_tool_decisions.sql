CREATE TABLE IF NOT EXISTS tool_decisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    project_path TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    tool_input_summary TEXT,
    tool_use_id TEXT,
    hook_decision TEXT,
    hook_tier INTEGER,
    hook_reason TEXT,
    hook_duration_us INTEGER,
    outcome TEXT NOT NULL DEFAULT 'pending',
    tool_duration_ms INTEGER,
    reviewed BOOLEAN NOT NULL DEFAULT FALSE,
    reviewed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tool_decisions_session ON tool_decisions(session_id);
CREATE INDEX IF NOT EXISTS idx_tool_decisions_project ON tool_decisions(project_path, tool_name);
CREATE INDEX IF NOT EXISTS idx_tool_decisions_unreviewed ON tool_decisions(reviewed, project_path);
CREATE INDEX IF NOT EXISTS idx_tool_decisions_pending ON tool_decisions(outcome, tool_use_id);
