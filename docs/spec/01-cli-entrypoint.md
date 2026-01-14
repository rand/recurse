# SPEC-01: CLI Entry Point

**Status: COMPLETE**

> Wire up the `recurse` command to integrate all subsystems into a usable CLI.

## Overview

The CLI entry point connects all implemented subsystems (RLM orchestration, hypergraph memory, embeddings, Python REPL, budget tracking) into a cohesive command-line interface.

## Current State

**Implemented:**
- `cmd/recurse/main.go` - CLI entrypoint
- `internal/cmd/root.go` - Root command with all flags
- `internal/cmd/memory.go` - Memory subcommands (search, stats, gc, export)
- `internal/cmd/config.go` - Config subcommands (show, edit, validate, path)
- `internal/config/` - Configuration loading with merging logic
- Integration tests in `internal/cmd/*_test.go`
- Documentation in `docs/user/configuration.md`

## Requirements

### [SPEC-01.01] Command Structure

```
recurse [flags] [command]

Commands:
  (default)     Start interactive TUI session
  repl          Start Python REPL directly
  memory        Memory management commands
  config        Configuration management

Flags:
  --project     Project directory (default: cwd)
  --budget      Token budget limit
  --model       Primary model (default: claude-sonnet-4-20250514)
  --debug       Enable debug logging
  --no-tui      Run in non-interactive mode
```

### [SPEC-01.02] Initialization Sequence

1. Load configuration from:
   - `~/.config/recurse/config.yaml` (user defaults)
   - `.recurse.yaml` (project-specific)
   - Environment variables (`RECURSE_*`)
   - Command-line flags (highest priority)

2. Initialize subsystems in order:
   - Logger setup
   - SQLite database connection
   - Hypergraph store
   - Embedding provider (local CodeRankEmbed)
   - Budget tracker
   - Python REPL process
   - RLM orchestrator
   - TUI (if interactive)

3. Graceful shutdown:
   - Stop Python REPL
   - Close embedding server
   - Sync memory to disk
   - Close database

### [SPEC-01.03] Configuration Schema

```yaml
# ~/.config/recurse/config.yaml
model:
  primary: "claude-sonnet-4-20250514"
  meta: "claude-haiku-4-5-20251201"

budget:
  daily_limit: 1000000  # tokens
  warning_threshold: 0.8

memory:
  database: "~/.local/share/recurse/memory.db"
  embedding_model: "nomic-ai/CodeRankEmbed"

repl:
  timeout: 30s
  memory_limit: 512  # MB
```

### [SPEC-01.04] Environment Variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `RECURSE_MODEL` | Primary LLM model | claude-sonnet-4-20250514 |
| `RECURSE_DB` | Database path | ~/.local/share/recurse/memory.db |
| `EMBEDDING_PROVIDER` | local or voyage | local |
| `EMBEDDING_MODEL` | Model name | nomic-ai/CodeRankEmbed |
| `ANTHROPIC_API_KEY` | API key | (required) |

### [SPEC-01.05] Subcommands

#### `recurse memory`
```
recurse memory search <query>   Search memory
recurse memory stats            Show memory statistics
recurse memory gc               Run garbage collection
recurse memory export           Export to JSON
```

#### `recurse config`
```
recurse config show             Show effective config
recurse config edit             Open config in editor
recurse config validate         Validate configuration
```

## Implementation Tasks

- [x] Create config package with loading/merging logic
- [x] Wire up initialization sequence in main.go
- [x] Add graceful shutdown handling
- [x] Implement memory subcommand
- [x] Implement config subcommand
- [x] Add --debug flag with structured logging
- [x] Write integration tests
- [x] Document configuration schema and environment variables

## Dependencies

- `internal/rlm/` - RLM orchestration
- `internal/memory/` - Hypergraph and embeddings
- `internal/budget/` - Budget tracking
- `internal/tui/` - Terminal UI
- `pkg/python/` - REPL bootstrap

## Acceptance Criteria

1. `recurse` starts TUI with all subsystems initialized
2. `recurse --no-tui` works for scripting
3. Configuration loads from all sources with correct priority
4. Graceful shutdown preserves memory state
5. Memory subcommands work correctly
