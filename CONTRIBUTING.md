# Contributing to Recurse

This is a personal project, but contributions are welcome.

## Development Setup

### Prerequisites

- Go 1.22+
- Python 3.10+ with uv (`pip install uv`)
- SQLite 3.40+
- Node.js 20+ (for Bubble Tea TUI development)

### Initial Setup

```bash
# Clone the repository
git clone https://github.com/your-username/recurse.git
cd recurse

# Install beads
curl -fsSL https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh | bash
bd init --quiet

# Make Claude Code hooks executable
chmod +x .claude/hooks/*.sh

# Build
go build ./...

# Run tests
go test ./...

# Install pre-commit hooks
cp scripts/pre-commit .git/hooks/
chmod +x .git/hooks/pre-commit
```

### Python REPL Setup

```bash
# Create virtual environment with uv
cd pkg/python
uv venv
source .venv/bin/activate
uv pip install -r requirements.txt
```

## Development Workflow

### Starting Work

1. Run `/start-session` to orient yourself
2. Pick a task from `bd ready`
3. Mark it in progress: `bd update ID --status in_progress`

### During Work

- File discovered issues immediately with `bd create`
- Keep tests passing
- Follow the coding standards in `CLAUDE.md`

### Finishing Work

1. Run `/end-session` to wrap up
2. Ensure tests pass: `go test ./...`
3. Run linter: `golangci-lint run ./...`
4. Commit with conventional format
5. Sync issues: `bd sync`

## Code Style

- Follow [Effective Go](https://go.dev/doc/effective_go)
- Use `golangci-lint` with the project configuration
- Write table-driven tests
- Document public APIs with godoc comments

## Commit Messages

Use conventional commits:

```
type(scope): description

[optional body]

[optional footer]
```

Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`

Scopes: `rlm`, `memory`, `tui`, `budget`, `agent`

Examples:
```
feat(memory): add hypergraph node creation
fix(rlm): handle REPL timeout correctly
docs(readme): update installation instructions
```

## Issue Tracking

Use `bd` (beads) for all issue tracking:

```bash
# Create issue
bd create "Title" -t bug -p 1

# Link to parent
bd dep add CHILD_ID PARENT_ID --type parent-child

# Mark blocker
bd dep add BLOCKED_ID BLOCKER_ID --type blocks

# Close with context
bd close ID --reason "Description of what was done"
```

**Never use markdown TODO lists.** Always file as `bd` issues.

## Code Review

Before submitting:

1. Run `/code-review` to self-review
2. Fix all blocking issues
3. File non-blocking issues to `bd`
4. Ensure CI passes
