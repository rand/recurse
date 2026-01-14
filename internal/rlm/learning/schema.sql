-- Learning persistence schema for ContinuousLearner

-- Routing adjustments: learned model preferences by query type
CREATE TABLE IF NOT EXISTS routing_adjustments (
    key TEXT PRIMARY KEY,               -- Format: "queryType:modelID"
    adjustment REAL NOT NULL DEFAULT 0, -- Adjustment factor (-1 to 1)
    observation_count INTEGER NOT NULL DEFAULT 0,
    last_observed TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_routing_last_observed ON routing_adjustments(last_observed);

-- Strategy preferences: learned strategy preferences by query type
CREATE TABLE IF NOT EXISTS strategy_preferences (
    key TEXT PRIMARY KEY,               -- Format: "queryType:strategy"
    preference REAL NOT NULL DEFAULT 0, -- Preference score (-1 to 1)
    observation_count INTEGER NOT NULL DEFAULT 0,
    last_observed TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_strategy_last_observed ON strategy_preferences(last_observed);

-- Raw outcomes for analysis and retraining
CREATE TABLE IF NOT EXISTS outcomes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    query TEXT NOT NULL,
    query_features TEXT NOT NULL,       -- JSON blob of QueryFeatures
    strategy_used TEXT NOT NULL,
    model_used TEXT NOT NULL,
    depth_reached INTEGER NOT NULL DEFAULT 0,
    tools_used TEXT,                    -- JSON array of tool names
    success INTEGER NOT NULL DEFAULT 0, -- Boolean as integer
    quality_score REAL NOT NULL DEFAULT 0,
    cost REAL NOT NULL DEFAULT 0,
    latency_ms INTEGER NOT NULL DEFAULT 0,
    recorded_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_outcomes_recorded_at ON outcomes(recorded_at);
CREATE INDEX IF NOT EXISTS idx_outcomes_model ON outcomes(model_used);
CREATE INDEX IF NOT EXISTS idx_outcomes_strategy ON outcomes(strategy_used);

-- Metadata for schema versioning
CREATE TABLE IF NOT EXISTS schema_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT OR IGNORE INTO schema_meta (key, value) VALUES ('version', '1');
INSERT OR IGNORE INTO schema_meta (key, value) VALUES ('created_at', datetime('now'));
