package orchestrator

import (
	"time"

	"github.com/rand/recurse/internal/rlm/async"
)

// AsyncConfig configures async execution behavior.
type AsyncConfig struct {
	// Enabled controls whether async execution is active.
	Enabled bool

	// MaxParallel is the maximum concurrent operations.
	MaxParallel int

	// TimeoutPerOp is the default timeout per operation.
	TimeoutPerOp time.Duration

	// PartialFailure determines behavior when some operations fail.
	PartialFailure async.PartialFailureStrategy
}

// DefaultAsyncConfig returns sensible defaults for async execution.
func DefaultAsyncConfig() AsyncConfig {
	return AsyncConfig{
		Enabled:        true,
		MaxParallel:    4,
		TimeoutPerOp:   30 * time.Second,
		PartialFailure: async.ContinueOnError,
	}
}

// NewAsyncExecutor creates an executor configured with the given options.
// The orchestrator parameter must implement the async.Orchestrator interface.
func NewAsyncExecutor(orchestrator async.Orchestrator, cfg AsyncConfig) *async.Executor {
	return async.NewExecutor(orchestrator, async.ExecutorConfig{
		MaxParallel:    cfg.MaxParallel,
		TimeoutPerOp:   cfg.TimeoutPerOp,
		PartialFailure: cfg.PartialFailure,
	})
}

// Re-export async types for convenience

// Operation re-exports async.Operation for use in orchestration.
type Operation = async.Operation

// ExecutionPlan re-exports async.ExecutionPlan.
type ExecutionPlan = async.ExecutionPlan

// ExecutionResult re-exports async.ExecutionResult.
type AsyncExecutionResult = async.ExecutionResult

// ExecutionStrategy re-exports async.ExecutionStrategy.
type ExecutionStrategy = async.ExecutionStrategy

// Execution strategy constants.
const (
	StrategyParallel    = async.StrategyParallel
	StrategySequential  = async.StrategySequential
	StrategySpeculative = async.StrategySpeculative
)

// PartialFailureStrategy re-exports async.PartialFailureStrategy.
type PartialFailureStrategy = async.PartialFailureStrategy

// Partial failure mode constants.
const (
	FailFast        = async.FailFast
	ContinueOnError = async.ContinueOnError
)
