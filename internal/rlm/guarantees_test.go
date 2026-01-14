package rlm

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViolationType_String(t *testing.T) {
	tests := []struct {
		vtype    ViolationType
		expected string
	}{
		{ViolationBudget, "budget"},
		{ViolationTime, "time"},
		{ViolationDepth, "depth"},
		{ViolationCalls, "calls"},
		{ViolationType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.vtype.String())
		})
	}
}

func TestViolationSeverity_String(t *testing.T) {
	tests := []struct {
		severity ViolationSeverity
		expected string
	}{
		{SeverityWarning, "warning"},
		{SeveritySoft, "soft"},
		{SeverityHard, "hard"},
		{ViolationSeverity(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.severity.String())
		})
	}
}

func TestDegradationStrategy_String(t *testing.T) {
	tests := []struct {
		strategy DegradationStrategy
		expected string
	}{
		{DegradationFail, "fail"},
		{DegradationPartial, "partial"},
		{DegradationSynthesize, "synthesize"},
		{DegradationFallback, "fallback"},
		{DegradationStrategy(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.strategy.String())
		})
	}
}

func TestDefaultGuaranteesConfig(t *testing.T) {
	cfg := DefaultGuaranteesConfig()

	assert.Equal(t, 1.0, cfg.MaxCostUSD)
	assert.Equal(t, 5*time.Minute, cfg.MaxDuration)
	assert.Equal(t, 50, cfg.MaxRecursiveCalls)
	assert.Equal(t, 100000, cfg.MaxTokens)
}

func TestNewExecutionGuarantees(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        2.0,
		MaxDuration:       10 * time.Minute,
		MaxRecursiveCalls: 100,
		MaxTokens:         200000,
	}

	g := NewExecutionGuarantees(cfg)

	require.NotNil(t, g)
	assert.Equal(t, 2.0, g.maxCostUSD)
	assert.Equal(t, 10*time.Minute, g.maxDuration)
	assert.Equal(t, 100, g.maxRecursiveCalls)
	assert.Equal(t, 200000, g.maxTokens)
	assert.False(t, g.startTime.IsZero())
	assert.False(t, g.deadline.IsZero())
}

func TestCanProceed_WithinBudget(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        1.0,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 10,
		MaxTokens:         1000,
	}
	g := NewExecutionGuarantees(cfg)

	assert.True(t, g.CanProceed(0.1))
	assert.True(t, g.CanProceed(0.5))
	assert.True(t, g.CanProceed(0.99))
}

func TestCanProceed_CostExceeded(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        1.0,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 10,
		MaxTokens:         1000,
	}
	g := NewExecutionGuarantees(cfg)

	g.RecordCost(0.9)

	// Should fail if estimated cost would exceed budget
	assert.False(t, g.CanProceed(0.2))
	assert.True(t, g.CanProceed(0.05))
}

func TestCanProceed_CallsExceeded(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        100.0,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 3,
		MaxTokens:         100000,
	}
	g := NewExecutionGuarantees(cfg)

	g.RecordCall()
	g.RecordCall()
	assert.True(t, g.CanProceed(0.1))

	g.RecordCall()
	assert.False(t, g.CanProceed(0.1))
}

func TestCanProceed_TokensExceeded(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        100.0,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 100,
		MaxTokens:         1000,
	}
	g := NewExecutionGuarantees(cfg)

	g.RecordTokens(1000)
	assert.False(t, g.CanProceed(0.1))
}

func TestCanProceedWithTokens(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        100.0,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 100,
		MaxTokens:         1000,
	}
	g := NewExecutionGuarantees(cfg)

	assert.True(t, g.CanProceedWithTokens(500))
	assert.True(t, g.CanProceedWithTokens(1000))
	assert.False(t, g.CanProceedWithTokens(1001))

	g.RecordTokens(800)
	assert.False(t, g.CanProceedWithTokens(300))
	assert.True(t, g.CanProceedWithTokens(100))
}

func TestMustStop_NoViolations(t *testing.T) {
	g := NewExecutionGuarantees(DefaultGuaranteesConfig())
	assert.False(t, g.MustStop())
}

func TestMustStop_HardViolation(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        0.1,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 100,
		MaxTokens:         100000,
	}
	g := NewExecutionGuarantees(cfg)

	g.RecordCost(0.15)

	assert.True(t, g.MustStop())
}

func TestCheck_NoViolations(t *testing.T) {
	g := NewExecutionGuarantees(DefaultGuaranteesConfig())
	violations := g.Check()
	assert.Empty(t, violations)
}

func TestCheck_CostWarning(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        1.0,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 100,
		MaxTokens:         100000,
	}
	g := NewExecutionGuarantees(cfg)

	g.RecordCost(0.91) // 91% of budget

	violations := g.Check()
	require.Len(t, violations, 1)
	assert.Equal(t, ViolationBudget, violations[0].Type)
	assert.Equal(t, SeverityWarning, violations[0].Severity)
}

func TestCheck_CostHard(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        1.0,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 100,
		MaxTokens:         100000,
	}
	g := NewExecutionGuarantees(cfg)

	g.RecordCost(1.0) // 100% of budget

	violations := g.Check()
	require.Len(t, violations, 1)
	assert.Equal(t, ViolationBudget, violations[0].Type)
	assert.Equal(t, SeverityHard, violations[0].Severity)
}

func TestCheck_TokenWarning(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        100.0,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 100,
		MaxTokens:         1000,
	}
	g := NewExecutionGuarantees(cfg)

	g.RecordTokens(910) // 91% of budget

	violations := g.Check()
	require.Len(t, violations, 1)
	assert.Equal(t, ViolationBudget, violations[0].Type)
	assert.Equal(t, SeverityWarning, violations[0].Severity)
}

func TestCheck_TokenHard(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        100.0,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 100,
		MaxTokens:         1000,
	}
	g := NewExecutionGuarantees(cfg)

	g.RecordTokens(1000) // 100% of budget

	violations := g.Check()
	require.Len(t, violations, 1)
	assert.Equal(t, ViolationBudget, violations[0].Type)
	assert.Equal(t, SeverityHard, violations[0].Severity)
}

func TestCheck_CallsWarning(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        100.0,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 10,
		MaxTokens:         100000,
	}
	g := NewExecutionGuarantees(cfg)

	for i := 0; i < 9; i++ {
		g.RecordCall()
	}

	violations := g.Check()
	require.Len(t, violations, 1)
	assert.Equal(t, ViolationCalls, violations[0].Type)
	assert.Equal(t, SeverityWarning, violations[0].Severity)
}

func TestCheck_CallsHard(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        100.0,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 5,
		MaxTokens:         100000,
	}
	g := NewExecutionGuarantees(cfg)

	for i := 0; i < 5; i++ {
		g.RecordCall()
	}

	violations := g.Check()
	require.Len(t, violations, 1)
	assert.Equal(t, ViolationCalls, violations[0].Type)
	assert.Equal(t, SeverityHard, violations[0].Severity)
}

func TestCheck_MultipleViolations(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        0.1,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 2,
		MaxTokens:         100,
	}
	g := NewExecutionGuarantees(cfg)

	g.RecordCost(0.2)
	g.RecordTokens(200)
	g.RecordCall()
	g.RecordCall()

	violations := g.Check()
	assert.GreaterOrEqual(t, len(violations), 3)

	// All should be hard violations
	for _, v := range violations {
		assert.Equal(t, SeverityHard, v.Severity)
	}
}

func TestOnBudgetExhausted_CostLimit(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        0.5,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 100,
		MaxTokens:         100000,
	}
	g := NewExecutionGuarantees(cfg)

	g.RecordCost(0.5)

	plan := g.OnBudgetExhausted()
	require.NotNil(t, plan)
	assert.Contains(t, plan.Reason, "cost limit")
	assert.Equal(t, DegradationFail, plan.Strategy) // No checkpoint
	assert.True(t, plan.CanRetry)
	assert.Equal(t, 1.0, plan.SuggestedBudget.MaxCostUSD) // 2x original
}

func TestOnBudgetExhausted_TokenLimit(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        100.0,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 100,
		MaxTokens:         1000,
	}
	g := NewExecutionGuarantees(cfg)

	g.RecordTokens(1000)

	plan := g.OnBudgetExhausted()
	require.NotNil(t, plan)
	assert.Contains(t, plan.Reason, "token limit")
}

func TestOnBudgetExhausted_CallLimit(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        100.0,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 5,
		MaxTokens:         100000,
	}
	g := NewExecutionGuarantees(cfg)

	for i := 0; i < 5; i++ {
		g.RecordCall()
	}

	plan := g.OnBudgetExhausted()
	require.NotNil(t, plan)
	assert.Contains(t, plan.Reason, "call limit")
}

func TestOnBudgetExhausted_WithCheckpoint(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        0.5,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 100,
		MaxTokens:         100000,
	}
	g := NewExecutionGuarantees(cfg)

	g.Checkpoint("partial result here")
	g.RecordCost(0.5)

	plan := g.OnBudgetExhausted()
	require.NotNil(t, plan)
	assert.Equal(t, DegradationPartial, plan.Strategy)
	assert.Equal(t, "partial result here", plan.PartialResult)
}

func TestRecordCost(t *testing.T) {
	g := NewExecutionGuarantees(DefaultGuaranteesConfig())

	g.RecordCost(0.1)
	g.RecordCost(0.2)

	usage := g.Usage()
	assert.InDelta(t, 0.3, usage.CostUSD, 0.001)
}

func TestRecordTokens(t *testing.T) {
	g := NewExecutionGuarantees(DefaultGuaranteesConfig())

	g.RecordTokens(100)
	g.RecordTokens(200)

	usage := g.Usage()
	assert.Equal(t, 300, usage.Tokens)
}

func TestRecordCall(t *testing.T) {
	g := NewExecutionGuarantees(DefaultGuaranteesConfig())

	g.RecordCall()
	g.RecordCall()
	g.RecordCall()

	usage := g.Usage()
	assert.Equal(t, 3, usage.Calls)
}

func TestEnterExitRecursion(t *testing.T) {
	g := NewExecutionGuarantees(DefaultGuaranteesConfig())

	err := g.EnterRecursion()
	assert.NoError(t, err)
	err = g.EnterRecursion()
	assert.NoError(t, err)
	err = g.EnterRecursion()
	assert.NoError(t, err)

	usage := g.Usage()
	assert.Equal(t, 3, usage.MaxDepthSeen)

	g.ExitRecursion()
	g.ExitRecursion()

	// MaxDepthSeen should remain 3
	usage = g.Usage()
	assert.Equal(t, 3, usage.MaxDepthSeen)

	// Can't go below 0
	g.ExitRecursion()
	g.ExitRecursion()
	g.ExitRecursion() // Extra exit, should not panic
}

func TestCheckpoint(t *testing.T) {
	g := NewExecutionGuarantees(DefaultGuaranteesConfig())

	g.RecordCost(0.1)
	g.RecordTokens(500)
	g.RecordCall()
	g.EnterRecursion()

	g.Checkpoint("first checkpoint")

	cp := g.LastCheckpoint()
	require.NotNil(t, cp)
	assert.Equal(t, "first checkpoint", cp.PartialResult)
	assert.InDelta(t, 0.1, cp.CostUsed, 0.001)
	assert.Equal(t, 500, cp.TokensUsed)
	assert.Equal(t, 1, cp.CallsUsed)
	assert.Equal(t, 1, cp.Depth)
}

func TestCheckpoint_Multiple(t *testing.T) {
	g := NewExecutionGuarantees(DefaultGuaranteesConfig())

	g.Checkpoint("first")
	g.Checkpoint("second")
	g.Checkpoint("third")

	cp := g.LastCheckpoint()
	require.NotNil(t, cp)
	assert.Equal(t, "third", cp.PartialResult)
}

func TestLastCheckpoint_Empty(t *testing.T) {
	g := NewExecutionGuarantees(DefaultGuaranteesConfig())

	cp := g.LastCheckpoint()
	assert.Nil(t, cp)
}

func TestUsage(t *testing.T) {
	g := NewExecutionGuarantees(DefaultGuaranteesConfig())

	g.RecordCost(0.25)
	g.RecordTokens(5000)
	g.RecordCall()
	g.RecordCall()
	g.EnterRecursion()
	g.EnterRecursion()

	usage := g.Usage()
	assert.InDelta(t, 0.25, usage.CostUSD, 0.001)
	assert.Equal(t, 5000, usage.Tokens)
	assert.Equal(t, 2, usage.Calls)
	assert.Equal(t, 2, usage.MaxDepthSeen)
	assert.Greater(t, usage.Duration, time.Duration(0))
}

func TestRemainingBudget(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        1.0,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 10,
		MaxTokens:         1000,
	}
	g := NewExecutionGuarantees(cfg)

	g.RecordCost(0.3)
	g.RecordTokens(400)
	g.RecordCall()
	g.RecordCall()
	g.RecordCall()

	remaining := g.RemainingBudget()
	assert.InDelta(t, 0.7, remaining.CostUSD, 0.001)
	assert.Equal(t, 600, remaining.Tokens)
	assert.Equal(t, 7, remaining.Calls)
	assert.Greater(t, remaining.Duration, 4*time.Minute)
}

func TestViolations_RecordedOnExceed(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        0.1,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 100,
		MaxTokens:         100000,
	}
	g := NewExecutionGuarantees(cfg)

	// Start with no violations
	assert.Empty(t, g.Violations())

	// Exceed budget
	g.RecordCost(0.2)

	// Should have recorded a violation
	violations := g.Violations()
	assert.Len(t, violations, 1)
	assert.Equal(t, ViolationBudget, violations[0].Type)
	assert.Equal(t, SeverityHard, violations[0].Severity)
}

func TestViolations_OnlyRecordedOnce(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        0.1,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 100,
		MaxTokens:         100000,
	}
	g := NewExecutionGuarantees(cfg)

	// Exceed budget multiple times
	g.RecordCost(0.2)
	g.RecordCost(0.1)
	g.RecordCost(0.1)

	// Should still only have one violation
	violations := g.Violations()
	assert.Len(t, violations, 1)
}

func TestSetOnViolation(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        0.1,
		MaxDuration:       5 * time.Minute,
		MaxRecursiveCalls: 100,
		MaxTokens:         100000,
	}
	g := NewExecutionGuarantees(cfg)

	var receivedViolation *Violation
	var wg sync.WaitGroup
	wg.Add(1)

	g.SetOnViolation(func(v Violation) {
		receivedViolation = &v
		wg.Done()
	})

	g.RecordCost(0.2)

	// Wait for callback (runs in goroutine)
	wg.Wait()

	require.NotNil(t, receivedViolation)
	assert.Equal(t, ViolationBudget, receivedViolation.Type)
}

func TestContext_ReturnsDeadlineContext(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        100.0,
		MaxDuration:       1 * time.Second,
		MaxRecursiveCalls: 100,
		MaxTokens:         100000,
	}
	g := NewExecutionGuarantees(cfg)

	ctx, cancel := g.Context(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	assert.True(t, ok)
	assert.False(t, deadline.IsZero())
}

func TestContext_AlreadyPastDeadline(t *testing.T) {
	cfg := GuaranteesConfig{
		MaxCostUSD:        100.0,
		MaxDuration:       1 * time.Nanosecond, // Effectively instant
		MaxRecursiveCalls: 100,
		MaxTokens:         100000,
	}
	g := NewExecutionGuarantees(cfg)

	// Wait a bit to ensure we're past deadline
	time.Sleep(time.Millisecond)

	ctx, cancel := g.Context(context.Background())
	defer cancel()

	// Context should already be canceled
	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("context should be canceled when past deadline")
	}
}

func TestConcurrentAccess(t *testing.T) {
	g := NewExecutionGuarantees(DefaultGuaranteesConfig())

	var wg sync.WaitGroup
	numGoroutines := 100

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.RecordCost(0.001)
			g.RecordTokens(10)
			g.RecordCall()
			g.EnterRecursion()
			g.Checkpoint("test")
			g.ExitRecursion()
		}()
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = g.CanProceed(0.01)
			_ = g.CanProceedWithTokens(100)
			_ = g.Check()
			_ = g.Usage()
			_ = g.RemainingBudget()
			_ = g.MustStop()
		}()
	}

	wg.Wait()

	// Verify final state is consistent
	usage := g.Usage()
	assert.Equal(t, numGoroutines, usage.Calls)
	assert.Equal(t, numGoroutines*10, usage.Tokens)
	assert.InDelta(t, float64(numGoroutines)*0.001, usage.CostUSD, 0.0001)
}

// Benchmarks

func BenchmarkCanProceed(b *testing.B) {
	g := NewExecutionGuarantees(DefaultGuaranteesConfig())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.CanProceed(0.01)
	}
}

func BenchmarkCheck(b *testing.B) {
	g := NewExecutionGuarantees(DefaultGuaranteesConfig())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.Check()
	}
}

func BenchmarkRecordCost(b *testing.B) {
	g := NewExecutionGuarantees(DefaultGuaranteesConfig())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.RecordCost(0.0001)
	}
}

func BenchmarkCheckpoint(b *testing.B) {
	g := NewExecutionGuarantees(DefaultGuaranteesConfig())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Checkpoint("benchmark result")
	}
}
