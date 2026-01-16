# SPEC-09: Session Context Persistence and RLM Execution

> Fix the critical gaps preventing memory from storing useful task context and REPL from ever executing.

## Overview

The RLM system has sophisticated infrastructure for memory tiers, context externalization, and Python REPL execution, but none of it works in practice:

1. **Memory stores only metadata** - Experience nodes contain session timestamps and durations, not actual task content or learnings
2. **REPL never activates** - Race condition during startup, silent failures, tools show "none active"
3. **Orchestrator never uses REPL** - Meta-controller has 5 actions (DIRECT, DECOMPOSE, MEMORY_QUERY, SUBCALL, SYNTHESIZE), none trigger REPL execution
4. **No session synthesis** - When sessions end, no summary of "what was accomplished" is created

## Current State (Problems)

### Problem 1: Memory Stores Metadata, Not Content

When querying longterm experiences, users see:
```
[1] experience (longterm) - 2026-01-15 15:21
    Session 1768515660100809000 | Duration: 1h1m15.408910625s | Started: Jan 15 15:21
```

But NO information about:
- What task was being worked on
- What approach was taken
- What was learned
- What blockers were encountered

**Root Cause**: `AddExperience()` stores:
- `Content`: Brief description string
- `Metadata`: `{outcome, success, time}` only

**Evidence**: `internal/memory/tiers/task.go:252-274`

### Problem 2: REPL Never Activates

User reports: "REPL panes show none active whenever I check"

**Root Causes**:
1. **Race condition**: REPL started in goroutine (`app.go:140-146`), coordinator initialized immediately after
2. **Silent failures**: Startup errors only logged as warnings, not surfaced
3. **No readiness check**: Tools registered before REPL confirms ready
4. **Context cancellation**: Background goroutine can be cancelled before startup completes

**Evidence**: `internal/app/app.go:129-150`

### Problem 3: Orchestrator Never Executes REPL

The RLM orchestrator (`internal/rlm/orchestrator/core.go`) has this flow:
```
Meta-Controller.Decide() → executeAction() → one of 5 handlers
```

The 5 handlers:
- `executeDirect()` → Calls `mainClient.Complete()` (LLM text response)
- `executeDecompose()` → Breaks into chunks, recursive orchestrate()
- `executeMemoryQuery()` → Queries hypergraph store
- `executeSubcall()` → Recursive orchestrate() call
- `executeSynthesize()` → Combines partial results

**NO handler for REPL execution.** Even if LLM generates Python code, it's returned as text, never executed.

**Evidence**: `internal/rlm/orchestrator/core.go:298-318`

### Problem 4: No Session Synthesis

When `SessionEnd()` is called (`lifecycle.go:188`), it:
1. Consolidates session tier nodes
2. Promotes to longterm tier (just changes tier field)
3. Does NOT create a session summary node

**Result**: No aggregated knowledge of "Session X accomplished Y, encountered Z, learned W"

**Evidence**: `internal/memory/evolution/lifecycle.go:188-210`

## Requirements

### [SPEC-09.01] Session Summary Nodes

Create a dedicated session summary node at session end capturing:

```go
type SessionSummary struct {
    SessionID     string
    StartTime     time.Time
    EndTime       time.Time
    Duration      time.Duration

    // Content (what was done)
    TasksAttempted   []TaskSummary
    TasksCompleted   []TaskSummary
    TasksFailed      []TaskSummary

    // Learning (what was discovered)
    KeyInsights      []string
    BlockersHit      []string
    PatternsUsed     []string

    // Context (for resumption)
    ActiveFiles      []string
    UnfinishedWork   string
    NextSteps        []string
}

type TaskSummary struct {
    Description string
    Outcome     string
    Duration    time.Duration
    Success     bool
}
```

Features:
- Auto-generated on session end via reflection pass
- Uses LLM to synthesize from raw experiences
- Stores as `experience` node with subtype `session_summary`
- Content field contains human-readable summary
- Metadata contains structured JSON for programmatic access

### [SPEC-09.02] Enhanced Experience Storage

Expand experience node metadata to capture full context:

```go
type ExperienceMetadata struct {
    // Current fields
    Outcome   string    `json:"outcome"`
    Success   bool      `json:"success"`
    Time      time.Time `json:"time"`

    // New fields
    TaskDescription  string   `json:"task_description"`
    Approach         string   `json:"approach"`
    FilesModified    []string `json:"files_modified"`
    BlockersHit      []string `json:"blockers_hit"`
    InsightsGained   []string `json:"insights_gained"`
    RelatedDecisions []string `json:"related_decisions"`
    Duration         string   `json:"duration"`
}
```

Features:
- Richer context stored with each experience
- Links to decision nodes for reasoning trace
- File provenance for code-related experiences
- Blockers and insights for learning

### [SPEC-09.03] REPL Synchronous Startup

Fix the race condition by waiting for REPL before proceeding:

```go
// app.go changes
replMgr, err := repl.NewManager(repl.Options{...})
if err != nil {
    slog.Warn("Failed to create REPL manager", "error", err)
} else {
    // SYNCHRONOUS START with timeout
    startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    if err := replMgr.Start(startCtx); err != nil {
        slog.Error("REPL failed to start", "error", err)
        // Surface error to user via TUI notification
        app.Notifications <- Notification{
            Level:   "error",
            Title:   "REPL Unavailable",
            Message: fmt.Sprintf("Python REPL failed to start: %v", err),
        }
        app.REPLManager = nil  // Explicitly nil so tools know
    } else {
        slog.Info("Python REPL started successfully")
        app.REPLManager = replMgr
    }
}
```

Features:
- Synchronous startup with 30-second timeout
- Explicit error surfacing (not just warning)
- User notification on failure
- Clear nil state if startup fails

### [SPEC-09.04] REPL Activation Tool Trigger

Add automatic REPL activation when certain tools are used:

When `rlm_execute`, `rlm_peek`, or `rlm_externalize` are called:
1. Check if REPL is running
2. If not, attempt to start it synchronously
3. If start fails, return actionable error to agent
4. If start succeeds, proceed with tool execution

```go
func (t *RLMExecuteTool) ensureREPLRunning(ctx context.Context) error {
    if t.replManager.Running() {
        return nil
    }

    // Attempt to start
    startCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
    defer cancel()

    if err := t.replManager.Start(startCtx); err != nil {
        return fmt.Errorf("REPL not running and failed to start: %w. "+
            "Check Python availability with `which python3`", err)
    }

    return nil
}
```

### [SPEC-09.05] Orchestrator REPL Action

Add a new meta-controller action for REPL execution:

```go
const (
    ActionDirect      = "DIRECT"
    ActionDecompose   = "DECOMPOSE"
    ActionMemoryQuery = "MEMORY_QUERY"
    ActionSubcall     = "SUBCALL"
    ActionSynthesize  = "SYNTHESIZE"
    ActionExecute     = "EXECUTE"  // NEW: Execute code in REPL
)
```

New handler in `core.go`:
```go
func (c *Core) executeREPL(ctx context.Context, state meta.State, code string) (string, int, error) {
    if c.replManager == nil || !c.replManager.Running() {
        // Fallback to DIRECT if REPL unavailable
        return c.executeDirect(ctx, state)
    }

    result, err := c.replManager.Execute(ctx, code)
    if err != nil {
        return "", 0, fmt.Errorf("REPL execution failed: %w", err)
    }

    // Store execution as experience with rich metadata
    c.storeExecution(ctx, state.Task, code, result.Output, result.Success)

    return result.Output, estimateTokens(result.Output), nil
}
```

Update meta-controller prompt to include:
```
Use EXECUTE when:
- Task requires computation, data transformation, or analysis
- Need to test code before presenting
- Working with structured data (JSON, CSV, etc.)
- Need to verify numerical results
```

### [SPEC-09.06] Context Externalization Integration

Wire the existing `Wrapper.PrepareContext()` into the orchestration flow:

```go
func (c *Core) orchestrate(ctx context.Context, state meta.State, parentID string) (string, int, error) {
    // NEW: Check if context should be externalized
    if c.wrapper != nil && c.replManager != nil && c.replManager.Running() {
        prepared, err := c.wrapper.PrepareContextWithOptions(ctx, state.Task, state.Contexts, PrepareOptions{
            ForceMode: "", // Let wrapper decide
        })
        if err == nil && prepared.Mode == ModeRLM {
            // Context externalized to REPL variables
            state.ExternalizedContext = true
            state.SystemPrompt = prepared.SystemPrompt
        }
    }

    // Continue with meta-controller decision
    decision, err := c.meta.Decide(ctx, state)
    // ...
}
```

### [SPEC-09.07] Memory Query Content Display

Fix `formatNodeContent()` to display actual content, not just metadata:

```go
func formatNodeContent(nodeType string, content string, metadata json.RawMessage) string {
    var parts []string

    // Always include the actual content first
    if content != "" {
        parts = append(parts, content)
    }

    // Parse and display relevant metadata
    var data map[string]any
    if json.Unmarshal(metadata, &data) == nil {
        switch nodeType {
        case "experience":
            if outcome, ok := data["outcome"].(string); ok && outcome != "" {
                parts = append(parts, fmt.Sprintf("Outcome: %s", outcome))
            }
            if insights, ok := data["insights_gained"].([]any); ok && len(insights) > 0 {
                parts = append(parts, fmt.Sprintf("Insights: %v", insights))
            }
        case "decision":
            if rationale, ok := data["rationale"].(string); ok && rationale != "" {
                parts = append(parts, fmt.Sprintf("Rationale: %s", rationale))
            }
        }
    }

    return strings.Join(parts, "\n")
}
```

### [SPEC-09.08] Session Resumption Flow

Implement explicit session resumption when starting a new session:

```go
func (s *Service) ResumeSession(ctx context.Context) (*SessionContext, error) {
    // Query recent session summary nodes
    summaries, err := s.store.ListNodes(ctx, hypergraph.NodeFilter{
        Type:    hypergraph.NodeTypeExperience,
        Subtype: "session_summary",
        Tier:    hypergraph.TierLongterm,
        Limit:   3,
        OrderBy: "created_at DESC",
    })
    if err != nil || len(summaries) == 0 {
        return nil, nil // No previous sessions
    }

    latest := summaries[0]
    var summary SessionSummary
    json.Unmarshal(latest.Metadata, &summary)

    return &SessionContext{
        PreviousSession:  summary,
        UnfinishedWork:   summary.UnfinishedWork,
        RecommendedStart: summary.NextSteps,
        ActiveFiles:      summary.ActiveFiles,
    }, nil
}
```

When agent asks "What was I working on?":
1. Query session summaries
2. Return structured context with tasks, outcomes, next steps
3. Provide file context for quick resumption

## Implementation Tasks

### Phase 1: Fix REPL Activation (Critical) ✅
- [x] [SPEC-09.03] Synchronous REPL startup in app.go (commit 23b83d8)
- [x] [SPEC-09.03] Error surfacing to user via REPLStatusMsg (commit 23b83d8)
- [x] [SPEC-09.04] REPL activation trigger in tools via ensureREPLRunning() (commit 23b83d8)
- [x] Add REPL health check endpoint for TUI via REPLStatusMsg (commit 23b83d8)

### Phase 2: Enhance Memory Content ✅
- [x] [SPEC-09.02] Expand ExperienceMetadata struct (internal/memory/tiers/task.go)
- [x] [SPEC-09.02] Update AddExperience() to accept rich metadata (AddExperienceWithOptions)
- [x] [SPEC-09.02] Extend REPL protocol with rich experience fields (commit d22a439)
- [ ] [SPEC-09.07] Fix formatNodeContent() for proper display (optional - display concern)
- [ ] Add migration for existing experience nodes (optional - new nodes use rich format)

### Phase 3: Session Synthesis ✅
- [x] [SPEC-09.01] Define SessionSummary type (internal/rlm/synthesizer.go)
- [x] [SPEC-09.01] Implement reflection pass using LLM (LLMSynthesizer)
- [x] [SPEC-09.01] Create session summary node on SessionEnd() (lifecycle.go)
- [x] [SPEC-09.08] Implement ResumeSession() query (service.go)

### Phase 4: Orchestrator Integration ✅
- [x] [SPEC-09.05] Add ActionExecute to meta-controller (meta/controller.go)
- [x] [SPEC-09.05] Implement executeREPL() handler (orchestrator/core.go)
- [x] [SPEC-09.06] Wire Wrapper.PrepareContext() into orchestrate() (commit 428539a)
- [x] Update meta-controller prompt with EXECUTE guidance (meta/controller.go)

## Dependencies

- `internal/rlm/repl/` - REPL manager
- `internal/memory/tiers/` - Experience storage
- `internal/memory/evolution/` - Session lifecycle
- `internal/rlm/orchestrator/` - Meta-controller
- `internal/rlm/wrapper.go` - Context externalization

## Acceptance Criteria

1. **REPL Activation**: REPL panes show "active" when tools are available
2. **Memory Content**: `rlm_memory_query` returns task descriptions, not just timestamps
3. **Session Resumption**: "What was I working on?" returns actionable context
4. **REPL Execution**: Meta-controller can decide to execute code for computational tasks
5. **No Silent Failures**: All REPL startup failures surface to user

## Test Scenarios

### Scenario 1: Session Resumption
```
Session 1: Work on feature X, encounter blocker Y, find workaround Z
Session 1 ends → Session summary created

Session 2 starts
User: "What was I working on?"
Agent: "In your last session (1h15m), you worked on feature X.
        You hit blocker Y but found workaround Z.
        Next steps: [list from summary]"
```

### Scenario 2: REPL Execution
```
User: "Calculate the sum of all numbers in data.json"
Meta-controller: Decides ActionExecute (computational task)
Orchestrator: Calls replManager.Execute() with Python code
Result: Returns computed sum, stores experience with approach
```

### Scenario 3: Context Recovery
```
User: "Show me what I learned yesterday"
Agent: Queries experience nodes from yesterday
Returns: Actual task descriptions, outcomes, insights
NOT: Just session IDs and timestamps
```

## Risk Assessment

| Risk | Mitigation |
|------|------------|
| REPL startup slowness | 30-second timeout, deferred activation option |
| LLM costs for synthesis | Use Haiku for summaries, cache results |
| Migration complexity | Add new fields as optional, backfill gradually |
| Breaking existing tools | Additive changes only, preserve existing API |
