# Recurse

> Recursive Language Model (RLM) system with hypergraph memory for agentic coding.

**FIRST ACTION**: Run `bd onboard` if you haven't already, then `bd ready --json` to see available work.

---

## Slash Commands

| Command | Purpose |
|---------|---------|
| `/start-session` | Orient: sync issues, check ready work, verify builds |
| `/end-session` | Wrap up: file issues, sync tracker, commit changes |
| `/plan <feature>` | Create structured implementation plan |
| `/code-review` | Review current changes against checklist |
| `/test <package>` | Run tests for a specific package |

## Hooks (Automatic)

- **session-start.sh**: Syncs issues, shows ready work when session begins
- **pre-compact.sh**: Syncs issues before conversation compaction

---

## Quick Reference

```bash
# Build & test
go build ./...
go test ./...

# Run
./recurse

# Issue tracking
bd ready                    # What's ready to work on
bd create "Title" -p 1      # Create issue (P0-P4)
bd update ID --status in_progress
bd close ID --reason "Done"
bd sync                     # Sync with git
```

---

## Architecture

```
recurse/
├── cmd/recurse/          # CLI entrypoint
├── internal/
│   ├── agent/            # Extended from Crush
│   ├── rlm/              # RLM orchestration (REPL, meta-controller)
│   ├── memory/           # Tiered hypergraph memory
│   ├── budget/           # Cost/limit management
│   └── tui/              # Bubble Tea extensions
└── pkg/python/           # Python REPL bootstrap
```

**Key components:**
- **RLM Controller**: Decomposes tasks, manages sub-LM calls, synthesizes results
- **Meta-Controller**: LLM (Claude Haiku 4.5) decides orchestration strategy
- **Hypergraph Memory**: SQLite-backed, three tiers (task → session → long-term)
- **Python REPL**: External process with uv/ruff/ty/pydantic

---

## Development Process

### Before Any Work

1. Run `bd ready --json` to find available tasks
2. Pick a task and update status: `bd update ID --status in_progress`
3. Read relevant docs in `docs/process/`

### During Work

- File issues for discovered bugs/tasks: `bd create "Issue" -t bug -p 1`
- Link related work: `bd dep add NEW_ID PARENT_ID --type discovered-from`
- Keep tests passing: `go test ./...`

### After Work

1. Run tests and linter: `go test ./... && golangci-lint run ./...`
2. Close completed issues: `bd close ID --reason "Description"`
3. Sync tracker: `bd sync`
4. Commit with conventional format: `git commit -m "feat(rlm): add decomposition"`

### Code Review Criteria

- **Correctness**: Does it work? Any bugs?
- **Performance**: Compiler perf, generated code quality
- **Design**: Idiomatic Go, follows existing patterns
- **Testing**: Adequate coverage for new functionality

Blocking issues must be fixed before commit. Non-blocking issues: file as `bd` issues.

---

## Coding Standards

### Go Style

- Follow [Effective Go](https://go.dev/doc/effective_go)
- Use `golangci-lint` with project config
- Error handling: wrap with context using `fmt.Errorf("operation: %w", err)`
- Interfaces: accept interfaces, return concrete types

### Testing

- Table-driven tests preferred
- Use `testify/assert` for assertions
- Integration tests in `_test.go` files with build tags

### Commits

- Conventional commits: `type(scope): description`
- Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`
- Scopes: `rlm`, `memory`, `tui`, `budget`, `agent`

---

## Key Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| REPL Environment | External Python (uv/ruff/ty) | Full ecosystem, sandboxed |
| Hypergraph Storage | SQLite extension | Simple, embedded, expressive enough |
| Meta-Controller | Claude Haiku 4.5 | Fast, cheap for orchestration decisions |
| Embedding Model | Voyage-3 or VoyageCode3 | Best quality for code + text |
| Memory Tiers | Task → Session → Long-term | Balance recency with persistence |
| Git Integration | Full (traces → commits, diffs) | Reasoning provenance |

---

## Critical Files

| File | Purpose |
|------|---------|
| `CLAUDE.md` | This file - agent instructions |
| `docs/SPEC.md` | Full technical specification |
| `docs/process/*.md` | Development workflows |
| `.beads/` | Issue tracking database |

---

## Memory System

Memory is per-project with optional cross-project queries.

**Tiers:**
- **Task**: Working memory for current problem (aggressive consolidation)
- **Session**: Accumulated context for coding session
- **Long-term**: Persistent knowledge (Ebbinghaus decay + access amplification)

**Evolution:**
- Task → Session: On task completion
- Session → Long-term: Session end with reflection pass
- Long-term pruning: Archive below threshold, never delete

---

## Related Documentation

- `docs/SPEC.md` - Complete technical specification
- `docs/process/planning.md` - How to plan complex features
- `docs/process/testing.md` - Testing strategy
- `docs/process/code-review.md` - Review checklist
