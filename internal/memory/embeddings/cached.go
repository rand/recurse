package embeddings

import (
	"context"
	"sync"
)

const defaultCacheSize = 1000

// CachedProvider wraps a Provider with an LRU cache for embeddings.
type CachedProvider struct {
	provider Provider
	cache    *lruCache
}

// CachedProviderOption is a functional option for CachedProvider.
type CachedProviderOption func(*cachedConfig)

type cachedConfig struct {
	maxSize int
}

// WithCacheSize sets the maximum number of cached embeddings.
func WithCacheSize(size int) CachedProviderOption {
	return func(c *cachedConfig) {
		c.maxSize = size
	}
}

// NewCachedProvider wraps a provider with caching.
func NewCachedProvider(provider Provider, opts ...CachedProviderOption) *CachedProvider {
	cfg := cachedConfig{
		maxSize: defaultCacheSize,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return &CachedProvider{
		provider: provider,
		cache:    newLRUCache(cfg.maxSize),
	}
}

// Embed generates embeddings, using cached values when available.
func (p *CachedProvider) Embed(ctx context.Context, texts []string) ([]Vector, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	results := make([]Vector, len(texts))
	var toEmbed []string
	var toEmbedIdx []int

	// Check cache for each text
	for i, text := range texts {
		if vec, ok := p.cache.Get(text); ok {
			results[i] = vec
		} else {
			toEmbed = append(toEmbed, text)
			toEmbedIdx = append(toEmbedIdx, i)
		}
	}

	// Embed uncached texts
	if len(toEmbed) > 0 {
		vectors, err := p.provider.Embed(ctx, toEmbed)
		if err != nil {
			return nil, err
		}

		for i, vec := range vectors {
			idx := toEmbedIdx[i]
			results[idx] = vec
			p.cache.Set(toEmbed[i], vec)
		}
	}

	return results, nil
}

// Dimensions returns the embedding dimension.
func (p *CachedProvider) Dimensions() int {
	return p.provider.Dimensions()
}

// Model returns the model identifier.
func (p *CachedProvider) Model() string {
	return p.provider.Model()
}

// CacheStats returns cache statistics.
func (p *CachedProvider) CacheStats() (hits, misses int) {
	return p.cache.Stats()
}

// lruCache is a simple LRU cache for embeddings.
type lruCache struct {
	mu       sync.RWMutex
	maxSize  int
	items    map[string]*lruItem
	order    []string // Most recently used at end
	hits     int
	misses   int
}

type lruItem struct {
	key   string
	value Vector
}

func newLRUCache(maxSize int) *lruCache {
	return &lruCache{
		maxSize: maxSize,
		items:   make(map[string]*lruItem),
		order:   make([]string, 0, maxSize),
	}
}

// Get retrieves a value from the cache.
func (c *lruCache) Get(key string) (Vector, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, ok := c.items[key]
	if !ok {
		c.misses++
		return nil, false
	}

	// Move to end (most recently used)
	c.moveToEnd(key)
	c.hits++

	return item.value, true
}

// Set adds or updates a value in the cache.
func (c *lruCache) Set(key string, value Vector) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if item, ok := c.items[key]; ok {
		item.value = value
		c.moveToEnd(key)
		return
	}

	// Evict if at capacity
	for len(c.items) >= c.maxSize && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.items, oldest)
	}

	// Add new item
	c.items[key] = &lruItem{key: key, value: value}
	c.order = append(c.order, key)
}

// Stats returns hit and miss counts.
func (c *lruCache) Stats() (hits, misses int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hits, c.misses
}

func (c *lruCache) moveToEnd(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			c.order = append(c.order, key)
			return
		}
	}
}
