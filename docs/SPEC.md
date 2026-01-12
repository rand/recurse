# Recurse Technical Specification

> A recursive language model (RLM) system with hypergraph memory for agentic coding.
> 
> Fork of [Charmbracelet Crush](https://github.com/charmbracelet/crush), extended with RLM orchestration, tiered memory, and embedded reasoning traces.

---

## Table of Contents

1. [Overview](#1-overview)
2. [Architecture](#2-architecture)
3. [RLM Orchestration](#3-rlm-orchestration)
4. [Tiered Hypergraph Memory](#4-tiered-hypergraph-memory)
5. [Embedded Reasoning Traces](#5-embedded-reasoning-traces)
6. [Budget Management](#6-budget-management)
7. [TUI Extensions](#7-tui-extensions)
8. [Module Structure](#8-module-structure)
9. [Configuration](#9-configuration)
10. [Implementation Phases](#10-implementation-phases)

---

## 1. Overview

**Recurse** is a recursive, memory-augmented agentic coding environment. It extends Crush with three core capabilities:

1. **RLM Orchestration**: Treat large contexts as manipulable objects in a Python REPL, enabling recursive sub-LM calls for arbitrarily long context reasoning
2. **Tiered Hypergraph Memory**: Persistent knowledge structure capturing higher-order relations across task, session, and long-term tiers
3. **Embedded Reasoning Traces**: Deciduous-style decision graphs as first-class memory entities

### Design Principles

- **Personal tool**: Daily driver for solo agentic coding, not a commercial product
- **RLM-native**: Prompts are manipulable objects, not just context stuffing
- **Memory-first**: Knowledge persists and evolves across sessions
- **Transparency**: Full visibility into reasoning, recursion, and resource usage
- **Git-integrated**: Reasoning traces link to commits; memory syncs via git

### Key Research Papers

| Paper | Core Insight |
|-------|--------------|
| [RLM (2512.24601)](https://arxiv.org/abs/2512.24601) | Prompts as manipulable objects; recursive self-orchestration |
| [HGMem (2512.23959)](https://arxiv.org/abs/2512.23959) | Hypergraph for higher-order memory correlations |
| [ACE (2510.04618)](https://arxiv.org/abs/2510.04618) | Structured context evolution; avoid brevity bias |
| [MemEvolve (2512.18746)](https://arxiv.org/abs/2512.18746) | Meta-evolution of memory architecture |

---

## 2. Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         TUI Layer                               │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────────┐    │
│  │  Chat    │ │  Budget  │ │  Memory  │ │  RLM Trace View  │    │
│  │  Panel   │ │  Status  │ │Inspector │ │  (REPL stream)   │    │
│  └──────────┘ └──────────┘ └──────────┘ └──────────────────┘    │
├─────────────────────────────────────────────────────────────────┤
│                     Orchestration Layer                         │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                   Meta-Controller                       │    │
│  │  (Claude Haiku 4.5: recursion, memory, budget decisions)│    │
│  └─────────────────────────────────────────────────────────┘    │
│  ┌──────────────────────┐  ┌──────────────────────────────┐     │
│  │    RLM Controller    │  │      Budget Manager          │     │
│  │  • Decomposition     │  │  • Token tracking            │     │
│  │  • Sub-LM dispatch   │  │  • Cost accounting           │     │
│  │  • Result synthesis  │  │  • Limit enforcement         │     │
│  └──────────────────────┘  └──────────────────────────────┘     │
├─────────────────────────────────────────────────────────────────┤
│                      Memory Substrate                           │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │               Tiered Hypergraph Memory                  │    │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────────────────────┐  │    │
│  │  │  Task   │→ │ Session │→ │      Long-term          │  │    │
│  │  │ Working │  │ Accum.  │  │  (persistent across     │  │    │
│  │  │ Memory  │  │         │  │   projects/sessions)    │  │    │
│  │  └─────────┘  └─────────┘  └─────────────────────────┘  │    │
│  │                                                         │    │
│  │  ┌──────────────────────────────────────────────────┐   │    │
│  │  │            Reasoning Trace Layer                 │   │    │
│  │  │  Goals → Decisions → Options → Actions → Outcomes│   │    │
│  │  └──────────────────────────────────────────────────┘   │    │
│  └─────────────────────────────────────────────────────────┘    │
├─────────────────────────────────────────────────────────────────┤
│                      Execution Layer                            │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐    │
│  │  Python    │ │   Code     │ │    LLM     │ │  External  │    │
│  │   REPL     │ │Understanding││  Interface │ │   Tools    │    │
│  │(uv/ruff/ty)│ │(LSP/TS/AST)│ │ (Fantasy)  │ │  (MCP/git) │    │
│  └────────────┘ └────────────┘ └────────────┘ └────────────┘    │
├─────────────────────────────────────────────────────────────────┤
│                      Storage Layer                              │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                SQLite + Extensions                      │    │
│  │  • Conversations (from Crush)                           │    │
│  │  • Hypergraph tables (nodes, hyperedges, membership)    │    │
│  │  • Memory evolution audit log                           │    │
│  │  • Budget/usage history                                 │    │
│  └─────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
```

---

## 3. RLM Orchestration

### 3.1 Python REPL Environment

**Runtime**: External Python process managed via subprocess with IPC over stdin/stdout (JSON-RPC style).

**Bundled tooling** (installed via uv into isolated venv on first run):

| Tool | Purpose |
|------|---------|
| `uv` | Fast package management |
| `ruff` | Linting and formatting |
| `ty` | Type checking |
| `pydantic` | Data validation/serialization |

**Pre-installed libraries**:
```python
import re, json, ast, pathlib, itertools, collections
import pydantic
```

**Sandbox constraints**:
- Filesystem: Read access to project, write to temp directory only
- Network: Disabled by default (configurable)
- Execution time: Configurable timeout (default 30s per cell)
- Memory: Configurable limit (default 1GB)

### 3.2 RLM Controller

```go
type RLMController struct {
    repl        *PythonREPL
    metaCtrl    *MetaController
    budget      *BudgetManager
    memory      *HypergraphMemory
}

// Primary entry point - meta-controller decides strategy
func (r *RLMController) Process(ctx context.Context, task Task) (Result, error)

// Expose context as REPL variable
func (r *RLMController) Externalize(name string, content string) error

// Execute agent-generated code in REPL
func (r *RLMController) Execute(code string) (output string, err error)

// Spawn sub-LM call on a snippet
func (r *RLMController) SubCall(ctx context.Context, prompt string, snippet string) (string, error)

// Synthesize results from multiple sub-calls
func (r *RLMController) Synthesize(results []SubCallResult) (string, error)
```

**Agent-available tools**:

| Tool | Description |
|------|-------------|
| `rlm_externalize` | Store content as named REPL variable |
| `rlm_peek` | Examine slice of externalized content |
| `rlm_execute` | Run Python code in REPL |
| `rlm_subcall` | Invoke sub-LM on snippet with prompt |
| `rlm_status` | Get current recursion depth, budget remaining |

### 3.3 Meta-Controller

LLM-based controller (Claude Haiku 4.5) that decides orchestration strategy.

**Decision points**:
- **Direct vs Recursive**: Is the task answerable within current context?
- **Memory query**: Should we retrieve from hypergraph first?
- **Decomposition strategy**: How to chunk (by file, function, concept)?
- **Budget allocation**: Tokens/sub-calls for this subtask?
- **Termination**: Is the result sufficient?

**Meta-controller prompt structure**:
```
You are an orchestration controller. Given a task and context, decide the strategy.

Current state:
- Task: {task_description}
- Context size: {tokens} tokens
- Budget remaining: {budget}
- Recursion depth: {depth}/{max_depth}
- Memory hints: {relevant_memory_summary}

Options:
1. DIRECT - Answer directly using current context
2. DECOMPOSE - Break into subtasks with strategy: {file|function|concept|custom}
3. MEMORY_QUERY - Retrieve from hypergraph: {query}
4. SUBCALL - Invoke sub-LM on specific snippet
5. SYNTHESIZE - Combine existing partial results

Output JSON: {"action": "...", "params": {...}, "reasoning": "..."}
```

---

## 4. Tiered Hypergraph Memory

### 4.1 Hypergraph Structure

A hypergraph extends graphs by allowing edges (hyperedges) to connect any number of nodes. This enables representing higher-order relations like "function X calls Y, Z with types A, B" as a single semantic unit.

**Node types**:
| Type | Description |
|------|-------------|
| `entity` | Code elements (files, functions, types, variables) |
| `fact` | Extracted knowledge ("UserService depends on DatabasePool") |
| `experience` | Interaction patterns ("User prefers concise explanations") |
| `decision` | Reasoning trace (goals, decisions, options, actions, outcomes) |
| `snippet` | Verbatim content with provenance |

**Hyperedge semantics**:
| Type | Description |
|------|-------------|
| `relation` | Connects entities with typed relationship |
| `composition` | Groups related facts into higher-order structure |
| `causation` | Links decisions to outcomes |
| `context` | Associates snippets with semantic meaning |

### 4.2 SQLite Schema

```sql
-- Core hypergraph structure
CREATE TABLE nodes (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,  -- entity|fact|experience|decision|snippet
    subtype TEXT,        -- file|function|goal|action|etc
    content TEXT NOT NULL,
    embedding BLOB,      -- vector for similarity search
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    access_count INTEGER DEFAULT 0,
    last_accessed TIMESTAMP,
    tier TEXT DEFAULT 'task',  -- task|session|longterm
    confidence REAL DEFAULT 1.0,
    provenance TEXT,     -- JSON: source file, line, commit, etc
    metadata TEXT        -- JSON: flexible additional data
);

CREATE TABLE hyperedges (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,  -- relation|composition|causation|context
    label TEXT,          -- human-readable description
    weight REAL DEFAULT 1.0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    metadata TEXT
);

CREATE TABLE membership (
    hyperedge_id TEXT REFERENCES hyperedges(id),
    node_id TEXT REFERENCES nodes(id),
    role TEXT,           -- subject|object|context|participant
    position INTEGER,    -- ordering within hyperedge
    PRIMARY KEY (hyperedge_id, node_id, role)
);

-- Reasoning trace (Deciduous-style)
CREATE TABLE decisions (
    node_id TEXT PRIMARY KEY REFERENCES nodes(id),
    decision_type TEXT NOT NULL,  -- goal|decision|option|action|outcome|observation
    confidence INTEGER CHECK(confidence BETWEEN 0 AND 100),
    prompt TEXT,         -- user prompt that triggered this
    files TEXT,          -- associated files (JSON array)
    branch TEXT,         -- git branch
    commit_hash TEXT,
    parent_id TEXT REFERENCES decisions(node_id),
    status TEXT DEFAULT 'active'  -- active|completed|rejected
);

-- Memory evolution audit log
CREATE TABLE evolution_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    operation TEXT NOT NULL,  -- create|consolidate|promote|decay|prune
    node_ids TEXT,       -- JSON array of affected nodes
    from_tier TEXT,
    to_tier TEXT,
    reasoning TEXT,
    metadata TEXT
);

-- Indices
CREATE INDEX idx_nodes_tier ON nodes(tier);
CREATE INDEX idx_nodes_type ON nodes(type, subtype);
CREATE INDEX idx_nodes_accessed ON nodes(last_accessed);
CREATE INDEX idx_decisions_type ON decisions(decision_type);
CREATE INDEX idx_decisions_branch ON decisions(branch);
```

### 4.3 Memory Tiers & Evolution

**Tier definitions**:

| Tier | Scope | Lifetime | Consolidation |
|------|-------|----------|---------------|
| **Task** | Current problem context | Until task completion | Aggressive - only relevant facts |
| **Session** | Current coding session | Until session end | Moderate - retain useful patterns |
| **Long-term** | Persistent knowledge | Indefinite (with decay) | Conservative - proven valuable |

**Evolution operations** (inspired by ACE + MemEvolve):

1. **Formation** (task tier):
   - Extract facts from agent interactions
   - Capture reasoning traces as they happen
   - Store code snippets with semantic annotations
   - Create hyperedges for detected relations

2. **Consolidation** (task → session):
   - Triggered: task completion or explicit save
   - Process: Reflection pass generates summary nodes
   - Merge redundant facts (deduplication)
   - Strengthen frequently-traversed hyperedges
   - **Preserve detail** (avoid brevity bias) - link summary to source nodes

3. **Promotion** (session → long-term):
   - Triggered: session end or manual promotion
   - Process: Curation pass evaluates lasting value
   - Criteria: access frequency, recency, relevance to codebase
   - Apply Ebbinghaus-inspired decay to weight old memories
   - Create "crystallized" nodes - compressed but linked to full history

4. **Decay & Pruning** (long-term maintenance):
   - Temporal decay: `weight *= decay_factor ^ days_since_access`
   - Access amplification: `weight *= 1 + log(access_count)`
   - Pruning threshold: Archive nodes below threshold
   - **Never delete**: Move to "archive" tier, excluded from retrieval but queryable

5. **Meta-evolution** (architecture adaptation):
   - Track retrieval success/failure patterns
   - Adjust hyperedge weights based on utility
   - Propose new node subtypes when patterns emerge
   - Surface to user for approval before structural changes

### 4.4 Embedding Model

**Recommendation**: **Voyage-3** for general text/code mix, or **VoyageCode3** for code-heavy workloads.

| Model | Strengths | Context |
|-------|-----------|---------|
| VoyageCode3 | Best for code understanding, 32K context | Code-heavy memory |
| Voyage-3 | Balanced text+code, high quality | Mixed memory |
| OpenAI text-embedding-3-large | Good fallback, 3072 dims | If Voyage unavailable |

**Quality bias**: Prioritize retrieval accuracy over cost/speed for memory operations.

### 4.5 Memory Tools

| Tool | Description |
|------|-------------|
| `memory_store` | Add node(s) to hypergraph with automatic tier assignment |
| `memory_query` | Semantic search across tiers with filters |
| `memory_relate` | Create hyperedge connecting nodes |
| `memory_trace` | Log reasoning step (goal/decision/action/outcome) |
| `memory_promote` | Explicitly promote node to higher tier |
| `memory_inspect` | Get full node details including provenance |
| `memory_graph` | Return subgraph around specified nodes |

### 4.6 Project Scope

- **Per-project memory** by default
- Cross-project queries when:
  - Explicitly directed by user
  - Looking for patterns/experiences that might transfer
  - Searching for previously solved similar problems

---

## 5. Embedded Reasoning Traces

Deciduous concepts as first-class memory entities, automatically linked to git.

### 5.1 Node Subtypes for Decisions

| Subtype | Description |
|---------|-------------|
| `goal` | High-level objective |
| `decision` | Choice point |
| `option` | Approach considered (may be chosen or rejected) |
| `action` | Implementation step |
| `outcome` | Result of actions |
| `observation` | Discovery or insight during work |

### 5.2 Hyperedge Types for Reasoning

| Type | Connects |
|------|----------|
| `spawns` | goal → decision |
| `considers` | decision → option |
| `chooses` | decision → option (selected) |
| `rejects` | decision → option (with reason) |
| `implements` | decision → action |
| `produces` | action → outcome |
| `informs` | observation → decision |

### 5.3 Git Integration

**Auto-linking to commits**:
- When an action is completed, capture current commit hash
- Store pre/post diffs as snippet nodes linked to the action
- Enable "what changed for this decision" queries

**Trace capture**:
```go
type ReasoningTrace struct {
    GoalID      string
    DecisionID  string
    ChosenOption string
    RejectedOptions []RejectedOption
    Actions     []ActionRecord
    Outcome     string
    Branch      string
    CommitHash  string
    Diffs       []DiffRecord
}
```

---

## 6. Budget Management

### 6.1 Trackable Metrics

```go
type BudgetState struct {
    // Token counts
    InputTokens      int64
    OutputTokens     int64
    CachedTokens     int64
    
    // Cost (USD)
    TotalCost        float64
    
    // RLM-specific
    RecursionDepth   int
    SubCallCount     int
    REPLExecutions   int
    
    // Time
    SessionDuration  time.Duration
    WallClockTime    time.Duration
}

type BudgetLimits struct {
    MaxInputTokens    int64         // Per task
    MaxOutputTokens   int64         // Per task
    MaxTotalCost      float64       // Per session
    MaxRecursionDepth int
    MaxSubCalls       int           // Per task
    MaxSessionTime    time.Duration
}
```

### 6.2 Configuration

```yaml
budget:
  defaults:
    max_input_tokens: 100000
    max_output_tokens: 50000
    max_total_cost: 5.00
    max_recursion_depth: 5
    max_subcalls_per_task: 20
    max_session_hours: 8
  alerts:
    cost_warning_threshold: 0.80
    token_warning_threshold: 0.75
  meta_controller:
    model: "claude-haiku-4-5"
    max_tokens_per_decision: 500
```

---

## 7. TUI Extensions

All panels built with Bubble Tea, matching Crush's existing TUI patterns.

### 7.1 Status Bar (Always Visible)

```
[Tokens: 12.4k/100k ▓▓▓▓░░░░░░] [Cost: $0.23/$5.00 ▓▓░░░░░░░░] [Depth: 2/5] [⏱ 45m]
```

### 7.2 New Panels

**RLM Trace View** (toggle with `Ctrl+R`):
```
┌─ RLM Reasoning ─────────────────────────────────────────┐
│ [2] Analyzing codebase structure...                     │
│   ├─ Decomposing by module                              │
│   ├─ [2.1] Examining internal/agent/ (3 files)          │
│   │     └─ Found: coordinator.go, session.go, agent.go  │
│   ├─ [2.2] Examining internal/memory/ (queued)          │
│   └─ Budget: 15.2k tokens used, 84.8k remaining         │
└─────────────────────────────────────────────────────────┘
```

**REPL View** (toggle with `Ctrl+P`):
```
┌─ Python REPL ───────────────────────────────────────────┐
│ >>> context[:500]                                       │
│ 'package agent\n\nimport (\n\t"context"\n\t"fmt"...'    │
│                                                         │
│ >>> len(re.findall(r'func \w+', context))               │
│ 23                                                      │
│                                                         │
│ >>> # Agent is filtering functions...                   │
└─────────────────────────────────────────────────────────┘
```

**Memory Inspector** (toggle with `Ctrl+M`):
```
┌─ Memory Graph ──────────────────────────────────────────┐
│ Task (12 nodes) → Session (45 nodes) → Long-term (892)  │
│                                                         │
│ Recent:                                                 │
│ • [fact] RLMController manages recursion (task)         │
│ • [decision] Use external Python REPL (session)         │
│ • [entity] internal/agent/coordinator.go (longterm)     │
│                                                         │
│ [/] Search  [g] Graph view  [t] Trace view              │
└─────────────────────────────────────────────────────────┘
```

**Reasoning Trace View** (toggle with `Ctrl+T`):
```
┌─ Reasoning Trace ───────────────────────────────────────┐
│ ◉ Goal: Implement RLM orchestration layer               │
│ ├─◉ Decision: REPL environment choice                   │
│ │ ├─○ Option: Embedded Starlark (rejected: limited)     │
│ │ ├─● Option: External Python (chosen: full ecosystem)  │
│ │ └─◉ Action: Implement subprocess manager              │
│ │     └─◉ Outcome: REPL working, needs sandbox          │
│ └─◉ Decision: Sub-call dispatch strategy                │
│   └─ (in progress...)                                   │
└─────────────────────────────────────────────────────────┘
```

### 7.3 Keybindings

| Key | Action |
|-----|--------|
| `Ctrl+R` | Toggle RLM trace view |
| `Ctrl+P` | Toggle REPL view |
| `Ctrl+M` | Toggle memory inspector |
| `Ctrl+T` | Toggle reasoning trace |
| `Ctrl+L` | Toggle limits panel |
| `Ctrl+B` | Show budget details |

---

## 8. Module Structure

```
recurse/
├── cmd/
│   └── recurse/
│       └── main.go
├── internal/
│   ├── agent/                    # Extended from Crush
│   │   ├── coordinator.go        # Modified: inject RLM + memory
│   │   ├── session.go
│   │   └── agent.go
│   ├── rlm/                      # NEW
│   │   ├── controller.go         # RLM orchestration
│   │   ├── meta.go               # Meta-controller (Claude Haiku 4.5)
│   │   ├── repl/
│   │   │   ├── manager.go        # Python process management
│   │   │   ├── sandbox.go        # Execution constraints
│   │   │   └── protocol.go       # JSON-RPC IPC
│   │   ├── decompose.go          # Prompt decomposition strategies
│   │   └── synthesize.go         # Result aggregation
│   ├── memory/                   # NEW
│   │   ├── hypergraph/
│   │   │   ├── store.go          # SQLite-backed storage
│   │   │   ├── node.go           # Node types and operations
│   │   │   ├── edge.go           # Hyperedge types
│   │   │   ├── query.go          # Traversal, similarity search
│   │   │   └── schema.sql        # DDL
│   │   ├── tiers/
│   │   │   ├── task.go           # Working memory
│   │   │   ├── session.go        # Session accumulator
│   │   │   └── longterm.go       # Persistent store
│   │   ├── evolution/
│   │   │   ├── consolidate.go    # Task → Session
│   │   │   ├── promote.go        # Session → Long-term
│   │   │   ├── decay.go          # Temporal decay
│   │   │   └── meta.go           # Architecture adaptation
│   │   └── reasoning/
│   │       ├── trace.go          # Decision capture
│   │       └── types.go          # Goal/Decision/Option/etc
│   ├── budget/                   # NEW
│   │   ├── tracker.go            # Usage tracking
│   │   ├── limits.go             # Limit enforcement
│   │   └── reporter.go           # Usage reports
│   ├── tools/                    # Extended from Crush
│   │   ├── rlm_*.go              # RLM tools
│   │   ├── memory_*.go           # Memory tools
│   │   └── decision_*.go         # Reasoning trace tools
│   └── tui/                      # Extended from Crush
│       ├── rlm_view.go           # RLM trace panel
│       ├── repl_view.go          # REPL panel
│       ├── memory_view.go        # Memory inspector
│       ├── trace_view.go         # Reasoning trace
│       ├── budget_view.go        # Budget status/limits
│       └── status_bar.go         # Always-visible metrics
├── pkg/
│   └── python/                   # Python-side code
│       ├── bootstrap.py          # REPL initialization
│       ├── sandbox.py            # Execution sandbox
│       └── requirements.txt      # uv/ruff/ty/pydantic
└── schema/
    └── memory.sql                # Full DDL
```

---

## 9. Configuration

### 9.1 Config File Location

```
~/.config/recurse/config.yaml      # Global defaults
./.recurse/config.yaml             # Project-specific overrides
```

### 9.2 Full Config Schema

```yaml
# LLM Configuration
llm:
  provider: "anthropic"  # anthropic|openai|bedrock|gemini
  model: "claude-sonnet-4"
  api_key_env: "ANTHROPIC_API_KEY"

# Meta-Controller
meta_controller:
  model: "claude-haiku-4-5"
  max_tokens_per_decision: 500
  temperature: 0.3

# Embedding Model
embedding:
  provider: "voyage"  # voyage|openai
  model: "voyage-3"   # voyage-3|voyage-code-3|text-embedding-3-large
  api_key_env: "VOYAGE_API_KEY"

# Budget Limits
budget:
  defaults:
    max_input_tokens: 100000
    max_output_tokens: 50000
    max_total_cost: 5.00
    max_recursion_depth: 5
    max_subcalls_per_task: 20
    max_session_hours: 8
  alerts:
    cost_warning_threshold: 0.80
    token_warning_threshold: 0.75

# REPL Configuration
repl:
  python_path: "python3"  # Or path to specific Python
  sandbox:
    filesystem_read: ["./"]
    filesystem_write: ["/tmp/recurse"]
    network: false
    timeout_seconds: 30
    memory_limit_mb: 1024
  packages:
    - "pydantic"
    # Additional packages installed via uv

# Memory Configuration
memory:
  database_path: "./.recurse/memory.db"
  tiers:
    task:
      max_nodes: 1000
      auto_consolidate: true
    session:
      max_nodes: 5000
      promote_threshold: 0.7  # Confidence threshold for promotion
    longterm:
      decay_factor: 0.995     # Per day
      prune_threshold: 0.1
      archive_enabled: true
  evolution:
    consolidation_interval: "5m"
    reflection_on_task_complete: true

# Git Integration
git:
  capture_diffs: true
  link_commits: true
  trace_branch_prefix: "trace/"

# TUI
tui:
  default_panels:
    - "chat"
    - "status"
  keybindings:
    toggle_rlm_trace: "ctrl+r"
    toggle_repl: "ctrl+p"
    toggle_memory: "ctrl+m"
    toggle_reasoning: "ctrl+t"
```

---

## 10. Implementation Phases

### Phase 1: Foundation (MVP)
- [ ] Fork Crush, establish build pipeline
- [ ] Basic RLM loop: externalize → peek → execute → subcall
- [ ] Python REPL with uv/ruff/ty/pydantic bootstrap
- [ ] Simple budget tracking (tokens, cost)
- [ ] Status bar in TUI

### Phase 2: Memory
- [ ] Hypergraph SQLite schema
- [ ] Basic store/query operations
- [ ] Task tier only (no evolution yet)
- [ ] Memory inspector panel

### Phase 3: Orchestration
- [ ] Meta-controller integration (Claude Haiku 4.5)
- [ ] Decomposition strategies (file, function, concept)
- [ ] Result synthesis
- [ ] RLM trace view

### Phase 4: Evolution
- [ ] Tier promotion logic (task → session → long-term)
- [ ] Consolidation algorithms (avoid brevity bias)
- [ ] Decay and pruning
- [ ] Evolution audit log

### Phase 5: Reasoning Traces
- [ ] Deciduous node types (goal/decision/option/action/outcome)
- [ ] Reasoning trace capture
- [ ] Git integration (commits, diffs)
- [ ] Trace visualization

### Phase 6: Polish
- [ ] REPL view panel
- [ ] Full TUI panel suite
- [ ] Configuration refinement
- [ ] Documentation

---

## Appendix A: Key Resources

| Resource | URL |
|----------|-----|
| Crush (base) | https://github.com/charmbracelet/crush |
| RLM Paper | https://arxiv.org/abs/2512.24601 |
| HGMem Paper | https://arxiv.org/abs/2512.23959 |
| HGMem Code | https://github.com/Encyclomen/HGMem |
| ACE Paper | https://arxiv.org/abs/2510.04618 |
| MemEvolve Paper | https://arxiv.org/abs/2512.18746 |
| Deciduous | https://github.com/notactuallytreyanastasio/deciduous |
| Beads (bd) | https://github.com/steveyegge/beads |
| Voyage AI | https://docs.voyageai.com |

---

## Appendix B: Glossary

| Term | Definition |
|------|------------|
| **RLM** | Recursive Language Model - treats prompts as manipulable objects |
| **Hypergraph** | Graph where edges can connect multiple nodes |
| **Hyperedge** | Edge connecting arbitrary number of nodes |
| **Meta-controller** | LLM that decides orchestration strategy |
| **Externalize** | Move content from context to REPL variable |
| **Sub-call** | Recursive LLM invocation on a slice of content |
| **Tier** | Memory level (task, session, long-term) |
| **Consolidation** | Merging/summarizing memory between tiers |
| **Decay** | Gradual weight reduction for unused memories |
| **Reasoning trace** | Decision graph captured during work |
