// Package rlm implements the Recursive Language Model orchestration system.
//
// This file provides backwards-compatible exports from the modular orchestrator package.
// New code should import github.com/rand/recurse/internal/rlm/orchestrator directly.
package rlm

import (
	"context"

	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/rlm/meta"
	"github.com/rand/recurse/internal/rlm/orchestrator"
)

// Re-export types from orchestrator package for backwards compatibility.
type (
	ExecutionResult = orchestrator.ExecutionResult
	TraceEvent      = orchestrator.TraceEvent
	TraceRecorder   = orchestrator.TraceRecorder
)

// Controller orchestrates RLM operations with integrated memory.
// This wraps the modular orchestrator.Core type.
type Controller struct {
	core *orchestrator.Core
}

// ControllerConfig configures the RLM controller.
type ControllerConfig struct {
	// MaxTokenBudget is the maximum tokens per request.
	MaxTokenBudget int

	// MaxRecursionDepth limits decomposition depth.
	MaxRecursionDepth int

	// MemoryQueryLimit is max results from memory queries.
	MemoryQueryLimit int

	// StoreDecisions persists decisions to memory graph.
	StoreDecisions bool

	// TraceEnabled enables trace recording.
	TraceEnabled bool

	// Recovery configures error recovery behavior.
	Recovery RecoveryConfig

	// EnableAsyncExecution enables parallel execution for decomposition.
	EnableAsyncExecution bool

	// MaxParallelOps is the maximum concurrent operations (default: 4).
	MaxParallelOps int
}

// DefaultControllerConfig returns sensible defaults.
func DefaultControllerConfig() ControllerConfig {
	return ControllerConfig{
		MaxTokenBudget:    100000,
		MaxRecursionDepth: 5,
		MemoryQueryLimit:  10,
		StoreDecisions:    true,
		TraceEnabled:      true,
		Recovery:          DefaultRecoveryConfig(),
	}
}

// NewController creates a new RLM controller with memory integration.
func NewController(
	metaCtrl *meta.Controller,
	mainClient meta.LLMClient,
	store *hypergraph.Store,
	cfg ControllerConfig,
) *Controller {
	return &Controller{
		core: orchestrator.NewCore(metaCtrl, mainClient, store, orchestrator.CoreConfig{
			MaxTokenBudget:       cfg.MaxTokenBudget,
			MaxRecursionDepth:    cfg.MaxRecursionDepth,
			MemoryQueryLimit:     cfg.MemoryQueryLimit,
			StoreDecisions:       cfg.StoreDecisions,
			TraceEnabled:         cfg.TraceEnabled,
			Recovery:             toOrchestratorRecoveryConfig(cfg.Recovery),
			EnableAsyncExecution: cfg.EnableAsyncExecution,
			MaxParallelOps:       cfg.MaxParallelOps,
		}),
	}
}

// SetTracer sets the trace recorder.
func (c *Controller) SetTracer(tracer TraceRecorder) {
	c.core.SetTracer(tracer)
}

// Tracer returns the trace recorder.
func (c *Controller) Tracer() TraceRecorder {
	return c.core.Tracer()
}

// Execute runs the RLM orchestration loop for a task.
func (c *Controller) Execute(ctx context.Context, task string) (*ExecutionResult, error) {
	return c.core.Execute(ctx, task)
}

// Core returns the underlying orchestrator.Core for direct access.
func (c *Controller) Core() *orchestrator.Core {
	return c.core
}

// toOrchestratorRecoveryConfig converts the local RecoveryConfig to orchestrator.RecoveryConfig.
func toOrchestratorRecoveryConfig(cfg RecoveryConfig) orchestrator.RecoveryConfig {
	return orchestrator.RecoveryConfig{
		MaxRetries:        cfg.MaxRetries,
		RetryDelay:        cfg.RetryDelay,
		EnableDegradation: cfg.EnableDegradation,
		LogErrors:         cfg.LogErrors,
	}
}
