// Package rlmcore provides bindings to the rlm-core Rust library.
//
// This package wraps the rlm-core Go bindings, enabling:
// - SqliteMemoryStore for hypergraph memory
// - PatternClassifier for complexity detection
// - TrajectoryEvent for observability
//
// Set RLM_USE_CORE=true to enable these bindings.
package rlmcore

import (
	"github.com/rand/recurse/internal/config"
	core "github.com/rand/rlm-core/go/rlmcore"
)

var initialized bool

// Available returns true if rlm-core bindings are available and enabled.
func Available() bool {
	return config.UseRlmCore() && ensureInit()
}

// ensureInit initializes the rlm-core library if not already done.
func ensureInit() bool {
	if initialized {
		return true
	}
	if err := core.Init(); err != nil {
		return false
	}
	initialized = true
	return true
}

// Version returns the rlm-core library version.
func Version() (string, error) {
	if !ensureInit() {
		return "", ErrNotAvailable
	}
	return core.Version(), nil
}

// Shutdown cleans up rlm-core resources.
func Shutdown() {
	if initialized {
		core.Shutdown()
		initialized = false
	}
}
