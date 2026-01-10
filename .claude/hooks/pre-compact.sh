#!/bin/bash
# .claude/hooks/pre-compact.sh
# Runs before Claude Code compacts the conversation

# Sync issues to ensure state is preserved
if command -v bd &> /dev/null; then
    bd sync 2>/dev/null || true
fi
