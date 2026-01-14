# Prompt Caching Strategy Design

> Design document for `recurse-bab`: [SPEC] Prompt Caching Strategy Design

## Overview

This document specifies a prompt caching strategy for the RLM system to reduce latency and cost through Claude's prompt caching feature. By structuring prompts with stable prefixes and ephemeral cache control, we can achieve up to 90% cost reduction and 85% latency reduction for repeated context.

## Problem Statement

### Current State

The RLM system makes multiple recursive calls with overlapping context:

```
Call 1: [System Prompt] + [Context A] + [Query 1]
Call 2: [System Prompt] + [Context A] + [Query 2]
Call 3: [System Prompt] + [Context A] + [Subquery 1a]
Call 4: [System Prompt] + [Context A] + [Subquery 1b]
```

**Current cost model**:
- Each call pays full input token cost
- No reuse of common prefixes
- Redundant processing of system prompts

### Claude Prompt Caching

Claude supports ephemeral caching with:
- Cache creation: 25% premium on first call
- Cache hit: 90% discount on subsequent calls
- Cache TTL: 5 minutes from last use
- Minimum cacheable: 1024 tokens (Sonnet), 2048 tokens (Haiku)

### Expected Improvements

| Scenario | Without Caching | With Caching | Savings |
|----------|-----------------|--------------|---------|
| 4-chunk decomposition | 4x full cost | 1.25x + 3x0.1 = 1.55x | 61% |
| 10-chunk decomposition | 10x full cost | 1.25x + 9x0.1 = 2.15x | 79% |
| Session with 50 calls | 50x full cost | 1.25x + 49x0.1 = 6.15x | 88% |

## Design Goals

1. **Maximize cache hits**: Structure prompts for stable prefixes
2. **Minimize cache creation cost**: Only cache content used >1 time
3. **Session-aware caching**: Share cache across decomposition calls
4. **Adaptive strategy**: Adjust caching based on query patterns
5. **Provider abstraction**: Support non-caching providers gracefully

## Core Types

### Cache Control

```go
// internal/rlm/cache/control.go

// CacheControl specifies caching behavior for a message block.
type CacheControl struct {
    Type    CacheType
    TTL     time.Duration // Hint for cache duration
}

type CacheType string

const (
    CacheTypeNone      CacheType = ""
    CacheTypeEphemeral CacheType = "ephemeral"
)

// CacheableBlock represents content that can be cached.
type CacheableBlock struct {
    Content      string
    Role         MessageRole
    CacheControl *CacheControl
    TokenCount   int // Estimated tokens for this block
}
```

### Prompt Structure

```go
// internal/rlm/cache/prompt.go

// StructuredPrompt organizes content for optimal caching.
type StructuredPrompt struct {
    // System blocks - highest cache priority
    SystemBlocks []CacheableBlock

    // Shared context - cached across decomposition calls
    SharedContext []CacheableBlock

    // Query-specific content - not cached
    QueryContent []CacheableBlock
}

// ToAnthropicParams converts to Anthropic API format.
func (p *StructuredPrompt) ToAnthropicParams() anthropic.MessageParams {
    var systemBlocks []anthropic.SystemBlock
    var messages []anthropic.MessageParam

    // System blocks with cache control
    for _, block := range p.SystemBlocks {
        sb := anthropic.SystemBlock{
            Type: anthropic.SystemBlockTypeText,
            Text: block.Content,
        }
        if block.CacheControl != nil && block.CacheControl.Type == CacheTypeEphemeral {
            sb.CacheControl = &anthropic.CacheControlEphemeral{}
        }
        systemBlocks = append(systemBlocks, sb)
    }

    // Shared context as user message with cache control
    if len(p.SharedContext) > 0 {
        var contextParts []anthropic.ContentBlock
        for _, block := range p.SharedContext {
            cb := anthropic.TextBlock{Type: "text", Text: block.Content}
            if block.CacheControl != nil && block.CacheControl.Type == CacheTypeEphemeral {
                cb.CacheControl = &anthropic.CacheControlEphemeral{}
            }
            contextParts = append(contextParts, cb)
        }
        messages = append(messages, anthropic.MessageParam{
            Role:    anthropic.RoleUser,
            Content: contextParts,
        })
    }

    // Query content without caching
    for _, block := range p.QueryContent {
        messages = append(messages, anthropic.MessageParam{
            Role:    anthropic.Role(block.Role),
            Content: []anthropic.ContentBlock{
                anthropic.TextBlock{Type: "text", Text: block.Content},
            },
        })
    }

    return anthropic.MessageParams{
        System:   systemBlocks,
        Messages: messages,
    }
}
```

### Cache Strategy

```go
// internal/rlm/cache/strategy.go

// Strategy determines caching behavior based on context.
type Strategy interface {
    // ShouldCache decides whether to cache a block.
    ShouldCache(block *CacheableBlock, context *CacheContext) bool

    // StructurePrompt organizes blocks for optimal caching.
    StructurePrompt(blocks []CacheableBlock, context *CacheContext) *StructuredPrompt
}

// CacheContext provides information for caching decisions.
type CacheContext struct {
    // Session information
    SessionID      string
    TaskID         string
    DecompositionID string

    // Expected reuse
    ExpectedCalls  int  // How many calls will share this context
    IsDecomposition bool // Part of a decomposition chain

    // Token budget
    MaxCachedTokens int

    // Previous cache state
    ActiveCacheKeys []string
}
```

### Default Strategy

```go
// internal/rlm/cache/default_strategy.go

type DefaultStrategy struct {
    minTokensToCache   int     // Minimum tokens to justify caching
    expectedReuseRatio float64 // Expected cache hit ratio
}

func NewDefaultStrategy() *DefaultStrategy {
    return &DefaultStrategy{
        minTokensToCache:   1024, // Claude minimum for Sonnet
        expectedReuseRatio: 0.8,
    }
}

func (s *DefaultStrategy) ShouldCache(block *CacheableBlock, ctx *CacheContext) bool {
    // Don't cache small blocks
    if block.TokenCount < s.minTokensToCache {
        return false
    }

    // Always cache in decomposition chains
    if ctx.IsDecomposition && ctx.ExpectedCalls > 1 {
        return true
    }

    // Cache if expected reuse > break-even
    // Break-even: 1.25x creation cost = N * 0.1x hit savings
    // N = 1.25 / 0.9 ≈ 1.4 calls
    return ctx.ExpectedCalls >= 2
}

func (s *DefaultStrategy) StructurePrompt(
    blocks []CacheableBlock,
    ctx *CacheContext,
) *StructuredPrompt {
    prompt := &StructuredPrompt{}

    // Sort by stability: system > shared context > query
    for _, block := range blocks {
        switch block.Role {
        case RoleSystem:
            if s.ShouldCache(&block, ctx) {
                block.CacheControl = &CacheControl{Type: CacheTypeEphemeral}
            }
            prompt.SystemBlocks = append(prompt.SystemBlocks, block)

        case RoleContext:
            if s.ShouldCache(&block, ctx) {
                block.CacheControl = &CacheControl{Type: CacheTypeEphemeral}
            }
            prompt.SharedContext = append(prompt.SharedContext, block)

        default:
            // Query content - no caching
            prompt.QueryContent = append(prompt.QueryContent, block)
        }
    }

    return prompt
}
```

## Decomposition Caching

### Decomposition Session

When a task is decomposed, all subtasks share the same context:

```go
// internal/rlm/cache/decomposition.go

// DecompositionSession manages caching for a decomposition chain.
type DecompositionSession struct {
    id            string
    sharedContext *StructuredPrompt
    strategy      Strategy
    mu            sync.RWMutex
}

// NewDecompositionSession creates a session with shared cached context.
func NewDecompositionSession(
    id string,
    systemPrompt string,
    sharedContext string,
    expectedCalls int,
) *DecompositionSession {
    ctx := &CacheContext{
        DecompositionID: id,
        ExpectedCalls:   expectedCalls,
        IsDecomposition: true,
    }

    strategy := NewDefaultStrategy()

    blocks := []CacheableBlock{
        {
            Content:    systemPrompt,
            Role:       RoleSystem,
            TokenCount: estimateTokens(systemPrompt),
        },
        {
            Content:    sharedContext,
            Role:       RoleContext,
            TokenCount: estimateTokens(sharedContext),
        },
    }

    return &DecompositionSession{
        id:            id,
        sharedContext: strategy.StructurePrompt(blocks, ctx),
        strategy:      strategy,
    }
}

// PrepareSubcall prepares a prompt for a decomposition subcall.
func (s *DecompositionSession) PrepareSubcall(query string) *StructuredPrompt {
    s.mu.RLock()
    defer s.mu.RUnlock()

    // Clone shared context
    prompt := &StructuredPrompt{
        SystemBlocks:  s.sharedContext.SystemBlocks,
        SharedContext: s.sharedContext.SharedContext,
    }

    // Add query-specific content
    prompt.QueryContent = append(prompt.QueryContent, CacheableBlock{
        Content:    query,
        Role:       RoleUser,
        TokenCount: estimateTokens(query),
    })

    return prompt
}
```

### Integration with Controller

```go
// internal/rlm/controller.go

func (c *Controller) executeDecompose(
    ctx context.Context,
    state meta.State,
    decision *meta.Decision,
    parentID string,
) (string, int, error) {
    // ... existing decomposition logic ...

    // Create caching session for this decomposition
    session := cache.NewDecompositionSession(
        parentID,
        c.systemPrompt,
        state.Context, // Shared context for all chunks
        len(chunks),
    )

    // Execute chunks with cached context
    var results []synthesize.SubCallResult
    for i, chunk := range chunks {
        prompt := session.PrepareSubcall(chunk)

        response, tokens, err := c.llmClient.Complete(ctx, prompt.ToAnthropicParams())
        if err != nil {
            return "", totalTokens, err
        }

        results = append(results, synthesize.SubCallResult{
            Index:   i,
            Content: response,
            Tokens:  tokens,
        })
        totalTokens += tokens
    }

    // ... synthesis ...
}
```

## Multi-Level Caching

### Cache Hierarchy

```
Level 1: System Prompt (stable across all calls)
    ↓
Level 2: Session Context (stable within session)
    ↓
Level 3: Task Context (stable within decomposition)
    ↓
Level 4: Query (varies per call)
```

```go
// internal/rlm/cache/hierarchy.go

type CacheHierarchy struct {
    levels []CacheLevel
}

type CacheLevel struct {
    Name        string
    Content     string
    TokenCount  int
    CacheKey    string
    ShouldCache bool
}

func (h *CacheHierarchy) BuildPrompt() *StructuredPrompt {
    prompt := &StructuredPrompt{}

    for i, level := range h.levels {
        block := CacheableBlock{
            Content:    level.Content,
            TokenCount: level.TokenCount,
        }

        // Cache all levels except the last (query)
        if level.ShouldCache && i < len(h.levels)-1 {
            block.CacheControl = &CacheControl{Type: CacheTypeEphemeral}
        }

        switch i {
        case 0:
            block.Role = RoleSystem
            prompt.SystemBlocks = append(prompt.SystemBlocks, block)
        case len(h.levels) - 1:
            block.Role = RoleUser
            prompt.QueryContent = append(prompt.QueryContent, block)
        default:
            block.Role = RoleContext
            prompt.SharedContext = append(prompt.SharedContext, block)
        }
    }

    return prompt
}
```

### Session Manager

```go
// internal/rlm/cache/session_manager.go

type SessionManager struct {
    sessions map[string]*CacheHierarchy
    mu       sync.RWMutex

    // Metrics
    cacheHits   prometheus.Counter
    cacheMisses prometheus.Counter
    savings     prometheus.Counter
}

func (m *SessionManager) GetOrCreate(
    sessionID string,
    systemPrompt string,
    sessionContext string,
) *CacheHierarchy {
    m.mu.Lock()
    defer m.mu.Unlock()

    if hierarchy, ok := m.sessions[sessionID]; ok {
        m.cacheHits.Inc()
        return hierarchy
    }

    m.cacheMisses.Inc()

    hierarchy := &CacheHierarchy{
        levels: []CacheLevel{
            {
                Name:        "system",
                Content:     systemPrompt,
                TokenCount:  estimateTokens(systemPrompt),
                CacheKey:    hashContent(systemPrompt),
                ShouldCache: true,
            },
            {
                Name:        "session",
                Content:     sessionContext,
                TokenCount:  estimateTokens(sessionContext),
                CacheKey:    hashContent(sessionContext),
                ShouldCache: true,
            },
        },
    }

    m.sessions[sessionID] = hierarchy
    return hierarchy
}

func (m *SessionManager) AddTaskContext(
    sessionID string,
    taskContext string,
) {
    m.mu.Lock()
    defer m.mu.Unlock()

    if hierarchy, ok := m.sessions[sessionID]; ok {
        hierarchy.levels = append(hierarchy.levels, CacheLevel{
            Name:        "task",
            Content:     taskContext,
            TokenCount:  estimateTokens(taskContext),
            CacheKey:    hashContent(taskContext),
            ShouldCache: true,
        })
    }
}
```

## Adaptive Caching

### Cache Analytics

```go
// internal/rlm/cache/analytics.go

type CacheAnalytics struct {
    calls       []CallRecord
    mu          sync.Mutex
}

type CallRecord struct {
    Timestamp     time.Time
    SessionID     string
    CacheHit      bool
    InputTokens   int
    CachedTokens  int
    ActualCost    float64
    EstimatedCost float64 // Without caching
}

func (a *CacheAnalytics) RecordCall(record CallRecord) {
    a.mu.Lock()
    defer a.mu.Unlock()

    // Calculate savings
    if record.CacheHit {
        // Cache hit: 90% savings on cached portion
        record.ActualCost = float64(record.InputTokens-record.CachedTokens) +
            float64(record.CachedTokens)*0.1
        record.EstimatedCost = float64(record.InputTokens)
    } else {
        // Cache creation: 25% premium
        record.ActualCost = float64(record.InputTokens) * 1.25
        record.EstimatedCost = float64(record.InputTokens)
    }

    a.calls = append(a.calls, record)
}

func (a *CacheAnalytics) GetSavingsRate() float64 {
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
```

### Adaptive Strategy

```go
// internal/rlm/cache/adaptive.go

type AdaptiveStrategy struct {
    base       Strategy
    analytics  *CacheAnalytics
    threshold  float64 // Minimum savings rate to continue caching
}

func (s *AdaptiveStrategy) ShouldCache(block *CacheableBlock, ctx *CacheContext) bool {
    // Check if caching is worthwhile based on recent performance
    savingsRate := s.analytics.GetSavingsRate()

    if savingsRate < s.threshold {
        // Caching not paying off - be more selective
        return block.TokenCount >= 2048 && ctx.ExpectedCalls >= 3
    }

    return s.base.ShouldCache(block, ctx)
}
```

## Provider Abstraction

### Cache-Aware Client

```go
// internal/rlm/cache/client.go

type CacheAwareClient struct {
    inner      LLMClient
    strategy   Strategy
    analytics  *CacheAnalytics
    manager    *SessionManager
}

func (c *CacheAwareClient) Complete(
    ctx context.Context,
    sessionID string,
    query string,
    opts CompletionOptions,
) (string, int, error) {
    // Get or create session hierarchy
    hierarchy := c.manager.GetOrCreate(
        sessionID,
        opts.SystemPrompt,
        opts.SessionContext,
    )

    // Add query as final level
    levels := append(hierarchy.levels, CacheLevel{
        Name:        "query",
        Content:     query,
        TokenCount:  estimateTokens(query),
        ShouldCache: false,
    })

    tempHierarchy := &CacheHierarchy{levels: levels}
    prompt := tempHierarchy.BuildPrompt()

    // Make the call
    start := time.Now()
    response, usage, err := c.inner.Complete(ctx, prompt.ToAnthropicParams())
    if err != nil {
        return "", 0, err
    }

    // Record analytics
    c.analytics.RecordCall(CallRecord{
        Timestamp:    start,
        SessionID:    sessionID,
        CacheHit:     usage.CacheReadTokens > 0,
        InputTokens:  usage.InputTokens,
        CachedTokens: usage.CacheReadTokens,
    })

    return response, usage.InputTokens + usage.OutputTokens, nil
}
```

### Non-Caching Fallback

```go
// internal/rlm/cache/fallback.go

type FallbackClient struct {
    caching    *CacheAwareClient
    nonCaching LLMClient
}

func (c *FallbackClient) Complete(
    ctx context.Context,
    sessionID string,
    query string,
    opts CompletionOptions,
) (string, int, error) {
    // Check if caching is supported
    if c.caching != nil && opts.Provider == "anthropic" {
        return c.caching.Complete(ctx, sessionID, query, opts)
    }

    // Fall back to non-caching client
    return c.nonCaching.Complete(ctx, buildSimplePrompt(query, opts))
}
```

## Observability

### Metrics

```go
var (
    cacheHitRate = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "rlm_cache_hit_rate",
            Help: "Rate of cache hits vs misses",
        },
        []string{"session_type"},
    )

    cacheSavingsRate = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "rlm_cache_savings_rate",
            Help: "Percentage cost savings from caching",
        },
    )

    cachedTokens = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "rlm_cached_tokens_total",
            Help: "Total tokens served from cache",
        },
    )

    cacheCreationCost = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "rlm_cache_creation_cost_total",
            Help: "Additional tokens spent on cache creation (25% premium)",
        },
    )
)
```

### Tracing

```go
func (c *CacheAwareClient) Complete(
    ctx context.Context,
    sessionID string,
    query string,
    opts CompletionOptions,
) (string, int, error) {
    ctx, span := tracer.Start(ctx, "CacheAwareClient.Complete",
        trace.WithAttributes(
            attribute.String("session_id", sessionID),
            attribute.Int("query_tokens", estimateTokens(query)),
        ),
    )
    defer span.End()

    // ... completion logic ...

    span.SetAttributes(
        attribute.Bool("cache_hit", usage.CacheReadTokens > 0),
        attribute.Int("cached_tokens", usage.CacheReadTokens),
        attribute.Float64("savings_rate", float64(usage.CacheReadTokens)/float64(usage.InputTokens)),
    )

    return response, tokens, nil
}
```

## Testing Strategy

### Unit Tests

```go
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
            name:     "decomposition - always cache",
            block:    CacheableBlock{TokenCount: 1500},
            context:  CacheContext{IsDecomposition: true, ExpectedCalls: 4},
            expected: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := strategy.ShouldCache(&tt.block, &tt.context)
            assert.Equal(t, tt.expected, result)
        })
    }
}

func TestDecompositionSession_PrepareSubcall(t *testing.T) {
    session := NewDecompositionSession(
        "test-session",
        "You are a helpful assistant.",
        "Context about the task at hand with relevant details.",
        4,
    )

    prompt1 := session.PrepareSubcall("First query")
    prompt2 := session.PrepareSubcall("Second query")

    // Shared context should be identical
    assert.Equal(t, prompt1.SystemBlocks, prompt2.SystemBlocks)
    assert.Equal(t, prompt1.SharedContext, prompt2.SharedContext)

    // Query content should differ
    assert.NotEqual(t, prompt1.QueryContent, prompt2.QueryContent)

    // Cache control should be set on shared content
    assert.NotNil(t, prompt1.SystemBlocks[0].CacheControl)
    assert.Equal(t, CacheTypeEphemeral, prompt1.SystemBlocks[0].CacheControl.Type)
}
```

### Integration Tests

```go
func TestCacheAwareClient_Integration(t *testing.T) {
    if os.Getenv("ANTHROPIC_API_KEY") == "" {
        t.Skip("ANTHROPIC_API_KEY not set")
    }

    client := NewCacheAwareClient(NewAnthropicClient(), NewDefaultStrategy())

    // First call - cache creation
    _, usage1, err := client.Complete(context.Background(), "test-session", "What is 2+2?", CompletionOptions{
        SystemPrompt:   strings.Repeat("System context. ", 200), // ~800 tokens
        SessionContext: strings.Repeat("Session context. ", 300), // ~1200 tokens
    })
    require.NoError(t, err)

    // Second call - should hit cache
    _, usage2, err := client.Complete(context.Background(), "test-session", "What is 3+3?", CompletionOptions{
        SystemPrompt:   strings.Repeat("System context. ", 200),
        SessionContext: strings.Repeat("Session context. ", 300),
    })
    require.NoError(t, err)

    // Verify cache hit
    assert.Greater(t, usage2.CacheReadTokens, 0)
    assert.Less(t, usage2.InputTokens, usage1.InputTokens)
}
```

## Migration Path

### Phase 1: Opt-in Caching

```go
type ControllerConfig struct {
    EnablePromptCaching bool
    CacheStrategy       string // "default", "adaptive", "aggressive"
}
```

### Phase 2: Default for Decomposition

Enable caching by default for decomposition chains where benefit is guaranteed.

### Phase 3: Session-Wide Caching

Extend caching to session-level context sharing.

## Success Criteria

1. **Cost reduction**: >50% savings on decomposition chains
2. **Latency reduction**: >30% time-to-first-token improvement
3. **Cache hit rate**: >70% for decomposition subcalls
4. **No correctness impact**: Caching is transparent to output quality
5. **Graceful degradation**: System works without caching enabled

## Appendix: Token Estimation

```go
func estimateTokens(text string) int {
    // Rough estimate: ~4 characters per token for English
    // More accurate would use tiktoken
    return len(text) / 4
}

func hashContent(content string) string {
    h := sha256.Sum256([]byte(content))
    return hex.EncodeToString(h[:8])
}
```

## Appendix: Claude Caching Constraints

| Constraint | Value |
|-----------|-------|
| Minimum cacheable (Sonnet) | 1024 tokens |
| Minimum cacheable (Haiku) | 2048 tokens |
| Cache TTL | 5 minutes |
| Creation premium | +25% |
| Hit discount | -90% |
| Max cache breakpoints | 4 per request |
