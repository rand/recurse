# SPEC-11: Embedding Integration

## Overview

[SPEC-11.01] The system SHALL integrate embedding-based semantic search into the hypergraph memory system to enable retrieval based on semantic similarity rather than exact keyword matching.

[SPEC-11.02] The embedding system MUST support multiple embedding providers through a common interface, with Voyage AI as the primary provider.

[SPEC-11.03] Search MUST combine keyword (FTS5) and semantic (embedding) approaches using Reciprocal Rank Fusion (RRF) for optimal retrieval quality.

> **Informative**: Research basis includes A-MEM (NeurIPS 2025) on agentic memory with semantic retrieval and Zep's temporal knowledge graphs with embeddings.

## Provider Interface

[SPEC-11.04] The `Provider` interface SHALL define the contract for embedding generation:
```go
type Provider interface {
    Embed(ctx context.Context, texts []string) ([]Vector, error)
    Dimensions() int
    Model() string
}
```

[SPEC-11.05] All providers MUST support batch embedding to minimize API calls and latency.

[SPEC-11.06] The provider factory SHALL auto-detect the appropriate provider based on environment variables:
- `VOYAGE_API_KEY` present → Voyage provider
- Otherwise → Local provider (CodeRankEmbed)

[SPEC-11.07] Provider selection MAY be overridden via `EMBEDDING_PROVIDER` environment variable.

## Vector Type

[SPEC-11.08] The `Vector` type SHALL be defined as `[]float32` for memory efficiency.

[SPEC-11.09] Vector operations SHALL include:

| Method | Description |
|--------|-------------|
| `Similarity(other)` | Cosine similarity (-1 to 1) |
| `Distance(other)` | L2 Euclidean distance |
| `Normalize()` | Unit vector in same direction |
| `ToBytes()` | Serialize for storage |

[SPEC-11.10] `VectorFromBytes` SHALL deserialize vectors stored as BLOBs.

## Voyage AI Provider

[SPEC-11.11] The Voyage provider SHALL support the following models:

| Model | Dimensions | Use Case |
|-------|------------|----------|
| `voyage-3` | 1024 | General purpose (default) |
| `voyage-code-3` | 1024 | Code-optimized |
| `voyage-3-lite` | 1024 | Lower latency |
| `voyage-3-large` | 1024 | Higher quality |

[SPEC-11.12] The Voyage provider MUST implement rate limiting (default: 10 requests/second) using token bucket algorithm.

[SPEC-11.13] Configuration SHALL support functional options:
```go
provider, err := NewVoyageProvider(
    WithAPIKey(key),
    WithModel("voyage-code-3"),
    WithRateLimit(5.0),
    WithTimeout(60*time.Second),
)
```

[SPEC-11.14] The provider SHALL use HTTP connection pooling (max 100 idle connections, 10 per host).

## Local Provider

[SPEC-11.15] The local provider SHALL use CodeRankEmbed via a Python server for embedding generation without external API dependencies.

[SPEC-11.16] Local embeddings SHOULD be used for development and testing when Voyage API keys are unavailable.

## Embedding Index

[SPEC-11.17] The embedding index SHALL store vectors in SQLite:
```sql
CREATE TABLE node_embeddings (
    node_id TEXT PRIMARY KEY,
    embedding BLOB NOT NULL,
    model TEXT NOT NULL,
    dimensions INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
)
```

[SPEC-11.18] The index SHALL support both synchronous and asynchronous indexing:
- `IndexSync(ctx, nodeID, content)` - blocks until complete
- `IndexAsync(nodeID, content)` - queues for background processing

[SPEC-11.19] Background indexing SHALL use configurable workers and batch processing:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `BatchSize` | 32 | Texts per embedding request |
| `Workers` | 2 | Concurrent background workers |
| `QueueSize` | 1000 | Maximum pending requests |

[SPEC-11.20] When the background queue is full, new requests SHALL be dropped with a warning log.

## Similarity Search

[SPEC-11.21] `Search(ctx, query, limit)` SHALL embed the query and find similar nodes.

[SPEC-11.22] `SearchByVector(ctx, vector, limit)` SHALL find nodes similar to a pre-computed vector.

[SPEC-11.23] Similarity search SHALL compute cosine similarity against all stored embeddings.

> **Informative**: The current O(n) scan implementation works for moderate scale. For production with >100k nodes, sqlite-vec or pgvector is recommended.

[SPEC-11.24] Results SHALL be sorted by descending similarity and limited to the requested count.

## Hybrid Search

[SPEC-11.25] Hybrid search SHALL combine keyword and semantic search results using Reciprocal Rank Fusion.

[SPEC-11.26] The RRF formula SHALL be:
```
RRF(d) = Σ weight / (k + rank(d))
```
where k is the RRF constant (default: 60).

[SPEC-11.27] Hybrid search SHALL be configured with:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `Alpha` | 0.7 | Semantic weight (0=keyword only, 1=semantic only) |
| `K` | 60 | RRF constant |

[SPEC-11.28] The `HybridSearcher` SHALL:
1. Execute keyword search via `Store.SearchByContent`
2. Execute semantic search via `Index.Search`
3. Combine results using RRF
4. Hydrate any semantic-only results with full node data
5. Apply filters (type, tier, subtype, confidence)
6. Record outcomes for meta-evolution tracking

[SPEC-11.29] If semantic search fails, hybrid search SHALL fall back to keyword-only results with a warning log.

[SPEC-11.30] Alpha MAY be adjusted at runtime via `SetAlpha(float64)`.

## Outcome Recording

[SPEC-11.31] Hybrid search SHALL record retrieval outcomes for meta-evolution tracking:
```go
type RetrievalOutcome struct {
    QueryHash      string    // For grouping related queries
    QueryType      string    // computational, retrieval, etc.
    NodeID         string
    NodeType       string
    NodeSubtype    string
    RelevanceScore float64
    WasUsed        bool      // Updated via feedback
    ContextTokens  int
    LatencyMs      int
    Timestamp      time.Time
}
```

[SPEC-11.32] The `OutcomeRecorder` interface SHALL allow pluggable storage:
```go
type OutcomeRecorder interface {
    RecordOutcome(ctx context.Context, outcome RetrievalOutcome) error
}
```

[SPEC-11.33] Query hashing SHALL use SHA-256, truncated to 16 hex characters for efficiency.

## Caching

[SPEC-11.34] The `CachedProvider` decorator MAY cache embeddings to avoid redundant API calls.

[SPEC-11.35] Cache keys SHALL be content hashes to detect identical texts.

[SPEC-11.36] Cache eviction SHOULD use LRU policy with configurable maximum size.

## Metrics

[SPEC-11.37] The embedding system SHALL collect metrics:

| Metric | Type | Description |
|--------|------|-------------|
| `embed_duration` | Histogram | Time to embed batch |
| `embed_count` | Counter | Total embeddings generated |
| `embed_errors` | Counter | Failed embedding requests |
| `search_duration` | Histogram | Vector search time |
| `hybrid_search_duration` | Histogram | Full hybrid search time |
| `queue_depth` | Gauge | Pending background requests |

[SPEC-11.38] Metrics SHALL be accessible via `Index.Metrics()`.

## Implementation Location

[SPEC-11.39] Embedding types and providers SHALL be in `internal/memory/embeddings/`:
- `provider.go` - Interface and Vector type
- `voyage.go` - Voyage AI provider
- `local.go` - Local CodeRankEmbed provider
- `cached.go` - Caching decorator
- `index.go` - Storage and search
- `metrics.go` - Observability

[SPEC-11.40] Hybrid search SHALL be in `internal/memory/hypergraph/hybrid_search.go`.

## Error Handling

[SPEC-11.41] Provider errors SHALL be wrapped with context (e.g., "voyage request: connection refused").

[SPEC-11.42] Rate limit violations SHALL wait (with context) rather than fail immediately.

[SPEC-11.43] The system SHALL NOT panic on embedding failures; graceful degradation to keyword search is preferred.

## Configuration Example

[SPEC-11.44] A complete configuration example:
```go
// Create provider
provider, err := embeddings.NewVoyageProvider(
    embeddings.WithModel("voyage-code-3"),
)

// Create index
index, err := embeddings.NewIndex(db, embeddings.IndexConfig{
    Provider:  provider,
    BatchSize: 32,
    Workers:   2,
    Metrics:   metrics,
})

// Create hybrid searcher
searcher := hypergraph.NewHybridSearcher(store, index, hypergraph.HybridConfig{
    Alpha:           0.7,
    OutcomeRecorder: evolutionManager,
})

// Search
results, err := searcher.Search(ctx, "authentication flow", hypergraph.SearchOptions{
    Types: []hypergraph.NodeType{hypergraph.NodeTypeFact, hypergraph.NodeTypeSnippet},
    Limit: 10,
})
```
