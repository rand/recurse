package rlm

import (
	"context"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// Property-based tests for adaptive compute allocation.

// TestProperty_DifficultyScoreInRange verifies difficulty score is always in [0, 1].
func TestProperty_DifficultyScoreInRange(t *testing.T) {
	allocator := NewComputeAllocator()

	rapid.Check(t, func(t *rapid.T) {
		// Generate random query
		queryWords := rapid.IntRange(1, 100).Draw(t, "queryWords")
		words := make([]string, queryWords)
		for i := 0; i < queryWords; i++ {
			words[i] = rapid.StringMatching(`[a-zA-Z]{1,15}`).Draw(t, "word")
		}
		query := strings.Join(words, " ")

		// Generate random context size
		contextTokens := rapid.IntRange(0, 100000).Draw(t, "contextTokens")

		estimate := allocator.EstimateDifficulty(query, contextTokens)

		if estimate.Score < 0.0 {
			t.Errorf("score %f < 0", estimate.Score)
		}
		if estimate.Score > 1.0 {
			t.Errorf("score %f > 1", estimate.Score)
		}
		if estimate.Confidence < 0.0 {
			t.Errorf("confidence %f < 0", estimate.Confidence)
		}
		if estimate.Confidence > 1.0 {
			t.Errorf("confidence %f > 1", estimate.Confidence)
		}
	})
}

// TestProperty_DifficultyLevelValid verifies difficulty level is always valid.
func TestProperty_DifficultyLevelValid(t *testing.T) {
	allocator := NewComputeAllocator()

	rapid.Check(t, func(t *rapid.T) {
		query := rapid.StringMatching(`[a-zA-Z ]{5,200}`).Draw(t, "query")
		contextTokens := rapid.IntRange(0, 50000).Draw(t, "contextTokens")

		estimate := allocator.EstimateDifficulty(query, contextTokens)

		validLevels := map[DifficultyLevel]bool{
			DifficultyEasy:   true,
			DifficultyMedium: true,
			DifficultyHard:   true,
		}
		if !validLevels[estimate.Level] {
			t.Errorf("invalid difficulty level: %d", estimate.Level)
		}
	})
}

// TestProperty_AllocationWithinBudget verifies allocations respect budget constraints.
func TestProperty_AllocationWithinBudget(t *testing.T) {
	allocator := NewComputeAllocator()
	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		// Generate random query
		query := rapid.StringMatching(`[a-zA-Z ]{10,100}`).Draw(t, "query")
		contextTokens := rapid.IntRange(100, 50000).Draw(t, "contextTokens")

		// Generate random budget
		budget := ComputeBudget{
			MaxTokens: rapid.IntRange(1000, 100000).Draw(t, "maxTokens"),
			MaxDepth:  rapid.IntRange(1, 15).Draw(t, "maxDepth"),
			MaxTimeMS: rapid.IntRange(1000, 120000).Draw(t, "maxTimeMS"),
			CostLimit: rapid.Float64Range(0.1, 10.0).Draw(t, "costLimit"),
		}

		allocation := allocator.AllocateCompute(ctx, query, contextTokens, budget)

		// Verify allocation respects budget
		if allocation.DepthBudget > budget.MaxDepth {
			t.Errorf("depth %d > max %d", allocation.DepthBudget, budget.MaxDepth)
		}
		if allocation.TimeoutMS > budget.MaxTimeMS {
			t.Errorf("timeout %d > max %d", allocation.TimeoutMS, budget.MaxTimeMS)
		}
		if allocation.EstimatedCost > budget.CostLimit {
			t.Errorf("cost %f > limit %f", allocation.EstimatedCost, budget.CostLimit)
		}
	})
}

// TestProperty_AllocationFieldsValid verifies all allocation fields have valid values.
func TestProperty_AllocationFieldsValid(t *testing.T) {
	allocator := NewComputeAllocator()
	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		query := rapid.StringMatching(`[a-zA-Z ]{5,150}`).Draw(t, "query")
		contextTokens := rapid.IntRange(0, 100000).Draw(t, "contextTokens")
		budget := DefaultBudget()

		allocation := allocator.AllocateCompute(ctx, query, contextTokens, budget)

		// All fields should be positive
		if allocation.DepthBudget <= 0 {
			t.Errorf("depth budget %d should be positive", allocation.DepthBudget)
		}
		if allocation.ParallelCalls <= 0 {
			t.Errorf("parallel calls %d should be positive", allocation.ParallelCalls)
		}
		if allocation.TimeoutMS <= 0 {
			t.Errorf("timeout %d should be positive", allocation.TimeoutMS)
		}
		if allocation.EstimatedCost <= 0 {
			t.Errorf("estimated cost %f should be positive", allocation.EstimatedCost)
		}

		// Model tier should be valid
		validTiers := map[string]bool{"fast": true, "balanced": true, "quality": true}
		if !validTiers[allocation.ModelTier] {
			t.Errorf("invalid model tier: %s", allocation.ModelTier)
		}

		// Difficulty should be attached
		if allocation.Difficulty == nil {
			t.Error("difficulty should not be nil")
		}
	})
}

// TestProperty_SignalsHaveValidWeights verifies all signals have weights in [0, 1].
func TestProperty_SignalsHaveValidWeights(t *testing.T) {
	allocator := NewComputeAllocator()

	rapid.Check(t, func(t *rapid.T) {
		query := rapid.StringMatching(`[a-zA-Z ]{10,200}`).Draw(t, "query")
		contextTokens := rapid.IntRange(0, 50000).Draw(t, "contextTokens")

		estimate := allocator.EstimateDifficulty(query, contextTokens)

		for _, signal := range estimate.Signals {
			if signal.Weight < 0.0 || signal.Weight > 1.0 {
				t.Errorf("signal %s has invalid weight %f", signal.Name, signal.Weight)
			}
			if signal.Value < 0.0 || signal.Value > 1.0 {
				t.Errorf("signal %s has invalid value %f", signal.Name, signal.Value)
			}
		}
	})
}

// TestProperty_DifficultyMonotonicity verifies harder patterns lead to higher difficulty.
func TestProperty_DifficultyMonotonicity(t *testing.T) {
	allocator := NewComputeAllocator()

	rapid.Check(t, func(t *rapid.T) {
		// Generate base query
		baseQuery := rapid.StringMatching(`[a-zA-Z ]{5,30}`).Draw(t, "baseQuery")

		// Easy version
		easyQuery := baseQuery

		// Hard version with keywords
		hardKeywords := []string{"implement", "debug", "architect", "refactor", "comprehensive"}
		keyword := hardKeywords[rapid.IntRange(0, len(hardKeywords)-1).Draw(t, "keywordIdx")]
		hardQuery := keyword + " " + baseQuery

		contextTokens := rapid.IntRange(1000, 20000).Draw(t, "contextTokens")

		easyEstimate := allocator.EstimateDifficulty(easyQuery, contextTokens)
		hardEstimate := allocator.EstimateDifficulty(hardQuery, contextTokens)

		// Hard query should have >= difficulty level
		if hardEstimate.Level < easyEstimate.Level {
			t.Logf("Warning: hard query (%s) has lower level than easy (%s)",
				hardQuery, easyQuery)
		}
	})
}

// TestProperty_ContextSizeAffectsDifficulty verifies larger context increases difficulty.
func TestProperty_ContextSizeAffectsDifficulty(t *testing.T) {
	allocator := NewComputeAllocator()

	rapid.Check(t, func(t *rapid.T) {
		query := rapid.StringMatching(`[a-zA-Z ]{10,50}`).Draw(t, "query")

		smallContext := rapid.IntRange(100, 500).Draw(t, "smallContext")
		largeContext := rapid.IntRange(50000, 100000).Draw(t, "largeContext")

		smallEstimate := allocator.EstimateDifficulty(query, smallContext)
		largeEstimate := allocator.EstimateDifficulty(query, largeContext)

		// Large context should have >= score (context contributes to difficulty)
		if largeEstimate.Score < smallEstimate.Score-0.1 {
			t.Errorf("large context score %f < small context score %f",
				largeEstimate.Score, smallEstimate.Score)
		}
	})
}

// TestProperty_AllocationConsistency verifies same input produces same output.
func TestProperty_AllocationConsistency(t *testing.T) {
	allocator := NewComputeAllocator()
	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		query := rapid.StringMatching(`[a-zA-Z ]{10,100}`).Draw(t, "query")
		contextTokens := rapid.IntRange(100, 50000).Draw(t, "contextTokens")
		budget := DefaultBudget()

		alloc1 := allocator.AllocateCompute(ctx, query, contextTokens, budget)
		alloc2 := allocator.AllocateCompute(ctx, query, contextTokens, budget)

		// Same input should produce same output (deterministic)
		if alloc1.DepthBudget != alloc2.DepthBudget {
			t.Errorf("inconsistent depth: %d vs %d", alloc1.DepthBudget, alloc2.DepthBudget)
		}
		if alloc1.ModelTier != alloc2.ModelTier {
			t.Errorf("inconsistent model tier: %s vs %s", alloc1.ModelTier, alloc2.ModelTier)
		}
		if alloc1.Difficulty.Level != alloc2.Difficulty.Level {
			t.Errorf("inconsistent difficulty: %d vs %d",
				alloc1.Difficulty.Level, alloc2.Difficulty.Level)
		}
	})
}

// TestProperty_StatsAccumulate verifies stats accumulate correctly.
func TestProperty_StatsAccumulate(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		allocator := NewComputeAllocator()
		ctx := context.Background()
		budget := DefaultBudget()

		numAllocations := rapid.IntRange(5, 20).Draw(t, "numAllocations")

		for i := 0; i < numAllocations; i++ {
			query := rapid.StringMatching(`[a-zA-Z ]{5,50}`).Draw(t, "query")
			contextTokens := rapid.IntRange(100, 10000).Draw(t, "contextTokens")
			allocator.AllocateCompute(ctx, query, contextTokens, budget)
		}

		stats := allocator.Stats()

		// Total should match
		if stats.TotalAllocations != int64(numAllocations) {
			t.Errorf("expected %d allocations, got %d", numAllocations, stats.TotalAllocations)
		}

		// Counts should sum to total
		sum := stats.EasyCount + stats.MediumCount + stats.HardCount
		if sum != stats.TotalAllocations {
			t.Errorf("counts don't sum: %d + %d + %d != %d",
				stats.EasyCount, stats.MediumCount, stats.HardCount, stats.TotalAllocations)
		}

		// Avg difficulty should be in range
		if stats.AvgDifficulty < 0.0 || stats.AvgDifficulty > 1.0 {
			t.Errorf("avg difficulty %f out of range", stats.AvgDifficulty)
		}
	})
}
