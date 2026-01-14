# SPEC-05: Context Compression

> Efficient handling of large contexts by compressing information while preserving semantic content.

## Overview

Context compression reduces token costs and enables processing of contexts that would otherwise exceed limits. It preserves meaning while reducing size, with query-aware optimization.

## Current State

- Full context passed to every LLM call
- Large contexts consume budget rapidly
- Irrelevant context dilutes attention
- Context limits may be exceeded

## Compression Opportunity

| Context Type | Typical Size | Expected Ratio |
|-------------|--------------|----------------|
| Source code | 50K tokens | 3:1 |
| Documentation | 20K tokens | 5:1 |
| Conversation history | 10K tokens | 4:1 |
| Tool outputs | 5K tokens | 2:1 |

## Requirements

### [SPEC-05.01] Compression Methods

Support multiple compression strategies:

| Method | Description | Best For |
|--------|-------------|----------|
| Extractive | Select key sentences | Structured content |
| Abstractive | Generate summary via LLM | Narrative content |
| Hybrid | Two-stage: extract then abstract | Large content |
| Hierarchical | Multi-level summaries | Very large content |

### [SPEC-05.02] Compression Options

```go
type CompressionOptions struct {
    TargetRatio     float64   // e.g., 0.25 = compress to 25%
    TargetTokens    int       // Alternative: absolute token target
    MinTokens       int       // Never compress below this
    PreferredMethod CompressionMethod
    PreserveCode    bool      // Keep code blocks intact
    PreserveQuotes  bool      // Keep direct quotes
    PreserveNumbers bool      // Keep numeric data
    QueryContext    string    // Optimize for this query
    MinConfidence   float64   // Minimum compression quality
}
```

### [SPEC-05.03] Extractive Compression

Select important sentences based on:

- Position score (earlier = more important)
- Query relevance (embedding similarity)
- Sentence length (prefer informative)
- Entity presence (keep named entities)

Algorithm:
1. Split into sentences
2. Score each sentence
3. Select top sentences up to target
4. Reconstruct in original order

### [SPEC-05.04] Abstractive Compression

Generate summaries using LLM:

```
Prompt:
- Target token count
- Preservation requirements (code, quotes, numbers)
- Query context for relevance optimization
- Content to compress

Output:
- Compressed version
- Key concepts extracted
```

### [SPEC-05.05] Hierarchical Compression

Create multi-level summaries for large content:

```
Level 0: 50% of original
Level 1: 25% of original
Level 2: 12.5% of original
```

Select appropriate level based on token budget.

### [SPEC-05.06] Incremental Compression

Efficiently update compressed content:

- Cache compressed versions by content ID
- Detect change ratio (diff analysis)
- Minor changes (<10%): incremental update
- Major changes: full recompression

### [SPEC-05.07] Context Manager

Orchestrate compression for RLM calls:

```go
func (m *Manager) PrepareContext(
    ctx context.Context,
    contexts []ContextChunk,
    query string,
    tokenBudget int,
) (string, error)
```

Features:
- Budget allocation across chunks
- Relevance-weighted allocation
- Cache hit optimization
- Graceful degradation

## Implementation Tasks

- [ ] Implement ExtractiveCompressor
- [ ] Implement AbstractiveCompressor
- [ ] Create HybridCompressor
- [ ] Build HierarchicalCompressor
- [ ] Add IncrementalCompressor with caching
- [ ] Create Context Manager
- [ ] Integrate with RLM Controller
- [ ] Add observability metrics
- [ ] Write tests

## Dependencies

- `internal/memory/embeddings/` - Query relevance
- `internal/rlm/` - Controller integration
- LLM client for abstractive compression

## Acceptance Criteria

1. Achieve 3:1 or better compression on typical content
2. >90% of key information retained (human evaluation)
3. Query-aware compression improves answer quality
4. Compression latency <1s for 10K tokens
5. >50% cache hit rate for repeated contexts
