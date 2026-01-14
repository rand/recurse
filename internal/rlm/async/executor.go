package async

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// Orchestrator is the interface for executing RLM operations.
// This allows the executor to call back into the controller.
type Orchestrator interface {
	// Orchestrate executes a single operation and returns response, tokens, error.
	Orchestrate(ctx context.Context, op *Operation) (string, int, error)
}

// BudgetManager provides budget-aware parallelism control.
type BudgetManager interface {
	// RemainingBudget returns the remaining token budget.
	RemainingBudget(ctx context.Context) int

	// EstimatedCostPerOp returns the estimated tokens per operation.
	EstimatedCostPerOp() int
}

// Executor handles parallel and speculative execution of RLM operations.
type Executor struct {
	orchestrator Orchestrator
	budgetMgr    BudgetManager
	config       ExecutorConfig
	mu           sync.Mutex
}

// NewExecutor creates a new async executor.
func NewExecutor(orchestrator Orchestrator, config ExecutorConfig) *Executor {
	if config.MaxParallel <= 0 {
		config.MaxParallel = 4
	}
	return &Executor{
		orchestrator: orchestrator,
		config:       config,
	}
}

// SetBudgetManager sets the budget manager for cost-aware parallelism.
func (e *Executor) SetBudgetManager(bm BudgetManager) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.budgetMgr = bm
}

// ExecutePlan runs operations according to their dependency graph.
func (e *Executor) ExecutePlan(ctx context.Context, plan *ExecutionPlan) (*ExecutionResult, error) {
	switch plan.Strategy {
	case StrategySequential:
		return e.executeSequential(ctx, plan)
	case StrategySpeculative:
		ops := make([]*Operation, 0, len(plan.Operations))
		for _, op := range plan.Operations {
			ops = append(ops, op)
		}
		specResult, err := e.ExecuteSpeculative(ctx, ops)
		if err != nil {
			return nil, err
		}
		result := NewExecutionResult()
		result.AddResult(specResult.Winner)
		result.TotalTokens = specResult.TotalTokens
		result.Duration = specResult.Duration
		return result, nil
	default:
		return e.executeDependencyAware(ctx, plan)
	}
}

// ExecuteParallel executes independent operations concurrently.
func (e *Executor) ExecuteParallel(ctx context.Context, ops []*Operation) (*ExecutionResult, error) {
	if len(ops) == 0 {
		return NewExecutionResult(), nil
	}

	result := NewExecutionResult()
	var mu sync.Mutex
	start := time.Now()

	// Use semaphore for parallelism control
	parallelism := e.effectiveParallelism(ctx, len(ops))
	sem := make(chan struct{}, parallelism)

	g, ctx := errgroup.WithContext(ctx)

	for _, op := range ops {
		op := op // capture for goroutine

		g.Go(func() error {
			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return ctx.Err()
			}

			// Apply per-operation timeout if configured
			opCtx := ctx
			if op.Timeout > 0 {
				var cancel context.CancelFunc
				opCtx, cancel = context.WithTimeout(ctx, op.Timeout)
				defer cancel()
			} else if e.config.TimeoutPerOp > 0 {
				var cancel context.CancelFunc
				opCtx, cancel = context.WithTimeout(ctx, e.config.TimeoutPerOp)
				defer cancel()
			}

			// Execute operation
			opStart := time.Now()
			response, tokens, err := e.orchestrator.Orchestrate(opCtx, op)

			opResult := &OperationResult{
				ID:       op.ID,
				Response: response,
				Tokens:   tokens,
				Duration: time.Since(opStart),
				Error:    err,
			}

			mu.Lock()
			result.AddResult(opResult)
			mu.Unlock()

			// Handle partial failure strategy
			if err != nil && e.config.PartialFailure == FailFast {
				return err
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		// If we're in FailFast mode, this is a real error
		if e.config.PartialFailure == FailFast {
			result.Duration = time.Since(start)
			return result, err
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// ExecuteSpeculative races alternatives and returns the first success.
func (e *Executor) ExecuteSpeculative(ctx context.Context, alternatives []*Operation) (*SpeculativeResult, error) {
	if len(alternatives) == 0 {
		return nil, errors.New("no alternatives provided")
	}

	// Single alternative - just execute it
	if len(alternatives) == 1 {
		start := time.Now()
		response, tokens, err := e.orchestrator.Orchestrate(ctx, alternatives[0])
		if err != nil {
			return nil, err
		}
		return &SpeculativeResult{
			Winner: &OperationResult{
				ID:       alternatives[0].ID,
				Response: response,
				Tokens:   tokens,
				Duration: time.Since(start),
			},
			TotalTokens: tokens,
			Duration:    time.Since(start),
		}, nil
	}

	// Create cancellable context for speculation
	specCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	start := time.Now()
	result := &SpeculativeResult{
		Cancelled: make([]string, 0, len(alternatives)-1),
	}

	// Channel for first successful result
	winnerCh := make(chan *OperationResult, 1)
	var wg sync.WaitGroup
	var tokensMu sync.Mutex

	for _, alt := range alternatives {
		alt := alt
		wg.Add(1)

		go func() {
			defer wg.Done()

			opStart := time.Now()
			response, tokens, err := e.orchestrator.Orchestrate(specCtx, alt)

			tokensMu.Lock()
			result.TotalTokens += tokens
			tokensMu.Unlock()

			opResult := &OperationResult{
				ID:       alt.ID,
				Response: response,
				Tokens:   tokens,
				Duration: time.Since(opStart),
				Error:    err,
			}

			// Only successful results can win
			if err == nil {
				select {
				case winnerCh <- opResult:
					// We're the winner, cancel others
					cancel()
				default:
					// Someone else already won
				}
			}
		}()
	}

	// Wait for winner or all to fail
	go func() {
		wg.Wait()
		close(winnerCh)
	}()

	winner, ok := <-winnerCh
	if !ok {
		return nil, errors.New("all speculative alternatives failed")
	}

	result.Winner = winner
	result.Duration = time.Since(start)

	// Mark non-winners as cancelled
	for _, alt := range alternatives {
		if alt.ID != winner.ID {
			result.Cancelled = append(result.Cancelled, alt.ID)
		}
	}

	return result, nil
}

// executeSequential executes operations one at a time.
func (e *Executor) executeSequential(ctx context.Context, plan *ExecutionPlan) (*ExecutionResult, error) {
	result := NewExecutionResult()
	start := time.Now()

	// Sort operations by priority (higher first)
	ops := make([]*Operation, 0, len(plan.Operations))
	for _, op := range plan.Operations {
		ops = append(ops, op)
	}
	sortByPriority(ops)

	for _, op := range ops {
		opStart := time.Now()
		response, tokens, err := e.orchestrator.Orchestrate(ctx, op)

		opResult := &OperationResult{
			ID:       op.ID,
			Response: response,
			Tokens:   tokens,
			Duration: time.Since(opStart),
			Error:    err,
		}
		result.AddResult(opResult)

		if err != nil && e.config.PartialFailure == FailFast {
			result.Duration = time.Since(start)
			return result, err
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// executeDependencyAware executes operations respecting dependencies.
func (e *Executor) executeDependencyAware(ctx context.Context, plan *ExecutionPlan) (*ExecutionResult, error) {
	result := NewExecutionResult()
	start := time.Now()
	completed := make(map[string]bool)

	for len(completed) < len(plan.Operations) {
		// Find ready operations
		ready := plan.GetReadyOperations(completed)

		if len(ready) == 0 && len(completed) < len(plan.Operations) {
			return nil, errors.New("circular dependency detected")
		}

		// Execute ready operations in parallel
		batchResult, err := e.ExecuteParallel(ctx, ready)
		if err != nil && e.config.PartialFailure == FailFast {
			result.Duration = time.Since(start)
			return result, err
		}

		// Merge results
		for id, opResult := range batchResult.Results {
			result.AddResult(opResult)
			completed[id] = true

			// If this operation failed, mark dependents as failed
			if opResult.Error != nil && e.config.PartialFailure != ContinueOnError {
				e.markDependentsFailed(plan, id, opResult.Error, result, completed)
			}
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// markDependentsFailed marks operations that depend on a failed operation as failed.
func (e *Executor) markDependentsFailed(
	plan *ExecutionPlan,
	failedID string,
	failedErr error,
	result *ExecutionResult,
	completed map[string]bool,
) {
	for opID, deps := range plan.DependsOn {
		if completed[opID] {
			continue
		}
		for _, dep := range deps {
			if dep == failedID {
				result.AddResult(&OperationResult{
					ID:    opID,
					Error: fmt.Errorf("dependency %s failed: %w", failedID, failedErr),
				})
				completed[opID] = true
				// Recursively mark dependents
				e.markDependentsFailed(plan, opID, failedErr, result, completed)
				break
			}
		}
	}
}

// effectiveParallelism calculates the actual parallelism based on budget.
func (e *Executor) effectiveParallelism(ctx context.Context, numOps int) int {
	limit := e.config.MaxParallel

	// Reduce if budget is constrained
	e.mu.Lock()
	bm := e.budgetMgr
	e.mu.Unlock()

	if bm != nil {
		remaining := bm.RemainingBudget(ctx)
		estimatedPerOp := bm.EstimatedCostPerOp()

		if remaining > 0 && estimatedPerOp > 0 {
			budgetLimit := remaining / estimatedPerOp
			if budgetLimit < limit {
				limit = budgetLimit
			}
		}
	}

	// Never exceed number of operations
	if limit > numOps {
		limit = numOps
	}

	// Always allow at least 1
	if limit < 1 {
		limit = 1
	}

	return limit
}

// sortByPriority sorts operations by priority (higher first).
func sortByPriority(ops []*Operation) {
	for i := 0; i < len(ops)-1; i++ {
		for j := i + 1; j < len(ops); j++ {
			if ops[j].Priority > ops[i].Priority {
				ops[i], ops[j] = ops[j], ops[i]
			}
		}
	}
}
