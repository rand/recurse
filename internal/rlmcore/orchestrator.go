package rlmcore

import (
	core "github.com/rand/rlm-core/go/rlmcore"
)

// ExecutionMode represents the orchestration execution mode.
// When RLM_USE_CORE=true, mode selection uses rlm-core's logic.
type ExecutionMode = core.ExecutionMode

// Mode constants
const (
	ExecutionModeMicro    = core.ExecutionModeMicro
	ExecutionModeFast     = core.ExecutionModeFast
	ExecutionModeBalanced = core.ExecutionModeBalanced
	ExecutionModeThorough = core.ExecutionModeThorough
)

// ComplexitySignals contains signals used for execution mode selection.
type ComplexitySignals = core.ComplexitySignals

// OrchestratorConfig contains configuration for the orchestrator.
type OrchestratorConfig struct {
	inner *core.OrchestratorConfig
}

// OrchestratorBuilder builds orchestrator configurations.
type OrchestratorBuilder struct {
	inner *core.OrchestratorBuilder
}

// ExecutionModeFromSignals selects execution mode based on complexity signals.
func ExecutionModeFromSignals(signals *ComplexitySignals) (ExecutionMode, error) {
	if !Available() {
		return ExecutionModeMicro, ErrNotAvailable
	}
	return core.ExecutionModeFromSignals(signals), nil
}

// NewOrchestratorConfigDefault creates a config with default values.
func NewOrchestratorConfigDefault() (*OrchestratorConfig, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner := core.NewOrchestratorConfigDefault()
	return &OrchestratorConfig{inner: inner}, nil
}

// NewOrchestratorConfigFromJSON creates a config from JSON.
func NewOrchestratorConfigFromJSON(jsonStr string) (*OrchestratorConfig, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner, err := core.NewOrchestratorConfigFromJSON(jsonStr)
	if err != nil {
		return nil, err
	}
	return &OrchestratorConfig{inner: inner}, nil
}

// Free releases the config resources.
func (c *OrchestratorConfig) Free() {
	if c.inner != nil {
		c.inner.Free()
		c.inner = nil
	}
}

// MaxDepth returns the maximum recursion depth.
func (c *OrchestratorConfig) MaxDepth() uint32 {
	return c.inner.MaxDepth()
}

// DefaultSpawnREPL returns whether REPL spawning is enabled by default.
func (c *OrchestratorConfig) DefaultSpawnREPL() bool {
	return c.inner.DefaultSpawnREPL()
}

// REPLTimeoutMs returns the REPL timeout in milliseconds.
func (c *OrchestratorConfig) REPLTimeoutMs() uint64 {
	return c.inner.REPLTimeoutMs()
}

// MaxTokensPerCall returns the max tokens per call.
func (c *OrchestratorConfig) MaxTokensPerCall() uint64 {
	return c.inner.MaxTokensPerCall()
}

// TotalTokenBudget returns the total token budget.
func (c *OrchestratorConfig) TotalTokenBudget() uint64 {
	return c.inner.TotalTokenBudget()
}

// CostBudgetUSD returns the cost budget in USD.
func (c *OrchestratorConfig) CostBudgetUSD() float64 {
	return c.inner.CostBudgetUSD()
}

// ToJSON serializes the config to JSON.
func (c *OrchestratorConfig) ToJSON() (string, error) {
	return c.inner.ToJSON()
}

// NewOrchestratorBuilder creates a new builder with default values.
func NewOrchestratorBuilder() (*OrchestratorBuilder, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner := core.NewOrchestratorBuilder()
	return &OrchestratorBuilder{inner: inner}, nil
}

// Free releases the builder resources.
func (b *OrchestratorBuilder) Free() {
	if b.inner != nil {
		b.inner.Free()
		b.inner = nil
	}
}

// MaxDepth sets the maximum recursion depth.
func (b *OrchestratorBuilder) MaxDepth(depth uint32) *OrchestratorBuilder {
	b.inner.MaxDepth(depth)
	return b
}

// DefaultSpawnREPL sets whether to spawn REPL by default.
func (b *OrchestratorBuilder) DefaultSpawnREPL(spawn bool) *OrchestratorBuilder {
	b.inner.DefaultSpawnREPL(spawn)
	return b
}

// REPLTimeoutMs sets the REPL timeout in milliseconds.
func (b *OrchestratorBuilder) REPLTimeoutMs(timeout uint64) *OrchestratorBuilder {
	b.inner.REPLTimeoutMs(timeout)
	return b
}

// TotalTokenBudget sets the total token budget.
func (b *OrchestratorBuilder) TotalTokenBudget(budget uint64) *OrchestratorBuilder {
	b.inner.TotalTokenBudget(budget)
	return b
}

// CostBudgetUSD sets the cost budget in USD.
func (b *OrchestratorBuilder) CostBudgetUSD(budget float64) *OrchestratorBuilder {
	b.inner.CostBudgetUSD(budget)
	return b
}

// ExecutionMode sets the execution mode.
func (b *OrchestratorBuilder) ExecutionMode(mode ExecutionMode) *OrchestratorBuilder {
	b.inner.ExecutionMode(mode)
	return b
}

// Build creates the config from the builder.
func (b *OrchestratorBuilder) Build() *OrchestratorConfig {
	inner := b.inner.Build()
	b.inner = nil // consumed
	return &OrchestratorConfig{inner: inner}
}

// GetMode returns the current execution mode.
func (b *OrchestratorBuilder) GetMode() ExecutionMode {
	return b.inner.GetMode()
}
