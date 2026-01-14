package rlm

import (
	"context"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// Property-based tests for ExecutionGuarantees.

// TestProperty_CanProceedFalseAtLimit verifies CanProceed returns false when any limit is reached.
func TestProperty_CanProceedFalseAtLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random limits
		maxCost := rapid.Float64Range(0.1, 10.0).Draw(t, "maxCost")
		maxCalls := rapid.IntRange(1, 100).Draw(t, "maxCalls")
		maxTokens := rapid.IntRange(100, 100000).Draw(t, "maxTokens")

		cfg := GuaranteesConfig{
			MaxCostUSD:        maxCost,
			MaxDuration:       5 * time.Minute,
			MaxRecursiveCalls: maxCalls,
			MaxTokens:         maxTokens,
		}
		g := NewExecutionGuarantees(cfg)

		// Exhaust cost budget
		g.RecordCost(maxCost)

		// Should not be able to proceed with any additional cost
		if g.CanProceed(0.001) {
			t.Errorf("CanProceed should be false after cost limit reached")
		}
	})
}

// TestProperty_CanProceedFalseAtCallLimit verifies CanProceed returns false when call limit is reached.
func TestProperty_CanProceedFalseAtCallLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxCalls := rapid.IntRange(1, 50).Draw(t, "maxCalls")

		cfg := GuaranteesConfig{
			MaxCostUSD:        100.0,
			MaxDuration:       5 * time.Minute,
			MaxRecursiveCalls: maxCalls,
			MaxTokens:         100000,
		}
		g := NewExecutionGuarantees(cfg)

		// Record exactly maxCalls
		for i := 0; i < maxCalls; i++ {
			g.RecordCall()
		}

		// Should not be able to proceed
		if g.CanProceed(0.001) {
			t.Errorf("CanProceed should be false after call limit reached (calls=%d, max=%d)",
				g.callsUsed, maxCalls)
		}
	})
}

// TestProperty_CanProceedFalseAtTokenLimit verifies CanProceed returns false when token limit is reached.
func TestProperty_CanProceedFalseAtTokenLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxTokens := rapid.IntRange(100, 10000).Draw(t, "maxTokens")

		cfg := GuaranteesConfig{
			MaxCostUSD:        100.0,
			MaxDuration:       5 * time.Minute,
			MaxRecursiveCalls: 100,
			MaxTokens:         maxTokens,
		}
		g := NewExecutionGuarantees(cfg)

		// Record exactly maxTokens
		g.RecordTokens(maxTokens)

		// Should not be able to proceed
		if g.CanProceed(0.001) {
			t.Errorf("CanProceed should be false after token limit reached")
		}
	})
}

// TestProperty_CostUsedNeverExceedsRecorded verifies cost tracking is accurate.
func TestProperty_CostUsedNeverExceedsRecorded(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		g := NewExecutionGuarantees(DefaultGuaranteesConfig())

		// Generate random costs to record
		numRecords := rapid.IntRange(1, 100).Draw(t, "numRecords")
		var totalCost float64

		for i := 0; i < numRecords; i++ {
			cost := rapid.Float64Range(0.001, 0.1).Draw(t, "cost")
			g.RecordCost(cost)
			totalCost += cost
		}

		usage := g.Usage()
		if usage.CostUSD < totalCost-0.0001 || usage.CostUSD > totalCost+0.0001 {
			t.Errorf("cost tracking mismatch: recorded %.6f, usage %.6f", totalCost, usage.CostUSD)
		}
	})
}

// TestProperty_TokensUsedEqualsRecorded verifies token tracking is accurate.
func TestProperty_TokensUsedEqualsRecorded(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		g := NewExecutionGuarantees(DefaultGuaranteesConfig())

		numRecords := rapid.IntRange(1, 100).Draw(t, "numRecords")
		var totalTokens int

		for i := 0; i < numRecords; i++ {
			tokens := rapid.IntRange(1, 1000).Draw(t, "tokens")
			g.RecordTokens(tokens)
			totalTokens += tokens
		}

		usage := g.Usage()
		if usage.Tokens != totalTokens {
			t.Errorf("token tracking mismatch: recorded %d, usage %d", totalTokens, usage.Tokens)
		}
	})
}

// TestProperty_CallsUsedEqualsRecorded verifies call tracking is accurate.
func TestProperty_CallsUsedEqualsRecorded(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		g := NewExecutionGuarantees(DefaultGuaranteesConfig())

		numCalls := rapid.IntRange(1, 100).Draw(t, "numCalls")

		for i := 0; i < numCalls; i++ {
			g.RecordCall()
		}

		usage := g.Usage()
		if usage.Calls != numCalls {
			t.Errorf("call tracking mismatch: recorded %d, usage %d", numCalls, usage.Calls)
		}
	})
}

// TestProperty_RemainingBudgetConsistent verifies remaining = max - used.
func TestProperty_RemainingBudgetConsistent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxCost := rapid.Float64Range(1.0, 10.0).Draw(t, "maxCost")
		maxTokens := rapid.IntRange(1000, 100000).Draw(t, "maxTokens")
		maxCalls := rapid.IntRange(10, 100).Draw(t, "maxCalls")

		cfg := GuaranteesConfig{
			MaxCostUSD:        maxCost,
			MaxDuration:       5 * time.Minute,
			MaxRecursiveCalls: maxCalls,
			MaxTokens:         maxTokens,
		}
		g := NewExecutionGuarantees(cfg)

		// Record some usage
		usedCost := rapid.Float64Range(0.0, maxCost*0.8).Draw(t, "usedCost")
		usedTokens := rapid.IntRange(0, int(float64(maxTokens)*0.8)).Draw(t, "usedTokens")
		usedCalls := rapid.IntRange(0, int(float64(maxCalls)*0.8)).Draw(t, "usedCalls")

		g.RecordCost(usedCost)
		g.RecordTokens(usedTokens)
		for i := 0; i < usedCalls; i++ {
			g.RecordCall()
		}

		remaining := g.RemainingBudget()
		usage := g.Usage()

		// Verify: remaining + used = max
		if remaining.CostUSD+usage.CostUSD < maxCost-0.0001 || remaining.CostUSD+usage.CostUSD > maxCost+0.0001 {
			t.Errorf("cost inconsistent: remaining %.4f + used %.4f != max %.4f",
				remaining.CostUSD, usage.CostUSD, maxCost)
		}

		if remaining.Tokens+usage.Tokens != maxTokens {
			t.Errorf("tokens inconsistent: remaining %d + used %d != max %d",
				remaining.Tokens, usage.Tokens, maxTokens)
		}

		if remaining.Calls+usage.Calls != maxCalls {
			t.Errorf("calls inconsistent: remaining %d + used %d != max %d",
				remaining.Calls, usage.Calls, maxCalls)
		}
	})
}

// TestProperty_ViolationsOnlyRecordedOnce verifies violations aren't duplicated.
func TestProperty_ViolationsOnlyRecordedOnce(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxCost := rapid.Float64Range(0.1, 1.0).Draw(t, "maxCost")

		cfg := GuaranteesConfig{
			MaxCostUSD:        maxCost,
			MaxDuration:       5 * time.Minute,
			MaxRecursiveCalls: 100,
			MaxTokens:         100000,
		}
		g := NewExecutionGuarantees(cfg)

		// Exceed budget multiple times
		numExceeds := rapid.IntRange(2, 10).Draw(t, "numExceeds")
		for i := 0; i < numExceeds; i++ {
			g.RecordCost(maxCost / 2)
		}

		violations := g.Violations()

		// Count cost violations
		costViolations := 0
		for _, v := range violations {
			if v.Type == ViolationBudget && v.Severity == SeverityHard {
				costViolations++
			}
		}

		if costViolations > 1 {
			t.Errorf("cost violation recorded multiple times: %d", costViolations)
		}
	})
}

// TestProperty_CheckpointsPreserveState verifies checkpoint state is accurate.
func TestProperty_CheckpointsPreserveState(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		g := NewExecutionGuarantees(DefaultGuaranteesConfig())

		// Record some state
		cost := rapid.Float64Range(0.01, 0.5).Draw(t, "cost")
		tokens := rapid.IntRange(100, 10000).Draw(t, "tokens")
		calls := rapid.IntRange(1, 20).Draw(t, "calls")
		depth := rapid.IntRange(1, 5).Draw(t, "depth")
		result := rapid.StringMatching(`[a-zA-Z0-9 ]{10,100}`).Draw(t, "result")

		g.RecordCost(cost)
		g.RecordTokens(tokens)
		for i := 0; i < calls; i++ {
			g.RecordCall()
		}
		for i := 0; i < depth; i++ {
			g.EnterRecursion()
		}

		g.Checkpoint(result)

		cp := g.LastCheckpoint()
		if cp == nil {
			t.Fatal("checkpoint should not be nil")
		}

		if cp.PartialResult != result {
			t.Errorf("result mismatch: %s != %s", cp.PartialResult, result)
		}
		if cp.CostUsed < cost-0.0001 || cp.CostUsed > cost+0.0001 {
			t.Errorf("cost mismatch: %.4f != %.4f", cp.CostUsed, cost)
		}
		if cp.TokensUsed != tokens {
			t.Errorf("tokens mismatch: %d != %d", cp.TokensUsed, tokens)
		}
		if cp.CallsUsed != calls {
			t.Errorf("calls mismatch: %d != %d", cp.CallsUsed, calls)
		}
		if cp.Depth != depth {
			t.Errorf("depth mismatch: %d != %d", cp.Depth, depth)
		}
	})
}

// TestProperty_MustStopOnlyWhenHardViolation verifies MustStop semantics.
func TestProperty_MustStopOnlyWhenHardViolation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxCost := rapid.Float64Range(0.5, 2.0).Draw(t, "maxCost")

		cfg := GuaranteesConfig{
			MaxCostUSD:        maxCost,
			MaxDuration:       5 * time.Minute,
			MaxRecursiveCalls: 100,
			MaxTokens:         100000,
		}
		g := NewExecutionGuarantees(cfg)

		// Use less than max - should not stop
		usedCost := rapid.Float64Range(0.0, maxCost*0.89).Draw(t, "usedCost")
		g.RecordCost(usedCost)

		if g.MustStop() {
			t.Errorf("MustStop should be false when under limits (used %.4f, max %.4f)",
				usedCost, maxCost)
		}

		// Exceed max - should stop
		g.RecordCost(maxCost)

		if !g.MustStop() {
			t.Errorf("MustStop should be true when over limits")
		}
	})
}

// TestProperty_ContextDeadlineIsSet verifies Context() returns deadline context.
func TestProperty_ContextDeadlineIsSet(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		duration := rapid.IntRange(1, 300).Draw(t, "durationSeconds")

		cfg := GuaranteesConfig{
			MaxCostUSD:        100.0,
			MaxDuration:       time.Duration(duration) * time.Second,
			MaxRecursiveCalls: 100,
			MaxTokens:         100000,
		}
		g := NewExecutionGuarantees(cfg)

		ctx, cancel := g.Context(context.Background())
		defer cancel()

		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("context should have deadline")
		}

		// Deadline should be approximately startTime + duration
		expectedDeadline := time.Now().Add(time.Duration(duration) * time.Second)
		diff := deadline.Sub(expectedDeadline)
		if diff < -time.Second || diff > time.Second {
			t.Errorf("deadline off by too much: %v", diff)
		}
	})
}

// TestProperty_MaxDepthSeenTracksCorrectly verifies depth tracking.
func TestProperty_MaxDepthSeenTracksCorrectly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		g := NewExecutionGuarantees(DefaultGuaranteesConfig())

		maxDepth := rapid.IntRange(1, 20).Draw(t, "maxDepth")

		// Enter to maxDepth, then exit partially
		for i := 0; i < maxDepth; i++ {
			g.EnterRecursion()
		}

		exitCount := rapid.IntRange(0, maxDepth).Draw(t, "exitCount")
		for i := 0; i < exitCount; i++ {
			g.ExitRecursion()
		}

		usage := g.Usage()
		if usage.MaxDepthSeen != maxDepth {
			t.Errorf("MaxDepthSeen should be %d, got %d", maxDepth, usage.MaxDepthSeen)
		}
	})
}

// TestProperty_DegradationPlanHasSuggestedBudget verifies OnBudgetExhausted.
func TestProperty_DegradationPlanHasSuggestedBudget(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxCost := rapid.Float64Range(0.1, 5.0).Draw(t, "maxCost")
		maxCalls := rapid.IntRange(5, 50).Draw(t, "maxCalls")
		maxTokens := rapid.IntRange(1000, 50000).Draw(t, "maxTokens")

		cfg := GuaranteesConfig{
			MaxCostUSD:        maxCost,
			MaxDuration:       5 * time.Minute,
			MaxRecursiveCalls: maxCalls,
			MaxTokens:         maxTokens,
		}
		g := NewExecutionGuarantees(cfg)

		// Exhaust some limit
		g.RecordCost(maxCost)

		plan := g.OnBudgetExhausted()

		// Suggested budget should be 2x original
		if plan.SuggestedBudget.MaxCostUSD != maxCost*2 {
			t.Errorf("suggested cost should be 2x: %.4f != %.4f",
				plan.SuggestedBudget.MaxCostUSD, maxCost*2)
		}
		if plan.SuggestedBudget.MaxRecursiveCalls != maxCalls*2 {
			t.Errorf("suggested calls should be 2x: %d != %d",
				plan.SuggestedBudget.MaxRecursiveCalls, maxCalls*2)
		}
		if plan.SuggestedBudget.MaxTokens != maxTokens*2 {
			t.Errorf("suggested tokens should be 2x: %d != %d",
				plan.SuggestedBudget.MaxTokens, maxTokens*2)
		}
	})
}
