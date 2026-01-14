package rlm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDifficultyLevel_String(t *testing.T) {
	tests := []struct {
		level    DifficultyLevel
		expected string
	}{
		{DifficultyEasy, "easy"},
		{DifficultyMedium, "medium"},
		{DifficultyHard, "hard"},
		{DifficultyLevel(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.level.String())
		})
	}
}

func TestComputeAllocator_EstimateDifficulty_EasyQueries(t *testing.T) {
	allocator := NewComputeAllocator()

	easyQueries := []string{
		"what is 2+2?",
		"what is the capital?",
		"is this correct?",
		"who is the author?",
	}

	for _, query := range easyQueries {
		t.Run(query, func(t *testing.T) {
			estimate := allocator.EstimateDifficulty(query, 100)
			assert.Equal(t, DifficultyEasy, estimate.Level, "query: %s", query)
			assert.Less(t, estimate.Score, 0.5)
		})
	}
}

func TestComputeAllocator_EstimateDifficulty_MediumQueries(t *testing.T) {
	allocator := NewComputeAllocator()

	mediumQueries := []string{
		"summarize this document for me",
		"compare these two approaches",
		"analyze the performance metrics",
		"list all the dependencies",
		"explain how this works, then show examples",
	}

	for _, query := range mediumQueries {
		t.Run(query, func(t *testing.T) {
			estimate := allocator.EstimateDifficulty(query, 5000)
			assert.GreaterOrEqual(t, int(estimate.Level), int(DifficultyMedium), "query: %s", query)
		})
	}
}

func TestComputeAllocator_EstimateDifficulty_HardQueries(t *testing.T) {
	allocator := NewComputeAllocator()

	hardQueries := []string{
		"debug this error in the authentication module",
		"implement a new caching system",
		"architect a microservices infrastructure",
		"refactor the entire codebase for better performance",
		"write comprehensive tests for all modules and packages",
	}

	for _, query := range hardQueries {
		t.Run(query, func(t *testing.T) {
			estimate := allocator.EstimateDifficulty(query, 20000)
			assert.Equal(t, DifficultyHard, estimate.Level, "query: %s", query)
			// Hard queries should have meaningful signals detected
			assert.Greater(t, len(estimate.Signals), 1)
		})
	}
}

func TestComputeAllocator_EstimateDifficulty_ContextSizeAffectsDifficulty(t *testing.T) {
	allocator := NewComputeAllocator()
	query := "explain this"

	// Small context
	smallEstimate := allocator.EstimateDifficulty(query, 500)

	// Large context should increase difficulty
	largeEstimate := allocator.EstimateDifficulty(query, 50000)

	assert.GreaterOrEqual(t, largeEstimate.Score, smallEstimate.Score)
}

func TestComputeAllocator_EstimateDifficulty_QueryLengthAffectsDifficulty(t *testing.T) {
	allocator := NewComputeAllocator()

	// Short query
	shortEstimate := allocator.EstimateDifficulty("help", 1000)

	// Long query
	longQuery := "I need help understanding how to implement a distributed caching system " +
		"that handles multiple data centers with eventual consistency and automatic failover " +
		"while maintaining high performance and low latency for read operations"
	longEstimate := allocator.EstimateDifficulty(longQuery, 1000)

	assert.Greater(t, longEstimate.Score, shortEstimate.Score)
}

func TestComputeAllocator_EstimateDifficulty_SignalsPresent(t *testing.T) {
	allocator := NewComputeAllocator()

	estimate := allocator.EstimateDifficulty("debug this error", 5000)

	// Should have pattern signal + length signal + context signal
	assert.GreaterOrEqual(t, len(estimate.Signals), 2)

	// Check for expected signals
	hasDebugSignal := false
	hasContextSignal := false
	for _, s := range estimate.Signals {
		if s.Name == "debug" {
			hasDebugSignal = true
		}
		if s.Name == "context_size" {
			hasContextSignal = true
		}
	}
	assert.True(t, hasDebugSignal, "should have debug pattern signal")
	assert.True(t, hasContextSignal, "should have context size signal")
}

func TestComputeAllocator_EstimateDifficulty_ScoreInRange(t *testing.T) {
	allocator := NewComputeAllocator()

	queries := []string{
		"what is 2+2",
		"summarize this",
		"implement a complex distributed system with comprehensive error handling",
		"",
		"a very long query " + string(make([]byte, 1000)),
	}

	for _, query := range queries {
		estimate := allocator.EstimateDifficulty(query, 10000)
		assert.GreaterOrEqual(t, estimate.Score, 0.0, "score should be >= 0")
		assert.LessOrEqual(t, estimate.Score, 1.0, "score should be <= 1")
		assert.GreaterOrEqual(t, estimate.Confidence, 0.0, "confidence should be >= 0")
		assert.LessOrEqual(t, estimate.Confidence, 1.0, "confidence should be <= 1")
	}
}

func TestComputeAllocator_AllocateCompute_EasyAllocation(t *testing.T) {
	allocator := NewComputeAllocator()
	ctx := context.Background()
	budget := DefaultBudget()

	allocation := allocator.AllocateCompute(ctx, "what is 2+2?", 100, budget)

	require.NotNil(t, allocation)
	assert.Equal(t, DifficultyEasy, allocation.Difficulty.Level)
	assert.Equal(t, "fast", allocation.ModelTier)
	assert.Equal(t, 1, allocation.ParallelCalls)
	assert.LessOrEqual(t, allocation.DepthBudget, 2)
	assert.Less(t, allocation.EstimatedCost, budget.CostLimit*0.2)
}

func TestComputeAllocator_AllocateCompute_MediumAllocation(t *testing.T) {
	allocator := NewComputeAllocator()
	ctx := context.Background()
	budget := DefaultBudget()

	allocation := allocator.AllocateCompute(ctx, "summarize this document and analyze key points", 5000, budget)

	require.NotNil(t, allocation)
	assert.GreaterOrEqual(t, int(allocation.Difficulty.Level), int(DifficultyMedium))
	assert.Equal(t, "balanced", allocation.ModelTier)
	assert.Equal(t, 2, allocation.ParallelCalls)
	assert.LessOrEqual(t, allocation.DepthBudget, 5)
}

func TestComputeAllocator_AllocateCompute_HardAllocation(t *testing.T) {
	allocator := NewComputeAllocator()
	ctx := context.Background()
	budget := DefaultBudget()

	allocation := allocator.AllocateCompute(ctx, "architect a comprehensive microservices system", 30000, budget)

	require.NotNil(t, allocation)
	assert.Equal(t, DifficultyHard, allocation.Difficulty.Level)
	assert.Equal(t, "quality", allocation.ModelTier)
	assert.Equal(t, 4, allocation.ParallelCalls)
	assert.Equal(t, budget.MaxDepth, allocation.DepthBudget)
}

func TestComputeAllocator_AllocateCompute_RespectsCustomBudget(t *testing.T) {
	allocator := NewComputeAllocator()
	ctx := context.Background()

	customBudget := ComputeBudget{
		MaxTokens: 10000,
		MaxDepth:  3,
		MaxTimeMS: 5000,
		CostLimit: 0.5,
	}

	// Hard query should still respect budget limits
	allocation := allocator.AllocateCompute(ctx, "implement comprehensive test coverage", 20000, customBudget)

	require.NotNil(t, allocation)
	assert.LessOrEqual(t, allocation.DepthBudget, customBudget.MaxDepth)
	assert.LessOrEqual(t, allocation.TimeoutMS, customBudget.MaxTimeMS)
	assert.LessOrEqual(t, allocation.EstimatedCost, customBudget.CostLimit)
}

func TestComputeAllocator_Stats(t *testing.T) {
	allocator := NewComputeAllocator()
	ctx := context.Background()
	budget := DefaultBudget()

	// Make several allocations
	allocator.AllocateCompute(ctx, "what is 2+2?", 100, budget)        // Easy
	allocator.AllocateCompute(ctx, "summarize this", 1000, budget)     // Medium
	allocator.AllocateCompute(ctx, "debug this error", 10000, budget)  // Hard
	allocator.AllocateCompute(ctx, "implement feature", 10000, budget) // Hard

	stats := allocator.Stats()

	assert.Equal(t, int64(4), stats.TotalAllocations)
	assert.Equal(t, int64(1), stats.EasyCount)
	assert.Equal(t, int64(1), stats.MediumCount)
	assert.Equal(t, int64(2), stats.HardCount)
	assert.Greater(t, stats.AvgDifficulty, 0.0)
	assert.Less(t, stats.AvgDifficulty, 1.0)
}

func TestDefaultBudget(t *testing.T) {
	budget := DefaultBudget()

	assert.Greater(t, budget.MaxTokens, 0)
	assert.Greater(t, budget.MaxDepth, 0)
	assert.Greater(t, budget.MaxTimeMS, 0)
	assert.Greater(t, budget.CostLimit, 0.0)
}

func TestEstimateDifficulty_StandaloneFunction(t *testing.T) {
	estimate := EstimateDifficulty("what is 2+2?", 100)

	require.NotNil(t, estimate)
	assert.Equal(t, DifficultyEasy, estimate.Level)
}

func TestAllocateCompute_StandaloneFunction(t *testing.T) {
	ctx := context.Background()

	allocation := AllocateCompute(ctx, "what is 2+2?", 100)

	require.NotNil(t, allocation)
	assert.NotNil(t, allocation.Difficulty)
	assert.Greater(t, allocation.DepthBudget, 0)
	assert.NotEmpty(t, allocation.ModelTier)
}

func TestAllocateComputeWithBudget_StandaloneFunction(t *testing.T) {
	ctx := context.Background()
	budget := ComputeBudget{
		MaxTokens: 5000,
		MaxDepth:  5,
		MaxTimeMS: 10000,
		CostLimit: 0.3,
	}

	allocation := AllocateComputeWithBudget(ctx, "summarize this", 1000, budget)

	require.NotNil(t, allocation)
	assert.LessOrEqual(t, allocation.DepthBudget, budget.MaxDepth)
	assert.LessOrEqual(t, allocation.TimeoutMS, budget.MaxTimeMS)
}

// Benchmark tests

func BenchmarkEstimateDifficulty(b *testing.B) {
	allocator := NewComputeAllocator()
	query := "implement a comprehensive error handling system"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = allocator.EstimateDifficulty(query, 10000)
	}
}

func BenchmarkAllocateCompute(b *testing.B) {
	allocator := NewComputeAllocator()
	ctx := context.Background()
	budget := DefaultBudget()
	query := "implement a comprehensive error handling system"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = allocator.AllocateCompute(ctx, query, 10000, budget)
	}
}

func BenchmarkEstimateDifficulty_VaryingComplexity(b *testing.B) {
	allocator := NewComputeAllocator()
	queries := []string{
		"what is 2+2?",
		"summarize this document",
		"implement comprehensive test coverage for all modules",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		query := queries[i%len(queries)]
		_ = allocator.EstimateDifficulty(query, 10000)
	}
}
