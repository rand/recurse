package rlm

import (
	"github.com/rand/recurse/internal/rlm/orchestrator"
	"github.com/rand/recurse/internal/rlm/repl"
)

// ContextLoader is an alias for orchestrator.ContextLoader for backwards compatibility.
type ContextLoader = orchestrator.ContextLoader

// NewContextLoader creates a new context loader.
func NewContextLoader(replMgr *repl.Manager) *ContextLoader {
	return orchestrator.NewContextLoader(replMgr)
}

// estimateTokens returns a rough estimate of token count for text.
func estimateTokens(text string) int {
	return len(text) / 4
}

// truncate truncates a string to the given max length.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
