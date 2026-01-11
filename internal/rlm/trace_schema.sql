-- RLM Trace Events Schema
-- Persistent storage for orchestration traces

-- Trace events table
CREATE TABLE IF NOT EXISTS trace_events (
    id TEXT PRIMARY KEY,
    session_id TEXT,  -- optional link to chat session
    type TEXT NOT NULL,  -- decision, decompose, subcall, synthesize, memory_query, execute
    action TEXT NOT NULL,
    details TEXT,
    tokens INTEGER NOT NULL DEFAULT 0,
    duration_ns INTEGER NOT NULL DEFAULT 0,  -- nanoseconds
    depth INTEGER NOT NULL DEFAULT 0,
    parent_id TEXT,
    status TEXT NOT NULL DEFAULT 'pending',  -- pending, running, completed, failed
    created_at INTEGER NOT NULL  -- Unix timestamp in milliseconds
);

CREATE INDEX IF NOT EXISTS idx_trace_events_session ON trace_events(session_id);
CREATE INDEX IF NOT EXISTS idx_trace_events_type ON trace_events(type);
CREATE INDEX IF NOT EXISTS idx_trace_events_parent ON trace_events(parent_id);
CREATE INDEX IF NOT EXISTS idx_trace_events_created ON trace_events(created_at DESC);

-- Trace statistics (aggregated for performance)
CREATE TABLE IF NOT EXISTS trace_stats (
    id INTEGER PRIMARY KEY CHECK (id = 1),  -- singleton row
    total_events INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0,
    total_duration_ns INTEGER NOT NULL DEFAULT 0,
    max_depth INTEGER NOT NULL DEFAULT 0,
    events_by_type TEXT NOT NULL DEFAULT '{}'  -- JSON map
);

-- Initialize singleton stats row
INSERT OR IGNORE INTO trace_stats (id) VALUES (1);

-- Trigger to update stats on insert
CREATE TRIGGER IF NOT EXISTS update_trace_stats_on_insert
AFTER INSERT ON trace_events
BEGIN
    UPDATE trace_stats SET
        total_events = total_events + 1,
        total_tokens = total_tokens + NEW.tokens,
        total_duration_ns = total_duration_ns + NEW.duration_ns,
        max_depth = MAX(max_depth, NEW.depth),
        events_by_type = json_set(
            events_by_type,
            '$.' || NEW.type,
            COALESCE(json_extract(events_by_type, '$.' || NEW.type), 0) + 1
        )
    WHERE id = 1;
END;

-- Trigger to update stats on delete
CREATE TRIGGER IF NOT EXISTS update_trace_stats_on_delete
AFTER DELETE ON trace_events
BEGIN
    UPDATE trace_stats SET
        total_events = total_events - 1,
        total_tokens = total_tokens - OLD.tokens,
        total_duration_ns = total_duration_ns - OLD.duration_ns,
        events_by_type = json_set(
            events_by_type,
            '$.' || OLD.type,
            MAX(0, COALESCE(json_extract(events_by_type, '$.' || OLD.type), 0) - 1)
        )
    WHERE id = 1;
END;
