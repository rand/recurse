# Embedding Integration Design

> Design document for `recurse-eim`: [SPEC] Embedding Integration Design

## Overview

This document specifies the integration of embedding-based semantic search into the hypergraph memory system. The current implementation uses keyword matching (LIKE-based search) which misses semantically similar content. Embedding integration enables retrieval of conceptually related memories even when exact keywords don't match.

## Problem Statement

### Current State

The existing `SearchByContent` in `internal/memory/hypergraph/query.go` uses SQL LIKE:

```go
sqlQuery := `SELECT ... FROM nodes WHERE content LIKE ?`
args := []any{"%" + query + "%"}
```

**Limitations**:
- Misses synonyms: "error" doesn't find "exception" or "failure"
- Misses paraphrases: "how to parse JSON" doesn't find "deserialize JSON data"
- No concept clustering: related ideas stored separately aren't connected
- Linear scan: O(n) search over all nodes

### Expected Improvements

| Metric | Keyword Only | With Embeddings |
|--------|--------------|-----------------|
| Recall (relevant items found) | ~40% | ~85% |
| Semantic similarity matching | No | Yes |
| Query latency (10K nodes) | 50ms | 20ms (with index) |
| Cross-language support | No | Yes (multilingual models) |

## Design Goals

1. **Hybrid search**: Combine keyword and semantic for best of both
2. **Lazy embedding**: Embed on demand, not at insert time (for speed)
3. **Background indexing**: Batch embedding without blocking operations
4. **Provider abstraction**: Support Voyage, OpenAI, or local models
5. **Graceful degradation**: Fall back to keyword if embedding fails

## Core Types

### Embedding Provider Interface

```go
// internal/memory/embeddings/provider.go

// Provider generates embeddings from text.
type Provider interface {
    // Embed generates embeddings for one or more texts.
    // Returns vectors of the same length as input texts.
    Embed(ctx context.Context, texts []string) ([]Vector, error)

    // Dimensions returns the embedding dimension for this model.
    Dimensions() int

    // Model returns the model identifier.
    Model() string
}

// Vector is a dense embedding vector.
type Vector []float32

// Similarity computes cosine similarity between two vectors.
func (v Vector) Similarity(other Vector) float32 {
    if len(v) != len(other) {
        return 0
    }
    var dot, normV, normO float32
    for i := range v {
        dot += v[i] * other[i]
        normV += v[i] * v[i]
        normO += other[i] * other[i]
    }
    if normV == 0 || normO == 0 {
        return 0
    }
    return dot / (sqrt(normV) * sqrt(normO))
}
```

### Voyage Provider

```go
// internal/memory/embeddings/voyage.go

type VoyageProvider struct {
    apiKey     string
    model      string // "voyage-3" or "voyage-code-3"
    httpClient *http.Client
    rateLimit  *rate.Limiter
}

type VoyageConfig struct {
    APIKey    string
    Model     string        // Default: "voyage-3"
    RateLimit float64       // Requests per second (default: 10)
    Timeout   time.Duration // Default: 30s
}

func NewVoyageProvider(cfg VoyageConfig) (*VoyageProvider, error) {
    if cfg.APIKey == "" {
        return nil, errors.New("voyage API key required")
    }
    if cfg.Model == "" {
        cfg.Model = "voyage-3"
    }
    if cfg.RateLimit == 0 {
        cfg.RateLimit = 10
    }
    if cfg.Timeout == 0 {
        cfg.Timeout = 30 * time.Second
    }

    return &VoyageProvider{
        apiKey:     cfg.APIKey,
        model:      cfg.Model,
        httpClient: &http.Client{Timeout: cfg.Timeout},
        rateLimit:  rate.NewLimiter(rate.Limit(cfg.RateLimit), 1),
    }, nil
}

func (p *VoyageProvider) Embed(ctx context.Context, texts []string) ([]Vector, error) {
    if err := p.rateLimit.Wait(ctx); err != nil {
        return nil, fmt.Errorf("rate limit: %w", err)
    }

    req := voyageRequest{
        Model: p.model,
        Input: texts,
    }

    body, _ := json.Marshal(req)
    httpReq, _ := http.NewRequestWithContext(ctx, "POST",
        "https://api.voyageai.com/v1/embeddings", bytes.NewReader(body))
    httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
    httpReq.Header.Set("Content-Type", "application/json")

    resp, err := p.httpClient.Do(httpReq)
    if err != nil {
        return nil, fmt.Errorf("voyage request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("voyage error %d: %s", resp.StatusCode, body)
    }

    var result voyageResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, fmt.Errorf("decode response: %w", err)
    }

    vectors := make([]Vector, len(result.Data))
    for i, d := range result.Data {
        vectors[i] = d.Embedding
    }

    return vectors, nil
}

func (p *VoyageProvider) Dimensions() int {
    switch p.model {
    case "voyage-3", "voyage-3-lite":
        return 1024
    case "voyage-code-3":
        return 1024
    default:
        return 1024
    }
}

func (p *VoyageProvider) Model() string {
    return p.model
}

type voyageRequest struct {
    Model string   `json:"model"`
    Input []string `json:"input"`
}

type voyageResponse struct {
    Data []struct {
        Embedding Vector `json:"embedding"`
    } `json:"data"`
}
```

### Embedding Index

SQLite vec extension for vector storage and search:

```go
// internal/memory/embeddings/index.go

type Index struct {
    db         *sql.DB
    provider   Provider
    batchSize  int
    background chan indexRequest
}

type IndexConfig struct {
    Provider    Provider
    BatchSize   int           // Texts to embed per batch (default: 32)
    Workers     int           // Background workers (default: 2)
    QueueSize   int           // Background queue depth (default: 1000)
}

func NewIndex(db *sql.DB, cfg IndexConfig) (*Index, error) {
    if cfg.BatchSize == 0 {
        cfg.BatchSize = 32
    }
    if cfg.Workers == 0 {
        cfg.Workers = 2
    }
    if cfg.QueueSize == 0 {
        cfg.QueueSize = 1000
    }

    idx := &Index{
        db:         db,
        provider:   cfg.Provider,
        batchSize:  cfg.BatchSize,
        background: make(chan indexRequest, cfg.QueueSize),
    }

    // Initialize vec0 virtual table
    if err := idx.initSchema(); err != nil {
        return nil, err
    }

    // Start background workers
    for i := 0; i < cfg.Workers; i++ {
        go idx.worker()
    }

    return idx, nil
}

func (idx *Index) initSchema() error {
    // Create virtual table for vector search
    // Using sqlite-vec extension
    _, err := idx.db.Exec(fmt.Sprintf(`
        CREATE VIRTUAL TABLE IF NOT EXISTS node_embeddings USING vec0(
            node_id TEXT PRIMARY KEY,
            embedding FLOAT[%d]
        )
    `, idx.provider.Dimensions()))
    return err
}
```

### Hybrid Search

```go
// internal/memory/hypergraph/hybrid_search.go

type HybridSearcher struct {
    store     *Store
    index     *embeddings.Index
    alpha     float64 // Weight for semantic vs keyword (0=keyword, 1=semantic)
}

type HybridConfig struct {
    Alpha           float64 // Default: 0.7 (favor semantic)
    KeywordBoost    float64 // Boost for exact keyword matches
    RecencyWeight   float64 // Weight for recency in scoring
    AccessWeight    float64 // Weight for access count in scoring
}

func NewHybridSearcher(store *Store, index *embeddings.Index, cfg HybridConfig) *HybridSearcher {
    if cfg.Alpha == 0 {
        cfg.Alpha = 0.7
    }
    return &HybridSearcher{
        store: store,
        index: index,
        alpha: cfg.Alpha,
    }
}

// Search performs hybrid keyword + semantic search.
func (h *HybridSearcher) Search(
    ctx context.Context,
    query string,
    opts SearchOptions,
) ([]*SearchResult, error) {
    limit := opts.Limit
    if limit == 0 {
        limit = 20
    }

    // Fetch more candidates for fusion
    candidateLimit := limit * 3

    // 1. Keyword search
    keywordResults, err := h.store.SearchByContent(ctx, query, SearchOptions{
        Types:         opts.Types,
        Tiers:         opts.Tiers,
        Subtypes:      opts.Subtypes,
        MinConfidence: opts.MinConfidence,
        Limit:         candidateLimit,
    })
    if err != nil {
        return nil, fmt.Errorf("keyword search: %w", err)
    }

    // 2. Semantic search
    semanticResults, err := h.index.Search(ctx, query, candidateLimit, opts)
    if err != nil {
        // Fall back to keyword-only on embedding failure
        return keywordResults[:min(len(keywordResults), limit)], nil
    }

    // 3. Reciprocal Rank Fusion
    return h.reciprocalRankFusion(keywordResults, semanticResults, limit), nil
}

// reciprocalRankFusion combines two ranked lists using RRF.
// RRF(d) = Σ 1/(k + rank(d)) for each list
func (h *HybridSearcher) reciprocalRankFusion(
    keyword, semantic []*SearchResult,
    limit int,
) []*SearchResult {
    const k = 60 // RRF constant (standard value)

    scores := make(map[string]float64)
    nodes := make(map[string]*Node)

    // Score keyword results (weighted by 1-alpha)
    for i, r := range keyword {
        id := r.Node.ID
        scores[id] += (1 - h.alpha) / float64(k+i+1)
        nodes[id] = r.Node
    }

    // Score semantic results (weighted by alpha)
    for i, r := range semantic {
        id := r.Node.ID
        scores[id] += h.alpha / float64(k+i+1)
        nodes[id] = r.Node
    }

    // Sort by combined score
    type scored struct {
        id    string
        score float64
    }
    var all []scored
    for id, score := range scores {
        all = append(all, scored{id, score})
    }
    sort.Slice(all, func(i, j int) bool {
        return all[i].score > all[j].score
    })

    // Take top limit
    results := make([]*SearchResult, 0, limit)
    for i := 0; i < len(all) && i < limit; i++ {
        results = append(results, &SearchResult{
            Node:  nodes[all[i].id],
            Score: all[i].score,
        })
    }

    return results
}
```

## Background Indexing

### Index Queue

```go
type indexRequest struct {
    nodeID  string
    content string
    done    chan error // Optional: for sync indexing
}

func (idx *Index) worker() {
    batch := make([]indexRequest, 0, idx.batchSize)
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    flush := func() {
        if len(batch) == 0 {
            return
        }

        texts := make([]string, len(batch))
        for i, req := range batch {
            texts[i] = req.content
        }

        vectors, err := idx.provider.Embed(context.Background(), texts)

        for i, req := range batch {
            if err != nil {
                if req.done != nil {
                    req.done <- err
                }
                continue
            }

            if storeErr := idx.storeVector(req.nodeID, vectors[i]); storeErr != nil {
                if req.done != nil {
                    req.done <- storeErr
                }
                continue
            }

            if req.done != nil {
                req.done <- nil
            }
        }

        batch = batch[:0]
    }

    for {
        select {
        case req, ok := <-idx.background:
            if !ok {
                flush()
                return
            }
            batch = append(batch, req)
            if len(batch) >= idx.batchSize {
                flush()
            }

        case <-ticker.C:
            flush()
        }
    }
}

// IndexAsync queues a node for background embedding.
func (idx *Index) IndexAsync(nodeID, content string) {
    select {
    case idx.background <- indexRequest{nodeID: nodeID, content: content}:
    default:
        // Queue full, log warning
    }
}

// IndexSync embeds and stores immediately, waiting for completion.
func (idx *Index) IndexSync(ctx context.Context, nodeID, content string) error {
    done := make(chan error, 1)
    select {
    case idx.background <- indexRequest{nodeID: nodeID, content: content, done: done}:
    case <-ctx.Done():
        return ctx.Err()
    }

    select {
    case err := <-done:
        return err
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

### Lazy Embedding Strategy

Embed on first search, not on insert:

```go
// internal/memory/hypergraph/store.go

func (s *Store) CreateNode(ctx context.Context, node *Node) error {
    // ... existing insert logic ...

    // Queue for background embedding if index available
    if s.embeddingIndex != nil && len(node.Content) > 0 {
        s.embeddingIndex.IndexAsync(node.ID, node.Content)
    }

    return nil
}

// EnsureEmbedded blocks until a node's embedding is available.
func (s *Store) EnsureEmbedded(ctx context.Context, nodeID string) error {
    // Check if already embedded
    if s.embeddingIndex.HasEmbedding(nodeID) {
        return nil
    }

    // Get node content
    node, err := s.GetNode(ctx, nodeID)
    if err != nil {
        return err
    }

    // Embed synchronously
    return s.embeddingIndex.IndexSync(ctx, nodeID, node.Content)
}
```

## Vector Search

### SQLite vec Integration

```go
func (idx *Index) storeVector(nodeID string, vec Vector) error {
    _, err := idx.db.Exec(`
        INSERT OR REPLACE INTO node_embeddings (node_id, embedding)
        VALUES (?, ?)
    `, nodeID, vectorToBlob(vec))
    return err
}

func (idx *Index) Search(
    ctx context.Context,
    query string,
    limit int,
    opts SearchOptions,
) ([]*SearchResult, error) {
    // Embed query
    vectors, err := idx.provider.Embed(ctx, []string{query})
    if err != nil {
        return nil, fmt.Errorf("embed query: %w", err)
    }
    queryVec := vectors[0]

    // Vector search with sqlite-vec
    rows, err := idx.db.QueryContext(ctx, `
        SELECT node_id, distance
        FROM node_embeddings
        WHERE embedding MATCH ?
        ORDER BY distance
        LIMIT ?
    `, vectorToBlob(queryVec), limit)
    if err != nil {
        return nil, fmt.Errorf("vector search: %w", err)
    }
    defer rows.Close()

    var results []*SearchResult
    for rows.Next() {
        var nodeID string
        var distance float32
        if err := rows.Scan(&nodeID, &distance); err != nil {
            return nil, err
        }

        // Convert distance to similarity (1 - distance for L2, direct for cosine)
        similarity := 1 - distance

        results = append(results, &SearchResult{
            Node:  &Node{ID: nodeID}, // Hydrate later
            Score: float64(similarity),
        })
    }

    // Hydrate nodes
    for _, r := range results {
        node, err := idx.getNode(ctx, r.Node.ID)
        if err != nil {
            continue
        }
        r.Node = node
    }

    return results, nil
}

func vectorToBlob(v Vector) []byte {
    buf := make([]byte, len(v)*4)
    for i, f := range v {
        binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
    }
    return buf
}
```

## Integration Points

### Store Integration

```go
// internal/memory/hypergraph/store.go

type Store struct {
    db             *sql.DB
    mu             sync.RWMutex
    path           string
    embeddingIndex *embeddings.Index     // NEW
    hybridSearcher *HybridSearcher       // NEW
}

type Options struct {
    Path              string
    CreateIfNotExists bool
    EmbeddingProvider embeddings.Provider  // NEW
    EmbeddingConfig   embeddings.IndexConfig // NEW
}

func NewStore(opts Options) (*Store, error) {
    // ... existing setup ...

    if opts.EmbeddingProvider != nil {
        idx, err := embeddings.NewIndex(db, embeddings.IndexConfig{
            Provider:  opts.EmbeddingProvider,
            BatchSize: opts.EmbeddingConfig.BatchSize,
            Workers:   opts.EmbeddingConfig.Workers,
        })
        if err != nil {
            return nil, fmt.Errorf("init embedding index: %w", err)
        }
        store.embeddingIndex = idx
        store.hybridSearcher = NewHybridSearcher(store, idx, HybridConfig{})
    }

    return store, nil
}

// Search uses hybrid search if embeddings available, otherwise keyword.
func (s *Store) Search(ctx context.Context, query string, opts SearchOptions) ([]*SearchResult, error) {
    if s.hybridSearcher != nil {
        return s.hybridSearcher.Search(ctx, query, opts)
    }
    return s.SearchByContent(ctx, query, opts)
}
```

### RLM Controller Integration

```go
// internal/rlm/controller.go

func (c *Controller) retrieveContext(ctx context.Context, query string) ([]ContextChunk, error) {
    // Use hybrid search for memory retrieval
    results, err := c.memoryStore.Search(ctx, query, hypergraph.SearchOptions{
        Tiers:         []hypergraph.Tier{hypergraph.TierSession, hypergraph.TierLongTerm},
        MinConfidence: 0.3,
        Limit:         10,
    })
    if err != nil {
        return nil, err
    }

    var chunks []ContextChunk
    for _, r := range results {
        chunks = append(chunks, ContextChunk{
            Content:    r.Node.Content,
            Source:     r.Node.ID,
            Relevance:  r.Score,
            Tier:       string(r.Node.Tier),
        })
    }

    return chunks, nil
}
```

## Caching

### Query Cache

Cache query embeddings to avoid re-embedding repeated queries:

```go
type QueryCache struct {
    cache *lru.Cache[string, Vector]
    mu    sync.RWMutex
}

func NewQueryCache(size int) *QueryCache {
    cache, _ := lru.New[string, Vector](size)
    return &QueryCache{cache: cache}
}

func (c *QueryCache) Get(query string) (Vector, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.cache.Get(query)
}

func (c *QueryCache) Set(query string, vec Vector) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.cache.Add(query, vec)
}
```

### Provider with Cache

```go
type CachedProvider struct {
    provider Provider
    cache    *QueryCache
}

func (p *CachedProvider) Embed(ctx context.Context, texts []string) ([]Vector, error) {
    results := make([]Vector, len(texts))
    var toEmbed []string
    var toEmbedIdx []int

    for i, text := range texts {
        if vec, ok := p.cache.Get(text); ok {
            results[i] = vec
        } else {
            toEmbed = append(toEmbed, text)
            toEmbedIdx = append(toEmbedIdx, i)
        }
    }

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
```

## Observability

### Metrics

```go
var (
    embeddingLatency = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "embeddings_latency_seconds",
            Help:    "Embedding generation latency",
            Buckets: prometheus.ExponentialBuckets(0.01, 2, 10),
        },
        []string{"provider", "batch_size"},
    )

    embeddingQueueDepth = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "embeddings_queue_depth",
            Help: "Number of pending embedding requests",
        },
    )

    hybridSearchLatency = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "hybrid_search_latency_seconds",
            Help:    "Hybrid search latency by phase",
            Buckets: prometheus.ExponentialBuckets(0.001, 2, 12),
        },
        []string{"phase"}, // "keyword", "semantic", "fusion"
    )

    embeddingCacheHits = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "embeddings_cache_hits_total",
            Help: "Number of embedding cache hits",
        },
    )
)
```

## Testing Strategy

### Unit Tests

```go
func TestVoyageProvider_Embed(t *testing.T) {
    if os.Getenv("VOYAGE_API_KEY") == "" {
        t.Skip("VOYAGE_API_KEY not set")
    }

    provider, _ := NewVoyageProvider(VoyageConfig{
        APIKey: os.Getenv("VOYAGE_API_KEY"),
        Model:  "voyage-3",
    })

    vectors, err := provider.Embed(context.Background(), []string{
        "Hello world",
        "Goodbye world",
    })

    require.NoError(t, err)
    assert.Len(t, vectors, 2)
    assert.Len(t, vectors[0], 1024)

    // Similar texts should have high similarity
    sim := vectors[0].Similarity(vectors[1])
    assert.Greater(t, sim, float32(0.7))
}

func TestHybridSearch_RRF(t *testing.T) {
    searcher := &HybridSearcher{alpha: 0.7}

    keyword := []*SearchResult{
        {Node: &Node{ID: "a"}, Score: 1.0},
        {Node: &Node{ID: "b"}, Score: 0.8},
        {Node: &Node{ID: "c"}, Score: 0.6},
    }
    semantic := []*SearchResult{
        {Node: &Node{ID: "b"}, Score: 1.0},
        {Node: &Node{ID: "d"}, Score: 0.9},
        {Node: &Node{ID: "a"}, Score: 0.7},
    }

    results := searcher.reciprocalRankFusion(keyword, semantic, 3)

    // "b" should rank highest (appears in both lists highly)
    assert.Equal(t, "b", results[0].Node.ID)
    // "a" should rank second (top keyword, mid semantic)
    assert.Equal(t, "a", results[1].Node.ID)
}
```

### Integration Tests

```go
func TestHybridSearch_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    store := newTestStore(t, Options{
        EmbeddingProvider: newTestProvider(),
    })
    defer store.Close()

    // Insert test nodes
    nodes := []string{
        "How to handle errors in Go programming",
        "Error handling best practices for Golang",
        "Python exception management guide",
        "JavaScript async error patterns",
    }

    for i, content := range nodes {
        err := store.CreateNode(context.Background(), &Node{
            ID:      fmt.Sprintf("node-%d", i),
            Type:    NodeTypeKnowledge,
            Content: content,
            Tier:    TierSession,
        })
        require.NoError(t, err)
    }

    // Wait for background indexing
    time.Sleep(500 * time.Millisecond)

    // Search should find related Go content
    results, err := store.Search(context.Background(), "golang error handling", SearchOptions{
        Limit: 2,
    })

    require.NoError(t, err)
    assert.Len(t, results, 2)

    // Top results should be the Go-related nodes
    ids := []string{results[0].Node.ID, results[1].Node.ID}
    assert.Contains(t, ids, "node-0")
    assert.Contains(t, ids, "node-1")
}
```

## Migration Path

### Phase 1: Optional Provider

```go
// Embeddings are optional - system works without them
store, _ := NewStore(Options{
    Path: "memory.db",
    // No embedding provider = keyword-only search
})
```

### Phase 2: Background Migration

```go
// Migrate existing nodes to have embeddings
func (s *Store) MigrateEmbeddings(ctx context.Context) error {
    rows, _ := s.db.QueryContext(ctx, `
        SELECT n.id, n.content
        FROM nodes n
        LEFT JOIN node_embeddings e ON n.id = e.node_id
        WHERE e.node_id IS NULL AND n.content != ''
        LIMIT 1000
    `)
    defer rows.Close()

    for rows.Next() {
        var id, content string
        rows.Scan(&id, &content)
        s.embeddingIndex.IndexAsync(id, content)
    }

    return nil
}
```

### Phase 3: Required for Semantic Features

Once validated, semantic search becomes the default for relevant queries.

## Success Criteria

1. **Recall improvement**: >80% recall on semantic search benchmark
2. **Latency**: Hybrid search <100ms for 10K nodes
3. **Cost**: <$0.0001 per query (Voyage pricing)
4. **Reliability**: Graceful degradation to keyword on embedding failure
5. **Background indexing**: <1% impact on node insert latency

## Appendix: Model Selection

| Model | Dimensions | Speed | Quality | Use Case |
|-------|-----------|-------|---------|----------|
| voyage-3 | 1024 | Fast | High | General text |
| voyage-code-3 | 1024 | Fast | High | Code-heavy |
| text-embedding-3-small | 1536 | Fast | Medium | Budget option |
| nomic-embed-text | 768 | Local | Medium | Privacy-sensitive |

**Recommendation**: Use `voyage-3` for general use, `voyage-code-3` for code-heavy sessions. Support swapping via config.
