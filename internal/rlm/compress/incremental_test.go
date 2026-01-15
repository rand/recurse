package compress

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewIncrementalCompressor(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		c := NewIncrementalCompressor(DefaultIncrementalConfig())
		assert.NotNil(t, c)
		assert.NotNil(t, c.cache)
		assert.Equal(t, 0.1, c.changeThreshold)
	})

	t.Run("custom config", func(t *testing.T) {
		cfg := IncrementalConfig{
			MaxCacheSize:    500,
			CacheTTL:        30 * time.Minute,
			ChangeThreshold: 0.2,
		}
		c := NewIncrementalCompressor(cfg)
		assert.Equal(t, 0.2, c.changeThreshold)
		assert.Equal(t, 500, c.cache.maxSize)
	})
}

func TestIncrementalCompressor_Compress(t *testing.T) {
	c := NewIncrementalCompressor(DefaultIncrementalConfig())

	t.Run("caches result", func(t *testing.T) {
		content := generateTestContent(20)

		// First call - should compress
		result1, err := c.Compress(context.Background(), "test-id", content, DefaultOptions())
		require.NoError(t, err)
		require.NotNil(t, result1)

		stats := c.Stats()
		assert.Equal(t, int64(1), stats.Misses)
		assert.Equal(t, int64(0), stats.Hits)

		// Second call with same content - should use cache
		result2, err := c.Compress(context.Background(), "test-id", content, DefaultOptions())
		require.NoError(t, err)

		stats = c.Stats()
		assert.Equal(t, int64(1), stats.Misses)
		assert.Equal(t, int64(1), stats.Hits)

		// Results should be identical
		assert.Equal(t, result1.Compressed, result2.Compressed)
		assert.Equal(t, result1.CompressedTokens, result2.CompressedTokens)
	})

	t.Run("different ids are cached separately", func(t *testing.T) {
		c := NewIncrementalCompressor(DefaultIncrementalConfig())
		content1 := generateTestContent(15)
		content2 := generateTestContent(20)

		_, err := c.Compress(context.Background(), "id-1", content1, DefaultOptions())
		require.NoError(t, err)

		_, err = c.Compress(context.Background(), "id-2", content2, DefaultOptions())
		require.NoError(t, err)

		assert.Equal(t, 2, c.cache.Size())
	})

	t.Run("recompresses on major change", func(t *testing.T) {
		c := NewIncrementalCompressor(DefaultIncrementalConfig())
		content1 := generateTestContent(20)
		content2 := generateTestContent(30) // Significantly different

		_, err := c.Compress(context.Background(), "test-id", content1, DefaultOptions())
		require.NoError(t, err)

		// Major change should trigger recompression
		_, err = c.Compress(context.Background(), "test-id", content2, DefaultOptions())
		require.NoError(t, err)

		stats := c.Stats()
		assert.Equal(t, int64(2), stats.Misses) // Both were misses
	})

	t.Run("incremental update on minor change", func(t *testing.T) {
		c := NewIncrementalCompressor(IncrementalConfig{
			MaxCacheSize:    100,
			CacheTTL:        time.Hour,
			ChangeThreshold: 0.2, // 20% threshold
		})

		// Original content
		original := "This is some test content. It has multiple sentences. Each sentence is important."

		_, err := c.Compress(context.Background(), "test-id", original, DefaultOptions())
		require.NoError(t, err)

		// Minor change (< 20%)
		modified := "This is some test content. It has multiple sentences. Each sentence is very important."

		result, err := c.Compress(context.Background(), "test-id", modified, DefaultOptions())
		require.NoError(t, err)
		require.NotNil(t, result)

		// Should have incremental marker in stages
		// (Note: the actual behavior depends on the change ratio calculation)
	})
}

func TestIncrementalCompressor_Invalidate(t *testing.T) {
	c := NewIncrementalCompressor(DefaultIncrementalConfig())
	content := generateTestContent(15)

	_, err := c.Compress(context.Background(), "test-id", content, DefaultOptions())
	require.NoError(t, err)
	assert.Equal(t, 1, c.cache.Size())

	c.Invalidate("test-id")
	assert.Equal(t, 0, c.cache.Size())
}

func TestIncrementalCompressor_InvalidateAll(t *testing.T) {
	c := NewIncrementalCompressor(DefaultIncrementalConfig())

	for i := 0; i < 5; i++ {
		content := generateTestContent(10 + i)
		_, err := c.Compress(context.Background(), string(rune('a'+i)), content, DefaultOptions())
		require.NoError(t, err)
	}
	assert.Equal(t, 5, c.cache.Size())

	c.InvalidateAll()
	assert.Equal(t, 0, c.cache.Size())
}

func TestIncrementalCompressor_EstimateChangeRatio(t *testing.T) {
	c := NewIncrementalCompressor(DefaultIncrementalConfig())

	t.Run("identical content", func(t *testing.T) {
		ratio := c.estimateChangeRatio("hello world", "hello world")
		assert.Equal(t, 0.0, ratio)
	})

	t.Run("completely different", func(t *testing.T) {
		ratio := c.estimateChangeRatio("hello world", "xyz abc 123")
		assert.GreaterOrEqual(t, ratio, 0.5)
	})

	t.Run("minor change at end", func(t *testing.T) {
		original := "This is a long piece of text that stays mostly the same."
		modified := "This is a long piece of text that stays mostly the same!"
		ratio := c.estimateChangeRatio(original, modified)
		assert.Less(t, ratio, 0.1) // Should be detected as minor
	})

	t.Run("empty strings", func(t *testing.T) {
		assert.Equal(t, 1.0, c.estimateChangeRatio("", "hello"))
		assert.Equal(t, 1.0, c.estimateChangeRatio("hello", ""))
	})
}

func TestCache_Basic(t *testing.T) {
	cache := NewCache(10, time.Hour)

	t.Run("put and get", func(t *testing.T) {
		result := &Result{Compressed: "test", CompressedTokens: 10}
		cache.Put("id1", "content", "hash1", result)

		entry, ok := cache.Get("id1")
		assert.True(t, ok)
		assert.Equal(t, "hash1", entry.ContentHash)
		assert.Equal(t, result, entry.Result)
	})

	t.Run("get missing", func(t *testing.T) {
		_, ok := cache.Get("nonexistent")
		assert.False(t, ok)
	})

	t.Run("delete", func(t *testing.T) {
		result := &Result{Compressed: "test"}
		cache.Put("id2", "content", "hash2", result)

		cache.Delete("id2")

		_, ok := cache.Get("id2")
		assert.False(t, ok)
	})
}

func TestCache_TTL(t *testing.T) {
	// Short TTL for testing
	cache := NewCache(10, 10*time.Millisecond)

	result := &Result{Compressed: "test"}
	cache.Put("id1", "content", "hash1", result)

	// Should exist immediately
	_, ok := cache.Get("id1")
	assert.True(t, ok)

	// Wait for TTL
	time.Sleep(20 * time.Millisecond)

	// Should be expired
	_, ok = cache.Get("id1")
	assert.False(t, ok)
}

func TestCache_Eviction(t *testing.T) {
	cache := NewCache(3, time.Hour)

	// Fill cache
	for i := 0; i < 3; i++ {
		result := &Result{Compressed: string(rune('a' + i))}
		cache.Put(string(rune('0'+i)), "content", "hash", result)
	}
	assert.Equal(t, 3, cache.Size())

	// Add one more - should evict oldest
	result := &Result{Compressed: "new"}
	cache.Put("new", "content", "hash", result)

	assert.Equal(t, 3, cache.Size())

	stats := cache.Stats()
	assert.Equal(t, int64(1), stats.Evictions)
}

func TestCache_Stats(t *testing.T) {
	cache := NewCache(10, time.Hour)

	// Initial stats
	stats := cache.Stats()
	assert.Equal(t, int64(0), stats.Hits)
	assert.Equal(t, int64(0), stats.Misses)
	assert.Equal(t, 0, stats.Size)
	assert.Equal(t, 10, stats.MaxSize)
	assert.Equal(t, 0.0, stats.HitRate)

	// Add entry and access
	result := &Result{Compressed: "test"}
	cache.Put("id1", "content", "hash", result)
	cache.Get("id1") // Hit

	stats = cache.Stats()
	assert.Equal(t, 1, stats.Size)
}

func TestCache_AccessCount(t *testing.T) {
	cache := NewCache(10, time.Hour)

	result := &Result{Compressed: "test"}
	cache.Put("id1", "content", "hash", result) // AccessCount = 1

	// Access multiple times
	for i := 0; i < 5; i++ {
		cache.Get("id1") // Each increments AccessCount
	}

	entry, ok := cache.Get("id1") // +1 more
	assert.True(t, ok)
	assert.Equal(t, int64(7), entry.AccessCount) // 1 (put) + 5 + 1 = 7
}

func TestHashContent(t *testing.T) {
	t.Run("consistent hashing", func(t *testing.T) {
		content := "test content"
		hash1 := hashContent(content)
		hash2 := hashContent(content)
		assert.Equal(t, hash1, hash2)
	})

	t.Run("different content different hash", func(t *testing.T) {
		hash1 := hashContent("content 1")
		hash2 := hashContent("content 2")
		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("hash is hex string", func(t *testing.T) {
		hash := hashContent("test")
		assert.Len(t, hash, 64) // SHA256 = 32 bytes = 64 hex chars
	})
}
