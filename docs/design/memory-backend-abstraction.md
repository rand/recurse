# Memory Backend Abstraction Design

> Design document for `recurse-65d`: [SPEC] Memory Backend Abstraction Design

## Overview

This document specifies the memory backend abstraction layer, enabling pluggable storage backends for the hypergraph memory system. This abstraction allows swapping between SQLite (default), PostgreSQL (scale), or custom backends without changing application code.

## Problem Statement

### Current State

Tight coupling to SQLite:

```go
type Store struct {
    db *sql.DB // SQLite-specific
}

func (s *Store) CreateNode(ctx context.Context, node *Node) error {
    _, err := s.db.Exec(`INSERT INTO nodes ...`) // SQLite SQL
}
```

**Issues**:
- Cannot use PostgreSQL for multi-instance deployments
- Cannot use specialized vector databases for embeddings
- Testing requires SQLite setup
- No in-memory option for unit tests

## Design Goals

1. **Backend agnostic**: Core code works with any backend
2. **Feature parity**: All backends support core operations
3. **Capability discovery**: Query backend-specific features
4. **Transaction support**: ACID guarantees where available
5. **Migration path**: Easy transition between backends

## Core Types

### Backend Interface

```go
// internal/memory/backend/backend.go

// Backend defines the storage interface for hypergraph memory.
type Backend interface {
    // Node operations
    NodeStore

    // Edge operations
    HyperedgeStore

    // Query operations
    QueryEngine

    // Transaction support
    Transactor

    // Lifecycle
    Close() error
    Ping(ctx context.Context) error
}

type NodeStore interface {
    CreateNode(ctx context.Context, node *Node) error
    GetNode(ctx context.Context, id string) (*Node, error)
    UpdateNode(ctx context.Context, node *Node) error
    DeleteNode(ctx context.Context, id string) error
    ListNodes(ctx context.Context, filter NodeFilter) ([]*Node, error)
}

type HyperedgeStore interface {
    CreateHyperedge(ctx context.Context, edge *Hyperedge) error
    GetHyperedge(ctx context.Context, id string) (*Hyperedge, error)
    UpdateHyperedge(ctx context.Context, edge *Hyperedge) error
    DeleteHyperedge(ctx context.Context, id string) error
    AddMembership(ctx context.Context, m Membership) error
    RemoveMembership(ctx context.Context, edgeID, nodeID string) error
}

type QueryEngine interface {
    SearchByContent(ctx context.Context, query string, opts SearchOptions) ([]*SearchResult, error)
    SearchByEmbedding(ctx context.Context, vec []float32, opts SearchOptions) ([]*SearchResult, error)
    GetConnected(ctx context.Context, nodeID string, opts TraversalOptions) ([]*ConnectedNode, error)
    GetSubgraph(ctx context.Context, nodeIDs []string, depth int) (*Subgraph, error)
}

type Transactor interface {
    BeginTx(ctx context.Context) (Transaction, error)
    WithTx(ctx context.Context, fn func(tx Transaction) error) error
}

type Transaction interface {
    NodeStore
    HyperedgeStore
    Commit() error
    Rollback() error
}
```

### Capability System

```go
// internal/memory/backend/capabilities.go

// Capabilities describes what a backend supports.
type Capabilities struct {
    VectorSearch       bool
    FullTextSearch     bool
    Transactions       bool
    ConcurrentReads    bool
    ConcurrentWrites   bool
    Streaming          bool
    ChangeNotifications bool
    MaxConnections     int
    MaxVectorDimensions int
}

type CapabilityProvider interface {
    Capabilities() Capabilities
}

// FeatureGate checks if a feature is available.
func FeatureGate(backend Backend, feature string) bool {
    caps := backend.(CapabilityProvider).Capabilities()
    switch feature {
    case "vector_search":
        return caps.VectorSearch
    case "fts":
        return caps.FullTextSearch
    case "transactions":
        return caps.Transactions
    default:
        return false
    }
}
```

### Filter Types

```go
// internal/memory/backend/filter.go

type NodeFilter struct {
    IDs           []string
    Types         []NodeType
    Tiers         []Tier
    Subtypes      []string
    MinConfidence float64
    CreatedAfter  *time.Time
    CreatedBefore *time.Time
    Limit         int
    Offset        int
    OrderBy       string
    OrderDesc     bool
}

type SearchOptions struct {
    Types         []NodeType
    Tiers         []Tier
    MinConfidence float64
    Limit         int
    IncludeArchived bool
}

type TraversalOptions struct {
    Direction   TraversalDirection
    MaxDepth    int
    EdgeTypes   []HyperedgeType
    NodeTypes   []NodeType
    Tiers       []Tier
    MaxResults  int
}
```

## SQLite Backend

### Implementation

```go
// internal/memory/backend/sqlite/backend.go

type SQLiteBackend struct {
    db   *sql.DB
    path string
    mu   sync.RWMutex
}

type Config struct {
    Path              string
    CreateIfNotExists bool
    WALMode           bool
    BusyTimeout       time.Duration
}

func New(cfg Config) (*SQLiteBackend, error) {
    dsn := buildDSN(cfg)
    db, err := sql.Open("sqlite3", dsn)
    if err != nil {
        return nil, err
    }

    backend := &SQLiteBackend{db: db, path: cfg.Path}
    if err := backend.initSchema(); err != nil {
        return nil, err
    }

    return backend, nil
}

func (b *SQLiteBackend) Capabilities() Capabilities {
    return Capabilities{
        VectorSearch:       true, // With sqlite-vec extension
        FullTextSearch:     true, // FTS5
        Transactions:       true,
        ConcurrentReads:    true,
        ConcurrentWrites:   false, // SQLite single-writer
        MaxVectorDimensions: 2048,
    }
}

func (b *SQLiteBackend) CreateNode(ctx context.Context, node *Node) error {
    b.mu.Lock()
    defer b.mu.Unlock()

    _, err := b.db.ExecContext(ctx, `
        INSERT INTO nodes (id, type, subtype, content, embedding, tier, confidence, provenance, metadata, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, node.ID, node.Type, node.Subtype, node.Content, node.Embedding,
       node.Tier, node.Confidence, node.Provenance, node.Metadata,
       node.CreatedAt, node.UpdatedAt)

    return err
}

func (b *SQLiteBackend) SearchByEmbedding(
    ctx context.Context,
    vec []float32,
    opts SearchOptions,
) ([]*SearchResult, error) {
    b.mu.RLock()
    defer b.mu.RUnlock()

    // Use sqlite-vec for vector search
    rows, err := b.db.QueryContext(ctx, `
        SELECT n.*, e.distance
        FROM nodes n
        JOIN node_embeddings e ON n.id = e.node_id
        WHERE e.embedding MATCH ?
        ORDER BY e.distance
        LIMIT ?
    `, vectorToBlob(vec), opts.Limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    return scanSearchResults(rows)
}
```

## PostgreSQL Backend

### Implementation

```go
// internal/memory/backend/postgres/backend.go

type PostgresBackend struct {
    pool *pgxpool.Pool
}

type Config struct {
    ConnString    string
    MaxConns      int
    MinConns      int
    MaxConnLife   time.Duration
}

func New(ctx context.Context, cfg Config) (*PostgresBackend, error) {
    poolConfig, err := pgxpool.ParseConfig(cfg.ConnString)
    if err != nil {
        return nil, err
    }

    poolConfig.MaxConns = int32(cfg.MaxConns)
    poolConfig.MinConns = int32(cfg.MinConns)
    poolConfig.MaxConnLifetime = cfg.MaxConnLife

    pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
    if err != nil {
        return nil, err
    }

    backend := &PostgresBackend{pool: pool}
    if err := backend.initSchema(ctx); err != nil {
        return nil, err
    }

    return backend, nil
}

func (b *PostgresBackend) Capabilities() Capabilities {
    return Capabilities{
        VectorSearch:       true, // pgvector
        FullTextSearch:     true, // tsvector
        Transactions:       true,
        ConcurrentReads:    true,
        ConcurrentWrites:   true, // MVCC
        MaxConnections:     100,
        MaxVectorDimensions: 16000, // pgvector limit
    }
}

func (b *PostgresBackend) CreateNode(ctx context.Context, node *Node) error {
    _, err := b.pool.Exec(ctx, `
        INSERT INTO nodes (id, type, subtype, content, embedding, tier, confidence, provenance, metadata, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
    `, node.ID, node.Type, node.Subtype, node.Content, pgvector.NewVector(node.Embedding),
       node.Tier, node.Confidence, node.Provenance, node.Metadata,
       node.CreatedAt, node.UpdatedAt)

    return err
}

func (b *PostgresBackend) SearchByEmbedding(
    ctx context.Context,
    vec []float32,
    opts SearchOptions,
) ([]*SearchResult, error) {
    // Use pgvector for similarity search
    rows, err := b.pool.Query(ctx, `
        SELECT n.*, 1 - (n.embedding <=> $1) as similarity
        FROM nodes n
        WHERE n.tier != 'archive'
        ORDER BY n.embedding <=> $1
        LIMIT $2
    `, pgvector.NewVector(vec), opts.Limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    return scanSearchResultsPgx(rows)
}
```

## In-Memory Backend

### Implementation

```go
// internal/memory/backend/memory/backend.go

// MemoryBackend provides an in-memory implementation for testing.
type MemoryBackend struct {
    nodes      map[string]*Node
    edges      map[string]*Hyperedge
    membership map[string][]Membership // edgeID -> members
    mu         sync.RWMutex
}

func New() *MemoryBackend {
    return &MemoryBackend{
        nodes:      make(map[string]*Node),
        edges:      make(map[string]*Hyperedge),
        membership: make(map[string][]Membership),
    }
}

func (b *MemoryBackend) Capabilities() Capabilities {
    return Capabilities{
        VectorSearch:     true,
        FullTextSearch:   true,
        Transactions:     false, // No ACID in memory
        ConcurrentReads:  true,
        ConcurrentWrites: true,
    }
}

func (b *MemoryBackend) CreateNode(ctx context.Context, node *Node) error {
    b.mu.Lock()
    defer b.mu.Unlock()

    if _, exists := b.nodes[node.ID]; exists {
        return ErrNodeExists
    }

    b.nodes[node.ID] = node.Clone()
    return nil
}

func (b *MemoryBackend) SearchByEmbedding(
    ctx context.Context,
    vec []float32,
    opts SearchOptions,
) ([]*SearchResult, error) {
    b.mu.RLock()
    defer b.mu.RUnlock()

    var results []*SearchResult
    for _, node := range b.nodes {
        if node.Embedding == nil {
            continue
        }

        similarity := cosineSimilarity(vec, node.Embedding)
        results = append(results, &SearchResult{
            Node:  node.Clone(),
            Score: float64(similarity),
        })
    }

    // Sort by score descending
    sort.Slice(results, func(i, j int) bool {
        return results[i].Score > results[j].Score
    })

    if opts.Limit > 0 && len(results) > opts.Limit {
        results = results[:opts.Limit]
    }

    return results, nil
}
```

## Backend Registry

### Registry

```go
// internal/memory/backend/registry.go

type Factory func(ctx context.Context, config map[string]any) (Backend, error)

var (
    registry = make(map[string]Factory)
    mu       sync.RWMutex
)

func Register(name string, factory Factory) {
    mu.Lock()
    defer mu.Unlock()
    registry[name] = factory
}

func New(ctx context.Context, name string, config map[string]any) (Backend, error) {
    mu.RLock()
    factory, ok := registry[name]
    mu.RUnlock()

    if !ok {
        return nil, fmt.Errorf("unknown backend: %s", name)
    }

    return factory(ctx, config)
}

func init() {
    Register("sqlite", func(ctx context.Context, cfg map[string]any) (Backend, error) {
        return sqlite.New(sqlite.ConfigFromMap(cfg))
    })

    Register("postgres", func(ctx context.Context, cfg map[string]any) (Backend, error) {
        return postgres.New(ctx, postgres.ConfigFromMap(cfg))
    })

    Register("memory", func(ctx context.Context, cfg map[string]any) (Backend, error) {
        return memory.New(), nil
    })
}
```

## Store Adapter

### Adapter Pattern

```go
// internal/memory/hypergraph/store.go

// Store wraps a backend with high-level operations.
type Store struct {
    backend Backend
}

func NewStore(backend Backend) *Store {
    return &Store{backend: backend}
}

// NewStoreFromConfig creates a store from configuration.
func NewStoreFromConfig(ctx context.Context, cfg StoreConfig) (*Store, error) {
    backend, err := backendpkg.New(ctx, cfg.Backend, cfg.BackendConfig)
    if err != nil {
        return nil, err
    }
    return NewStore(backend), nil
}

// Search uses the best available search method.
func (s *Store) Search(ctx context.Context, query string, opts SearchOptions) ([]*SearchResult, error) {
    caps := s.backend.(CapabilityProvider).Capabilities()

    // Prefer vector search if available and embeddings exist
    if caps.VectorSearch && s.hasEmbeddingIndex() {
        return s.hybridSearch(ctx, query, opts)
    }

    // Fall back to content search
    return s.backend.SearchByContent(ctx, query, opts)
}
```

## Migration Support

### Schema Migrator

```go
// internal/memory/backend/migrate/migrator.go

type Migrator struct {
    backend Backend
    migrations []Migration
}

type Migration struct {
    Version     int
    Description string
    Up          func(ctx context.Context, tx Transaction) error
    Down        func(ctx context.Context, tx Transaction) error
}

func (m *Migrator) Migrate(ctx context.Context) error {
    currentVersion := m.getCurrentVersion(ctx)

    for _, migration := range m.migrations {
        if migration.Version <= currentVersion {
            continue
        }

        if err := m.backend.WithTx(ctx, func(tx Transaction) error {
            if err := migration.Up(ctx, tx); err != nil {
                return err
            }
            return m.setVersion(ctx, tx, migration.Version)
        }); err != nil {
            return fmt.Errorf("migration %d failed: %w", migration.Version, err)
        }
    }

    return nil
}
```

## Testing

### Test Helpers

```go
// internal/memory/backend/testing/helpers.go

// NewTestBackend creates an in-memory backend for testing.
func NewTestBackend(t *testing.T) Backend {
    t.Helper()
    return memory.New()
}

// BackendTestSuite runs standard tests against any backend.
func BackendTestSuite(t *testing.T, backend Backend) {
    t.Run("CreateNode", func(t *testing.T) {
        node := &Node{ID: "test-1", Type: NodeTypeKnowledge, Content: "test"}
        err := backend.CreateNode(context.Background(), node)
        require.NoError(t, err)

        retrieved, err := backend.GetNode(context.Background(), "test-1")
        require.NoError(t, err)
        assert.Equal(t, node.Content, retrieved.Content)
    })

    t.Run("SearchByContent", func(t *testing.T) {
        // Create test nodes
        for i := 0; i < 10; i++ {
            node := &Node{
                ID:      fmt.Sprintf("search-%d", i),
                Type:    NodeTypeKnowledge,
                Content: fmt.Sprintf("test content %d", i),
            }
            backend.CreateNode(context.Background(), node)
        }

        results, err := backend.SearchByContent(context.Background(), "content 5", SearchOptions{Limit: 5})
        require.NoError(t, err)
        assert.NotEmpty(t, results)
    })
}
```

## Success Criteria

1. **Compatibility**: All backends pass the same test suite
2. **Performance**: PostgreSQL handles 10x concurrent load vs SQLite
3. **Simplicity**: Backend switch requires only config change
4. **Testing**: In-memory backend enables fast unit tests
5. **Migration**: Smooth data migration between backends
