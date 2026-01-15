package compress

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		m := NewManager(DefaultManagerConfig())
		assert.NotNil(t, m)
		assert.NotNil(t, m.base)
		assert.NotNil(t, m.hierarchical)
		assert.NotNil(t, m.incremental)
	})

	t.Run("custom config", func(t *testing.T) {
		cfg := ManagerConfig{
			CacheSize:          500,
			HierarchicalLevels: []float64{0.6, 0.3},
		}
		m := NewManager(cfg)
		assert.NotNil(t, m)
	})
}

func TestManager_PrepareContext(t *testing.T) {
	m := NewManager(DefaultManagerConfig())

	t.Run("empty chunks", func(t *testing.T) {
		result, err := m.PrepareContext(context.Background(), nil, "", 1000)
		require.NoError(t, err)
		assert.Equal(t, "", result.Content)
		assert.Equal(t, 0, result.OriginalTokens)
	})

	t.Run("single chunk under budget", func(t *testing.T) {
		chunks := []ContextChunk{
			{ID: "test", Content: "Hello world", Type: "text"},
		}

		result, err := m.PrepareContext(context.Background(), chunks, "", 1000)
		require.NoError(t, err)
		assert.Equal(t, "Hello world", result.Content)
		assert.Equal(t, 1.0, result.Ratio)
		assert.Len(t, result.ChunkResults, 1)
		assert.Equal(t, MethodPassthrough, result.ChunkResults[0].Method)
	})

	t.Run("multiple chunks under budget", func(t *testing.T) {
		chunks := []ContextChunk{
			{ID: "chunk1", Content: "First chunk content.", Type: "file"},
			{ID: "chunk2", Content: "Second chunk content.", Type: "search"},
		}

		result, err := m.PrepareContext(context.Background(), chunks, "", 1000)
		require.NoError(t, err)
		assert.Contains(t, result.Content, "First chunk")
		assert.Contains(t, result.Content, "Second chunk")
		assert.Len(t, result.ChunkResults, 2)
	})

	t.Run("chunks requiring compression", func(t *testing.T) {
		// Create chunks that exceed budget
		chunks := []ContextChunk{
			{ID: "large1", Content: generateTestContent(30), Type: "file", Relevance: 0.8},
			{ID: "large2", Content: generateTestContent(30), Type: "search", Relevance: 0.2},
		}

		// Set a tight budget
		budget := 200

		result, err := m.PrepareContext(context.Background(), chunks, "test query", budget)
		require.NoError(t, err)

		// Should have compressed
		assert.Less(t, result.CompressedTokens, result.OriginalTokens)
		assert.Less(t, result.Ratio, 1.0)

		// Higher relevance chunk should get more tokens
		assert.Greater(t, result.ChunkResults[0].Allocated, result.ChunkResults[1].Allocated)
	})

	t.Run("respects chunk types in stats", func(t *testing.T) {
		m := NewManager(DefaultManagerConfig())
		chunks := []ContextChunk{
			{ID: "f1", Content: generateTestContent(30), Type: "file"},
			{ID: "s1", Content: generateTestContent(30), Type: "search"},
			{ID: "m1", Content: generateTestContent(30), Type: "memory"},
		}

		// Use tight budget to trigger compression (and thus stats update)
		_, err := m.PrepareContext(context.Background(), chunks, "", 200)
		require.NoError(t, err)

		stats := m.Stats()
		assert.Equal(t, int64(3), stats.TotalChunksProcessed)
	})
}

func TestManager_CompressChunk(t *testing.T) {
	m := NewManager(DefaultManagerConfig())

	chunk := ContextChunk{
		ID:      "test-chunk",
		Content: generateTestContent(25),
		Type:    "file",
	}

	opts := Options{
		TargetRatio: 0.5,
		MinTokens:   10,
	}

	result, err := m.CompressChunk(context.Background(), chunk, opts)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Less(t, result.CompressedTokens, result.OriginalTokens)
}

func TestManager_CompressHierarchical(t *testing.T) {
	m := NewManager(DefaultManagerConfig())

	content := generateTestContent(50)
	result, err := m.CompressHierarchical(context.Background(), content, DefaultOptions())
	require.NoError(t, err)

	assert.NotEmpty(t, result.Levels)
	assert.Greater(t, result.OriginalTokens, 0)
}

func TestManager_AllocateBudget(t *testing.T) {
	m := NewManager(DefaultManagerConfig())

	t.Run("equal relevance", func(t *testing.T) {
		chunks := []ContextChunk{
			{ID: "a", Content: "content a"},
			{ID: "b", Content: "content b"},
		}
		relevance := []float64{0.5, 0.5}
		originalTokens := []int{100, 100}
		budget := 100

		allocations := m.allocateBudget(chunks, relevance, originalTokens, budget)

		assert.Len(t, allocations, 2)
		// Each should get roughly half
		assert.InDelta(t, 50, allocations[0], 10)
		assert.InDelta(t, 50, allocations[1], 10)
	})

	t.Run("unequal relevance", func(t *testing.T) {
		chunks := []ContextChunk{
			{ID: "a", Content: "content a"},
			{ID: "b", Content: "content b"},
		}
		relevance := []float64{0.8, 0.2}
		originalTokens := []int{100, 100}
		budget := 100

		allocations := m.allocateBudget(chunks, relevance, originalTokens, budget)

		// Higher relevance should get more
		assert.Greater(t, allocations[0], allocations[1])
	})

	t.Run("respects original size cap", func(t *testing.T) {
		chunks := []ContextChunk{
			{ID: "small", Content: "small"},
			{ID: "large", Content: "large content here"},
		}
		relevance := []float64{0.5, 0.5}
		originalTokens := []int{10, 100}
		budget := 200

		allocations := m.allocateBudget(chunks, relevance, originalTokens, budget)

		// Small chunk shouldn't get more than its original size
		assert.LessOrEqual(t, allocations[0], originalTokens[0])
	})

	t.Run("ensures minimum allocation", func(t *testing.T) {
		chunks := []ContextChunk{
			{ID: "a", Content: generateTestContent(10)},
			{ID: "b", Content: generateTestContent(10)},
		}
		relevance := []float64{0.99, 0.01} // Very skewed
		originalTokens := []int{200, 200}
		budget := 200

		allocations := m.allocateBudget(chunks, relevance, originalTokens, budget)

		// Even low relevance chunk should get minimum
		assert.GreaterOrEqual(t, allocations[1], 50)
	})
}

func TestManager_Cache(t *testing.T) {
	m := NewManager(DefaultManagerConfig())

	// First compression
	chunks := []ContextChunk{
		{ID: "cached-chunk", Content: generateTestContent(20), Type: "file"},
	}

	_, err := m.PrepareContext(context.Background(), chunks, "", 50)
	require.NoError(t, err)

	// Second call should hit cache
	_, err = m.PrepareContext(context.Background(), chunks, "", 50)
	require.NoError(t, err)

	cacheStats := m.CacheStats()
	assert.Greater(t, cacheStats.Hits, int64(0))
}

func TestManager_InvalidateCache(t *testing.T) {
	m := NewManager(DefaultManagerConfig())

	chunks := []ContextChunk{
		{ID: "test", Content: generateTestContent(15), Type: "file"},
	}

	_, err := m.PrepareContext(context.Background(), chunks, "", 100)
	require.NoError(t, err)

	// Invalidate specific entry
	m.InvalidateCacheEntry("test")

	// Invalidate all
	m.InvalidateCache()

	cacheStats := m.CacheStats()
	assert.Equal(t, 0, cacheStats.Size)
}

func TestManager_Stats(t *testing.T) {
	m := NewManager(DefaultManagerConfig())

	// Perform some compressions
	for i := 0; i < 3; i++ {
		chunks := []ContextChunk{
			{ID: string(rune('a' + i)), Content: generateTestContent(20), Type: "file"},
		}
		_, err := m.PrepareContext(context.Background(), chunks, "", 100)
		require.NoError(t, err)
	}

	stats := m.Stats()
	assert.Equal(t, int64(3), stats.TotalCompressions)
	assert.Equal(t, int64(3), stats.TotalChunksProcessed)
	assert.Greater(t, stats.TotalTokensOriginal, int64(0))
}

func TestManager_Accessors(t *testing.T) {
	m := NewManager(DefaultManagerConfig())

	assert.NotNil(t, m.Base())
	assert.NotNil(t, m.Hierarchical())
	assert.NotNil(t, m.Incremental())
}
