# Planning Process

This document describes how to plan complex features and changes in Recurse.

---

## Before You Start

1. **Check existing issues**: `bd list --status open` 
2. **Understand the scope**: Read relevant docs in `docs/` and code in `internal/`
3. **Identify dependencies**: What must exist before this work can happen?

---

## Planning a Feature

### 1. Create an Epic Issue

For significant features, create an epic to track the overall effort:

```bash
bd create "Epic: Implement RLM orchestration" -t epic -p 1
```

### 2. Break Down into Tasks

Decompose the epic into discrete, testable tasks:

```bash
# Create child tasks linked to the epic
bd create "Implement Python REPL manager" -p 1
bd dep add CHILD_ID EPIC_ID --type parent-child

bd create "Add sandbox constraints" -p 2
bd dep add CHILD_ID EPIC_ID --type parent-child
```

### 3. Identify Blockers

Mark dependencies between tasks:

```bash
# Task B depends on Task A completing first
bd dep add TASK_B TASK_A --type blocks
```

### 4. Write a Design Doc (Optional)

For complex features, create a design document in `docs/design/`:

```markdown
# Design: Feature Name

## Problem
What problem are we solving?

## Proposed Solution
High-level approach.

## Alternatives Considered
What else did we consider and why did we reject it?

## Implementation Plan
Ordered list of steps.

## Testing Strategy
How will we verify correctness?

## Open Questions
Things we need to figure out.
```

---

## Task Estimation Guidelines

| Priority | Scope | Expected Effort |
|----------|-------|-----------------|
| P0 | Critical blocker | Fix immediately |
| P1 | Core functionality | 1-2 days |
| P2 | Important feature | 3-5 days |
| P3 | Nice to have | 1+ week |
| P4 | Future consideration | Backlog |

---

## Working on a Task

1. **Pick from ready work**: `bd ready --json | jq '.[0]'`
2. **Mark as in progress**: `bd update ID --status in_progress`
3. **Discover new work?**: File it immediately:
   ```bash
   bd create "Found: need to handle edge case X" -t bug -p 2
   bd dep add NEW_ID CURRENT_ID --type discovered-from
   ```
4. **Complete the task**: `bd close ID --reason "Implemented X, added Y tests"`

---

## Dependency Visualization

See the full dependency graph for a task:

```bash
bd dep tree TASK_ID
```

Example output:
```
bd-a1b2 [epic] Implement RLM orchestration
├── bd-f14c [task] Implement Python REPL manager (in_progress)
│   ├── bd-3e7a [task] Add sandbox constraints (open)
│   └── bd-8b2c [task] Implement JSON-RPC protocol (open)
├── bd-9d4e [task] Meta-controller integration (blocked)
│   └── blocks: bd-f14c
└── bd-1a5f [task] Result synthesis (open)
```

---

## Best Practices

1. **Small, focused tasks**: Each task should be completable in one session
2. **Clear acceptance criteria**: Know when a task is "done"
3. **Link related work**: Use `bd dep add` liberally
4. **File discovered work immediately**: Don't let observations get lost
5. **Update status honestly**: Helps with planning and visibility
6. **Close with context**: The `--reason` helps future debugging
