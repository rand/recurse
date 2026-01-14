package embeddings

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/rand/recurse/internal/rlm/observability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

func TestVector_Similarity(t *testing.T) {
	tests := []struct {
		name     string
		v1       Vector
		v2       Vector
		expected float32
		delta    float32
	}{
		{
			name:     "identical vectors",
			v1:       Vector{1, 0, 0},
			v2:       Vector{1, 0, 0},
			expected: 1.0,
			delta:    0.001,
		},
		{
			name:     "orthogonal vectors",
			v1:       Vector{1, 0, 0},
			v2:       Vector{0, 1, 0},
			expected: 0.0,
			delta:    0.001,
		},
		{
			name:     "opposite vectors",
			v1:       Vector{1, 0, 0},
			v2:       Vector{-1, 0, 0},
			expected: -1.0,
			delta:    0.001,
		},
		{
			name:     "similar vectors",
			v1:       Vector{1, 1, 0},
			v2:       Vector{1, 0, 0},
			expected: 0.707, // cos(45°)
			delta:    0.01,
		},
		{
			name:     "empty vectors",
			v1:       Vector{},
			v2:       Vector{},
			expected: 0.0,
			delta:    0.001,
		},
		{
			name:     "different lengths",
			v1:       Vector{1, 0},
			v2:       Vector{1, 0, 0},
			expected: 0.0,
			delta:    0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.v1.Similarity(tt.v2)
			assert.InDelta(t, tt.expected, result, float64(tt.delta))
		})
	}
}

func TestVector_ToBytes_RoundTrip(t *testing.T) {
	original := Vector{1.5, -2.3, 0.0, 100.123, -0.001}

	bytes := original.ToBytes()
	restored := VectorFromBytes(bytes)

	require.Len(t, restored, len(original))
	for i := range original {
		assert.InDelta(t, original[i], restored[i], 0.0001)
	}
}

func TestVector_Normalize(t *testing.T) {
	v := Vector{3, 4, 0}
	normalized := v.Normalize()

	// Check unit length
	var sum float32
	for _, val := range normalized {
		sum += val * val
	}
	assert.InDelta(t, 1.0, sum, 0.001)

	// Check direction preserved
	assert.InDelta(t, 0.6, normalized[0], 0.001)
	assert.InDelta(t, 0.8, normalized[1], 0.001)
}

// mockProvider is a test provider that returns predictable embeddings.
type mockProvider struct {
	embeddings map[string]Vector
	calls      int
	dimensions int
	model      string
}

func newMockProvider() *mockProvider {
	return &mockProvider{
		embeddings: make(map[string]Vector),
		dimensions: 3,
		model:      "mock-model",
	}
}

func (p *mockProvider) Embed(ctx context.Context, texts []string) ([]Vector, error) {
	p.calls++
	result := make([]Vector, len(texts))
	for i, text := range texts {
		if vec, ok := p.embeddings[text]; ok {
			result[i] = vec
		} else {
			// Generate deterministic embedding based on text
			result[i] = Vector{float32(len(text)), float32(len(text) % 10), 0.5}
		}
	}
	return result, nil
}

func (p *mockProvider) Dimensions() int { return p.dimensions }
func (p *mockProvider) Model() string   { return p.model }

func TestCachedProvider_CacheHit(t *testing.T) {
	mock := newMockProvider()
	cached := NewCachedProvider(mock, WithCacheSize(100))

	ctx := context.Background()

	// First call should hit the provider
	_, err := cached.Embed(ctx, []string{"hello"})
	require.NoError(t, err)
	assert.Equal(t, 1, mock.calls)

	// Second call should use cache
	_, err = cached.Embed(ctx, []string{"hello"})
	require.NoError(t, err)
	assert.Equal(t, 1, mock.calls) // No additional call

	// Different text should hit provider
	_, err = cached.Embed(ctx, []string{"world"})
	require.NoError(t, err)
	assert.Equal(t, 2, mock.calls)

	// Check stats
	hits, misses := cached.CacheStats()
	assert.Equal(t, 1, hits)
	assert.Equal(t, 2, misses)
}

func TestCachedProvider_MixedCacheHits(t *testing.T) {
	mock := newMockProvider()
	cached := NewCachedProvider(mock)

	ctx := context.Background()

	// Prime cache with one text
	_, err := cached.Embed(ctx, []string{"cached"})
	require.NoError(t, err)
	assert.Equal(t, 1, mock.calls)

	// Request with mix of cached and uncached
	_, err = cached.Embed(ctx, []string{"cached", "uncached", "also-uncached"})
	require.NoError(t, err)
	assert.Equal(t, 2, mock.calls) // Only 1 additional call for 2 uncached texts
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestIndex_StoreAndSearch(t *testing.T) {
	db := newTestDB(t)
	mock := newMockProvider()

	// Set up predictable embeddings
	mock.embeddings["hello world"] = Vector{1, 0, 0}
	mock.embeddings["goodbye world"] = Vector{0.9, 0.1, 0}
	mock.embeddings["something else"] = Vector{0, 1, 0}

	idx, err := NewIndex(db, IndexConfig{
		Provider:  mock,
		BatchSize: 10,
		Workers:   1,
	})
	require.NoError(t, err)
	defer idx.Close()

	ctx := context.Background()

	// Index some nodes
	require.NoError(t, idx.IndexSync(ctx, "node-1", "hello world"))
	require.NoError(t, idx.IndexSync(ctx, "node-2", "goodbye world"))
	require.NoError(t, idx.IndexSync(ctx, "node-3", "something else"))

	// Check count
	count, err := idx.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// Search for similar to "hello world"
	mock.embeddings["query"] = Vector{1, 0, 0} // Same as "hello world"
	results, err := idx.Search(ctx, "query", 10)
	require.NoError(t, err)

	// "hello world" should be most similar
	require.NotEmpty(t, results)
	assert.Equal(t, "node-1", results[0].NodeID)
	assert.InDelta(t, 1.0, results[0].Similarity, 0.01)

	// "goodbye world" should be next
	if len(results) >= 2 {
		assert.Equal(t, "node-2", results[1].NodeID)
	}
}

func TestIndex_AsyncIndexing(t *testing.T) {
	db := newTestDB(t)
	mock := newMockProvider()

	idx, err := NewIndex(db, IndexConfig{
		Provider:  mock,
		BatchSize: 2,
		Workers:   1,
	})
	require.NoError(t, err)
	defer idx.Close()

	// Queue async indexing
	idx.IndexAsync("async-1", "text one")
	idx.IndexAsync("async-2", "text two")
	idx.IndexAsync("async-3", "text three")

	// Wait for processing
	time.Sleep(300 * time.Millisecond)

	ctx := context.Background()
	count, err := idx.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestIndex_Delete(t *testing.T) {
	db := newTestDB(t)
	mock := newMockProvider()

	idx, err := NewIndex(db, IndexConfig{
		Provider: mock,
		Workers:  1,
	})
	require.NoError(t, err)
	defer idx.Close()

	ctx := context.Background()

	// Index a node
	require.NoError(t, idx.IndexSync(ctx, "to-delete", "content"))
	assert.True(t, idx.HasEmbedding("to-delete"))

	// Delete it
	require.NoError(t, idx.Delete(ctx, "to-delete"))
	assert.False(t, idx.HasEmbedding("to-delete"))
}

func TestIndex_GetEmbedding(t *testing.T) {
	db := newTestDB(t)
	mock := newMockProvider()
	mock.embeddings["test content"] = Vector{1, 2, 3}

	idx, err := NewIndex(db, IndexConfig{
		Provider: mock,
		Workers:  1,
	})
	require.NoError(t, err)
	defer idx.Close()

	ctx := context.Background()

	// Index a node
	require.NoError(t, idx.IndexSync(ctx, "test-node", "test content"))

	// Retrieve embedding
	vec, err := idx.GetEmbedding(ctx, "test-node")
	require.NoError(t, err)
	require.NotNil(t, vec)
	assert.Len(t, vec, 3)
	assert.InDelta(t, 1.0, vec[0], 0.001)
	assert.InDelta(t, 2.0, vec[1], 0.001)
	assert.InDelta(t, 3.0, vec[2], 0.001)
}

func TestEmbeddingMetrics(t *testing.T) {
	registry := observability.NewRegistry()
	metrics := NewEmbeddingMetrics(registry)

	// Record some operations
	metrics.RecordEmbed(100*time.Millisecond, 5)
	metrics.RecordCacheHit(3)
	metrics.RecordCacheMiss(2)
	metrics.RecordSearch(50 * time.Millisecond)
	metrics.RecordHybridSearch(75 * time.Millisecond)
	metrics.SetQueueDepth(10)
	metrics.SetIndexSize(1000)

	// Check cache hit rate
	rate := metrics.CacheHitRate()
	assert.InDelta(t, 0.6, rate, 0.01) // 3 hits / 5 total = 0.6

	// Snapshot should contain data
	snap := metrics.Snapshot()
	assert.NotEmpty(t, snap.Counters)
	assert.NotEmpty(t, snap.Gauges)
	assert.NotEmpty(t, snap.Histograms)
}

func TestCachedProvider_WithMetrics(t *testing.T) {
	mock := newMockProvider()
	registry := observability.NewRegistry()
	metrics := NewEmbeddingMetrics(registry)
	cached := NewCachedProvider(mock, WithCacheMetrics(metrics))

	ctx := context.Background()

	// First call - cache miss
	_, err := cached.Embed(ctx, []string{"hello"})
	require.NoError(t, err)

	// Second call - cache hit
	_, err = cached.Embed(ctx, []string{"hello"})
	require.NoError(t, err)

	// Check metrics recorded cache events
	rate := metrics.CacheHitRate()
	assert.InDelta(t, 0.5, rate, 0.01) // 1 hit, 1 miss
}

// Integration test with real Voyage API (skipped unless VOYAGE_API_KEY is set)
func TestVoyageProvider_Integration(t *testing.T) {
	apiKey := os.Getenv("VOYAGE_API_KEY")
	if apiKey == "" {
		t.Skip("VOYAGE_API_KEY not set, skipping integration test")
	}

	provider, err := NewVoyageProvider(WithAPIKey(apiKey))
	require.NoError(t, err)

	ctx := context.Background()
	vectors, err := provider.Embed(ctx, []string{
		"Hello world",
		"Goodbye world",
	})

	require.NoError(t, err)
	require.Len(t, vectors, 2)
	assert.Len(t, vectors[0], 1024)
	assert.Len(t, vectors[1], 1024)

	// Similar texts should have high similarity
	sim := vectors[0].Similarity(vectors[1])
	assert.Greater(t, sim, float32(0.7))
}

func TestLocalProvider_Config(t *testing.T) {
	// Test provider creation with options
	provider, err := NewLocalProvider(
		WithLocalURL("http://localhost:12345"),
		WithLocalModel("test-model"),
		WithAutoStart(false),
	)
	require.NoError(t, err)
	assert.Equal(t, "test-model", provider.Model())
}

func TestLocalProvider_IsRunning(t *testing.T) {
	// Test with a port that definitely has no server
	provider, err := NewLocalProvider(
		WithAutoStart(false),
		WithLocalURL("http://127.0.0.1:59999"), // Unlikely to be in use
	)
	require.NoError(t, err)
	assert.False(t, provider.IsRunning())
}

func TestNewProvider_AutoDetect(t *testing.T) {
	// Without VOYAGE_API_KEY, should default to local
	t.Setenv("VOYAGE_API_KEY", "")
	t.Setenv("EMBEDDING_PROVIDER", "")

	provider, err := NewProvider()
	require.NoError(t, err)

	// Should be local provider
	_, ok := provider.(*LocalProvider)
	assert.True(t, ok, "expected LocalProvider when no VOYAGE_API_KEY")
}

func TestNewProvider_ExplicitLocal(t *testing.T) {
	t.Setenv("EMBEDDING_PROVIDER", "local")

	provider, err := NewProvider()
	require.NoError(t, err)

	_, ok := provider.(*LocalProvider)
	assert.True(t, ok)
}

// Integration test with local server (skipped unless server is running)
func TestLocalProvider_Integration(t *testing.T) {
	provider, err := NewLocalProvider(WithAutoStart(false))
	require.NoError(t, err)

	if !provider.IsRunning() {
		t.Skip("Local embedding server not running, skipping integration test")
	}

	ctx := context.Background()
	vectors, err := provider.Embed(ctx, []string{
		"def factorial(n): return 1 if n <= 1 else n * factorial(n-1)",
		"def fibonacci(n): return n if n <= 1 else fibonacci(n-1) + fibonacci(n-2)",
	})

	require.NoError(t, err)
	require.Len(t, vectors, 2)
	assert.Len(t, vectors[0], 768) // CodeRankEmbed outputs 768-dim

	// Similar code should have reasonable similarity
	sim := vectors[0].Similarity(vectors[1])
	assert.Greater(t, sim, float32(0.5))
}
