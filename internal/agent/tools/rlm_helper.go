package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/rand/recurse/internal/rlm/repl"
)

// REPLStartTimeout is the timeout for lazy REPL activation.
const REPLStartTimeout = 10 * time.Second

// ensureREPLRunning checks if the REPL is running and attempts to start it if not.
// Returns nil if REPL is running (or was successfully started), error otherwise.
func ensureREPLRunning(ctx context.Context, replManager *repl.Manager) error {
	if replManager == nil {
		return fmt.Errorf("REPL manager not available. Python REPL could not be initialized. " +
			"Check that Python 3 is installed: `which python3`")
	}

	if replManager.Running() {
		return nil
	}

	// Attempt to start the REPL with timeout
	startCtx, cancel := context.WithTimeout(ctx, REPLStartTimeout)
	defer cancel()

	if err := replManager.Start(startCtx); err != nil {
		return fmt.Errorf("REPL not running and failed to start: %w. "+
			"Check that Python 3 is installed (`which python3`) and "+
			"the bootstrap script is accessible", err)
	}

	return nil
}
