# Context Compression Design

> Design document for `recurse-8z6`: [SPEC] Context Compression Design

## Overview

This document specifies context compression for the RLM system, enabling efficient handling of large contexts by compressing information while preserving semantic content. Compression reduces token costs and enables processing of contexts that would otherwise exceed limits.

## Problem Statement

### Current State

Full context passed to every LLM call:

```go
prompt := buildPrompt(fullContext, query) // May be 100K+ tokens
response, _ := llm.Complete(ctx, prompt)
```

**Issues**:
- Large contexts consume token budget rapidly
- Irrelevant context dilutes attention
- Repeated context across calls wastes tokens
- Context limits may be exceeded

### Compression Opportunity

| Context Type | Typical Size | Compressible | Expected Ratio |
|-------------|--------------|--------------|----------------|
| Source code | 50K tokens | Yes (structure) | 3:1 |
| Documentation | 20K tokens | Yes (summaries) | 5:1 |
| Conversation history | 10K tokens | Yes (key points) | 4:1 |
| Tool outputs | 5K tokens | Yes (results) | 2:1 |

## Design Goals

1. **Semantic preservation**: Retain meaning while reducing tokens
2. **Query-aware compression**: Keep content relevant to current query
3. **Hierarchical summaries**: Multi-level compression for large contexts
4. **Incremental compression**: Update compressed context efficiently
5. **Reversibility**: Access original content when needed

## Core Types

### Compressed Context

```go
// internal/rlm/compression/context.go

// CompressedContext represents compressed content with metadata.
type CompressedContext struct {
    ID             string
    Original       *OriginalContext
    Compressed     string
    CompressionRatio float64
    TokenCount     int
    Method         CompressionMethod
    Metadata       CompressionMetadata
}

type OriginalContext struct {
    Content    string
    TokenCount int
    Source     string
    Timestamp  time.Time
}

type CompressionMethod int

const (
    MethodExtractive  CompressionMethod = iota // Select key sentences
    MethodAbstractive                           // Generate summary
    MethodHybrid                                // Combine both
    MethodHierarchical                          // Multi-level
)

type CompressionMetadata struct {
    PreservedEntities []string  // Named entities kept
    KeyConcepts       []string  // Main concepts
    DroppedSections   []string  // What was removed
    ConfidenceScore   float64   // Compression quality estimate
}
```

### Compression Options

```go
// internal/rlm/compression/options.go

type CompressionOptions struct {
    // Target compression
    TargetRatio    float64 // e.g., 0.25 = compress to 25%
    TargetTokens   int     // Alternative: absolute token target
    MinTokens      int     // Never compress below this

    // Method selection
    PreferredMethod CompressionMethod
    AllowHybrid     bool

    // Content preservation
    PreserveCode    bool    // Keep code blocks intact
    PreserveQuotes  bool    // Keep direct quotes
    PreserveNumbers bool    // Keep numeric data
    QueryContext    string  // Optimize for this query

    // Quality thresholds
    MinConfidence   float64 // Minimum compression quality
}

func DefaultOptions() CompressionOptions {
    return CompressionOptions{
        TargetRatio:     0.3,
        MinTokens:       100,
        PreferredMethod: MethodHybrid,
        AllowHybrid:     true,
        PreserveCode:    true,
        PreserveQuotes:  true,
        PreserveNumbers: true,
        MinConfidence:   0.7,
    }
}
```

## Compressor Interface

### Interface Definition

```go
// internal/rlm/compression/compressor.go

// Compressor compresses context content.
type Compressor interface {
    // Compress reduces context size while preserving meaning.
    Compress(ctx context.Context, content string, opts CompressionOptions) (*CompressedContext, error)

    // CompressBatch compresses multiple contexts.
    CompressBatch(ctx context.Context, contents []string, opts CompressionOptions) ([]*CompressedContext, error)

    // Decompress expands compressed content (if reversible).
    Decompress(ctx context.Context, compressed *CompressedContext) (string, error)
}
```

### Extractive Compressor

```go
// internal/rlm/compression/extractive.go

// ExtractiveCompressor selects important sentences.
type ExtractiveCompressor struct {
    embedder  EmbeddingProvider
    tokenizer Tokenizer
}

func (c *ExtractiveCompressor) Compress(
    ctx context.Context,
    content string,
    opts CompressionOptions,
) (*CompressedContext, error) {
    originalTokens := c.tokenizer.Count(content)

    // Parse into sentences
    sentences := c.splitSentences(content)

    // Compute importance scores
    scores := c.scoreSentences(ctx, sentences, opts.QueryContext)

    // Select top sentences up to target
    targetTokens := c.computeTarget(originalTokens, opts)
    selected := c.selectSentences(sentences, scores, targetTokens)

    // Reconstruct compressed content
    compressed := strings.Join(selected, " ")

    return &CompressedContext{
        ID: generateID(),
        Original: &OriginalContext{
            Content:    content,
            TokenCount: originalTokens,
        },
        Compressed:       compressed,
        CompressionRatio: float64(c.tokenizer.Count(compressed)) / float64(originalTokens),
        TokenCount:       c.tokenizer.Count(compressed),
        Method:           MethodExtractive,
    }, nil
}

func (c *ExtractiveCompressor) scoreSentences(
    ctx context.Context,
    sentences []string,
    query string,
) []float64 {
    scores := make([]float64, len(sentences))

    // Position score (earlier = more important for many docs)
    for i := range sentences {
        scores[i] = 1.0 - float64(i)*0.01 // Decay with position
    }

    // Query relevance (if query provided)
    if query != "" && c.embedder != nil {
        queryEmb, _ := c.embedder.Embed(ctx, []string{query})
        sentEmbs, _ := c.embedder.Embed(ctx, sentences)

        for i, sentEmb := range sentEmbs {
            similarity := cosineSimilarity(queryEmb[0], sentEmb)
            scores[i] += similarity * 2.0 // Weight relevance highly
        }
    }

    // Length score (prefer informative sentences)
    for i, sent := range sentences {
        words := len(strings.Fields(sent))
        if words > 5 && words < 50 {
            scores[i] += 0.3
        }
    }

    return scores
}

func (c *ExtractiveCompressor) selectSentences(
    sentences []string,
    scores []float64,
    targetTokens int,
) []string {
    // Create index-score pairs and sort
    type scored struct {
        index int
        score float64
    }
    pairs := make([]scored, len(sentences))
    for i, s := range scores {
        pairs[i] = scored{i, s}
    }
    sort.Slice(pairs, func(i, j int) bool {
        return pairs[i].score > pairs[j].score
    })

    // Select top sentences until target reached
    var selected []int
    tokenCount := 0
    for _, p := range pairs {
        sentTokens := c.tokenizer.Count(sentences[p.index])
        if tokenCount+sentTokens > targetTokens {
            break
        }
        selected = append(selected, p.index)
        tokenCount += sentTokens
    }

    // Sort by original position
    sort.Ints(selected)

    result := make([]string, len(selected))
    for i, idx := range selected {
        result[i] = sentences[idx]
    }

    return result
}
```

### Abstractive Compressor

```go
// internal/rlm/compression/abstractive.go

// AbstractiveCompressor generates summaries using LLM.
type AbstractiveCompressor struct {
    client    LLMClient
    tokenizer Tokenizer
}

func (c *AbstractiveCompressor) Compress(
    ctx context.Context,
    content string,
    opts CompressionOptions,
) (*CompressedContext, error) {
    originalTokens := c.tokenizer.Count(content)
    targetTokens := c.computeTarget(originalTokens, opts)

    prompt := c.buildCompressionPrompt(content, targetTokens, opts)

    compressed, _, err := c.client.Complete(ctx, prompt)
    if err != nil {
        return nil, err
    }

    compressedTokens := c.tokenizer.Count(compressed)

    return &CompressedContext{
        ID: generateID(),
        Original: &OriginalContext{
            Content:    content,
            TokenCount: originalTokens,
        },
        Compressed:       compressed,
        CompressionRatio: float64(compressedTokens) / float64(originalTokens),
        TokenCount:       compressedTokens,
        Method:           MethodAbstractive,
        Metadata: CompressionMetadata{
            KeyConcepts: c.extractConcepts(compressed),
        },
    }, nil
}

func (c *AbstractiveCompressor) buildCompressionPrompt(
    content string,
    targetTokens int,
    opts CompressionOptions,
) string {
    preserveInstructions := ""
    if opts.PreserveCode {
        preserveInstructions += "- Keep code snippets intact\n"
    }
    if opts.PreserveQuotes {
        preserveInstructions += "- Preserve important quotes\n"
    }
    if opts.PreserveNumbers {
        preserveInstructions += "- Keep numeric data and statistics\n"
    }

    queryInstruction := ""
    if opts.QueryContext != "" {
        queryInstruction = fmt.Sprintf("\nOptimize the summary for answering: %s\n", opts.QueryContext)
    }

    return fmt.Sprintf(`Compress the following content to approximately %d tokens while preserving key information.

%s%s
Preservation requirements:
%s

Content to compress:
%s

Compressed version:`, targetTokens, queryInstruction, preserveInstructions, content)
}
```

### Hybrid Compressor

```go
// internal/rlm/compression/hybrid.go

// HybridCompressor combines extractive and abstractive methods.
type HybridCompressor struct {
    extractive  *ExtractiveCompressor
    abstractive *AbstractiveCompressor
    threshold   int // Token threshold for method selection
}

func (c *HybridCompressor) Compress(
    ctx context.Context,
    content string,
    opts CompressionOptions,
) (*CompressedContext, error) {
    tokenCount := c.extractive.tokenizer.Count(content)

    // For small content, use abstractive (better quality)
    if tokenCount < c.threshold {
        return c.abstractive.Compress(ctx, content, opts)
    }

    // For large content, use two-stage compression
    // Stage 1: Extractive to reduce size
    extractiveOpts := opts
    extractiveOpts.TargetRatio = 0.5 // First pass: 50%

    extracted, err := c.extractive.Compress(ctx, content, extractiveOpts)
    if err != nil {
        return nil, err
    }

    // Stage 2: Abstractive on reduced content
    abstractiveOpts := opts
    abstractiveOpts.TargetRatio = opts.TargetRatio / 0.5 // Remaining compression

    final, err := c.abstractive.Compress(ctx, extracted.Compressed, abstractiveOpts)
    if err != nil {
        return nil, err
    }

    // Update metadata
    final.Original = &OriginalContext{
        Content:    content,
        TokenCount: tokenCount,
    }
    final.CompressionRatio = float64(final.TokenCount) / float64(tokenCount)
    final.Method = MethodHybrid

    return final, nil
}
```

## Hierarchical Compression

### Multi-Level Compression

```go
// internal/rlm/compression/hierarchical.go

// HierarchicalCompressor creates multi-level summaries.
type HierarchicalCompressor struct {
    compressor Compressor
    levels     int // Number of compression levels
}

type HierarchicalContext struct {
    Levels []*CompressionLevel
}

type CompressionLevel struct {
    Level      int
    Content    string
    TokenCount int
    Ratio      float64 // Relative to original
}

func (c *HierarchicalCompressor) Compress(
    ctx context.Context,
    content string,
    opts CompressionOptions,
) (*HierarchicalContext, error) {
    result := &HierarchicalContext{
        Levels: make([]*CompressionLevel, c.levels),
    }

    originalTokens := estimateTokens(content)
    currentContent := content

    // Create progressively compressed levels
    for i := 0; i < c.levels; i++ {
        ratio := 1.0 / math.Pow(2, float64(i+1)) // 50%, 25%, 12.5%, ...

        levelOpts := opts
        levelOpts.TargetRatio = ratio

        compressed, err := c.compressor.Compress(ctx, currentContent, levelOpts)
        if err != nil {
            return nil, err
        }

        result.Levels[i] = &CompressionLevel{
            Level:      i,
            Content:    compressed.Compressed,
            TokenCount: compressed.TokenCount,
            Ratio:      float64(compressed.TokenCount) / float64(originalTokens),
        }

        currentContent = compressed.Compressed
    }

    return result, nil
}

// SelectLevel chooses appropriate compression level for budget.
func (h *HierarchicalContext) SelectLevel(tokenBudget int) *CompressionLevel {
    // Find the least compressed level that fits
    for i := len(h.Levels) - 1; i >= 0; i-- {
        if h.Levels[i].TokenCount <= tokenBudget {
            return h.Levels[i]
        }
    }

    // Return most compressed if none fit
    return h.Levels[len(h.Levels)-1]
}
```

## Incremental Compression

### Incremental Updates

```go
// internal/rlm/compression/incremental.go

// IncrementalCompressor updates compressed context efficiently.
type IncrementalCompressor struct {
    compressor Compressor
    cache      *CompressionCache
}

type CompressionCache struct {
    contexts map[string]*CompressedContext
    mu       sync.RWMutex
}

func (c *IncrementalCompressor) Update(
    ctx context.Context,
    id string,
    newContent string,
    opts CompressionOptions,
) (*CompressedContext, error) {
    // Check cache for existing compression
    c.cache.mu.RLock()
    existing, exists := c.cache.contexts[id]
    c.cache.mu.RUnlock()

    if !exists {
        // No existing compression, compress fully
        compressed, err := c.compressor.Compress(ctx, newContent, opts)
        if err != nil {
            return nil, err
        }
        compressed.ID = id
        c.cache.Set(id, compressed)
        return compressed, nil
    }

    // Compute diff
    diff := c.computeDiff(existing.Original.Content, newContent)

    if diff.ChangeRatio < 0.1 {
        // Minor changes: update incrementally
        return c.incrementalUpdate(ctx, existing, diff, opts)
    }

    // Major changes: recompress fully
    compressed, err := c.compressor.Compress(ctx, newContent, opts)
    if err != nil {
        return nil, err
    }
    compressed.ID = id
    c.cache.Set(id, compressed)
    return compressed, nil
}

type Diff struct {
    Added       []string
    Removed     []string
    ChangeRatio float64
}

func (c *IncrementalCompressor) incrementalUpdate(
    ctx context.Context,
    existing *CompressedContext,
    diff Diff,
    opts CompressionOptions,
) (*CompressedContext, error) {
    // Compress only the added content
    if len(diff.Added) > 0 {
        addedContent := strings.Join(diff.Added, "\n")
        addedCompressed, err := c.compressor.Compress(ctx, addedContent, opts)
        if err != nil {
            return nil, err
        }

        // Merge with existing
        merged := existing.Compressed + "\n" + addedCompressed.Compressed

        // Update existing
        existing.Compressed = merged
        existing.TokenCount = estimateTokens(merged)
        c.cache.Set(existing.ID, existing)
    }

    return existing, nil
}
```

## Context Manager

### Manager

```go
// internal/rlm/compression/manager.go

// Manager handles context compression for RLM execution.
type Manager struct {
    compressor Compressor
    hierarchy  *HierarchicalCompressor
    cache      *CompressionCache
    opts       CompressionOptions
}

func NewManager(opts CompressionOptions) *Manager {
    extractive := NewExtractiveCompressor()
    abstractive := NewAbstractiveCompressor()
    hybrid := NewHybridCompressor(extractive, abstractive)

    return &Manager{
        compressor: hybrid,
        hierarchy:  NewHierarchicalCompressor(hybrid, 3),
        cache:      NewCompressionCache(),
        opts:       opts,
    }
}

// PrepareContext compresses context for LLM call.
func (m *Manager) PrepareContext(
    ctx context.Context,
    contexts []ContextChunk,
    query string,
    tokenBudget int,
) (string, error) {
    opts := m.opts
    opts.QueryContext = query

    var compressed []string
    remainingBudget := tokenBudget

    for _, chunk := range contexts {
        // Check cache first
        cached := m.cache.Get(chunk.ID)
        if cached != nil && cached.TokenCount <= remainingBudget {
            compressed = append(compressed, cached.Compressed)
            remainingBudget -= cached.TokenCount
            continue
        }

        // Determine target for this chunk
        chunkBudget := m.allocateBudget(chunk, remainingBudget, len(contexts))

        // Compress
        opts.TargetTokens = chunkBudget
        result, err := m.compressor.Compress(ctx, chunk.Content, opts)
        if err != nil {
            continue // Skip failed compressions
        }

        m.cache.Set(chunk.ID, result)
        compressed = append(compressed, result.Compressed)
        remainingBudget -= result.TokenCount
    }

    return strings.Join(compressed, "\n\n---\n\n"), nil
}

func (m *Manager) allocateBudget(chunk ContextChunk, remaining int, total int) int {
    // Weight by relevance if available
    if chunk.Relevance > 0 {
        return int(float64(remaining) * chunk.Relevance / float64(total))
    }

    // Equal distribution
    return remaining / total
}
```

## Integration

### Controller Integration

```go
// internal/rlm/controller.go

func (c *Controller) prepareContextForCall(
    ctx context.Context,
    contexts []ContextChunk,
    query string,
    maxTokens int,
) (string, error) {
    totalTokens := 0
    for _, chunk := range contexts {
        totalTokens += chunk.TokenCount
    }

    // No compression needed
    if totalTokens <= maxTokens {
        return c.concatenateContexts(contexts), nil
    }

    // Compress to fit budget
    return c.compressionManager.PrepareContext(ctx, contexts, query, maxTokens)
}
```

## Observability

### Metrics

```go
var (
    compressionRatio = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "rlm_compression_ratio",
            Help:    "Compression ratio achieved",
            Buckets: prometheus.LinearBuckets(0.1, 0.1, 10),
        },
    )

    compressionLatency = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "rlm_compression_latency_seconds",
            Help:    "Compression latency by method",
            Buckets: prometheus.ExponentialBuckets(0.01, 2, 10),
        },
        []string{"method"},
    )

    tokensSaved = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "rlm_compression_tokens_saved_total",
            Help: "Tokens saved through compression",
        },
    )

    cacheHits = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "rlm_compression_cache_hits_total",
            Help: "Compression cache hits",
        },
    )
)
```

## Testing Strategy

### Unit Tests

```go
func TestExtractiveCompressor_SelectSentences(t *testing.T) {
    compressor := NewExtractiveCompressor()

    sentences := []string{
        "This is the most important sentence.",
        "This is less important.",
        "This is also important for the query.",
        "This is filler content.",
    }

    scores := []float64{0.9, 0.3, 0.8, 0.2}

    selected := compressor.selectSentences(sentences, scores, 50)

    assert.Len(t, selected, 2)
    assert.Contains(t, selected[0], "most important")
    assert.Contains(t, selected[1], "also important")
}

func TestHierarchicalCompressor_SelectLevel(t *testing.T) {
    ctx := &HierarchicalContext{
        Levels: []*CompressionLevel{
            {Level: 0, TokenCount: 1000},
            {Level: 1, TokenCount: 500},
            {Level: 2, TokenCount: 250},
        },
    }

    // Should select least compressed that fits
    level := ctx.SelectLevel(600)
    assert.Equal(t, 1, level.Level)

    // Should select most compressed if none fit
    level = ctx.SelectLevel(100)
    assert.Equal(t, 2, level.Level)
}

func TestHybridCompressor_TwoStage(t *testing.T) {
    compressor := NewHybridCompressor(
        NewExtractiveCompressor(),
        NewAbstractiveCompressor(),
    )

    largeContent := strings.Repeat("This is test content. ", 1000)

    result, err := compressor.Compress(context.Background(), largeContent, CompressionOptions{
        TargetRatio: 0.25,
    })

    require.NoError(t, err)
    assert.Equal(t, MethodHybrid, result.Method)
    assert.Less(t, result.CompressionRatio, 0.3)
}
```

## Success Criteria

1. **Compression ratio**: Achieve 3:1 or better on typical content
2. **Semantic preservation**: >90% of key information retained (human evaluation)
3. **Query relevance**: Query-aware compression improves answer quality
4. **Performance**: Compression latency <1s for 10K tokens
5. **Cache efficiency**: >50% cache hit rate for repeated contexts

## Appendix: Compression Strategies by Content Type

| Content Type | Best Method | Target Ratio | Preserve |
|-------------|-------------|--------------|----------|
| Source code | Extractive | 0.5 | Functions, classes |
| Documentation | Abstractive | 0.3 | Key concepts |
| Conversation | Hybrid | 0.4 | Recent turns, decisions |
| Tool output | Extractive | 0.3 | Results, errors |
| Error logs | Extractive | 0.2 | Stack traces, messages |
