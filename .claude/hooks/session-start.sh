#!/bin/bash
# .claude/hooks/session-start.sh
# Runs automatically when a Claude Code session starts

set -e

echo "🔄 Starting Recurse session..."

# Ensure bd is available
if ! command -v bd &> /dev/null; then
    echo "⚠️  bd not found. Install with: curl -fsSL https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh | bash"
    exit 0
fi

# Sync issues from git
echo "📥 Syncing issues..."
bd sync 2>/dev/null || true

# Show ready work
echo ""
echo "📋 Ready work:"
bd ready --limit 5 2>/dev/null || echo "  (no issues yet)"

echo ""
echo "✅ Session ready. Use /plan, /code-review, or /end-session commands."
