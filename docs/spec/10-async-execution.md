# SPEC-10: Async Recursive Execution

## Overview

[SPEC-10.01] The system SHALL implement asynchronous execution of RLM operations to reduce latency through parallelism, as identified in the RLM paper which notes "lack of asynchrony can cause each query to range from a few seconds to several minutes."

[SPEC-10.02] Async execution MUST support three execution strategies:
- **Parallel**: Execute independent operations concurrently
- **Speculative**: Race alternatives and cancel losers
- **Dependency-aware**: Execute respecting operation dependencies

[SPEC-10.03] The executor MUST be budget-aware, limiting parallelism based on remaining token budget.

> **Informative**: Expected latency reduction is 3-5x for multi-call queries when operations can be parallelized.

## Execution Strategies

### Parallel Execution

[SPEC-10.04] The `ExecuteParallel` function SHALL execute a set of independent operations concurrently up to a configurable parallelism limit.

[SPEC-10.05] Parallelism control SHALL use a semaphore pattern to limit concurrent goroutines:
```go
sem := make(chan struct{}, parallelism)
// Acquire before operation
sem <- struct{}{}
defer func() { <-sem }()
```

[SPEC-10.06] Each parallel operation MAY have an individual timeout, falling back to the executor's default `TimeoutPerOp` if unspecified.

[SPEC-10.07] Parallel execution SHALL collect all results, including both successes and failures, unless configured for fail-fast behavior.

### Speculative Execution

[SPEC-10.08] The `ExecuteSpeculative` function SHALL race multiple alternative operations and return the first successful result.

[SPEC-10.09] When a winner is determined, the executor SHALL cancel all other in-flight alternatives via context cancellation.

[SPEC-10.10] Speculative execution MUST track total tokens consumed across all alternatives, including cancelled operations that consumed tokens before cancellation.

[SPEC-10.11] The result SHALL indicate which operations were cancelled:
```go
type SpeculativeResult struct {
    Winner      *OperationResult  // First successful result
    Cancelled   []string          // IDs of cancelled alternatives
    TotalTokens int               // Sum across all operations
    Duration    time.Duration     // Wall-clock time
}
```

[SPEC-10.12] If all alternatives fail, the executor SHALL return an error indicating total failure.

### Dependency-Aware Execution

[SPEC-10.13] The `ExecutePlan` function SHALL execute operations respecting their dependency graph.

[SPEC-10.14] Operations with no unmet dependencies (ready operations) SHALL be executed in parallel batches.

[SPEC-10.15] The executor SHALL detect circular dependencies and return an error rather than deadlock.

[SPEC-10.16] When an operation fails:
- In `FailFast` mode: execution stops immediately
- In `FailDependents` mode: dependents are marked as failed
- In `ContinueOnError` mode: execution continues with remaining operations

## Operation Model

[SPEC-10.17] An Operation SHALL include:
```go
type Operation struct {
    ID        string            // Unique identifier
    Type      OperationType     // Query, Transform, Verify, Synthesize
    Input     string            // Operation input
    Context   map[string]any    // Additional context
    Priority  int               // Higher = more important
    Timeout   time.Duration     // Per-operation timeout (0 = use default)
    Metadata  map[string]string // Arbitrary metadata
}
```

[SPEC-10.18] Operation types SHALL include:
- `OpTypeQuery`: Information retrieval operations
- `OpTypeTransform`: Data transformation operations
- `OpTypeVerify`: Verification/validation operations
- `OpTypeSynthesize`: Result synthesis operations

## Execution Plan

[SPEC-10.19] An ExecutionPlan SHALL define the operations and their relationships:
```go
type ExecutionPlan struct {
    Operations map[string]*Operation  // ID -> Operation
    DependsOn  map[string][]string    // ID -> dependency IDs
    Strategy   ExecutionStrategy      // Sequential, Parallel, Speculative, DependencyAware
}
```

[SPEC-10.20] The `GetReadyOperations` method SHALL return operations whose dependencies are all satisfied.

[SPEC-10.21] Plans with `StrategyParallel` SHALL execute all operations concurrently regardless of declared dependencies.

## Budget-Aware Parallelism

[SPEC-10.22] The executor SHALL integrate with a BudgetManager interface:
```go
type BudgetManager interface {
    RemainingBudget(ctx context.Context) int
    EstimatedCostPerOp() int
}
```

[SPEC-10.23] Effective parallelism SHALL be calculated as:
```go
budgetLimit := remainingBudget / estimatedCostPerOp
effectiveParallelism := min(configuredMax, budgetLimit, numOperations)
```

[SPEC-10.24] When budget is exhausted, parallelism SHALL reduce to 1 (sequential execution) rather than failing.

## Partial Failure Handling

[SPEC-10.25] The executor SHALL support three partial failure strategies:

| Strategy | Behavior |
|----------|----------|
| `FailFast` | Stop on first error, return partial results |
| `FailDependents` | Mark dependent operations as failed, continue others |
| `ContinueOnError` | Execute all operations, collect all results |

[SPEC-10.26] Operation results SHALL track both success and failure:
```go
type OperationResult struct {
    ID       string
    Response string
    Tokens   int
    Duration time.Duration
    Error    error  // nil on success
}
```

[SPEC-10.27] Execution results SHALL aggregate all operation results:
```go
type ExecutionResult struct {
    Results     map[string]*OperationResult
    TotalTokens int
    Duration    time.Duration
}
```

## Configuration

[SPEC-10.28] Executor configuration SHALL include:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `MaxParallel` | 4 | Maximum concurrent operations |
| `TimeoutPerOp` | 30s | Default per-operation timeout |
| `PartialFailure` | FailDependents | Failure handling strategy |

[SPEC-10.29] Configuration MAY be adjusted at runtime via the executor's methods.

## Integration with Orchestrator

[SPEC-10.30] The executor SHALL call back to an Orchestrator interface for actual operation execution:
```go
type Orchestrator interface {
    Orchestrate(ctx context.Context, op *Operation) (response string, tokens int, error)
}
```

[SPEC-10.31] This design allows the executor to be used with different orchestration backends while maintaining consistent parallel execution semantics.

## Implementation Location

[SPEC-10.32] The async executor SHALL be implemented in `internal/rlm/async/executor.go`.

[SPEC-10.33] Supporting types (Operation, ExecutionPlan, etc.) SHALL be defined in `internal/rlm/async/types.go`.

[SPEC-10.34] Tests SHALL be located in `internal/rlm/async/executor_test.go` and achieve >80% coverage.

## Error Handling

[SPEC-10.35] Context cancellation SHALL be propagated to all in-flight operations immediately.

[SPEC-10.36] Timeout errors SHALL be distinguishable from other errors via `context.DeadlineExceeded`.

[SPEC-10.37] The executor SHALL NOT panic; all errors SHALL be returned to the caller.

## Observability

[SPEC-10.38] The executor SHOULD emit structured logs for:
- Execution start (strategy, operation count)
- Operation completion (ID, duration, tokens, error)
- Speculative winner selection
- Budget-constrained parallelism adjustments

[SPEC-10.39] Metrics SHOULD be collected for:
- Operations executed (by type, by outcome)
- Total tokens consumed
- Parallelism utilization
- Speculative execution win rates
