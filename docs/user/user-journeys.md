# User Journeys and OODA Loops

This document maps the intended user experiences for Recurse, an RLM-powered agentic coding environment.

---

## Primary User Journey: Agentic Coding Session

### 1. Start Session

```bash
recurse
```

The user launches Recurse in interactive mode. On startup:
- Memory system initializes (loads existing hypergraph from `.recurse/rlm.db`)
- Python REPL sandbox starts (uv/ruff/ty environment)
- Session checkpoint is checked for recovery (if previous session crashed)
- Ready work is shown if beads tracking is configured

### 2. Submit Complex Coding Task

User enters a multi-step request:
```
Refactor the authentication module to use JWT tokens instead of sessions.
Update all API endpoints and add proper error handling.
```

The system:
1. **Classifies** the task (computational, analytical, transformational, retrieval)
2. **Selects mode** based on classification and context size
3. **Queries memory** for relevant context (prior decisions, related code facts)

### 3. Observe RLM Decomposition

If RLM mode is selected, the user sees decomposition in the trace view (`Ctrl+P` → "RLM Trace"):

```
▶ Start: Refactor authentication module
  ◆ Decompose: concept strategy
    ● Execute: Analyze current session-based auth
    ● Execute: Design JWT token structure
    ● Execute: Plan endpoint migration
  ◀ Synthesize: Combined analysis
```

The trace shows:
- Task breakdown strategy (concept, sequential, parallel)
- Individual subtask execution
- Token usage per step
- Execution status (running, complete, error)

### 4. Python REPL Processing

For computational tasks, the Python REPL handles:
- Code analysis and transformation
- File system operations
- Data processing
- Memory queries via `memory_query()` callback

The REPL streams output to the trace view, showing:
- Python code being executed
- Intermediate results
- LLM sub-calls via `llm_call()` callback

### 5. Review Synthesized Response

After decomposition completes, results are synthesized:
- Partial results from subtasks are combined
- Coherent narrative is constructed
- Code changes are presented with context

The response appears in the chat panel with:
- Summary of changes made
- Files modified
- Decisions recorded to memory

### 6. Ask Follow-up Question

User asks a clarifying question:
```
What error codes should the JWT validation return?
```

The system:
1. **Queries memory** for the recent authentication decision
2. **Retrieves context** from the just-completed refactoring
3. **Responds directly** (no decomposition needed for retrieval tasks)

Memory access is visible in the Memory Inspector (`Ctrl+P` → search).

### 7. End Session

When the user exits (`Ctrl+C` or `/quit`):
- **Task memory** consolidates to **session memory**
- **Session memory** promotes important facts to **long-term memory**
- Confidence scores are updated based on access patterns
- Low-confidence nodes may decay toward archive tier
- Session checkpoint is cleared (clean exit)

---

## OODA Loop for Each Interaction

Every user interaction follows an OODA (Observe-Orient-Decide-Act) loop:

### Observe

The system observes the current state:

| Observation | Source |
|-------------|--------|
| User's task/question | Chat input |
| Current code state | File system, LSP |
| Memory context | Hypergraph query |
| Session history | Conversation state |
| Available resources | Budget manager |

### Orient

The meta-controller orients by analyzing:

| Analysis | Method |
|----------|--------|
| Task classification | Pattern matching + LLM fallback |
| Complexity estimation | Token count, keyword signals |
| Memory relevance | Semantic search on facts/decisions |
| Resource availability | Token budget, REPL status |

The orientation produces:
- Classification: `computational`, `analytical`, `transformational`, `retrieval`
- Confidence score (0.0-1.0)
- Signals that influenced classification

### Decide

Based on orientation, the meta-controller decides:

| Decision | Options |
|----------|---------|
| Execution mode | `DIRECT`, `RLM` |
| Action type | `DIRECT`, `DECOMPOSE`, `MEMORY_QUERY`, `SYNTHESIZE`, `DELEGATE` |
| Decomposition strategy | `concept`, `sequential`, `parallel` |
| Memory operations | Query, store, update |

Decision factors:
- Task complexity vs. direct capability
- Available context from memory
- Token budget constraints
- REPL availability for computational tasks

### Act

Execute the chosen strategy:

| Action | Execution |
|--------|-----------|
| DIRECT | Single LLM call with memory context |
| DECOMPOSE | Break into subtasks, recurse |
| MEMORY_QUERY | Search hypergraph, return relevant facts |
| SYNTHESIZE | Combine partial results into coherent response |
| DELEGATE | Pass to Python REPL for computation |

Results flow back through:
1. Trace events recorded for visibility
2. Decisions stored as memory nodes
3. Response presented to user
4. Memory updated based on interaction

---

## Jobs to Be Done

### 1. Understand Large Codebase Quickly

**Situation**: Developer joins a new project or returns after time away.

**Journey**:
1. Run `recurse` in project root
2. Ask: "What is the architecture of this project?"
3. System queries memory for prior analysis or performs fresh exploration
4. Decomposition walks through modules, dependencies, patterns
5. Synthesized response provides architectural overview
6. Key facts stored in long-term memory for future reference

**Success metrics**:
- Time to productive contribution reduced
- Accurate mental model formed
- Key decisions and patterns surfaced

### 2. Make Changes Across Multiple Files

**Situation**: Feature requires coordinated changes to API, models, tests.

**Journey**:
1. Describe the feature requirements
2. RLM decomposes into subtasks by file/concern
3. Each subtask analyzed and modified independently
4. Changes synthesized with dependency awareness
5. Test modifications aligned with implementation changes

**Success metrics**:
- All related files updated consistently
- No orphaned references
- Tests pass after changes

### 3. Remember Context Between Sessions

**Situation**: Multi-day feature work, picking up where left off.

**Journey**:
1. Start new session, memory loads from previous work
2. Ask: "What was I working on?"
3. System retrieves recent decisions, incomplete tasks
4. Context restored without re-explaining project state
5. Continue work with full historical context

**Success metrics**:
- Zero re-orientation time
- Prior decisions accessible
- No repeated mistakes

### 4. Track Reasoning for Future Reference

**Situation**: Need to understand why a decision was made weeks ago.

**Journey**:
1. Query memory: "Why did we use JWT instead of sessions?"
2. System retrieves decision node with reasoning
3. Linked trace shows the analysis that led to decision
4. Git integration links to implementing commit
5. Full provenance chain available

**Success metrics**:
- Decisions are discoverable
- Reasoning is preserved
- Implementation is traceable

---

## Memory Evolution Through Journeys

### During a Session

```
User Input → Task Memory (working)
                ↓ consolidation
           Session Memory (accumulated)
```

- Task memory aggressively consolidates to avoid bloat
- Important facts promoted based on access patterns
- Decisions recorded with reasoning

### Between Sessions

```
Session Memory → Long-term Memory
                     ↓ decay
                Archive (excluded from retrieval)
```

- High-value facts promoted to long-term
- Ebbinghaus decay applied to unused nodes
- Frequently accessed nodes amplified
- Below-threshold nodes archived (never deleted)

### Across Projects

```
Project A Long-term ←→ Cross-project queries ←→ Project B Long-term
```

- Each project has isolated memory by default
- Cross-project queries available for shared patterns
- Git sync enables memory sharing across machines

---

## Mode Selection Reference

| Task Type | Typical Mode | Reasoning |
|-----------|--------------|-----------|
| Simple question | DIRECT | Low complexity, no decomposition needed |
| Code explanation | DIRECT | Retrieval from context/memory |
| Multi-file refactor | RLM | Requires decomposition and synthesis |
| Data transformation | RLM | Computational, benefits from REPL |
| Architectural analysis | RLM | Complex, multi-step reasoning |
| Follow-up question | DIRECT | Context already established |

Mode can be forced via keyboard shortcuts:
- `Ctrl+Shift+R` - Force RLM mode
- `Ctrl+Shift+D` - Force Direct mode

---

## Debugging User Journeys

When a journey doesn't go as expected:

### Task Not Decomposed When It Should Be

1. Check mode indicator for classification
2. Review signals in mode selection details
3. Consider forcing RLM mode for this task type
4. File issue if classification consistently wrong

### Memory Not Retrieved

1. Open Memory Inspector (`Ctrl+P` → search)
2. Check if relevant facts exist
3. Verify tier (archived nodes excluded)
4. Check confidence threshold (min 0.5 by default)

### Context Lost Between Sessions

1. Check `.recurse/rlm.db` exists
2. Verify session ended cleanly (not crashed)
3. Review memory tier for promoted facts
4. Check decay settings if facts are archiving too fast

### REPL Not Available

1. Check Python environment (`uv`, `ruff`, `ty` installed)
2. Verify REPL process started (check logs)
3. Review error in trace view
4. Fallback to direct mode available
