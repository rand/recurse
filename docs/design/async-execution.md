# Async Recursive Execution Design

> Design document for `recurse-8yz`: [SPEC] Async Recursive Execution Design

## Overview

This document specifies the async execution model for the RLM system, enabling parallel processing of independent subtasks and speculative execution of alternative approaches. The current serial execution in `Controller.executeDecompose()` leaves significant performance on the table when subtasks are independent.

## Problem Statement

### Current Serial Execution

The existing implementation in `internal/rlm/controller.go` processes decomposed chunks sequentially:

```go
// Current: Serial execution (controller.go:~250)
func (c *Controller) executeDecompose(...) (string, int, error) {
    // ... decomposition ...

    var results []synthesize.SubCallResult
    for i, chunk := range chunks {
        // Each chunk waits for the previous to complete
        response, tokens, err := c.orchestrate(ctx, childState, parentID)
        if err != nil {
            return "", totalTokens, err
        }
        results = append(results, synthesize.SubCallResult{...})
        totalTokens += tokens
    }
    // ... synthesis ...
}
```

### Performance Impact

| Scenario | Serial Time | Parallel Time | Speedup |
|----------|-------------|---------------|---------|
| 4 independent chunks, 2s each | 8s | 2s | 4x |
| 8 chunks, 1.5s each | 12s | 1.5s | 8x |
| Mixed dependencies | Variable | Best-case 50% | 2x |

### When Parallelism Applies

Not all decompositions benefit from parallelism:

| Pattern | Parallel-Safe | Reason |
|---------|--------------|--------|
| Independent analysis chunks | Yes | No shared state |
| Sequential reasoning steps | No | Step N depends on step N-1 |
| Map-reduce style | Yes (map), No (reduce) | Map phase parallel, reduce sequential |
| Alternative approaches | Yes (speculative) | Race alternatives, use winner |

## Design Goals

1. **Maximize parallelism** for independent operations
2. **Preserve correctness** for dependent operations
3. **Enable speculative execution** with automatic cancellation
4. **Respect budget constraints** with dynamic parallelism limits
5. **Graceful degradation** on partial failures
6. **Minimal API changes** to existing Controller

## Core Types

### ExecutionPlan

Represents the dependency graph for a set of operations:

```go
// ExecutionPlan describes how to execute a set of operations.
type ExecutionPlan struct {
    // Operations to execute, keyed by unique ID
    Operations map[string]*Operation

    // DependsOn maps operation ID to IDs it depends on
    DependsOn map[string][]string

    // Strategy for this plan
    Strategy ExecutionStrategy
}

type Operation struct {
    ID       string
    Task     string          // The subtask to execute
    State    meta.State      // RLM state for this operation
    Priority int             // Higher = execute first when possible
    Timeout  time.Duration   // Per-operation timeout (0 = inherit)
}

type ExecutionStrategy int

const (
    // StrategyParallel executes independent ops concurrently
    StrategyParallel ExecutionStrategy = iota

    // StrategySpeculative races alternatives, cancels losers
    StrategySpeculative

    // StrategySequential forces serial execution (fallback)
    StrategySequential
)
```

### AsyncExecutor Interface

```go
// AsyncExecutor handles parallel and speculative execution of RLM operations.
type AsyncExecutor interface {
    // ExecutePlan runs operations according to their dependency graph.
    // Returns results keyed by operation ID.
    ExecutePlan(ctx context.Context, plan *ExecutionPlan) (*ExecutionResult, error)

    // ExecuteParallel is a convenience method for independent operations.
    ExecuteParallel(ctx context.Context, ops []*Operation) (*ExecutionResult, error)

    // ExecuteSpeculative races alternatives and returns the first success.
    // Cancels remaining operations once a winner is determined.
    ExecuteSpeculative(ctx context.Context, alternatives []*Operation) (*SpeculativeResult, error)
}
```

### Result Types

```go
// ExecutionResult contains outcomes from parallel execution.
type ExecutionResult struct {
    // Results keyed by operation ID
    Results map[string]*OperationResult

    // TotalTokens consumed across all operations
    TotalTokens int

    // Duration for the entire execution
    Duration time.Duration

    // PartialFailure indicates some (not all) operations failed
    PartialFailure bool
}

type OperationResult struct {
    ID       string
    Response string
    Tokens   int
    Duration time.Duration
    Error    error
}

// SpeculativeResult contains the winning alternative.
type SpeculativeResult struct {
    // Winner is the first successful operation
    Winner *OperationResult

    // Cancelled lists operations that were cancelled
    Cancelled []string

    // TotalTokens includes tokens from cancelled operations
    TotalTokens int
}
```

## Implementation

### DefaultAsyncExecutor

```go
type DefaultAsyncExecutor struct {
    controller   *Controller
    maxParallel  int           // Max concurrent operations
    budgetMgr    budget.Manager

    // Metrics
    parallelOps  prometheus.Counter
    specWins     prometheus.Counter
    specCancels  prometheus.Counter
}

func NewAsyncExecutor(ctrl *Controller, maxParallel int, bm budget.Manager) *DefaultAsyncExecutor {
    return &DefaultAsyncExecutor{
        controller:  ctrl,
        maxParallel: maxParallel,
        budgetMgr:   bm,
    }
}
```

### Parallel Execution with errgroup

```go
func (e *DefaultAsyncExecutor) ExecuteParallel(
    ctx context.Context,
    ops []*Operation,
) (*ExecutionResult, error) {
    result := &ExecutionResult{
        Results: make(map[string]*OperationResult),
    }

    var mu sync.Mutex
    start := time.Now()

    // Use semaphore for parallelism control
    sem := make(chan struct{}, e.effectiveParallelism(ctx, len(ops)))
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

            // Execute operation
            opStart := time.Now()
            response, tokens, err := e.controller.orchestrate(ctx, op.State, op.ID)

            opResult := &OperationResult{
                ID:       op.ID,
                Response: response,
                Tokens:   tokens,
                Duration: time.Since(opStart),
                Error:    err,
            }

            mu.Lock()
            result.Results[op.ID] = opResult
            result.TotalTokens += tokens
            mu.Unlock()

            // Don't fail entire group on single op failure
            // (partial failure mode)
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        return nil, err
    }

    result.Duration = time.Since(start)
    result.PartialFailure = e.hasPartialFailure(result)

    return result, nil
}
```

### Budget-Aware Parallelism

```go
func (e *DefaultAsyncExecutor) effectiveParallelism(ctx context.Context, numOps int) int {
    // Start with configured max
    limit := e.maxParallel

    // Reduce if budget is constrained
    if e.budgetMgr != nil {
        remaining := e.budgetMgr.RemainingBudget(ctx)
        estimatedPerOp := e.budgetMgr.EstimatedCostPerOp()

        // If budget is tight, reduce parallelism to avoid overshoot
        if remaining > 0 && estimatedPerOp > 0 {
            budgetLimit := int(remaining / estimatedPerOp)
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
```

### Speculative Execution

Speculative execution races multiple alternative approaches, using the first successful result:

```go
func (e *DefaultAsyncExecutor) ExecuteSpeculative(
    ctx context.Context,
    alternatives []*Operation,
) (*SpeculativeResult, error) {
    if len(alternatives) == 0 {
        return nil, errors.New("no alternatives provided")
    }

    // Create cancellable context for speculation
    specCtx, cancel := context.WithCancel(ctx)
    defer cancel()

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
            response, tokens, err := e.controller.orchestrate(specCtx, alt.State, alt.ID)

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

    // Mark non-winners as cancelled
    for _, alt := range alternatives {
        if alt.ID != winner.ID {
            result.Cancelled = append(result.Cancelled, alt.ID)
        }
    }

    e.specWins.Inc()
    e.specCancels.Add(float64(len(result.Cancelled)))

    return result, nil
}
```

### Dependency-Aware Execution

For operations with dependencies, we use topological execution:

```go
func (e *DefaultAsyncExecutor) ExecutePlan(
    ctx context.Context,
    plan *ExecutionPlan,
) (*ExecutionResult, error) {
    // Build reverse dependency map
    readyWhen := make(map[string]map[string]bool) // op -> deps still pending
    for opID, deps := range plan.DependsOn {
        readyWhen[opID] = make(map[string]bool)
        for _, dep := range deps {
            readyWhen[opID][dep] = true
        }
    }

    result := &ExecutionResult{
        Results: make(map[string]*OperationResult),
    }

    pending := make(map[string]*Operation)
    for id, op := range plan.Operations {
        pending[id] = op
    }

    var mu sync.Mutex
    start := time.Now()

    for len(pending) > 0 {
        // Find ready operations (no pending dependencies)
        var ready []*Operation
        for id, op := range pending {
            deps := readyWhen[id]
            if len(deps) == 0 {
                ready = append(ready, op)
            }
        }

        if len(ready) == 0 && len(pending) > 0 {
            return nil, errors.New("circular dependency detected")
        }

        // Execute ready operations in parallel
        batchResult, err := e.ExecuteParallel(ctx, ready)
        if err != nil {
            return nil, err
        }

        // Merge results and update dependencies
        mu.Lock()
        for id, opResult := range batchResult.Results {
            result.Results[id] = opResult
            result.TotalTokens += opResult.Tokens
            delete(pending, id)

            // Remove this op from others' dependency lists
            for otherID := range readyWhen {
                delete(readyWhen[otherID], id)
            }
        }
        mu.Unlock()

        // Check for failures that block dependents
        if batchResult.PartialFailure {
            // Mark dependents as failed
            for opID, deps := range plan.DependsOn {
                for _, dep := range deps {
                    if r, ok := result.Results[dep]; ok && r.Error != nil {
                        result.Results[opID] = &OperationResult{
                            ID:    opID,
                            Error: fmt.Errorf("dependency %s failed: %w", dep, r.Error),
                        }
                        delete(pending, opID)
                    }
                }
            }
        }
    }

    result.Duration = time.Since(start)
    result.PartialFailure = e.hasPartialFailure(result)

    return result, nil
}
```

## Integration with Controller

### Modified executeDecompose

```go
func (c *Controller) executeDecompose(
    ctx context.Context,
    state meta.State,
    decision *meta.Decision,
    parentID string,
) (string, int, error) {
    // ... existing decomposition logic ...

    // Build execution plan from chunks
    plan := c.buildExecutionPlan(chunks, state, parentID)

    // Execute using async executor
    execResult, err := c.asyncExecutor.ExecutePlan(ctx, plan)
    if err != nil {
        return "", 0, fmt.Errorf("async execution: %w", err)
    }

    // Handle partial failures
    if execResult.PartialFailure {
        // Decide: retry, degrade, or fail
        if c.shouldRetryPartialFailure(execResult) {
            return c.retryFailedOperations(ctx, execResult, plan)
        }
    }

    // Convert to synthesizer format
    var results []synthesize.SubCallResult
    for _, op := range plan.Operations {
        opResult := execResult.Results[op.ID]
        results = append(results, synthesize.SubCallResult{
            Index:    op.Priority, // Use priority as ordering
            Content:  opResult.Response,
            Tokens:   opResult.Tokens,
        })
    }

    // ... existing synthesis logic ...
}
```

### Dependency Detection

The meta-controller should hint at dependencies in its decomposition:

```go
type DecompositionHint struct {
    Chunks       []string
    Dependencies map[int][]int  // chunk index -> depends on indices
    Strategy     ExecutionStrategy
}

func (c *Controller) buildExecutionPlan(
    chunks []string,
    state meta.State,
    parentID string,
) *ExecutionPlan {
    plan := &ExecutionPlan{
        Operations: make(map[string]*Operation),
        DependsOn:  make(map[string][]string),
        Strategy:   StrategyParallel, // Default
    }

    // Check for dependency hints from meta-controller
    if hint, ok := state.Metadata["decomposition_hint"].(*DecompositionHint); ok {
        plan.Strategy = hint.Strategy

        for i, chunk := range hint.Chunks {
            opID := fmt.Sprintf("%s-chunk-%d", parentID, i)
            plan.Operations[opID] = &Operation{
                ID:       opID,
                Task:     chunk,
                State:    c.createChildState(state, chunk),
                Priority: len(chunks) - i, // Higher priority for earlier chunks
            }

            // Map dependencies
            if deps, ok := hint.Dependencies[i]; ok {
                for _, depIdx := range deps {
                    depID := fmt.Sprintf("%s-chunk-%d", parentID, depIdx)
                    plan.DependsOn[opID] = append(plan.DependsOn[opID], depID)
                }
            }
        }
    } else {
        // No hints: assume all independent (parallel)
        for i, chunk := range chunks {
            opID := fmt.Sprintf("%s-chunk-%d", parentID, i)
            plan.Operations[opID] = &Operation{
                ID:       opID,
                Task:     chunk,
                State:    c.createChildState(state, chunk),
                Priority: len(chunks) - i,
            }
        }
    }

    return plan
}
```

## Speculative Execution Use Cases

### Alternative Approaches

When the meta-controller identifies multiple valid approaches:

```go
func (c *Controller) executeWithSpeculation(
    ctx context.Context,
    state meta.State,
    approaches []string,
) (string, int, error) {
    alternatives := make([]*Operation, len(approaches))
    for i, approach := range approaches {
        alternatives[i] = &Operation{
            ID:    fmt.Sprintf("approach-%d", i),
            Task:  approach,
            State: state,
        }
    }

    result, err := c.asyncExecutor.ExecuteSpeculative(ctx, alternatives)
    if err != nil {
        return "", 0, err
    }

    return result.Winner.Response, result.TotalTokens, nil
}
```

### Early Exit on Confidence

For queries where we might find the answer quickly:

```go
type ConfidenceThreshold struct {
    MinConfidence float64
    MaxTokens     int
}

func (e *DefaultAsyncExecutor) ExecuteWithEarlyExit(
    ctx context.Context,
    ops []*Operation,
    threshold ConfidenceThreshold,
) (*ExecutionResult, error) {
    // Create cancellable context
    ctx, cancel := context.WithCancel(ctx)
    defer cancel()

    resultCh := make(chan *OperationResult, len(ops))

    // ... launch operations ...

    // Monitor for early exit condition
    for result := range resultCh {
        if result.Error == nil {
            confidence := e.extractConfidence(result.Response)
            if confidence >= threshold.MinConfidence {
                cancel() // Found high-confidence answer, cancel rest
                return &ExecutionResult{
                    Results:     map[string]*OperationResult{result.ID: result},
                    TotalTokens: result.Tokens,
                }, nil
            }
        }
    }

    // ... aggregate all results ...
}
```

## Error Handling

### Partial Failure Strategies

```go
type PartialFailureStrategy int

const (
    // FailFast aborts on first error
    FailFast PartialFailureStrategy = iota

    // ContinueOnError completes remaining ops
    ContinueOnError

    // RetryFailed retries failed ops up to N times
    RetryFailed
)

type ExecutorConfig struct {
    PartialFailure    PartialFailureStrategy
    MaxRetries        int
    RetryBackoff      time.Duration
    TimeoutPerOp      time.Duration
    BudgetPerOp       int  // Token budget per operation
}
```

### Graceful Degradation

When some operations fail, provide partial results:

```go
func (c *Controller) synthesizeWithPartialResults(
    ctx context.Context,
    execResult *ExecutionResult,
) (string, error) {
    var successful []synthesize.SubCallResult
    var failed []string

    for id, result := range execResult.Results {
        if result.Error != nil {
            failed = append(failed, id)
        } else {
            successful = append(successful, synthesize.SubCallResult{
                Content: result.Response,
                Tokens:  result.Tokens,
            })
        }
    }

    if len(successful) == 0 {
        return "", errors.New("all operations failed")
    }

    // Synthesize with note about missing pieces
    synthesis := c.synthesizer.Synthesize(ctx, successful)

    if len(failed) > 0 {
        synthesis = fmt.Sprintf(
            "%s\n\n[Note: %d of %d subtasks failed and are not reflected above]",
            synthesis,
            len(failed),
            len(execResult.Results),
        )
    }

    return synthesis, nil
}
```

## Observability

### Metrics

```go
var (
    asyncOpsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rlm_async_operations_total",
            Help: "Total async operations executed",
        },
        []string{"strategy", "status"},
    )

    asyncOpDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "rlm_async_operation_duration_seconds",
            Help:    "Duration of async operations",
            Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
        },
        []string{"strategy"},
    )

    parallelismEffective = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "rlm_async_effective_parallelism",
            Help: "Current effective parallelism level",
        },
    )

    speculativeWastedTokens = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "rlm_speculative_wasted_tokens_total",
            Help: "Tokens spent on cancelled speculative operations",
        },
    )
)
```

### Tracing

```go
func (e *DefaultAsyncExecutor) ExecuteParallel(
    ctx context.Context,
    ops []*Operation,
) (*ExecutionResult, error) {
    ctx, span := tracer.Start(ctx, "AsyncExecutor.ExecuteParallel",
        trace.WithAttributes(
            attribute.Int("num_operations", len(ops)),
            attribute.Int("max_parallelism", e.maxParallel),
        ),
    )
    defer span.End()

    effectiveParallel := e.effectiveParallelism(ctx, len(ops))
    span.SetAttributes(attribute.Int("effective_parallelism", effectiveParallel))

    // ... execution ...

    span.SetAttributes(
        attribute.Int("total_tokens", result.TotalTokens),
        attribute.Bool("partial_failure", result.PartialFailure),
    )

    return result, nil
}
```

## Testing Strategy

### Unit Tests

```go
func TestAsyncExecutor_ExecuteParallel(t *testing.T) {
    tests := []struct {
        name        string
        ops         []*Operation
        maxParallel int
        wantTokens  int
        wantErr     bool
    }{
        {
            name:        "all succeed",
            ops:         makeOps(4, nil),
            maxParallel: 4,
            wantTokens:  400, // 100 per op
        },
        {
            name:        "partial failure",
            ops:         makeOpsWithErrors(4, 1),
            maxParallel: 4,
            wantTokens:  300,
        },
        {
            name:        "parallelism limit",
            ops:         makeOps(8, nil),
            maxParallel: 2,
            wantTokens:  800,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            executor := NewAsyncExecutor(mockCtrl, tt.maxParallel, nil)
            result, err := executor.ExecuteParallel(context.Background(), tt.ops)

            if tt.wantErr {
                require.Error(t, err)
                return
            }

            require.NoError(t, err)
            assert.Equal(t, tt.wantTokens, result.TotalTokens)
        })
    }
}
```

### Concurrency Tests

```go
func TestAsyncExecutor_RaceConditions(t *testing.T) {
    // Run with -race flag
    executor := NewAsyncExecutor(mockCtrl, 10, nil)

    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            ops := makeOps(5, nil)
            _, _ = executor.ExecuteParallel(context.Background(), ops)
        }()
    }
    wg.Wait()
}

func TestAsyncExecutor_ContextCancellation(t *testing.T) {
    executor := NewAsyncExecutor(slowMockCtrl, 4, nil)

    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()

    ops := makeOps(10, nil) // Each takes 1s
    _, err := executor.ExecuteParallel(ctx, ops)

    require.Error(t, err)
    assert.True(t, errors.Is(err, context.DeadlineExceeded))
}
```

### Integration Tests

```go
func TestAsyncExecutor_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    ctrl := newTestController(t)
    executor := NewAsyncExecutor(ctrl, 4, nil)

    // Real decomposition task
    ops := []*Operation{
        {ID: "1", Task: "Analyze imports", State: testState},
        {ID: "2", Task: "Find unused vars", State: testState},
        {ID: "3", Task: "Check error handling", State: testState},
    }

    result, err := executor.ExecuteParallel(context.Background(), ops)
    require.NoError(t, err)
    assert.Len(t, result.Results, 3)

    for _, r := range result.Results {
        assert.NoError(t, r.Error)
        assert.NotEmpty(t, r.Response)
    }
}
```

## Migration Path

### Phase 1: Opt-in Parallel Execution

```go
type ControllerConfig struct {
    // Existing fields...

    // New: Enable async execution
    EnableAsyncExecution bool
    MaxParallelOps       int
}

func (c *Controller) executeDecompose(...) (string, int, error) {
    if c.config.EnableAsyncExecution {
        return c.executeDecomposeAsync(ctx, state, decision, parentID)
    }
    return c.executeDecomposeSerial(ctx, state, decision, parentID)
}
```

### Phase 2: Default Parallel

After validation, make parallel execution the default with opt-out:

```go
type ControllerConfig struct {
    DisableAsyncExecution bool  // Flip the default
}
```

### Phase 3: Speculative Execution

Add speculative execution for approach selection:

```go
type MetaDecision struct {
    // Existing fields...

    AlternativeApproaches []string  // If populated, use speculation
    SpeculationBudget     int       // Max tokens for speculation
}
```

## Success Criteria

1. **Performance**: 3x speedup for 4+ chunk decompositions
2. **Correctness**: All existing tests pass, no race conditions
3. **Reliability**: Partial failures don't cascade
4. **Efficiency**: Speculative wasted tokens <20% of total
5. **Observability**: Full tracing and metrics coverage

## Appendix: rlm-claude-code Reference

The rlm-claude-code implementation provides these key patterns we're adapting:

| rlm-claude-code Pattern | Our Adaptation |
|------------------------|----------------|
| `async_task_runner.py` with asyncio | `AsyncExecutor` with errgroup |
| `speculative_executor.py` racing | `ExecuteSpeculative` with cancel |
| Budget-aware parallelism | `effectiveParallelism()` |
| Partial failure handling | `ContinueOnError` strategy |
| Confidence-based early exit | `ExecuteWithEarlyExit()` |

Key differences:
- Go's concurrency model (goroutines) vs Python's asyncio
- errgroup for structured concurrency vs TaskGroup
- Context cancellation vs asyncio.CancelledError
- Explicit mutexes vs asyncio locks
