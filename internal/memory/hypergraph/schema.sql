-- Recurse Hypergraph Memory Schema
-- Based on HGMem paper (arxiv.org/abs/2512.23959)

-- Core hypergraph structure

-- Nodes represent entities, facts, experiences, decisions, and snippets
CREATE TABLE IF NOT EXISTS nodes (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL CHECK(type IN ('entity', 'fact', 'experience', 'decision', 'snippet')),
    subtype TEXT,  -- file|function|goal|action|etc
    content TEXT NOT NULL,
    embedding BLOB,  -- vector for similarity search
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    access_count INTEGER DEFAULT 0,
    last_accessed TIMESTAMP,
    tier TEXT DEFAULT 'task' CHECK(tier IN ('task', 'session', 'longterm', 'archive')),
    confidence REAL DEFAULT 1.0 CHECK(confidence >= 0 AND confidence <= 1),
    provenance TEXT,  -- JSON: source file, line, commit, etc
    metadata TEXT     -- JSON: flexible additional data
);

-- Hyperedges connect multiple nodes with semantic relationships
CREATE TABLE IF NOT EXISTS hyperedges (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL CHECK(type IN (
        'relation', 'composition', 'causation', 'context',
        -- Reasoning trace edge types (SPEC.md section 5.2)
        'spawns', 'considers', 'chooses', 'rejects', 'implements', 'produces', 'informs'
    )),
    label TEXT,  -- human-readable description
    weight REAL DEFAULT 1.0 CHECK(weight >= 0),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    metadata TEXT  -- JSON: flexible additional data
);

-- Membership links nodes to hyperedges with roles
CREATE TABLE IF NOT EXISTS membership (
    hyperedge_id TEXT NOT NULL REFERENCES hyperedges(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    role TEXT CHECK(role IN ('subject', 'object', 'context', 'participant')),
    position INTEGER,  -- ordering within hyperedge
    PRIMARY KEY (hyperedge_id, node_id, role)
);

-- Reasoning trace (Deciduous-style decision nodes)
CREATE TABLE IF NOT EXISTS decisions (
    node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    decision_type TEXT NOT NULL CHECK(decision_type IN ('goal', 'decision', 'option', 'action', 'outcome', 'observation')),
    confidence INTEGER CHECK(confidence BETWEEN 0 AND 100),
    prompt TEXT,       -- user prompt that triggered this
    files TEXT,        -- JSON array of associated files
    branch TEXT,       -- git branch
    commit_hash TEXT,
    parent_id TEXT REFERENCES decisions(node_id),
    status TEXT DEFAULT 'active' CHECK(status IN ('active', 'completed', 'rejected'))
);

-- Memory evolution audit log
CREATE TABLE IF NOT EXISTS evolution_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    operation TEXT NOT NULL CHECK(operation IN ('create', 'consolidate', 'promote', 'decay', 'prune', 'archive')),
    node_ids TEXT,    -- JSON array of affected node IDs
    from_tier TEXT,
    to_tier TEXT,
    reasoning TEXT,   -- explanation of why evolution occurred
    metadata TEXT     -- JSON: additional context
);

-- Retrieval outcome tracking for meta-evolution [SPEC-06.01]
CREATE TABLE IF NOT EXISTS retrieval_outcomes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    query_hash TEXT,              -- hash of query for grouping
    query_type TEXT,              -- computational, retrieval, analytical, transformational
    node_id TEXT,                 -- retrieved node
    node_type TEXT,               -- type of retrieved node
    node_subtype TEXT,            -- subtype if any
    relevance_score REAL CHECK(relevance_score >= 0 AND relevance_score <= 1),
    was_used BOOLEAN DEFAULT 0,   -- whether result was actually used
    context_tokens INTEGER,       -- tokens in context
    latency_ms INTEGER            -- retrieval latency
);

-- Meta-evolution proposals [SPEC-06.03]
CREATE TABLE IF NOT EXISTS proposals (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL CHECK(type IN ('new_subtype', 'rename_type', 'merge_types', 'split_type', 'retrieval_config', 'decay_adjust')),
    title TEXT NOT NULL,
    description TEXT,
    rationale TEXT,               -- why this change is suggested
    evidence TEXT,                -- JSON array of Evidence
    impact TEXT,                  -- JSON: ImpactAssessment
    changes TEXT,                 -- JSON array of SchemaChange
    confidence REAL CHECK(confidence >= 0 AND confidence <= 1),
    priority INTEGER CHECK(priority BETWEEN 1 AND 5),
    status TEXT DEFAULT 'pending' CHECK(status IN ('pending', 'approved', 'rejected', 'applied', 'deferred', 'failed')),
    status_note TEXT,             -- reason for rejection, etc.
    source_pattern TEXT,          -- pattern type that triggered this
    defer_until TIMESTAMP,
    applied_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Project association for multi-project support
CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    path TEXT NOT NULL UNIQUE,
    name TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_accessed TIMESTAMP
);

-- Link nodes to projects
CREATE TABLE IF NOT EXISTS node_projects (
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    PRIMARY KEY (node_id, project_id)
);

-- Indices for efficient queries

-- Node lookups
CREATE INDEX IF NOT EXISTS idx_nodes_tier ON nodes(tier);
CREATE INDEX IF NOT EXISTS idx_nodes_type ON nodes(type, subtype);
CREATE INDEX IF NOT EXISTS idx_nodes_accessed ON nodes(last_accessed);
CREATE INDEX IF NOT EXISTS idx_nodes_created ON nodes(created_at);
CREATE INDEX IF NOT EXISTS idx_nodes_confidence ON nodes(confidence);

-- Hyperedge lookups
CREATE INDEX IF NOT EXISTS idx_hyperedges_type ON hyperedges(type);
CREATE INDEX IF NOT EXISTS idx_hyperedges_weight ON hyperedges(weight);

-- Membership lookups (for graph traversal)
CREATE INDEX IF NOT EXISTS idx_membership_node ON membership(node_id);
CREATE INDEX IF NOT EXISTS idx_membership_edge ON membership(hyperedge_id);
CREATE INDEX IF NOT EXISTS idx_membership_role ON membership(role);

-- Decision tree navigation
CREATE INDEX IF NOT EXISTS idx_decisions_type ON decisions(decision_type);
CREATE INDEX IF NOT EXISTS idx_decisions_parent ON decisions(parent_id);
CREATE INDEX IF NOT EXISTS idx_decisions_branch ON decisions(branch);
CREATE INDEX IF NOT EXISTS idx_decisions_status ON decisions(status);

-- Evolution log queries
CREATE INDEX IF NOT EXISTS idx_evolution_operation ON evolution_log(operation);
CREATE INDEX IF NOT EXISTS idx_evolution_timestamp ON evolution_log(timestamp);

-- Retrieval outcomes queries
CREATE INDEX IF NOT EXISTS idx_outcomes_timestamp ON retrieval_outcomes(timestamp);
CREATE INDEX IF NOT EXISTS idx_outcomes_query_hash ON retrieval_outcomes(query_hash);
CREATE INDEX IF NOT EXISTS idx_outcomes_node_type ON retrieval_outcomes(node_type);
CREATE INDEX IF NOT EXISTS idx_outcomes_query_type ON retrieval_outcomes(query_type);
CREATE INDEX IF NOT EXISTS idx_outcomes_was_used ON retrieval_outcomes(was_used);

-- Proposals queries
CREATE INDEX IF NOT EXISTS idx_proposals_status ON proposals(status);
CREATE INDEX IF NOT EXISTS idx_proposals_type ON proposals(type);
CREATE INDEX IF NOT EXISTS idx_proposals_created ON proposals(created_at);

-- Project queries
CREATE INDEX IF NOT EXISTS idx_node_projects_project ON node_projects(project_id);

-- Triggers for automatic timestamp updates
CREATE TRIGGER IF NOT EXISTS update_node_timestamp
AFTER UPDATE ON nodes
BEGIN
    UPDATE nodes SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

-- Trigger to update access tracking
CREATE TRIGGER IF NOT EXISTS update_node_access
AFTER UPDATE OF access_count ON nodes
BEGIN
    UPDATE nodes SET last_accessed = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;
