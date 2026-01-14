// Package async provides parallel and speculative execution for RLM operations.
package async

import (
	"time"

	"github.com/rand/recurse/internal/rlm/meta"
)

// ExecutionStrategy determines how operations are executed.
type ExecutionStrategy int

const (
	// StrategyParallel executes independent ops concurrently.
	StrategyParallel ExecutionStrategy = iota

	// StrategySpeculative races alternatives, cancels losers.
	StrategySpeculative

	// StrategySequential forces serial execution (fallback).
	StrategySequential
)

func (s ExecutionStrategy) String() string {
	switch s {
	case StrategyParallel:
		return "parallel"
	case StrategySpeculative:
		return "speculative"
	case StrategySequential:
		return "sequential"
	default:
		return "unknown"
	}
}

// Operation represents a single executable operation.
type Operation struct {
	// ID uniquely identifies this operation.
	ID string

	// Task is the subtask to execute.
	Task string

	// State is the RLM state for this operation.
	State meta.State

	// Priority determines execution order when possible (higher = first).
	Priority int

	// Timeout is the per-operation timeout (0 = inherit from context).
	Timeout time.Duration

	// ParentID links this operation to its parent for tracing.
	ParentID string
}

// ExecutionPlan describes how to execute a set of operations.
type ExecutionPlan struct {
	// Operations to execute, keyed by unique ID.
	Operations map[string]*Operation

	// DependsOn maps operation ID to IDs it depends on.
	DependsOn map[string][]string

	// Strategy for this plan.
	Strategy ExecutionStrategy
}

// NewExecutionPlan creates an empty execution plan.
func NewExecutionPlan(strategy ExecutionStrategy) *ExecutionPlan {
	return &ExecutionPlan{
		Operations: make(map[string]*Operation),
		DependsOn:  make(map[string][]string),
		Strategy:   strategy,
	}
}

// AddOperation adds an operation to the plan.
func (p *ExecutionPlan) AddOperation(op *Operation) {
	p.Operations[op.ID] = op
}

// AddDependency marks that opID depends on dependsOnID.
func (p *ExecutionPlan) AddDependency(opID, dependsOnID string) {
	p.DependsOn[opID] = append(p.DependsOn[opID], dependsOnID)
}

// GetReadyOperations returns operations with no pending dependencies.
func (p *ExecutionPlan) GetReadyOperations(completed map[string]bool) []*Operation {
	var ready []*Operation
	for id, op := range p.Operations {
		if completed[id] {
			continue
		}
		deps := p.DependsOn[id]
		allDepsComplete := true
		for _, dep := range deps {
			if !completed[dep] {
				allDepsComplete = false
				break
			}
		}
		if allDepsComplete {
			ready = append(ready, op)
		}
	}
	return ready
}

// OperationResult contains the outcome of a single operation.
type OperationResult struct {
	// ID of the operation.
	ID string

	// Response from the operation.
	Response string

	// Tokens consumed.
	Tokens int

	// Duration of execution.
	Duration time.Duration

	// Error if the operation failed.
	Error error
}

// ExecutionResult contains outcomes from parallel execution.
type ExecutionResult struct {
	// Results keyed by operation ID.
	Results map[string]*OperationResult

	// TotalTokens consumed across all operations.
	TotalTokens int

	// Duration for the entire execution.
	Duration time.Duration

	// PartialFailure indicates some (not all) operations failed.
	PartialFailure bool
}

// NewExecutionResult creates an empty execution result.
func NewExecutionResult() *ExecutionResult {
	return &ExecutionResult{
		Results: make(map[string]*OperationResult),
	}
}

// AddResult adds an operation result.
func (r *ExecutionResult) AddResult(result *OperationResult) {
	r.Results[result.ID] = result
	r.TotalTokens += result.Tokens
	if result.Error != nil {
		r.PartialFailure = true
	}
}

// SuccessCount returns the number of successful operations.
func (r *ExecutionResult) SuccessCount() int {
	count := 0
	for _, result := range r.Results {
		if result.Error == nil {
			count++
		}
	}
	return count
}

// FailureCount returns the number of failed operations.
func (r *ExecutionResult) FailureCount() int {
	return len(r.Results) - r.SuccessCount()
}

// SpeculativeResult contains the winning alternative from speculative execution.
type SpeculativeResult struct {
	// Winner is the first successful operation.
	Winner *OperationResult

	// Cancelled lists operations that were cancelled.
	Cancelled []string

	// TotalTokens includes tokens from cancelled operations.
	TotalTokens int

	// Duration for the entire speculative execution.
	Duration time.Duration
}

// PartialFailureStrategy determines how to handle partial failures.
type PartialFailureStrategy int

const (
	// FailFast aborts on first error.
	FailFast PartialFailureStrategy = iota

	// ContinueOnError completes remaining ops despite errors.
	ContinueOnError

	// RetryFailed retries failed ops up to N times.
	RetryFailed
)

// ExecutorConfig configures the async executor.
type ExecutorConfig struct {
	// MaxParallel is the maximum concurrent operations.
	MaxParallel int

	// PartialFailure strategy.
	PartialFailure PartialFailureStrategy

	// MaxRetries for failed operations (when using RetryFailed strategy).
	MaxRetries int

	// RetryBackoff between retry attempts.
	RetryBackoff time.Duration

	// TimeoutPerOp is the default per-operation timeout.
	TimeoutPerOp time.Duration

	// BudgetPerOp is the token budget per operation.
	BudgetPerOp int
}

// DefaultExecutorConfig returns sensible defaults.
func DefaultExecutorConfig() ExecutorConfig {
	return ExecutorConfig{
		MaxParallel:    4,
		PartialFailure: ContinueOnError,
		MaxRetries:     2,
		RetryBackoff:   100 * time.Millisecond,
		TimeoutPerOp:   30 * time.Second,
		BudgetPerOp:    10000,
	}
}
