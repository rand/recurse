package rlmcore

import (
	"context"
	"fmt"
	"sync"

	core "github.com/rand/rlm-core/go/rlmcore"
)

// ReplHandle wraps the rlm-core ReplHandle.
type ReplHandle struct {
	inner *core.ReplHandle
	mu    sync.Mutex
}

// ReplPool wraps the rlm-core ReplPool.
type ReplPool struct {
	inner *core.ReplPool
}

// ReplConfig wraps the rlm-core ReplConfig.
type ReplConfig = core.ReplConfig

// ExecuteResult wraps the rlm-core ExecuteResult.
type ExecuteResult = core.ExecuteResult

// ReplStatus wraps the rlm-core ReplStatus.
type ReplStatus = core.ReplStatus

// DefaultReplConfig returns the default REPL configuration.
func DefaultReplConfig() (*ReplConfig, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	return core.DefaultReplConfig()
}

// SpawnReplDefault spawns a new REPL subprocess with default configuration.
func SpawnReplDefault() (*ReplHandle, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner, err := core.SpawnReplDefault()
	if err != nil {
		return nil, err
	}
	return &ReplHandle{inner: inner}, nil
}

// SpawnRepl spawns a new REPL subprocess with custom configuration.
func SpawnRepl(config *ReplConfig) (*ReplHandle, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner, err := core.SpawnRepl(config)
	if err != nil {
		return nil, err
	}
	return &ReplHandle{inner: inner}, nil
}

// Execute executes Python code in the REPL.
// The context is for cancellation compatibility but rlm-core handles timeouts internally.
func (h *ReplHandle) Execute(ctx context.Context, code string) (*ExecuteResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check context before executing
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	return h.inner.Execute(code)
}

// GetVariable gets a variable from the REPL namespace.
func (h *ReplHandle) GetVariable(name string) (any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.inner.GetVariable(name)
}

// SetVariable sets a variable in the REPL namespace.
func (h *ReplHandle) SetVariable(name string, value any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.inner.SetVariable(name, value)
}

// ResolveOperation resolves a deferred operation.
func (h *ReplHandle) ResolveOperation(operationID string, result any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.inner.ResolveOperation(operationID, result)
}

// ListVariables lists all variables in the REPL namespace.
func (h *ReplHandle) ListVariables() (map[string]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.inner.ListVariables()
}

// Status returns the REPL status.
func (h *ReplHandle) Status() (*ReplStatus, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.inner.Status()
}

// Reset resets the REPL state.
func (h *ReplHandle) Reset() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.inner.Reset()
}

// Shutdown shuts down the REPL subprocess.
func (h *ReplHandle) Shutdown() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.inner.Shutdown()
}

// IsAlive checks if the REPL subprocess is still running.
func (h *ReplHandle) IsAlive() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.inner.IsAlive()
}

// Free releases the REPL handle resources.
func (h *ReplHandle) Free() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.inner != nil {
		h.inner.Free()
		h.inner = nil
	}
}

// NewReplPoolDefault creates a new REPL pool with default configuration.
func NewReplPoolDefault(maxSize int) (*ReplPool, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner := core.NewReplPoolDefault(maxSize)
	return &ReplPool{inner: inner}, nil
}

// NewReplPool creates a new REPL pool with custom configuration.
func NewReplPool(config *ReplConfig, maxSize int) (*ReplPool, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner, err := core.NewReplPool(config, maxSize)
	if err != nil {
		return nil, err
	}
	return &ReplPool{inner: inner}, nil
}

// Acquire acquires a REPL handle from the pool.
func (p *ReplPool) Acquire() (*ReplHandle, error) {
	inner, err := p.inner.Acquire()
	if err != nil {
		return nil, err
	}
	return &ReplHandle{inner: inner}, nil
}

// Release releases a REPL handle back to the pool.
func (p *ReplPool) Release(h *ReplHandle) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.inner != nil {
		p.inner.Release(h.inner)
		h.inner = nil
	}
}

// Free releases the REPL pool resources.
func (p *ReplPool) Free() {
	if p.inner != nil {
		p.inner.Free()
		p.inner = nil
	}
}

// RLMCoreReplBridge adapts rlm-core ReplHandle to match the repl.Manager interface.
// This allows using rlm-core REPL alongside the existing Go implementation.
type RLMCoreReplBridge struct {
	pool   *ReplPool
	handle *ReplHandle
	mu     sync.Mutex
}

// NewRLMCoreReplBridge creates a new REPL bridge using rlm-core.
func NewRLMCoreReplBridge(maxPoolSize int) (*RLMCoreReplBridge, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	pool, err := NewReplPoolDefault(maxPoolSize)
	if err != nil {
		return nil, fmt.Errorf("create repl pool: %w", err)
	}
	return &RLMCoreReplBridge{pool: pool}, nil
}

// NewRLMCoreReplBridgeWithConfig creates a new REPL bridge with custom configuration.
func NewRLMCoreReplBridgeWithConfig(config *ReplConfig, maxPoolSize int) (*RLMCoreReplBridge, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	pool, err := NewReplPool(config, maxPoolSize)
	if err != nil {
		return nil, fmt.Errorf("create repl pool: %w", err)
	}
	return &RLMCoreReplBridge{pool: pool}, nil
}

// AcquireHandle acquires a REPL handle from the pool for use.
// Must call ReleaseHandle when done.
func (b *RLMCoreReplBridge) AcquireHandle() (*ReplHandle, error) {
	return b.pool.Acquire()
}

// ReleaseHandle releases a REPL handle back to the pool.
func (b *RLMCoreReplBridge) ReleaseHandle(h *ReplHandle) {
	b.pool.Release(h)
}

// Execute executes Python code using a pooled handle.
// This acquires a handle, executes, and releases it.
func (b *RLMCoreReplBridge) Execute(ctx context.Context, code string) (*ExecuteResult, error) {
	handle, err := b.pool.Acquire()
	if err != nil {
		return nil, fmt.Errorf("acquire handle: %w", err)
	}
	defer b.pool.Release(handle)

	return handle.Execute(ctx, code)
}

// ExecuteWithHandle executes Python code using a specific handle.
// Use this for multi-step operations that need state preservation.
func (b *RLMCoreReplBridge) ExecuteWithHandle(ctx context.Context, handle *ReplHandle, code string) (*ExecuteResult, error) {
	return handle.Execute(ctx, code)
}

// Close releases all resources.
func (b *RLMCoreReplBridge) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.handle != nil {
		b.pool.Release(b.handle)
		b.handle = nil
	}
	if b.pool != nil {
		b.pool.Free()
		b.pool = nil
	}
	return nil
}
