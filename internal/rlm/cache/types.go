// Package cache provides prompt caching optimization for Claude API calls.
package cache

import (
	"time"
)

// CacheType specifies the caching behavior for a message block.
type CacheType string

const (
	// CacheTypeNone disables caching for this block.
	CacheTypeNone CacheType = ""

	// CacheTypeEphemeral enables ephemeral caching (5 min TTL).
	CacheTypeEphemeral CacheType = "ephemeral"
)

// MessageRole identifies the role of a message in the conversation.
type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleContext   MessageRole = "context" // Internal: shared context block
)

// CacheControl specifies caching behavior for a message block.
type CacheControl struct {
	Type CacheType
	TTL  time.Duration // Hint for cache duration (default: 5 min)
}

// CacheableBlock represents content that can be cached.
type CacheableBlock struct {
	// Content is the text content of this block.
	Content string

	// Role identifies the message role.
	Role MessageRole

	// CacheControl specifies caching behavior.
	CacheControl *CacheControl

	// TokenCount is the estimated tokens for this block.
	TokenCount int
}

// StructuredPrompt organizes content for optimal caching.
type StructuredPrompt struct {
	// SystemBlocks are system-level instructions (highest cache priority).
	SystemBlocks []CacheableBlock

	// SharedContext is context shared across decomposition calls.
	SharedContext []CacheableBlock

	// QueryContent is query-specific content (not cached).
	QueryContent []CacheableBlock
}

// TotalTokens returns the total estimated tokens across all blocks.
func (p *StructuredPrompt) TotalTokens() int {
	total := 0
	for _, b := range p.SystemBlocks {
		total += b.TokenCount
	}
	for _, b := range p.SharedContext {
		total += b.TokenCount
	}
	for _, b := range p.QueryContent {
		total += b.TokenCount
	}
	return total
}

// CacheableTokens returns the tokens that can be cached.
func (p *StructuredPrompt) CacheableTokens() int {
	total := 0
	for _, b := range p.SystemBlocks {
		if b.CacheControl != nil && b.CacheControl.Type == CacheTypeEphemeral {
			total += b.TokenCount
		}
	}
	for _, b := range p.SharedContext {
		if b.CacheControl != nil && b.CacheControl.Type == CacheTypeEphemeral {
			total += b.TokenCount
		}
	}
	return total
}

// CacheContext provides information for caching decisions.
type CacheContext struct {
	// SessionID identifies the current session.
	SessionID string

	// TaskID identifies the current task.
	TaskID string

	// DecompositionID identifies the current decomposition chain.
	DecompositionID string

	// ExpectedCalls is how many calls will share this context.
	ExpectedCalls int

	// IsDecomposition indicates this is part of a decomposition chain.
	IsDecomposition bool

	// MaxCachedTokens limits the tokens that can be cached.
	MaxCachedTokens int

	// ActiveCacheKeys lists currently active cache keys.
	ActiveCacheKeys []string
}

// UsageStats tracks cache usage statistics.
type UsageStats struct {
	// InputTokens is the total input tokens.
	InputTokens int

	// OutputTokens is the total output tokens.
	OutputTokens int

	// CacheCreationTokens is tokens used for cache creation (25% premium).
	CacheCreationTokens int

	// CacheReadTokens is tokens served from cache (90% discount).
	CacheReadTokens int
}

// CacheHit returns true if any tokens were served from cache.
func (u *UsageStats) CacheHit() bool {
	return u.CacheReadTokens > 0
}

// EstimatedSavings calculates the cost savings from caching.
// Returns a value between 0 and 1 representing the percentage saved.
func (u *UsageStats) EstimatedSavings() float64 {
	if u.InputTokens == 0 {
		return 0
	}

	// Without caching: all input tokens at full price
	withoutCaching := float64(u.InputTokens)

	// With caching:
	// - Cache creation: 1.25x the cached tokens
	// - Cache read: 0.1x the cached tokens
	// - Non-cached: 1x
	nonCachedTokens := u.InputTokens - u.CacheCreationTokens - u.CacheReadTokens
	withCaching := float64(nonCachedTokens) +
		float64(u.CacheCreationTokens)*1.25 +
		float64(u.CacheReadTokens)*0.1

	if withoutCaching == 0 {
		return 0
	}

	return 1 - (withCaching / withoutCaching)
}

// CallRecord records a single API call for analytics.
type CallRecord struct {
	Timestamp     time.Time
	SessionID     string
	CacheHit      bool
	InputTokens   int
	CachedTokens  int
	ActualCost    float64
	EstimatedCost float64 // Cost without caching
}

// CacheMetrics tracks aggregate caching metrics.
type CacheMetrics struct {
	TotalCalls        int64
	CacheHits         int64
	CacheMisses       int64
	TotalTokens       int64
	CachedTokens      int64
	EstimatedSavings  float64
	CreationPremium   float64 // Extra cost from cache creation
}

// HitRate returns the cache hit rate (0-1).
func (m *CacheMetrics) HitRate() float64 {
	if m.TotalCalls == 0 {
		return 0
	}
	return float64(m.CacheHits) / float64(m.TotalCalls)
}

// Claude caching constraints.
const (
	// MinCacheableTokensSonnet is the minimum tokens to cache for Sonnet.
	MinCacheableTokensSonnet = 1024

	// MinCacheableTokensHaiku is the minimum tokens to cache for Haiku.
	MinCacheableTokensHaiku = 2048

	// CacheTTL is the cache time-to-live.
	CacheTTL = 5 * time.Minute

	// CacheCreationPremium is the extra cost for cache creation.
	CacheCreationPremium = 0.25

	// CacheHitDiscount is the discount for cache hits.
	CacheHitDiscount = 0.90

	// MaxCacheBreakpoints is the maximum cache breakpoints per request.
	MaxCacheBreakpoints = 4
)
