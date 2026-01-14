package cache

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{"empty", "", 0},
		{"short", "hello", 1},
		{"medium", "hello world", 2},
		{"longer", "This is a longer piece of text that should have more tokens", 14},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EstimateTokens(tt.text)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHashContent(t *testing.T) {
	hash1 := HashContent("hello world")
	hash2 := HashContent("hello world")
	hash3 := HashContent("different content")

	assert.Equal(t, hash1, hash2, "same content should produce same hash")
	assert.NotEqual(t, hash1, hash3, "different content should produce different hash")
	assert.Len(t, hash1, 16, "hash should be 16 characters (8 bytes hex)")
}

func TestDefaultStrategy_ShouldCache(t *testing.T) {
	strategy := NewDefaultStrategy()

	tests := []struct {
		name     string
		block    CacheableBlock
		context  CacheContext
		expected bool
	}{
		{
			name:     "small block - no cache",
			block:    CacheableBlock{TokenCount: 500},
			context:  CacheContext{ExpectedCalls: 5},
			expected: false,
		},
		{
			name:     "large block with reuse - cache",
			block:    CacheableBlock{TokenCount: 2000},
			context:  CacheContext{ExpectedCalls: 3},
			expected: true,
		},
		{
			name:     "decomposition - always cache large",
			block:    CacheableBlock{TokenCount: 1500},
			context:  CacheContext{IsDecomposition: true, ExpectedCalls: 4},
			expected: true,
		},
		{
			name:     "decomposition but small - no cache",
			block:    CacheableBlock{TokenCount: 500},
			context:  CacheContext{IsDecomposition: true, ExpectedCalls: 4},
			expected: false,
		},
		{
			name:     "single call - no cache",
			block:    CacheableBlock{TokenCount: 2000},
			context:  CacheContext{ExpectedCalls: 1},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := strategy.ShouldCache(&tt.block, &tt.context)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDefaultStrategy_StructurePrompt(t *testing.T) {
	strategy := NewDefaultStrategy()

	blocks := []CacheableBlock{
		{Content: "System prompt with enough tokens to cache", Role: RoleSystem, TokenCount: 1500},
		{Content: "Context data", Role: RoleContext, TokenCount: 2000},
		{Content: "User query", Role: RoleUser, TokenCount: 100},
	}

	ctx := &CacheContext{
		ExpectedCalls:   4,
		IsDecomposition: true,
	}

	prompt := strategy.StructurePrompt(blocks, ctx)

	assert.Len(t, prompt.SystemBlocks, 1)
	assert.Len(t, prompt.SharedContext, 1)
	assert.Len(t, prompt.QueryContent, 1)

	// System and context should be cached
	assert.NotNil(t, prompt.SystemBlocks[0].CacheControl)
	assert.Equal(t, CacheTypeEphemeral, prompt.SystemBlocks[0].CacheControl.Type)

	assert.NotNil(t, prompt.SharedContext[0].CacheControl)
	assert.Equal(t, CacheTypeEphemeral, prompt.SharedContext[0].CacheControl.Type)

	// Query content should not be cached
	assert.Nil(t, prompt.QueryContent[0].CacheControl)
}

func TestDecompositionSession_PrepareSubcall(t *testing.T) {
	session := NewDecompositionSession(
		"test-session",
		"You are a helpful assistant.",
		"Context about the task at hand with relevant details that should be long enough to cache.",
		4,
	)

	prompt1 := session.PrepareSubcall("First query")
	prompt2 := session.PrepareSubcall("Second query")

	// Shared context should be identical
	assert.Equal(t, prompt1.SystemBlocks, prompt2.SystemBlocks)
	assert.Equal(t, prompt1.SharedContext, prompt2.SharedContext)

	// Query content should differ
	assert.NotEqual(t, prompt1.QueryContent[0].Content, prompt2.QueryContent[0].Content)
	assert.Equal(t, "First query", prompt1.QueryContent[0].Content)
	assert.Equal(t, "Second query", prompt2.QueryContent[0].Content)
}

func TestDecompositionSession_CacheableTokens(t *testing.T) {
	// Use a long system prompt to ensure it's cacheable
	longSystemPrompt := make([]byte, 5000)
	for i := range longSystemPrompt {
		longSystemPrompt[i] = 'a'
	}

	session := NewDecompositionSession(
		"test-session",
		string(longSystemPrompt),
		"Short context", // Won't be cached (too small)
		4,
	)

	// Only the system prompt should contribute to cacheable tokens
	cacheableTokens := session.CacheableTokens()
	assert.Greater(t, cacheableTokens, 0)
}

func TestSessionManager_GetOrCreate(t *testing.T) {
	manager := NewSessionManager(nil)

	// First call creates new session
	h1 := manager.GetOrCreate("session-1", "system prompt", "context")
	assert.NotNil(t, h1)

	// Second call returns same session
	h2 := manager.GetOrCreate("session-1", "system prompt", "context")
	assert.Equal(t, h1, h2)

	// Different session ID creates new session
	h3 := manager.GetOrCreate("session-2", "system prompt", "context")
	assert.NotSame(t, h1, h3)

	// Check metrics
	metrics := manager.Metrics()
	assert.Equal(t, int64(1), metrics.CacheHits)  // Second call to session-1
	assert.Equal(t, int64(2), metrics.CacheMisses) // First calls to session-1 and session-2
}

func TestCacheHierarchy_BuildPrompt(t *testing.T) {
	hierarchy := &CacheHierarchy{
		levels: []CacheLevel{
			{Name: "system", Content: "System instructions", TokenCount: 2000, ShouldCache: true},
			{Name: "session", Content: "Session context", TokenCount: 1500, ShouldCache: true},
		},
	}

	prompt := hierarchy.BuildPrompt("What is the answer?")

	assert.Len(t, prompt.SystemBlocks, 1)
	assert.Len(t, prompt.SharedContext, 1)
	assert.Len(t, prompt.QueryContent, 1)

	assert.Equal(t, "System instructions", prompt.SystemBlocks[0].Content)
	assert.Equal(t, "Session context", prompt.SharedContext[0].Content)
	assert.Equal(t, "What is the answer?", prompt.QueryContent[0].Content)

	// Cached blocks should have cache control
	assert.NotNil(t, prompt.SystemBlocks[0].CacheControl)
	assert.NotNil(t, prompt.SharedContext[0].CacheControl)
}

func TestStructuredPrompt_TotalTokens(t *testing.T) {
	prompt := &StructuredPrompt{
		SystemBlocks:  []CacheableBlock{{TokenCount: 100}},
		SharedContext: []CacheableBlock{{TokenCount: 200}},
		QueryContent:  []CacheableBlock{{TokenCount: 50}},
	}

	assert.Equal(t, 350, prompt.TotalTokens())
}

func TestStructuredPrompt_CacheableTokens(t *testing.T) {
	prompt := &StructuredPrompt{
		SystemBlocks: []CacheableBlock{
			{TokenCount: 100, CacheControl: &CacheControl{Type: CacheTypeEphemeral}},
		},
		SharedContext: []CacheableBlock{
			{TokenCount: 200, CacheControl: &CacheControl{Type: CacheTypeEphemeral}},
			{TokenCount: 50}, // No cache control
		},
		QueryContent: []CacheableBlock{
			{TokenCount: 50}, // Query content never cached
		},
	}

	assert.Equal(t, 300, prompt.CacheableTokens())
}

func TestUsageStats_EstimatedSavings(t *testing.T) {
	tests := []struct {
		name     string
		stats    UsageStats
		minSave  float64
		maxSave  float64
	}{
		{
			name:    "no tokens",
			stats:   UsageStats{InputTokens: 0},
			minSave: 0,
			maxSave: 0,
		},
		{
			name: "all cache read",
			stats: UsageStats{
				InputTokens:     1000,
				CacheReadTokens: 1000,
			},
			minSave: 0.85, // Should be close to 90% savings
			maxSave: 0.95,
		},
		{
			name: "cache creation",
			stats: UsageStats{
				InputTokens:         1000,
				CacheCreationTokens: 1000,
			},
			minSave: -0.30, // 25% premium = negative savings
			maxSave: -0.20,
		},
		{
			name: "mixed",
			stats: UsageStats{
				InputTokens:         1000,
				CacheCreationTokens: 200,
				CacheReadTokens:     500,
			},
			minSave: 0.35, // Partial savings
			maxSave: 0.50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			savings := tt.stats.EstimatedSavings()
			assert.GreaterOrEqual(t, savings, tt.minSave)
			assert.LessOrEqual(t, savings, tt.maxSave)
		})
	}
}

// mockLLMClient is a test LLM client.
type mockLLMClient struct {
	responses      map[string]string
	usageStats     *UsageStats
	calls          int64
	completeCalled int64
}

func newMockLLMClient() *mockLLMClient {
	return &mockLLMClient{
		responses: make(map[string]string),
		usageStats: &UsageStats{
			InputTokens:     100,
			CacheReadTokens: 50,
		},
	}
}

func (m *mockLLMClient) Complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	atomic.AddInt64(&m.completeCalled, 1)
	atomic.AddInt64(&m.calls, 1)
	if resp, ok := m.responses[prompt]; ok {
		return resp, nil
	}
	return "mock response", nil
}

func (m *mockLLMClient) CompleteWithCaching(ctx context.Context, prompt *StructuredPrompt, maxTokens int) (string, *UsageStats, error) {
	atomic.AddInt64(&m.calls, 1)
	return "cached response", m.usageStats, nil
}

func TestCacheAwareClient_CompleteWithSession(t *testing.T) {
	mock := newMockLLMClient()
	client := NewCacheAwareClient(mock)

	response, usage, err := client.CompleteWithSession(
		context.Background(),
		"test-session",
		"What is 2+2?",
		CompletionOptions{
			SystemPrompt:   "You are a helpful assistant",
			SessionContext: "Math context",
			MaxTokens:      100,
		},
	)

	require.NoError(t, err)
	assert.Equal(t, "cached response", response)
	assert.NotNil(t, usage)

	// Verify metrics were recorded
	hits, misses := client.CacheStats()
	assert.Equal(t, int64(1), hits+misses)
}

func TestCacheAwareClient_CompleteWithDecomposition(t *testing.T) {
	mock := newMockLLMClient()
	client := NewCacheAwareClient(mock)

	session := NewDecompositionSession(
		"decomp-1",
		"System prompt",
		"Shared context for all subtasks",
		4,
	)

	// First subcall
	response1, usage1, err := client.CompleteWithDecomposition(
		context.Background(),
		session,
		"First subtask",
		100,
	)
	require.NoError(t, err)
	assert.NotEmpty(t, response1)
	assert.NotNil(t, usage1)

	// Second subcall
	response2, usage2, err := client.CompleteWithDecomposition(
		context.Background(),
		session,
		"Second subtask",
		100,
	)
	require.NoError(t, err)
	assert.NotEmpty(t, response2)
	assert.NotNil(t, usage2)

	// Both should have been called
	assert.Equal(t, int64(2), atomic.LoadInt64(&mock.calls))
}

func TestCacheAwareClient_CachingDisabled(t *testing.T) {
	mock := newMockLLMClient()
	client := NewCacheAwareClient(mock, WithCachingEnabled(false))

	_, _, err := client.CompleteWithSession(
		context.Background(),
		"test-session",
		"Query",
		CompletionOptions{MaxTokens: 100},
	)

	require.NoError(t, err)
	// Should have called Complete (not CompleteWithCaching)
	assert.Equal(t, int64(1), atomic.LoadInt64(&mock.completeCalled))
}

func TestAnalytics_RecordCall(t *testing.T) {
	analytics := NewAnalytics()

	// Record a cache hit
	analytics.RecordCall(CallRecord{
		Timestamp:   time.Now(),
		SessionID:   "session-1",
		CacheHit:    true,
		InputTokens: 1000,
		CachedTokens: 800,
	})

	// Record a cache miss
	analytics.RecordCall(CallRecord{
		Timestamp:   time.Now(),
		SessionID:   "session-1",
		CacheHit:    false,
		InputTokens: 1000,
	})

	savingsRate := analytics.GetSavingsRate()
	assert.Greater(t, savingsRate, 0.0)

	calls := analytics.GetRecentCalls(10)
	assert.Len(t, calls, 2)
}

func TestAnalytics_GetStats(t *testing.T) {
	analytics := NewAnalytics()

	// Record some calls
	for i := 0; i < 5; i++ {
		analytics.RecordCall(CallRecord{
			Timestamp:    time.Now(),
			SessionID:    "session-1",
			CacheHit:     i%2 == 0,
			InputTokens:  1000,
			CachedTokens: 500,
		})
	}

	stats := analytics.GetStats(time.Hour)
	assert.Equal(t, int64(5), stats.TotalCalls)
	assert.Equal(t, int64(3), stats.CacheHits)
	assert.Equal(t, int64(2), stats.CacheMisses)
}

func TestAggressiveStrategy_ShouldCache(t *testing.T) {
	strategy := NewAggressiveStrategy()

	// Aggressive strategy should cache on any decomposition
	ctx := &CacheContext{IsDecomposition: true, ExpectedCalls: 1}
	block := &CacheableBlock{TokenCount: 600} // Below default threshold but above aggressive

	assert.True(t, strategy.ShouldCache(block, ctx))
}

func TestConservativeStrategy_ShouldCache(t *testing.T) {
	strategy := NewConservativeStrategy()

	// Conservative strategy requires more reuse
	// Note: ConservativeStrategy has minTokensToCache = 2048
	ctx := &CacheContext{ExpectedCalls: 2}
	block := &CacheableBlock{TokenCount: 2100}

	assert.False(t, strategy.ShouldCache(block, ctx))

	// But will cache with more expected calls
	ctx.ExpectedCalls = 5
	assert.True(t, strategy.ShouldCache(block, ctx))
}

func TestCacheMetrics_HitRate(t *testing.T) {
	metrics := &CacheMetrics{
		TotalCalls: 10,
		CacheHits:  7,
	}

	assert.InDelta(t, 0.7, metrics.HitRate(), 0.001)

	// Zero calls
	emptyMetrics := &CacheMetrics{}
	assert.Equal(t, 0.0, emptyMetrics.HitRate())
}
