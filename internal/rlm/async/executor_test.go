package async

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rand/recurse/internal/rlm/meta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockOrchestrator is a test orchestrator that returns predictable results.
type mockOrchestrator struct {
	responses map[string]string
	tokens    map[string]int
	errors    map[string]error
	delay     time.Duration
	calls     int64
	mu        sync.Mutex
}

func newMockOrchestrator() *mockOrchestrator {
	return &mockOrchestrator{
		responses: make(map[string]string),
		tokens:    make(map[string]int),
		errors:    make(map[string]error),
	}
}

func (m *mockOrchestrator) Orchestrate(ctx context.Context, op *Operation) (string, int, error) {
	atomic.AddInt64(&m.calls, 1)

	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return "", 0, ctx.Err()
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err, ok := m.errors[op.ID]; ok {
		return "", 0, err
	}

	response := m.responses[op.ID]
	if response == "" {
		response = "response-" + op.ID
	}

	tokens := m.tokens[op.ID]
	if tokens == 0 {
		tokens = 100
	}

	return response, tokens, nil
}

func (m *mockOrchestrator) CallCount() int64 {
	return atomic.LoadInt64(&m.calls)
}

func TestExecutor_ExecuteParallel_AllSucceed(t *testing.T) {
	mock := newMockOrchestrator()
	executor := NewExecutor(mock, DefaultExecutorConfig())

	ops := []*Operation{
		{ID: "op-1", Task: "task 1", State: meta.State{}},
		{ID: "op-2", Task: "task 2", State: meta.State{}},
		{ID: "op-3", Task: "task 3", State: meta.State{}},
		{ID: "op-4", Task: "task 4", State: meta.State{}},
	}

	result, err := executor.ExecuteParallel(context.Background(), ops)
	require.NoError(t, err)

	assert.Len(t, result.Results, 4)
	assert.Equal(t, 400, result.TotalTokens) // 100 tokens each
	assert.False(t, result.PartialFailure)
	assert.Equal(t, 4, result.SuccessCount())
	assert.Equal(t, 0, result.FailureCount())
}

func TestExecutor_ExecuteParallel_PartialFailure(t *testing.T) {
	mock := newMockOrchestrator()
	mock.errors["op-2"] = errors.New("op-2 failed")

	executor := NewExecutor(mock, ExecutorConfig{
		MaxParallel:    4,
		PartialFailure: ContinueOnError,
	})

	ops := []*Operation{
		{ID: "op-1", Task: "task 1"},
		{ID: "op-2", Task: "task 2"},
		{ID: "op-3", Task: "task 3"},
	}

	result, err := executor.ExecuteParallel(context.Background(), ops)
	require.NoError(t, err) // No error because ContinueOnError

	assert.Len(t, result.Results, 3)
	assert.True(t, result.PartialFailure)
	assert.Equal(t, 2, result.SuccessCount())
	assert.Equal(t, 1, result.FailureCount())
	assert.NotNil(t, result.Results["op-2"].Error)
}

func TestExecutor_ExecuteParallel_FailFast(t *testing.T) {
	mock := newMockOrchestrator()
	mock.errors["op-1"] = errors.New("op-1 failed")
	mock.delay = 10 * time.Millisecond // Add delay so we can test fail-fast

	executor := NewExecutor(mock, ExecutorConfig{
		MaxParallel:    4,
		PartialFailure: FailFast,
	})

	ops := []*Operation{
		{ID: "op-1", Task: "task 1"},
		{ID: "op-2", Task: "task 2"},
		{ID: "op-3", Task: "task 3"},
	}

	result, err := executor.ExecuteParallel(context.Background(), ops)
	require.Error(t, err)
	assert.True(t, result.PartialFailure)
}

func TestExecutor_ExecuteParallel_ParallelismLimit(t *testing.T) {
	mock := newMockOrchestrator()
	mock.delay = 50 * time.Millisecond

	executor := NewExecutor(mock, ExecutorConfig{
		MaxParallel:    2, // Only 2 concurrent
		PartialFailure: ContinueOnError,
	})

	ops := []*Operation{
		{ID: "op-1", Task: "task 1"},
		{ID: "op-2", Task: "task 2"},
		{ID: "op-3", Task: "task 3"},
		{ID: "op-4", Task: "task 4"},
	}

	start := time.Now()
	result, err := executor.ExecuteParallel(context.Background(), ops)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Len(t, result.Results, 4)

	// With parallelism of 2 and 50ms per op, 4 ops should take ~100ms (2 batches)
	// Allow some tolerance
	assert.GreaterOrEqual(t, elapsed, 90*time.Millisecond)
}

func TestExecutor_ExecuteParallel_ContextCancellation(t *testing.T) {
	mock := newMockOrchestrator()
	mock.delay = 500 * time.Millisecond // Long delay

	// Use FailFast to propagate context cancellation error
	executor := NewExecutor(mock, ExecutorConfig{
		MaxParallel:    4,
		PartialFailure: FailFast,
	})

	ops := []*Operation{
		{ID: "op-1", Task: "task 1"},
		{ID: "op-2", Task: "task 2"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := executor.ExecuteParallel(ctx, ops)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestExecutor_ExecuteSpeculative_FirstWins(t *testing.T) {
	mock := newMockOrchestrator()
	mock.responses["fast"] = "fast response"
	mock.tokens["fast"] = 50

	// Make "slow" actually slow
	slowOrch := &slowMockOrchestrator{
		mock:   mock,
		slowID: "slow",
		delay:  200 * time.Millisecond,
	}

	executor := NewExecutor(slowOrch, DefaultExecutorConfig())

	alternatives := []*Operation{
		{ID: "fast", Task: "fast approach"},
		{ID: "slow", Task: "slow approach"},
	}

	start := time.Now()
	result, err := executor.ExecuteSpeculative(context.Background(), alternatives)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, "fast", result.Winner.ID)
	assert.Equal(t, "fast response", result.Winner.Response)
	assert.Contains(t, result.Cancelled, "slow")

	// Should complete quickly (before slow finishes)
	assert.Less(t, elapsed, 150*time.Millisecond)
}

func TestExecutor_ExecuteSpeculative_AllFail(t *testing.T) {
	mock := newMockOrchestrator()
	mock.errors["op-1"] = errors.New("op-1 failed")
	mock.errors["op-2"] = errors.New("op-2 failed")

	executor := NewExecutor(mock, DefaultExecutorConfig())

	alternatives := []*Operation{
		{ID: "op-1", Task: "approach 1"},
		{ID: "op-2", Task: "approach 2"},
	}

	_, err := executor.ExecuteSpeculative(context.Background(), alternatives)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all speculative alternatives failed")
}

func TestExecutor_ExecuteSpeculative_SingleAlternative(t *testing.T) {
	mock := newMockOrchestrator()
	mock.responses["only"] = "only response"

	executor := NewExecutor(mock, DefaultExecutorConfig())

	alternatives := []*Operation{
		{ID: "only", Task: "only approach"},
	}

	result, err := executor.ExecuteSpeculative(context.Background(), alternatives)
	require.NoError(t, err)
	assert.Equal(t, "only", result.Winner.ID)
	assert.Empty(t, result.Cancelled)
}

func TestExecutor_ExecutePlan_DependencyAware(t *testing.T) {
	mock := newMockOrchestrator()

	executor := NewExecutor(mock, DefaultExecutorConfig())

	plan := NewExecutionPlan(StrategyParallel)
	plan.AddOperation(&Operation{ID: "a", Task: "task a", Priority: 3})
	plan.AddOperation(&Operation{ID: "b", Task: "task b", Priority: 2})
	plan.AddOperation(&Operation{ID: "c", Task: "task c", Priority: 1})
	plan.AddDependency("b", "a") // b depends on a
	plan.AddDependency("c", "b") // c depends on b

	result, err := executor.ExecutePlan(context.Background(), plan)
	require.NoError(t, err)

	assert.Len(t, result.Results, 3)
	assert.Equal(t, 300, result.TotalTokens)

	// All should succeed
	for _, r := range result.Results {
		assert.NoError(t, r.Error)
	}
}

func TestExecutor_ExecutePlan_CircularDependency(t *testing.T) {
	mock := newMockOrchestrator()
	executor := NewExecutor(mock, DefaultExecutorConfig())

	plan := NewExecutionPlan(StrategyParallel)
	plan.AddOperation(&Operation{ID: "a", Task: "task a"})
	plan.AddOperation(&Operation{ID: "b", Task: "task b"})
	plan.AddDependency("a", "b")
	plan.AddDependency("b", "a") // Circular!

	_, err := executor.ExecutePlan(context.Background(), plan)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency")
}

func TestExecutor_ExecutePlan_Sequential(t *testing.T) {
	mock := newMockOrchestrator()
	var executionOrder []string
	var mu sync.Mutex

	orderedOrch := &orderTrackingOrchestrator{
		mock:  mock,
		order: &executionOrder,
		mu:    &mu,
	}

	executor := NewExecutor(orderedOrch, DefaultExecutorConfig())

	plan := NewExecutionPlan(StrategySequential)
	plan.AddOperation(&Operation{ID: "a", Task: "task a", Priority: 3})
	plan.AddOperation(&Operation{ID: "b", Task: "task b", Priority: 1})
	plan.AddOperation(&Operation{ID: "c", Task: "task c", Priority: 2})

	result, err := executor.ExecutePlan(context.Background(), plan)
	require.NoError(t, err)
	assert.Len(t, result.Results, 3)

	// Should execute in priority order (highest first)
	assert.Equal(t, []string{"a", "c", "b"}, executionOrder)
}

func TestExecutor_EmptyOperations(t *testing.T) {
	mock := newMockOrchestrator()
	executor := NewExecutor(mock, DefaultExecutorConfig())

	result, err := executor.ExecuteParallel(context.Background(), []*Operation{})
	require.NoError(t, err)
	assert.Empty(t, result.Results)
	assert.Equal(t, 0, result.TotalTokens)
}

func TestExecutionPlan_GetReadyOperations(t *testing.T) {
	plan := NewExecutionPlan(StrategyParallel)
	plan.AddOperation(&Operation{ID: "a", Task: "task a"})
	plan.AddOperation(&Operation{ID: "b", Task: "task b"})
	plan.AddOperation(&Operation{ID: "c", Task: "task c"})
	plan.AddDependency("b", "a")
	plan.AddDependency("c", "a")

	// Initially only "a" should be ready
	ready := plan.GetReadyOperations(map[string]bool{})
	assert.Len(t, ready, 1)
	assert.Equal(t, "a", ready[0].ID)

	// After "a" completes, "b" and "c" should be ready
	ready = plan.GetReadyOperations(map[string]bool{"a": true})
	assert.Len(t, ready, 2)
}

func TestExecutionResult_AddResult(t *testing.T) {
	result := NewExecutionResult()

	result.AddResult(&OperationResult{ID: "a", Tokens: 100})
	assert.Equal(t, 100, result.TotalTokens)
	assert.False(t, result.PartialFailure)

	result.AddResult(&OperationResult{ID: "b", Tokens: 50, Error: errors.New("failed")})
	assert.Equal(t, 150, result.TotalTokens)
	assert.True(t, result.PartialFailure)
}

// slowMockOrchestrator adds delay for specific operations.
type slowMockOrchestrator struct {
	mock   *mockOrchestrator
	slowID string
	delay  time.Duration
}

func (s *slowMockOrchestrator) Orchestrate(ctx context.Context, op *Operation) (string, int, error) {
	if op.ID == s.slowID {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return "", 0, ctx.Err()
		}
	}
	return s.mock.Orchestrate(ctx, op)
}

// orderTrackingOrchestrator tracks execution order.
type orderTrackingOrchestrator struct {
	mock  *mockOrchestrator
	order *[]string
	mu    *sync.Mutex
}

func (o *orderTrackingOrchestrator) Orchestrate(ctx context.Context, op *Operation) (string, int, error) {
	o.mu.Lock()
	*o.order = append(*o.order, op.ID)
	o.mu.Unlock()
	return o.mock.Orchestrate(ctx, op)
}

// Concurrency test - run with -race flag.
func TestExecutor_RaceConditions(t *testing.T) {
	mock := newMockOrchestrator()
	executor := NewExecutor(mock, ExecutorConfig{
		MaxParallel:    10,
		PartialFailure: ContinueOnError,
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ops := make([]*Operation, 5)
			for j := 0; j < 5; j++ {
				ops[j] = &Operation{
					ID:   fmt.Sprintf("op-%d-%d", i, j),
					Task: fmt.Sprintf("task-%d-%d", i, j),
				}
			}
			_, _ = executor.ExecuteParallel(context.Background(), ops)
		}(i)
	}
	wg.Wait()
}

