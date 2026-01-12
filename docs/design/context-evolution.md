# Context Evolution Design

> Research review of ACE paper (arxiv 2510.04618) and comparison with current consolidation implementation.
> Task: `recurse-b0y.5` - Verify consolidation patterns against ACE

## Overview

ACE (Agentic Context Engineering) treats contexts as "evolving playbooks that accumulate, refine, and organize strategies" through generation, reflection, and curation. This document compares ACE patterns with our current consolidation implementation.

## ACE Key Concepts

### Problems ACE Addresses

1. **Brevity Bias**: Systems that drop domain insights for concise summaries
2. **Context Collapse**: Iterative rewriting that erodes details progressively

### ACE Solution: Grow-and-Refine

ACE uses structured, incremental updates rather than full rewrites:

| Operation | Description |
|-----------|-------------|
| **Add** | New validated insights appended with unique IDs |
| **Merge** | Semantically similar items combined via embeddings |
| **Prune** | Low-utility items removed when exceeding token budget |

### ACE Data Structure

```
## SECTION_NAME
[section_slug-00000] helpful=X harmful=Y :: content
```

Each bullet contains:
- Unique identifier for provenance tracking
- Helpful/harmful counters for utility tracking
- Content with domain-specific knowledge preserved

## Current Implementation Analysis

### consolidate.go Review

| ACE Pattern | Current Implementation | Status |
|-------------|----------------------|--------|
| Source link preservation | `PreserveSourceLinks: true` | ✅ Implemented |
| Semantic deduplication | Content-based only (`normalizeContent`) | ⚠️ Partial |
| Unique identifiers | Uses hypergraph node IDs | ✅ Implemented |
| Helpful/harmful counters | Only `AccessCount` and `Confidence` | ⚠️ Partial |
| Grow-and-refine | Truncates to `MaxSummaryLength` | ❌ Brevity bias risk |
| Delta updates | Full summarization pass | ⚠️ Could improve |
| Token budget management | `MaxSummaryLength` only | ⚠️ Partial |

### Strengths

1. **Source Links Preserved**: The `PreserveSourceLinks` config ensures summaries link back to source nodes via `HyperedgeComposition`, matching ACE's provenance tracking.

2. **Confidence Propagation**: Average confidence is calculated for summaries, preserving reliability signals.

3. **Edge Strengthening**: The `strengthenEdges` function increases weight of frequently-traversed connections, similar to ACE's utility tracking.

### Gaps Identified

#### 1. Brevity Bias Risk

Current implementation truncates content aggressively:
```go
// generateSummary in consolidate.go
if len(content) > 200 {
    content = content[:197] + "..."  // Truncation loses detail
}
// ...
if len(summary) > c.config.MaxSummaryLength {
    summary = summary[:c.config.MaxSummaryLength-3] + "..."
}
```

**ACE Approach**: Contexts should function as comprehensive playbooks, preserving domain-specific heuristics. LLMs benefit from long, detailed contexts and can distill relevance autonomously.

**Recommendation**: Add `playbook_token_budget` style config (default: 80K tokens) instead of character-based truncation. Prioritize removal of low-utility items over truncation.

#### 2. Limited Utility Tracking

Current tracking:
- `AccessCount`: How often node is accessed
- `Confidence`: Reliability score

**ACE Tracking**:
- `helpful`: Count of times this insight led to success
- `harmful`: Count of times this insight led to failure
- Net utility: `helpful - harmful`

**Recommendation**: Add `HelpfulCount` and `HarmfulCount` to node metadata for outcome-based utility tracking.

#### 3. Simple Deduplication

Current: String normalization comparison
```go
func normalizeContent(content string) string {
    content = strings.ToLower(content)
    content = strings.Join(strings.Fields(content), " ")
    return content
}
```

**ACE Approach**: Semantic embedding comparison with configurable similarity threshold.

**Status**: Our hypergraph already supports embeddings. The `SimilarityThreshold: 0.85` config exists but isn't used for embedding-based comparison.

**Recommendation**: Use embeddings for deduplication when available:
```go
func (c *Consolidator) areSimilar(a, b *hypergraph.Node) bool {
    if len(a.Embedding) > 0 && len(b.Embedding) > 0 {
        return cosineSimilarity(a.Embedding, b.Embedding) > c.config.SimilarityThreshold
    }
    return normalizeContent(a.Content) == normalizeContent(b.Content)
}
```

#### 4. Missing Incremental Updates

Current summarization creates new summary nodes and links them to sources. It doesn't update existing summaries incrementally.

**ACE Approach**: Delta updates that modify existing context items in-place (updating counters) or append new items.

**Recommendation**: Before creating new summaries, check for existing summaries that could be extended:
```go
// Find existing summary for this group
existingSummary := c.findExistingSummary(ctx, group)
if existingSummary != nil {
    c.extendSummary(ctx, existingSummary, newNodes)
} else {
    c.createSummary(ctx, group, targetTier)
}
```

## Implementation Recommendations

### Phase 1: Prevent Brevity Bias (High Priority)

1. Replace character limits with token budget
2. Add `preserveFullContent` option to keep original content in metadata
3. Implement priority-based pruning (remove lowest utility first)

```go
type ConsolidationConfig struct {
    // ... existing fields ...

    // TokenBudget is the maximum tokens for consolidated context.
    // Replaces MaxSummaryLength for smarter space management.
    TokenBudget int

    // PreserveFullContent stores original content in node metadata
    // even when summary is created.
    PreserveFullContent bool

    // PruneByUtility removes lowest-utility items first when
    // exceeding budget, rather than truncating.
    PruneByUtility bool
}
```

### Phase 2: Enhanced Utility Tracking (Medium Priority)

1. Add `HelpfulCount` and `HarmfulCount` to node schema
2. Implement outcome callback to update counts after retrieval
3. Use net utility for pruning decisions

```go
// In hypergraph/node.go
type Node struct {
    // ... existing fields ...

    HelpfulCount int `json:"helpful_count"`
    HarmfulCount int `json:"harmful_count"`
}

// Net utility for pruning decisions
func (n *Node) NetUtility() int {
    return n.HelpfulCount - n.HarmfulCount
}
```

### Phase 3: Semantic Deduplication (Medium Priority)

1. Use embeddings for similarity comparison when available
2. Configurable fallback to content-based comparison
3. Merge similar nodes by combining content and summing counters

### Phase 4: Incremental Updates (Lower Priority)

1. Track which nodes have been summarized
2. Extend existing summaries rather than creating new ones
3. Implement "crystallization" for stable, high-utility patterns

## Metrics to Track

Based on ACE evaluation:

| Metric | Description | Target |
|--------|-------------|--------|
| Context Compression Ratio | Original / Consolidated size | < 5:1 |
| Information Retention | Key facts preserved after consolidation | > 95% |
| Retrieval Precision | Consolidated context leads to correct answers | > 90% |
| Utility Correlation | High-utility items retained vs pruned | > 0.8 |

## References

- [ACE Paper (arxiv 2510.04618)](https://arxiv.org/abs/2510.04618)
- [ACE GitHub](https://github.com/ace-agent/ace)
- [ACE HTML Version](https://arxiv.org/html/2510.04618v1)
- [SambaNova ACE Blog](https://sambanova.ai/blog/ace-open-sourced-on-github)
- Current implementation: `internal/memory/evolution/consolidate.go`

## Conclusion

The current consolidation implementation has a solid foundation with source link preservation and confidence tracking. The main gap is **brevity bias** from aggressive truncation. Implementing token-budget-based management and utility-based pruning (from ACE's grow-and-refine approach) would significantly improve context quality.

Priority order:
1. ⚠️ Fix brevity bias (replace truncation with utility-based pruning)
2. ⚠️ Add helpful/harmful outcome tracking
3. ⚠️ Enable semantic deduplication using existing embeddings
4. Future: Incremental summary updates
