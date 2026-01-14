package cache

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// LLMClient is the interface for the underlying LLM client.
type LLMClient interface {
	// Complete sends a prompt and returns the response.
	Complete(ctx context.Context, prompt string, maxTokens int) (string, error)

	// CompleteWithCaching sends a prompt with cache control and returns response with usage.
	CompleteWithCaching(ctx context.Context, prompt *StructuredPrompt, maxTokens int) (string, *UsageStats, error)
}

// CacheAwareClient wraps an LLM client with caching optimization.
type CacheAwareClient struct {
	inner    LLMClient
	strategy Strategy
	manager  *SessionManager
	metrics  *ClientMetrics

	// Configuration
	enableCaching bool
	model         string // "sonnet" or "haiku" affects min cache size
}

// ClientMetrics tracks client-level metrics.
type ClientMetrics struct {
	totalCalls     int64
	cacheHits      int64
	cacheMisses    int64
	totalTokens    int64
	cachedTokens   int64
	estimatedCost  float64
	actualCost     float64
	mu             sync.Mutex
}

// NewCacheAwareClient creates a new cache-aware client.
func NewCacheAwareClient(inner LLMClient, opts ...ClientOption) *CacheAwareClient {
	c := &CacheAwareClient{
		inner:         inner,
		strategy:      NewDefaultStrategy(),
		manager:       NewSessionManager(nil),
		metrics:       &ClientMetrics{},
		enableCaching: true,
		model:         "sonnet",
	}

	for _, opt := range opts {
		opt(c)
	}

	// Update strategy based on model
	if c.model == "haiku" {
		c.strategy = NewDefaultStrategyWithMinTokens(MinCacheableTokensHaiku)
		c.manager = NewSessionManager(c.strategy)
	}

	return c
}

// ClientOption configures the cache-aware client.
type ClientOption func(*CacheAwareClient)

// WithStrategy sets the caching strategy.
func WithStrategy(strategy Strategy) ClientOption {
	return func(c *CacheAwareClient) {
		c.strategy = strategy
		c.manager = NewSessionManager(strategy)
	}
}

// WithCachingEnabled enables or disables caching.
func WithCachingEnabled(enabled bool) ClientOption {
	return func(c *CacheAwareClient) {
		c.enableCaching = enabled
	}
}

// WithModel sets the model (affects minimum cache size).
func WithModel(model string) ClientOption {
	return func(c *CacheAwareClient) {
		c.model = model
	}
}

// Complete sends a simple prompt without caching.
func (c *CacheAwareClient) Complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	atomic.AddInt64(&c.metrics.totalCalls, 1)
	return c.inner.Complete(ctx, prompt, maxTokens)
}

// CompleteWithSession sends a prompt using session-based caching.
func (c *CacheAwareClient) CompleteWithSession(
	ctx context.Context,
	sessionID string,
	query string,
	opts CompletionOptions,
) (string, *UsageStats, error) {
	atomic.AddInt64(&c.metrics.totalCalls, 1)

	// If caching is disabled, fall back to simple completion
	if !c.enableCaching {
		response, err := c.inner.Complete(ctx, c.buildSimplePrompt(query, opts), opts.MaxTokens)
		return response, &UsageStats{InputTokens: EstimateTokens(query)}, err
	}

	// Get or create session hierarchy
	hierarchy := c.manager.GetOrCreate(sessionID, opts.SystemPrompt, opts.SessionContext)

	// Build prompt from hierarchy
	prompt := hierarchy.BuildPrompt(query)

	// Send with caching
	response, usage, err := c.inner.CompleteWithCaching(ctx, prompt, opts.MaxTokens)
	if err != nil {
		return "", nil, err
	}

	// Update metrics
	c.recordUsage(usage)

	return response, usage, nil
}

// CompleteWithDecomposition sends a prompt as part of a decomposition chain.
func (c *CacheAwareClient) CompleteWithDecomposition(
	ctx context.Context,
	session *DecompositionSession,
	query string,
	maxTokens int,
) (string, *UsageStats, error) {
	atomic.AddInt64(&c.metrics.totalCalls, 1)

	// If caching is disabled, fall back to simple completion
	if !c.enableCaching {
		response, err := c.inner.Complete(ctx, query, maxTokens)
		return response, &UsageStats{InputTokens: EstimateTokens(query)}, err
	}

	// Prepare prompt using session's shared context
	prompt := session.PrepareSubcall(query)

	// Send with caching
	response, usage, err := c.inner.CompleteWithCaching(ctx, prompt, maxTokens)
	if err != nil {
		return "", nil, err
	}

	// Update metrics
	c.recordUsage(usage)

	return response, usage, nil
}

// PrepareCacheablePrompt structures a prompt for optimal caching.
func (c *CacheAwareClient) PrepareCacheablePrompt(
	systemPrompt string,
	sharedContext string,
	query string,
	expectedCalls int,
) *StructuredPrompt {
	ctx := &CacheContext{
		ExpectedCalls:   expectedCalls,
		IsDecomposition: expectedCalls > 1,
	}

	blocks := []CacheableBlock{
		{
			Content:    systemPrompt,
			Role:       RoleSystem,
			TokenCount: EstimateTokens(systemPrompt),
		},
	}

	if sharedContext != "" {
		blocks = append(blocks, CacheableBlock{
			Content:    sharedContext,
			Role:       RoleContext,
			TokenCount: EstimateTokens(sharedContext),
		})
	}

	blocks = append(blocks, CacheableBlock{
		Content:    query,
		Role:       RoleUser,
		TokenCount: EstimateTokens(query),
	})

	return c.strategy.StructurePrompt(blocks, ctx)
}

// Metrics returns the current client metrics.
func (c *CacheAwareClient) Metrics() ClientMetrics {
	c.metrics.mu.Lock()
	defer c.metrics.mu.Unlock()
	return ClientMetrics{
		totalCalls:    atomic.LoadInt64(&c.metrics.totalCalls),
		cacheHits:     atomic.LoadInt64(&c.metrics.cacheHits),
		cacheMisses:   atomic.LoadInt64(&c.metrics.cacheMisses),
		totalTokens:   atomic.LoadInt64(&c.metrics.totalTokens),
		cachedTokens:  atomic.LoadInt64(&c.metrics.cachedTokens),
		estimatedCost: c.metrics.estimatedCost,
		actualCost:    c.metrics.actualCost,
	}
}

// CacheStats returns cache hit/miss counts.
func (c *CacheAwareClient) CacheStats() (hits, misses int64) {
	return atomic.LoadInt64(&c.metrics.cacheHits), atomic.LoadInt64(&c.metrics.cacheMisses)
}

// SavingsRate returns the estimated cost savings rate (0-1).
func (c *CacheAwareClient) SavingsRate() float64 {
	c.metrics.mu.Lock()
	defer c.metrics.mu.Unlock()
	if c.metrics.estimatedCost == 0 {
		return 0
	}
	return 1 - (c.metrics.actualCost / c.metrics.estimatedCost)
}

// CompletionOptions configures a completion request.
type CompletionOptions struct {
	SystemPrompt   string
	SessionContext string
	MaxTokens      int
}

func (c *CacheAwareClient) buildSimplePrompt(query string, opts CompletionOptions) string {
	var prompt string
	if opts.SystemPrompt != "" {
		prompt = opts.SystemPrompt + "\n\n"
	}
	if opts.SessionContext != "" {
		prompt += opts.SessionContext + "\n\n"
	}
	prompt += query
	return prompt
}

func (c *CacheAwareClient) recordUsage(usage *UsageStats) {
	if usage == nil {
		return
	}

	atomic.AddInt64(&c.metrics.totalTokens, int64(usage.InputTokens))
	atomic.AddInt64(&c.metrics.cachedTokens, int64(usage.CacheReadTokens))

	if usage.CacheHit() {
		atomic.AddInt64(&c.metrics.cacheHits, 1)
	} else {
		atomic.AddInt64(&c.metrics.cacheMisses, 1)
	}

	c.metrics.mu.Lock()
	// Estimated cost without caching
	c.metrics.estimatedCost += float64(usage.InputTokens)
	// Actual cost with caching
	nonCached := usage.InputTokens - usage.CacheCreationTokens - usage.CacheReadTokens
	c.metrics.actualCost += float64(nonCached) +
		float64(usage.CacheCreationTokens)*1.25 +
		float64(usage.CacheReadTokens)*0.1
	c.metrics.mu.Unlock()
}

// Analytics tracks cache usage analytics.
type Analytics struct {
	calls []CallRecord
	mu    sync.Mutex
}

// NewAnalytics creates a new analytics tracker.
func NewAnalytics() *Analytics {
	return &Analytics{
		calls: make([]CallRecord, 0, 100),
	}
}

// RecordCall records a single API call.
func (a *Analytics) RecordCall(record CallRecord) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Calculate costs
	if record.CacheHit {
		// Cache hit: 90% savings on cached portion
		record.ActualCost = float64(record.InputTokens-record.CachedTokens) +
			float64(record.CachedTokens)*0.1
	} else {
		// Cache creation: 25% premium
		record.ActualCost = float64(record.InputTokens) * 1.25
	}
	record.EstimatedCost = float64(record.InputTokens)

	a.calls = append(a.calls, record)

	// Keep only recent calls (prevent unbounded growth)
	if len(a.calls) > 1000 {
		a.calls = a.calls[len(a.calls)-500:]
	}
}

// GetSavingsRate calculates the overall savings rate.
func (a *Analytics) GetSavingsRate() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()

	var totalActual, totalEstimated float64
	for _, call := range a.calls {
		totalActual += call.ActualCost
		totalEstimated += call.EstimatedCost
	}

	if totalEstimated == 0 {
		return 0
	}

	return 1 - (totalActual / totalEstimated)
}

// GetRecentCalls returns recent call records.
func (a *Analytics) GetRecentCalls(n int) []CallRecord {
	a.mu.Lock()
	defer a.mu.Unlock()

	if n <= 0 || n > len(a.calls) {
		n = len(a.calls)
	}

	result := make([]CallRecord, n)
	copy(result, a.calls[len(a.calls)-n:])
	return result
}

// GetStats returns aggregate statistics.
func (a *Analytics) GetStats(since time.Duration) CacheMetrics {
	a.mu.Lock()
	defer a.mu.Unlock()

	cutoff := time.Now().Add(-since)
	stats := CacheMetrics{}

	for _, call := range a.calls {
		if call.Timestamp.Before(cutoff) {
			continue
		}

		stats.TotalCalls++
		stats.TotalTokens += int64(call.InputTokens)
		stats.CachedTokens += int64(call.CachedTokens)

		if call.CacheHit {
			stats.CacheHits++
		} else {
			stats.CacheMisses++
		}
	}

	if stats.TotalTokens > 0 {
		stats.EstimatedSavings = float64(stats.CachedTokens) * 0.9 / float64(stats.TotalTokens)
	}

	return stats
}
