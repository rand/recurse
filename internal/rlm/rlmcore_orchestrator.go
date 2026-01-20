package rlm

// RLMCoreOrchestratorBridge provides a bridge between the Go orchestrator
// and rlm-core's orchestrator configuration. When enabled (RLM_USE_CORE=true),
// it provides shared configuration and mode selection logic.
//
// The Go orchestrator modules handle:
// - Core execution loop with LLM calls
// - Intelligent analysis and task routing
// - Steering and context externalization
// - Checkpointing
//
// The rlm-core orchestrator module provides:
// - ExecutionMode: Mode selection logic (Micro, Fast, Balanced, Thorough)
// - OrchestratorConfig: Shared configuration (budgets, timeouts, depths)
// - ComplexitySignals: Signal-based mode selection
//
// This bridge integrates them by using rlm-core for configuration
// and mode selection while keeping LLM-dependent execution in Go.

import (
	"log/slog"

	"github.com/rand/recurse/internal/rlmcore"
)

// RLMCoreOrchestratorBridge wraps rlm-core orchestrator functions.
type RLMCoreOrchestratorBridge struct {
	config *rlmcore.OrchestratorConfig
	mode   rlmcore.ExecutionMode
}

// OrchestratorMode represents the rlm-core execution mode (Micro/Fast/Balanced/Thorough).
// This is distinct from ExecutionMode which is "direct" vs "rlm".
type OrchestratorMode = rlmcore.ExecutionMode

// ComplexitySignals is an alias for the rlmcore type.
type ComplexitySignals = rlmcore.ComplexitySignals

// Orchestrator mode constants (Micro/Fast/Balanced/Thorough)
const (
	OrchestratorModeMicro    = rlmcore.ExecutionModeMicro
	OrchestratorModeFast     = rlmcore.ExecutionModeFast
	OrchestratorModeBalanced = rlmcore.ExecutionModeBalanced
	OrchestratorModeThorough = rlmcore.ExecutionModeThorough
)

// NewRLMCoreOrchestratorBridge creates a new orchestrator bridge using rlm-core.
// Returns nil, nil if rlm-core is not available.
func NewRLMCoreOrchestratorBridge() (*RLMCoreOrchestratorBridge, error) {
	if !rlmcore.Available() {
		return nil, nil
	}

	config, err := rlmcore.NewOrchestratorConfigDefault()
	if err != nil {
		return nil, err
	}

	slog.Info("rlm-core orchestrator bridge initialized")
	return &RLMCoreOrchestratorBridge{
		config: config,
		mode:   rlmcore.ExecutionModeBalanced,
	}, nil
}

// NewRLMCoreOrchestratorBridgeWithConfig creates a bridge with custom config JSON.
func NewRLMCoreOrchestratorBridgeWithConfig(configJSON string) (*RLMCoreOrchestratorBridge, error) {
	if !rlmcore.Available() {
		return nil, nil
	}

	config, err := rlmcore.NewOrchestratorConfigFromJSON(configJSON)
	if err != nil {
		return nil, err
	}

	slog.Info("rlm-core orchestrator bridge initialized with custom config")
	return &RLMCoreOrchestratorBridge{
		config: config,
		mode:   rlmcore.ExecutionModeBalanced,
	}, nil
}

// SelectMode selects orchestrator mode based on complexity signals.
func (b *RLMCoreOrchestratorBridge) SelectMode(signals *ComplexitySignals) OrchestratorMode {
	mode, err := rlmcore.ExecutionModeFromSignals(signals)
	if err != nil {
		return rlmcore.ExecutionModeMicro
	}
	b.mode = mode
	return mode
}

// GetMode returns the current orchestrator mode.
func (b *RLMCoreOrchestratorBridge) GetMode() OrchestratorMode {
	return b.mode
}

// SetMode sets the orchestrator mode directly.
func (b *RLMCoreOrchestratorBridge) SetMode(mode OrchestratorMode) {
	b.mode = mode
}

// GetModeBudgetUSD returns the budget for the current mode.
func (b *RLMCoreOrchestratorBridge) GetModeBudgetUSD() float64 {
	return b.mode.BudgetUSD()
}

// GetModeMaxDepth returns the max depth for the current mode.
func (b *RLMCoreOrchestratorBridge) GetModeMaxDepth() uint32 {
	return b.mode.MaxDepth()
}

// GetModeName returns the name of the current mode.
func (b *RLMCoreOrchestratorBridge) GetModeName() string {
	return b.mode.String()
}

// Config returns the orchestrator configuration.
func (b *RLMCoreOrchestratorBridge) Config() *rlmcore.OrchestratorConfig {
	return b.config
}

// MaxDepth returns the max depth from config.
func (b *RLMCoreOrchestratorBridge) MaxDepth() uint32 {
	return b.config.MaxDepth()
}

// DefaultSpawnREPL returns whether REPL spawning is enabled.
func (b *RLMCoreOrchestratorBridge) DefaultSpawnREPL() bool {
	return b.config.DefaultSpawnREPL()
}

// REPLTimeoutMs returns the REPL timeout in milliseconds.
func (b *RLMCoreOrchestratorBridge) REPLTimeoutMs() uint64 {
	return b.config.REPLTimeoutMs()
}

// MaxTokensPerCall returns the max tokens per call.
func (b *RLMCoreOrchestratorBridge) MaxTokensPerCall() uint64 {
	return b.config.MaxTokensPerCall()
}

// TotalTokenBudget returns the total token budget.
func (b *RLMCoreOrchestratorBridge) TotalTokenBudget() uint64 {
	return b.config.TotalTokenBudget()
}

// CostBudgetUSD returns the cost budget in USD.
func (b *RLMCoreOrchestratorBridge) CostBudgetUSD() float64 {
	return b.config.CostBudgetUSD()
}

// ConfigToJSON serializes the config to JSON.
func (b *RLMCoreOrchestratorBridge) ConfigToJSON() (string, error) {
	return b.config.ToJSON()
}

// Close releases all resources.
func (b *RLMCoreOrchestratorBridge) Close() error {
	if b.config != nil {
		b.config.Free()
		b.config = nil
	}
	return nil
}

// UseRLMCoreOrchestrator returns true if rlm-core orchestrator should be used.
func UseRLMCoreOrchestrator() bool {
	return rlmcore.Available()
}

// BuildOrchestratorConfig creates a config using the builder pattern.
func BuildOrchestratorConfig(opts ...OrchestratorOption) (*rlmcore.OrchestratorConfig, error) {
	if !rlmcore.Available() {
		return nil, rlmcore.ErrNotAvailable
	}

	builder, err := rlmcore.NewOrchestratorBuilder()
	if err != nil {
		return nil, err
	}

	for _, opt := range opts {
		opt(builder)
	}

	return builder.Build(), nil
}

// OrchestratorOption is a functional option for building orchestrator config.
type OrchestratorOption func(*rlmcore.OrchestratorBuilder)

// WithMaxDepth sets the max recursion depth.
func WithMaxDepth(depth uint32) OrchestratorOption {
	return func(b *rlmcore.OrchestratorBuilder) {
		b.MaxDepth(depth)
	}
}

// WithDefaultSpawnREPL sets whether to spawn REPL by default.
func WithDefaultSpawnREPL(spawn bool) OrchestratorOption {
	return func(b *rlmcore.OrchestratorBuilder) {
		b.DefaultSpawnREPL(spawn)
	}
}

// WithREPLTimeout sets the REPL timeout in milliseconds.
func WithREPLTimeout(timeout uint64) OrchestratorOption {
	return func(b *rlmcore.OrchestratorBuilder) {
		b.REPLTimeoutMs(timeout)
	}
}

// WithTotalTokenBudget sets the total token budget.
func WithTotalTokenBudget(budget uint64) OrchestratorOption {
	return func(b *rlmcore.OrchestratorBuilder) {
		b.TotalTokenBudget(budget)
	}
}

// WithCostBudget sets the cost budget in USD.
func WithCostBudget(budget float64) OrchestratorOption {
	return func(b *rlmcore.OrchestratorBuilder) {
		b.CostBudgetUSD(budget)
	}
}

// WithOrchestratorMode sets the orchestrator mode.
func WithOrchestratorMode(mode OrchestratorMode) OrchestratorOption {
	return func(b *rlmcore.OrchestratorBuilder) {
		b.ExecutionMode(mode)
	}
}
