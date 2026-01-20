package rlm

// RLMCoreReplBridge provides a bridge between the Go REPL manager interface
// and rlm-core's ReplPool. When enabled (RLM_USE_CORE=true), it provides
// Python code execution via the rlm-core Rust implementation.

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/rand/recurse/internal/rlmcore"
)

// RLMCoreReplBridge wraps rlm-core REPL for use in the RLM service.
type RLMCoreReplBridge struct {
	bridge *rlmcore.RLMCoreReplBridge
}

// NewRLMCoreReplBridge creates a new REPL bridge using rlm-core.
// Returns nil, nil if rlm-core is not available.
func NewRLMCoreReplBridge(maxPoolSize int) (*RLMCoreReplBridge, error) {
	if !rlmcore.Available() {
		return nil, nil
	}

	bridge, err := rlmcore.NewRLMCoreReplBridge(maxPoolSize)
	if err != nil {
		return nil, fmt.Errorf("create repl bridge: %w", err)
	}

	slog.Info("rlm-core REPL bridge initialized", "max_pool_size", maxPoolSize)
	return &RLMCoreReplBridge{bridge: bridge}, nil
}

// Execute executes Python code and returns the result.
func (b *RLMCoreReplBridge) Execute(ctx context.Context, code string) (*RLMCoreExecuteResult, error) {
	result, err := b.bridge.Execute(ctx, code)
	if err != nil {
		return nil, err
	}
	return convertExecuteResult(result), nil
}

// AcquireHandle acquires a REPL handle for multi-step operations.
// The returned handle must be released with ReleaseHandle when done.
func (b *RLMCoreReplBridge) AcquireHandle() (*rlmcore.ReplHandle, error) {
	return b.bridge.AcquireHandle()
}

// ReleaseHandle releases a REPL handle back to the pool.
func (b *RLMCoreReplBridge) ReleaseHandle(h *rlmcore.ReplHandle) {
	b.bridge.ReleaseHandle(h)
}

// ExecuteWithHandle executes code using a specific handle.
func (b *RLMCoreReplBridge) ExecuteWithHandle(ctx context.Context, handle *rlmcore.ReplHandle, code string) (*RLMCoreExecuteResult, error) {
	result, err := b.bridge.ExecuteWithHandle(ctx, handle, code)
	if err != nil {
		return nil, err
	}
	return convertExecuteResult(result), nil
}

// Close releases all resources.
func (b *RLMCoreReplBridge) Close() error {
	if b.bridge != nil {
		return b.bridge.Close()
	}
	return nil
}

// RLMCoreExecuteResult represents the result of REPL code execution.
// This is a Go-friendly version of rlmcore.ExecuteResult.
type RLMCoreExecuteResult struct {
	Success           bool
	Result            any
	Stdout            string
	Stderr            string
	Error             string
	ErrorType         string
	ExecutionTimeMs   float64
	PendingOperations []string
}

// convertExecuteResult converts rlmcore.ExecuteResult to RLMCoreExecuteResult.
func convertExecuteResult(r *rlmcore.ExecuteResult) *RLMCoreExecuteResult {
	result := &RLMCoreExecuteResult{
		Success:           r.Success,
		Result:            r.Result,
		Stdout:            r.Stdout,
		Stderr:            r.Stderr,
		ExecutionTimeMs:   r.ExecutionTimeMs,
		PendingOperations: r.PendingOperations,
	}
	if r.Error != nil {
		result.Error = *r.Error
	}
	if r.ErrorType != nil {
		result.ErrorType = *r.ErrorType
	}
	return result
}

// UseRLMCoreRepl returns true if rlm-core REPL should be used.
func UseRLMCoreRepl() bool {
	return rlmcore.Available()
}
