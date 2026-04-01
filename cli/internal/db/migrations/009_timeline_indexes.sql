-- Covering indexes for the timeline UNION ALL query.
-- Eliminates temp B-tree sorts by providing (session_id, created_at DESC) order.
CREATE INDEX IF NOT EXISTS idx_refinements_session_time ON refinements(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tool_decisions_session_time ON tool_decisions(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_verification_session_time ON verification_events(session_id, created_at DESC);
