package rlm

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Integration tests and benchmarks for adaptive compute allocation.

// TestIntegration_RealWorldQueries tests allocation with realistic queries.
func TestIntegration_RealWorldQueries(t *testing.T) {
	allocator := NewComputeAllocator()
	ctx := context.Background()
	budget := DefaultBudget()

	testCases := []struct {
		name           string
		query          string
		contextTokens  int
		expectedLevel  DifficultyLevel
		expectedTier   string
		maxDepth       int
	}{
		{
			name:          "simple math",
			query:         "What is 15 + 27?",
			contextTokens: 100,
			expectedLevel: DifficultyEasy,
			expectedTier:  "fast",
			maxDepth:      2,
		},
		{
			name:          "code explanation",
			query:         "Explain what this function does",
			contextTokens: 500,
			expectedLevel: DifficultyMedium,
			expectedTier:  "balanced",
			maxDepth:      5,
		},
		{
			name:          "summarize document",
			query:         "Summarize the key points of this document",
			contextTokens: 5000,
			expectedLevel: DifficultyMedium,
			expectedTier:  "balanced",
			maxDepth:      5,
		},
		{
			name:          "debug error",
			query:         "Debug this error: connection refused when connecting to database",
			contextTokens: 10000,
			expectedLevel: DifficultyHard,
			expectedTier:  "quality",
			maxDepth:      10,
		},
		{
			name:          "implement feature",
			query:         "Implement a caching layer with LRU eviction policy",
			contextTokens: 15000,
			expectedLevel: DifficultyHard,
			expectedTier:  "quality",
			maxDepth:      10,
		},
		{
			name:          "architect system",
			query:         "Design the architecture for a distributed message queue system",
			contextTokens: 20000,
			expectedLevel: DifficultyHard,
			expectedTier:  "quality",
			maxDepth:      10,
		},
		{
			name:          "refactor codebase",
			query:         "Refactor this module to use dependency injection",
			contextTokens: 25000,
			expectedLevel: DifficultyHard,
			expectedTier:  "quality",
			maxDepth:      10,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			allocation := allocator.AllocateCompute(ctx, tc.query, tc.contextTokens, budget)

			assert.Equal(t, tc.expectedLevel, allocation.Difficulty.Level,
				"difficulty level mismatch for %s", tc.name)
			assert.Equal(t, tc.expectedTier, allocation.ModelTier,
				"model tier mismatch for %s", tc.name)
			assert.LessOrEqual(t, allocation.DepthBudget, tc.maxDepth,
				"depth budget exceeded for %s", tc.name)
		})
	}
}

// TestIntegration_EfficiencyComparison compares adaptive vs static allocation.
func TestIntegration_EfficiencyComparison(t *testing.T) {
	allocator := NewComputeAllocator()
	ctx := context.Background()
	budget := DefaultBudget()

	// Mix of queries with known difficulties
	queries := []struct {
		query         string
		contextTokens int
		difficulty    DifficultyLevel
	}{
		{"What is 2+2?", 100, DifficultyEasy},
		{"What time is it?", 50, DifficultyEasy},
		{"Summarize this", 1000, DifficultyMedium},
		{"Compare A and B", 2000, DifficultyMedium},
		{"Debug this error", 10000, DifficultyHard},
		{"Implement feature X", 15000, DifficultyHard},
	}

	// Static allocation (always uses max resources)
	staticCost := 0.0
	staticDepth := 0
	for range queries {
		staticCost += budget.CostLimit
		staticDepth += budget.MaxDepth
	}

	// Adaptive allocation
	adaptiveCost := 0.0
	adaptiveDepth := 0
	for _, q := range queries {
		alloc := allocator.AllocateCompute(ctx, q.query, q.contextTokens, budget)
		adaptiveCost += alloc.EstimatedCost
		adaptiveDepth += alloc.DepthBudget
	}

	// Adaptive should be significantly cheaper
	costRatio := adaptiveCost / staticCost
	depthRatio := float64(adaptiveDepth) / float64(staticDepth)

	t.Logf("Cost ratio (adaptive/static): %.2f", costRatio)
	t.Logf("Depth ratio (adaptive/static): %.2f", depthRatio)

	// Expect at least 2x efficiency gain (spec targets 4x)
	assert.Less(t, costRatio, 0.5, "adaptive should use <50%% of static cost")
	assert.Less(t, depthRatio, 0.6, "adaptive should use <60%% of static depth")
}

// TestIntegration_BatchAllocation tests allocating for multiple queries.
func TestIntegration_BatchAllocation(t *testing.T) {
	allocator := NewComputeAllocator()
	ctx := context.Background()
	budget := DefaultBudget()

	// Simulate a batch of incoming queries
	queries := []string{
		"What is Python?",
		"List the files in this directory",
		"Analyze the performance of this algorithm",
		"Implement a binary search tree",
		"Debug why this test is failing intermittently",
		"Refactor the authentication module to support OAuth2",
		"Design a comprehensive monitoring solution",
	}

	var allocations []*ComputeAllocation
	for _, q := range queries {
		alloc := allocator.AllocateCompute(ctx, q, 5000, budget)
		allocations = append(allocations, alloc)
	}

	// Verify we got a mix of difficulty levels
	levelCounts := make(map[DifficultyLevel]int)
	for _, a := range allocations {
		levelCounts[a.Difficulty.Level]++
	}

	assert.Greater(t, len(levelCounts), 1, "should have queries at different difficulty levels")

	// Stats should reflect allocations
	stats := allocator.Stats()
	assert.Equal(t, int64(len(queries)), stats.TotalAllocations)
}

// BenchmarkStaticVsAdaptive compares static and adaptive allocation performance.
func BenchmarkStaticVsAdaptive(b *testing.B) {
	queries := []struct {
		query         string
		contextTokens int
	}{
		{"what is 2+2", 100},
		{"summarize this document", 5000},
		{"debug this error in the authentication system", 20000},
	}

	b.Run("Static", func(b *testing.B) {
		// Static: always allocate max resources
		budget := DefaultBudget()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			q := queries[i%len(queries)]
			// Static allocation: constant regardless of query
			_ = &ComputeAllocation{
				DepthBudget:   budget.MaxDepth,
				ModelTier:     "quality",
				ParallelCalls: 4,
				TimeoutMS:     budget.MaxTimeMS,
				EstimatedCost: budget.CostLimit,
			}
			_ = q // use q to avoid optimization
		}
	})

	b.Run("Adaptive", func(b *testing.B) {
		allocator := NewComputeAllocator()
		ctx := context.Background()
		budget := DefaultBudget()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			q := queries[i%len(queries)]
			_ = allocator.AllocateCompute(ctx, q.query, q.contextTokens, budget)
		}
	})
}

// BenchmarkCostComparison measures cost savings from adaptive allocation.
func BenchmarkCostComparison(b *testing.B) {
	allocator := NewComputeAllocator()
	ctx := context.Background()
	budget := DefaultBudget()

	// Realistic query distribution: 60% easy, 30% medium, 10% hard
	queries := []struct {
		query         string
		contextTokens int
		weight        int // frequency weight
	}{
		{"what is the answer", 100, 20},
		{"show me the file", 200, 20},
		{"is this correct", 150, 20},
		{"summarize this", 2000, 10},
		{"compare these options", 3000, 10},
		{"analyze the data", 5000, 10},
		{"implement this feature", 15000, 5},
		{"debug this complex error", 20000, 5},
	}

	// Build weighted query list
	var weightedQueries []struct {
		query         string
		contextTokens int
	}
	for _, q := range queries {
		for i := 0; i < q.weight; i++ {
			weightedQueries = append(weightedQueries, struct {
				query         string
				contextTokens int
			}{q.query, q.contextTokens})
		}
	}

	b.Run("TotalCost", func(b *testing.B) {
		totalAdaptiveCost := 0.0
		totalStaticCost := 0.0

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			q := weightedQueries[i%len(weightedQueries)]
			alloc := allocator.AllocateCompute(ctx, q.query, q.contextTokens, budget)
			totalAdaptiveCost += alloc.EstimatedCost
			totalStaticCost += budget.CostLimit
		}

		b.ReportMetric(totalAdaptiveCost/float64(b.N), "adaptive_cost/op")
		b.ReportMetric(totalStaticCost/float64(b.N), "static_cost/op")
		b.ReportMetric(totalStaticCost/totalAdaptiveCost, "efficiency_ratio")
	})
}

// BenchmarkDifficultyPatterns benchmarks pattern matching performance.
func BenchmarkDifficultyPatterns(b *testing.B) {
	allocator := NewComputeAllocator()

	patternQueries := []string{
		"what is 2+2",                                          // trivial math
		"summarize this document for me please",                // summarize
		"compare these two different approaches to the problem", // compare
		"debug this error in the authentication module",        // debug
		"implement a comprehensive test suite for all modules", // implement + comprehensive
	}

	for _, query := range patternQueries {
		name := query
		if len(name) > 30 {
			name = name[:30] + "..."
		}
		b.Run(name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = allocator.EstimateDifficulty(query, 10000)
			}
		})
	}
}

// BenchmarkAllocationByDifficulty benchmarks allocation at each difficulty level.
func BenchmarkAllocationByDifficulty(b *testing.B) {
	allocator := NewComputeAllocator()
	ctx := context.Background()
	budget := DefaultBudget()

	levels := []struct {
		name          string
		query         string
		contextTokens int
	}{
		{"easy", "what is 2+2", 100},
		{"medium", "summarize this document", 5000},
		{"hard", "implement comprehensive error handling", 30000},
	}

	for _, l := range levels {
		b.Run(l.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = allocator.AllocateCompute(ctx, l.query, l.contextTokens, budget)
			}
		})
	}
}

// BenchmarkQueryLengthImpact benchmarks impact of query length on estimation.
func BenchmarkQueryLengthImpact(b *testing.B) {
	allocator := NewComputeAllocator()
	lengths := []int{5, 20, 50, 100, 200}

	for _, length := range lengths {
		b.Run(fmt.Sprintf("words-%d", length), func(b *testing.B) {
			query := strings.Repeat("word ", length)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = allocator.EstimateDifficulty(query, 10000)
			}
		})
	}
}

// BenchmarkContextSizeImpact benchmarks impact of context size on estimation.
func BenchmarkContextSizeImpact(b *testing.B) {
	allocator := NewComputeAllocator()
	query := "analyze this code"
	sizes := []int{100, 1000, 10000, 50000, 100000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("tokens-%d", size), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = allocator.EstimateDifficulty(query, size)
			}
		})
	}
}

// BenchmarkConcurrentAllocation benchmarks concurrent allocation requests.
func BenchmarkConcurrentAllocation(b *testing.B) {
	allocator := NewComputeAllocator()
	ctx := context.Background()
	budget := DefaultBudget()

	b.RunParallel(func(pb *testing.PB) {
		queries := []string{
			"what is this",
			"summarize the document",
			"implement the feature",
		}
		i := 0
		for pb.Next() {
			query := queries[i%len(queries)]
			_ = allocator.AllocateCompute(ctx, query, 5000, budget)
			i++
		}
	})
}
