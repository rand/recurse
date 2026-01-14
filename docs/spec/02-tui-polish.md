# SPEC-02: TUI Polish

> Complete the terminal UI with RLM trace view, memory inspector, and panel management.

## Overview

The TUI extends Crush's Bubble Tea interface with panels for RLM trace visualization, memory inspection, and budget monitoring. This spec covers completing and polishing these components.

## Current State

- Basic TUI structure from Crush exists
- `internal/tui/` has some extensions
- Missing: RLM trace view, memory inspector, panel switching

## Requirements

### [SPEC-02.01] Panel Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│ [Chat] [Memory] [RLM Trace] [Budget]              tokens: 1.2M │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│                     Active Panel Content                        │
│                                                                 │
├─────────────────────────────────────────────────────────────────┤
│ > input area                                                    │
└─────────────────────────────────────────────────────────────────┘

Keybindings:
  Tab / Shift+Tab    Cycle panels
  1-4                Jump to panel
  Ctrl+M             Toggle memory panel
  Ctrl+T             Toggle trace panel
```

### [SPEC-02.02] RLM Trace Panel

Display real-time RLM orchestration:

```
┌─ RLM Trace ─────────────────────────────────────────────────────┐
│ ▼ Query: "Implement user auth"                    depth: 0      │
│   ├─ Decompose: 3 subtasks                        tokens: 1.2K  │
│   │  ├─ [1/3] Design schema                       ✓ complete    │
│   │  │   └─ Sub-call: analyze existing models     tokens: 800   │
│   │  ├─ [2/3] Implement handlers                  ◐ running     │
│   │  │   └─ REPL: validate_schema(...)           executing...   │
│   │  └─ [3/3] Write tests                         ○ pending     │
│   └─ Synthesis: combining results...                            │
└─────────────────────────────────────────────────────────────────┘
```

Features:
- Tree view of decomposition hierarchy
- Real-time status updates (pending → running → complete/failed)
- Token usage per node
- REPL execution streaming
- Collapse/expand nodes
- Click to inspect details

### [SPEC-02.03] Memory Inspector Panel

Browse and search hypergraph memory:

```
┌─ Memory Inspector ──────────────────────────────────────────────┐
│ Search: [user auth_______________] [🔍] Filter: [All Tiers ▼]  │
├─────────────────────────────────────────────────────────────────┤
│ Results (12 nodes):                                             │
│                                                                 │
│ ● [fact] User authentication flow         session  conf: 0.92  │
│   "Users authenticate via JWT tokens..."                        │
│   Edges: → [code:auth.go] → [decision:jwt-vs-session]          │
│                                                                 │
│ ● [entity] AuthService                    longterm conf: 0.88  │
│   "Service handling user authentication..."                     │
│   Edges: → [fact:jwt-flow] → [code:service.go]                 │
│                                                                 │
│ ○ [decision] Use JWT over sessions        session  conf: 0.85  │
│   "Chose JWT for stateless auth..."                            │
└─────────────────────────────────────────────────────────────────┘
```

Features:
- Full-text + semantic search (hybrid)
- Filter by node type, tier, confidence
- Show connected edges
- Navigate relationships
- Edit/delete nodes (with confirmation)

### [SPEC-02.04] Budget Status Bar

Always-visible budget information:

```
┌─────────────────────────────────────────────────────────────────┐
│ Budget: ████████░░ 78% │ Today: 780K/1M │ Session: 45K │ $2.34 │
└─────────────────────────────────────────────────────────────────┘
```

Features:
- Progress bar with color coding (green → yellow → red)
- Daily limit tracking
- Session token count
- Estimated cost
- Click to expand detailed breakdown

### [SPEC-02.05] REPL Output View

Show Python REPL execution results:

```
┌─ REPL Output ───────────────────────────────────────────────────┐
│ >>> result = analyze_schema(context)                            │
│ Analyzing 15 files...                                           │
│ Found 3 models: User, Session, Token                            │
│                                                                 │
│ >>> result.summary()                                            │
│ {'models': 3, 'fields': 24, 'relations': 5}                    │
│                                                                 │
│ [execution time: 1.2s] [memory: 45MB]                          │
└─────────────────────────────────────────────────────────────────┘
```

Features:
- Syntax-highlighted code
- Streaming output
- Execution metrics
- Error highlighting
- Copy to clipboard

### [SPEC-02.06] Theming and Accessibility

- Dark/light mode support
- Configurable color schemes
- Accessible color contrasts
- Screen reader compatibility where possible
- Configurable keybindings

## Implementation Tasks

- [ ] Create panel manager with tab switching
- [ ] Implement RLM trace tree component
- [ ] Implement memory search/filter UI
- [ ] Add budget status bar component
- [ ] Create REPL output viewer
- [ ] Add keyboard navigation
- [ ] Implement panel resize/layout
- [ ] Add theming support
- [ ] Write component tests

## Dependencies

- `charm.land/bubbletea/v2` - TUI framework
- `charm.land/bubbles/v2` - UI components
- `charm.land/lipgloss/v2` - Styling
- `internal/rlm/` - Trace data
- `internal/memory/` - Memory queries

## Acceptance Criteria

1. All four panels render correctly
2. Keyboard navigation works smoothly
3. RLM trace updates in real-time
4. Memory search returns relevant results
5. Budget bar reflects actual usage
6. No visual glitches on terminal resize
