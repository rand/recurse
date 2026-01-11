# Beads Issue Tracker

This project uses [beads](https://github.com/steveyegge/beads) for issue tracking.

## Quick Reference

```bash
# Find ready work
bd ready

# Create an issue
bd create "Title" -p 1 -t bug

# Update status
bd update ID --status in_progress

# Close an issue
bd close ID --reason "Fixed"

# Sync with git
bd sync

# See dependency tree
bd dep tree ID
```

## Files

- `beads.jsonl` - Issue data (committed to git)
- `deletions.jsonl` - Deletion manifest
- `config.yaml` - Repository configuration
- `beads.db` - Local SQLite cache (gitignored)

## For AI Agents

Run `bd onboard` at the start of each session to get integration instructions.

Always use `bd` instead of markdown TODO lists for tracking work.
