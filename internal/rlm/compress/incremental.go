// Package compress provides context compression for the RLM system.
package compress

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// IncrementalCompressor provides caching and efficient updates for compression.
// It avoids redundant compression by caching results and detecting when
// content has changed enough to require recompression.
type IncrementalCompressor struct {
	base  *ContextCompressor
	cache *Cache

	// Change threshold: content changes below this percentage use incremental update
	changeThreshold float64
}

// IncrementalConfig configures the incremental compressor.
type IncrementalConfig struct {
	// Base compressor configuration.
	Base Config

	// Cache configuration.
	MaxCacheSize int
	CacheTTL     time.Duration

	// ChangeThreshold is the percentage of change below which we use incremental update.
	// Default: 0.1 (10%)
	ChangeThreshold float64
}

// DefaultIncrementalConfig returns sensible defaults.
func DefaultIncrementalConfig() IncrementalConfig {
	return IncrementalConfig{
		Base:            DefaultConfig(),
		MaxCacheSize:    1000,
		CacheTTL:        time.Hour,
		ChangeThreshold: 0.1,
	}
}

// NewIncrementalCompressor creates a new incremental compressor.
func NewIncrementalCompressor(cfg IncrementalConfig) *IncrementalCompressor {
	threshold := cfg.ChangeThreshold
	if threshold <= 0 {
		threshold = 0.1
	}

	return &IncrementalCompressor{
		base:            NewContextCompressor(cfg.Base),
		cache:           NewCache(cfg.MaxCacheSize, cfg.CacheTTL),
		changeThreshold: threshold,
	}
}

// Compress compresses content, using cache when possible.
// The id parameter should be a stable identifier for the content (e.g., file path).
func (c *IncrementalCompressor) Compress(ctx context.Context, id, content string, opts Options) (*Result, error) {
	contentHash := hashContent(content)

	// Check cache
	if entry, ok := c.cache.Get(id); ok {
		if entry.ContentHash == contentHash {
			// Exact match - return cached result
			c.cache.stats.mu.Lock()
			c.cache.stats.Hits++
			c.cache.stats.mu.Unlock()
			return entry.Result, nil
		}

		// Content changed - check if we can do incremental update
		changeRatio := c.estimateChangeRatio(entry.OriginalContent, content)
		if changeRatio < c.changeThreshold {
			// Minor change - attempt incremental update
			result, err := c.incrementalUpdate(ctx, entry, content, opts)
			if err == nil {
				c.cache.Put(id, content, contentHash, result)
				return result, nil
			}
			// Fall through to full recompression on error
		}
	}

	// Cache miss or major change - full compression
	c.cache.stats.mu.Lock()
	c.cache.stats.Misses++
	c.cache.stats.mu.Unlock()

	result, err := c.base.Compress(ctx, content, opts)
	if err != nil {
		return nil, err
	}

	c.cache.Put(id, content, contentHash, result)
	return result, nil
}

// Update explicitly updates content for an id, using incremental compression if possible.
func (c *IncrementalCompressor) Update(ctx context.Context, id, newContent string, opts Options) (*Result, error) {
	return c.Compress(ctx, id, newContent, opts)
}

// Invalidate removes an entry from the cache.
func (c *IncrementalCompressor) Invalidate(id string) {
	c.cache.Delete(id)
}

// InvalidateAll clears the entire cache.
func (c *IncrementalCompressor) InvalidateAll() {
	c.cache.Clear()
}

// Stats returns cache statistics.
func (c *IncrementalCompressor) Stats() CacheStats {
	return c.cache.Stats()
}

// estimateChangeRatio estimates how much content has changed.
// Returns a value between 0 (identical) and 1 (completely different).
func (c *IncrementalCompressor) estimateChangeRatio(oldContent, newContent string) float64 {
	if oldContent == newContent {
		return 0
	}

	oldLen := len(oldContent)
	newLen := len(newContent)

	if oldLen == 0 || newLen == 0 {
		return 1.0
	}

	// Quick heuristic based on length difference and common prefix/suffix
	lenDiff := abs(float64(newLen-oldLen)) / float64(max(oldLen, newLen))

	// Find common prefix length
	commonPrefix := 0
	minLen := min(oldLen, newLen)
	for i := 0; i < minLen && oldContent[i] == newContent[i]; i++ {
		commonPrefix++
	}

	// Find common suffix length
	commonSuffix := 0
	for i := 0; i < minLen-commonPrefix && oldContent[oldLen-1-i] == newContent[newLen-1-i]; i++ {
		commonSuffix++
	}

	// Estimate unchanged portion
	unchangedRatio := float64(commonPrefix+commonSuffix) / float64(max(oldLen, newLen))

	// Combine heuristics
	changeRatio := (lenDiff + (1.0 - unchangedRatio)) / 2.0
	if changeRatio > 1.0 {
		changeRatio = 1.0
	}

	return changeRatio
}

// incrementalUpdate attempts to update compression incrementally.
// For minor changes, we can often reuse most of the previous compression.
func (c *IncrementalCompressor) incrementalUpdate(ctx context.Context, entry *CacheEntry, newContent string, opts Options) (*Result, error) {
	// For now, we do a targeted recompression that might benefit from
	// the base compressor's internal optimizations.
	// A more sophisticated implementation could diff and patch the compressed output.

	result, err := c.base.Compress(ctx, newContent, opts)
	if err != nil {
		return nil, err
	}

	// Mark as incremental in metadata
	if result.Metadata.StagesUsed == nil {
		result.Metadata.StagesUsed = []string{}
	}
	result.Metadata.StagesUsed = append([]string{"incremental"}, result.Metadata.StagesUsed...)

	return result, nil
}

// Cache provides thread-safe caching of compression results.
type Cache struct {
	mu       sync.RWMutex
	entries  map[string]*CacheEntry
	maxSize  int
	ttl      time.Duration
	stats    cacheStats
	eviction *evictionList
}

type cacheStats struct {
	mu     sync.Mutex
	Hits   int64
	Misses int64
}

// CacheEntry stores a cached compression result.
type CacheEntry struct {
	ID              string
	ContentHash     string
	OriginalContent string
	Result          *Result
	CreatedAt       time.Time
	LastAccess      time.Time
	AccessCount     int64
}

// CacheStats contains cache statistics.
type CacheStats struct {
	Hits        int64
	Misses      int64
	Size        int
	MaxSize     int
	HitRate     float64
	Evictions   int64
	Expirations int64
}

// evictionList tracks entries in LRU order.
type evictionList struct {
	order    []string
	evicted  int64
	expired  int64
}

// NewCache creates a new cache with the given size limit and TTL.
func NewCache(maxSize int, ttl time.Duration) *Cache {
	if maxSize <= 0 {
		maxSize = 1000
	}
	if ttl <= 0 {
		ttl = time.Hour
	}

	return &Cache{
		entries:  make(map[string]*CacheEntry),
		maxSize:  maxSize,
		ttl:      ttl,
		eviction: &evictionList{order: make([]string, 0)},
	}
}

// Get retrieves an entry from the cache.
func (c *Cache) Get(id string) (*CacheEntry, bool) {
	c.mu.RLock()
	entry, ok := c.entries[id]
	c.mu.RUnlock()

	if !ok {
		return nil, false
	}

	// Check TTL
	if time.Since(entry.CreatedAt) > c.ttl {
		c.Delete(id)
		c.eviction.expired++
		return nil, false
	}

	// Update access time
	c.mu.Lock()
	entry.LastAccess = time.Now()
	entry.AccessCount++
	c.mu.Unlock()

	return entry, true
}

// Put stores an entry in the cache.
func (c *Cache) Put(id, originalContent, contentHash string, result *Result) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict if necessary
	for len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	now := time.Now()
	c.entries[id] = &CacheEntry{
		ID:              id,
		ContentHash:     contentHash,
		OriginalContent: originalContent,
		Result:          result,
		CreatedAt:       now,
		LastAccess:      now,
		AccessCount:     1,
	}

	// Update eviction order
	c.eviction.order = append(c.eviction.order, id)
}

// Delete removes an entry from the cache.
func (c *Cache) Delete(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, id)
}

// Clear removes all entries from the cache.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*CacheEntry)
	c.eviction.order = make([]string, 0)
}

// Stats returns cache statistics.
func (c *Cache) Stats() CacheStats {
	c.mu.RLock()
	size := len(c.entries)
	c.mu.RUnlock()

	c.stats.mu.Lock()
	hits := c.stats.Hits
	misses := c.stats.Misses
	c.stats.mu.Unlock()

	var hitRate float64
	total := hits + misses
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	return CacheStats{
		Hits:        hits,
		Misses:      misses,
		Size:        size,
		MaxSize:     c.maxSize,
		HitRate:     hitRate,
		Evictions:   c.eviction.evicted,
		Expirations: c.eviction.expired,
	}
}

// evictOldest removes the oldest entry. Must be called with lock held.
func (c *Cache) evictOldest() {
	if len(c.eviction.order) == 0 {
		return
	}

	// Find first valid entry in order list
	for len(c.eviction.order) > 0 {
		oldest := c.eviction.order[0]
		c.eviction.order = c.eviction.order[1:]

		if _, exists := c.entries[oldest]; exists {
			delete(c.entries, oldest)
			c.eviction.evicted++
			return
		}
	}
}

// Size returns the current number of entries.
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// hashContent computes a hash of the content for comparison.
func hashContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// Note: uses built-in min/max from Go 1.21+
