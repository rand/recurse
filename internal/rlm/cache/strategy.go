package cache

// Strategy determines caching behavior based on context.
type Strategy interface {
	// ShouldCache decides whether to cache a block.
	ShouldCache(block *CacheableBlock, ctx *CacheContext) bool

	// StructurePrompt organizes blocks for optimal caching.
	StructurePrompt(blocks []CacheableBlock, ctx *CacheContext) *StructuredPrompt

	// MinTokensToCache returns the minimum tokens required for caching.
	MinTokensToCache() int
}

// DefaultStrategy implements the default caching strategy.
type DefaultStrategy struct {
	minTokensToCache   int
	expectedReuseRatio float64
}

// NewDefaultStrategy creates a new default caching strategy.
func NewDefaultStrategy() *DefaultStrategy {
	return &DefaultStrategy{
		minTokensToCache:   MinCacheableTokensSonnet,
		expectedReuseRatio: 0.8,
	}
}

// NewDefaultStrategyWithMinTokens creates a strategy with custom minimum tokens.
func NewDefaultStrategyWithMinTokens(minTokens int) *DefaultStrategy {
	return &DefaultStrategy{
		minTokensToCache:   minTokens,
		expectedReuseRatio: 0.8,
	}
}

// MinTokensToCache returns the minimum tokens required for caching.
func (s *DefaultStrategy) MinTokensToCache() int {
	return s.minTokensToCache
}

// ShouldCache decides whether to cache a block.
func (s *DefaultStrategy) ShouldCache(block *CacheableBlock, ctx *CacheContext) bool {
	// Don't cache small blocks
	if block.TokenCount < s.minTokensToCache {
		return false
	}

	// Always cache in decomposition chains with multiple calls
	if ctx.IsDecomposition && ctx.ExpectedCalls > 1 {
		return true
	}

	// Cache if expected reuse justifies the creation premium
	// Break-even: 1.25x creation cost = N * 0.9x savings
	// N = 1.25 / 0.9 ≈ 1.4 calls
	// So we need at least 2 calls to benefit from caching
	return ctx.ExpectedCalls >= 2
}

// StructurePrompt organizes blocks for optimal caching.
func (s *DefaultStrategy) StructurePrompt(blocks []CacheableBlock, ctx *CacheContext) *StructuredPrompt {
	prompt := &StructuredPrompt{}
	cacheBreakpoints := 0

	for _, block := range blocks {
		// Determine if we should cache this block
		shouldCache := s.ShouldCache(&block, ctx) && cacheBreakpoints < MaxCacheBreakpoints

		// Create a copy of the block
		b := block

		if shouldCache {
			b.CacheControl = &CacheControl{Type: CacheTypeEphemeral}
			cacheBreakpoints++
		}

		// Route to appropriate section based on role
		switch block.Role {
		case RoleSystem:
			prompt.SystemBlocks = append(prompt.SystemBlocks, b)

		case RoleContext:
			prompt.SharedContext = append(prompt.SharedContext, b)

		case RoleUser, RoleAssistant:
			// Query content - typically not cached
			prompt.QueryContent = append(prompt.QueryContent, b)

		default:
			// Unknown role - treat as query content
			prompt.QueryContent = append(prompt.QueryContent, b)
		}
	}

	return prompt
}

// AggressiveStrategy caches more aggressively for high-reuse scenarios.
type AggressiveStrategy struct {
	*DefaultStrategy
}

// NewAggressiveStrategy creates an aggressive caching strategy.
func NewAggressiveStrategy() *AggressiveStrategy {
	return &AggressiveStrategy{
		DefaultStrategy: &DefaultStrategy{
			minTokensToCache:   MinCacheableTokensSonnet / 2, // Lower threshold
			expectedReuseRatio: 0.9,
		},
	}
}

// ShouldCache is more permissive for aggressive caching.
func (s *AggressiveStrategy) ShouldCache(block *CacheableBlock, ctx *CacheContext) bool {
	// Cache even smaller blocks
	if block.TokenCount < s.minTokensToCache {
		return false
	}

	// Cache on any decomposition
	if ctx.IsDecomposition {
		return true
	}

	// Cache even for single reuse (betting on future reuse)
	return ctx.ExpectedCalls >= 1
}

// ConservativeStrategy caches only when benefit is very clear.
type ConservativeStrategy struct {
	*DefaultStrategy
}

// NewConservativeStrategy creates a conservative caching strategy.
func NewConservativeStrategy() *ConservativeStrategy {
	return &ConservativeStrategy{
		DefaultStrategy: &DefaultStrategy{
			minTokensToCache:   MinCacheableTokensSonnet * 2, // Higher threshold
			expectedReuseRatio: 0.7,
		},
	}
}

// ShouldCache is more restrictive for conservative caching.
func (s *ConservativeStrategy) ShouldCache(block *CacheableBlock, ctx *CacheContext) bool {
	// Require larger blocks
	if block.TokenCount < s.minTokensToCache {
		return false
	}

	// Only cache in decomposition with many calls
	if ctx.IsDecomposition && ctx.ExpectedCalls >= 4 {
		return true
	}

	// Require more reuse to justify caching
	return ctx.ExpectedCalls >= 4
}

// AdaptiveStrategy adjusts caching based on observed hit rates.
type AdaptiveStrategy struct {
	base      Strategy
	metrics   *CacheMetrics
	threshold float64 // Minimum savings rate to continue aggressive caching
}

// NewAdaptiveStrategy creates an adaptive caching strategy.
func NewAdaptiveStrategy(metrics *CacheMetrics) *AdaptiveStrategy {
	return &AdaptiveStrategy{
		base:      NewDefaultStrategy(),
		metrics:   metrics,
		threshold: 0.3, // Require at least 30% savings
	}
}

// MinTokensToCache returns the minimum tokens based on current performance.
func (s *AdaptiveStrategy) MinTokensToCache() int {
	if s.metrics == nil {
		return s.base.MinTokensToCache()
	}

	// If savings are poor, be more conservative
	if s.metrics.EstimatedSavings < s.threshold {
		return MinCacheableTokensSonnet * 2
	}

	return s.base.MinTokensToCache()
}

// ShouldCache adapts based on observed performance.
func (s *AdaptiveStrategy) ShouldCache(block *CacheableBlock, ctx *CacheContext) bool {
	if s.metrics == nil {
		return s.base.ShouldCache(block, ctx)
	}

	// If caching isn't paying off, be more selective
	if s.metrics.EstimatedSavings < s.threshold {
		return block.TokenCount >= MinCacheableTokensSonnet*2 && ctx.ExpectedCalls >= 3
	}

	return s.base.ShouldCache(block, ctx)
}

// StructurePrompt delegates to the base strategy.
func (s *AdaptiveStrategy) StructurePrompt(blocks []CacheableBlock, ctx *CacheContext) *StructuredPrompt {
	return s.base.StructurePrompt(blocks, ctx)
}
