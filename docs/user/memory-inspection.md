# Memory Inspection and Debugging Guide

This guide covers the TUI features for inspecting memory, viewing reasoning traces, and understanding RLM mode selection.

## TUI Keyboard Shortcuts

### Global Shortcuts

| Shortcut | Action |
|----------|--------|
| `Ctrl+P` | Open command palette |
| `Ctrl+C` | Cancel current operation / Quit |
| `Ctrl+G` | Toggle help panel |
| `Ctrl+S` | Switch session |
| `Ctrl+N` | New session |
| `Ctrl+L` | Switch model |
| `Ctrl+F` | Open file picker (vision-capable models only) |
| `Ctrl+O` | Open external editor (when `$EDITOR` is set) |
| `Ctrl+Z` | Background current task |

### Command Palette

Open with `Ctrl+P` to access all commands. Use `Tab` to cycle between System Commands, User Commands, and MCP Prompts.

**Available Commands:**
- **New Session** (`Ctrl+N`) - Start a fresh conversation
- **Switch Session** (`Ctrl+S`) - Switch between existing sessions
- **Switch Model** (`Ctrl+L`) - Change the active model
- **Summarize Session** - Compact current session with AI summary
- **Toggle Thinking Mode** - Enable/disable extended thinking (Anthropic models)
- **Select Reasoning Effort** - Set reasoning level (OpenAI models)
- **Toggle Sidebar** - Switch between compact and normal layout
- **RLM Trace** - View orchestration trace events
- **Toggle Yolo Mode** - Enable/disable auto-approval of tool calls
- **Toggle Help** (`Ctrl+G`) - Show/hide keyboard shortcuts
- **Initialize Project** - Create/update project memory file
- **Quit** (`Ctrl+C`) - Exit the application

---

## RLM Trace View

The RLM Trace dialog shows the orchestration events during RLM (Recursive Language Model) execution.

### Opening RLM Trace

1. Press `Ctrl+P` to open command palette
2. Type "RLM" or "Trace"
3. Select "RLM Trace"

### Trace View Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| `Enter` | View event details |
| `↓` / `j` | Next event |
| `↑` / `k` | Previous event |
| `→` / `l` | Expand event |
| `←` / `h` | Collapse event |
| `c` | Clear trace history |
| `Esc` / `q` | Close dialog |

### Trace Event Types

Events are displayed with icons indicating their type:

| Icon | Event Type | Description |
|------|------------|-------------|
| `▶` | Start | Task decomposition started |
| `◆` | Decompose | Task broken into subtasks |
| `●` | Execute | Subtask execution |
| `◀` | Synthesize | Results being combined |
| `✓` | Complete | Task finished successfully |
| `✗` | Error | Task failed |
| `?` | Unknown | Unrecognized event |

### Status Indicators

| Icon | Status |
|------|--------|
| `✓` | Success |
| `✗` | Failed |
| `⋯` | Pending |
| `▶` | Running |

---

## Memory Inspector

The Memory Inspector allows browsing and searching the hypergraph memory system.

### Memory Inspector Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| `/` | Search memories |
| `r` | Show recent memories |
| `s` | Show memory statistics |
| `Enter` | View memory details |
| `↓` / `j` | Next item |
| `↑` / `k` | Previous item |
| `Esc` / `q` | Close dialog |

### Memory Node Types

Memories are categorized by type, shown with badges:

| Badge | Type | Description |
|-------|------|-------------|
| `[F]` | Fact | Verified information |
| `[E]` | Entity | Named entities (files, functions, etc.) |
| `[S]` | Snippet | Code snippets |
| `[D]` | Decision | Architectural decisions |
| `[X]` | Experience | Learned patterns |

### Memory Tiers

The memory system uses three tiers:

1. **Task Memory** - Working memory for current problem (aggressive consolidation)
2. **Session Memory** - Accumulated context for the coding session
3. **Long-term Memory** - Persistent knowledge with decay/amplification

### Memory Statistics

The stats view (`s`) shows:
- Total memory count
- Breakdown by type
- Memory usage metrics

---

## Mode Selection Indicator

The mode indicator shows whether RLM or Direct mode is active and why.

### Mode Types

| Mode | Badge Color | Description |
|------|-------------|-------------|
| RLM | Blue | Recursive decomposition with Python REPL |
| DIRECT | Green | Standard LLM response |

### Understanding Mode Selection

Mode selection considers:

1. **Task Classification** - Type of task detected:
   - `computational` - Math, counting, data processing → RLM
   - `analytical` - Complex analysis → RLM
   - `transformational` - Code transformation → RLM
   - `retrieval` - Information lookup → Direct
   - `unknown` - Ambiguous task

2. **Context Size** - Token count thresholds
3. **REPL Availability** - Whether Python sandbox is available
4. **User Override** - Manual mode forcing

### Mode Override

Force a specific mode using keyboard shortcuts:
- `Ctrl+Shift+R` - Force RLM mode
- `Ctrl+Shift+D` - Force Direct mode

When overridden, the indicator shows "(forced)" next to the mode badge.

### Mode Selection Details

The detailed view shows:
- Selected mode and reason
- Classification confidence percentage
- Signals that influenced classification
- Context token count
- Whether LLM fallback was used

---

## Debugging Workflows

### Investigating Why RLM Was/Wasn't Used

1. Open RLM Trace (`Ctrl+P` → "RLM Trace")
2. Look for mode selection events
3. Check the mode indicator for classification details
4. Review signals that influenced the decision

### Checking Memory State

1. Open Memory Inspector
2. Press `s` to view statistics
3. Press `/` to search for specific memories
4. Press `r` to see recently accessed memories

### Tracing Task Decomposition

1. Open RLM Trace
2. Look for `▶ Start` events
3. Follow `◆ Decompose` events to see subtasks
4. Check `● Execute` events for individual steps
5. Review `◀ Synthesize` for result combination

### Debugging Memory Issues

1. **Memory not found**: Use `/` search with different keywords
2. **Stale memory**: Check tier and last access time in details
3. **Wrong classification**: Review type badge and consider re-categorization
4. **Performance issues**: Check stats for memory count and distribution

---

## Logging and Diagnostics

### Debug Logging

Mode selection logs detailed information at debug level:

```
slog.Debug("mode selection",
    "mode", result.mode,
    "reason", result.reason,
    "classification", result.classification.Type,
    "confidence", result.classification.Confidence,
    "signals", result.classification.Signals,
    "used_llm_fallback", result.usedLLMFallback,
)
```

Enable debug logging to see mode selection details:
```bash
RECURSE_LOG_LEVEL=debug recurse
```

### Trace Files

Reasoning traces can be found in:
```
.reasoning_logs/sessions/
```

Each session creates a trace file with:
- Mode selection decisions
- Task decompositions
- Memory queries
- Synthesis steps
