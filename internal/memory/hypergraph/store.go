// Package hypergraph provides a SQLite-backed hypergraph memory store.
package hypergraph

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/rand/recurse/internal/memory/embeddings"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

//go:embed schema.sql
var schemaSQL string

// Store manages the hypergraph memory database.
type Store struct {
	db             *sql.DB
	mu             sync.RWMutex
	path           string
	embeddingIndex *embeddings.Index
	hybridSearcher *HybridSearcher
	logger         *slog.Logger
}

// Options configures the hypergraph store.
type Options struct {
	// Path to the SQLite database file.
	// If empty, uses in-memory database.
	Path string

	// CreateIfNotExists creates the database file if it doesn't exist.
	CreateIfNotExists bool

	// EmbeddingProvider enables semantic search with the given provider.
	// If nil, only keyword search is available.
	EmbeddingProvider embeddings.Provider

	// EmbeddingConfig configures the embedding index.
	EmbeddingConfig EmbeddingConfig

	// Logger for store operations.
	Logger *slog.Logger
}

// EmbeddingConfig configures the embedding index.
type EmbeddingConfig struct {
	BatchSize int // Texts to embed per batch (default: 32)
	Workers   int // Background workers (default: 2)
	QueueSize int // Background queue depth (default: 1000)

	// HybridAlpha is the weight for semantic vs keyword search.
	// 0 = keyword only, 1 = semantic only, default = 0.7.
	HybridAlpha float64
}

// NewStore creates a new hypergraph store with the given options.
func NewStore(opts Options) (*Store, error) {
	var dsn string

	if opts.Path == "" {
		dsn = "file::memory:?cache=shared"
	} else {
		// Ensure directory exists
		if opts.CreateIfNotExists {
			dir := filepath.Dir(opts.Path)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("create database directory: %w", err)
			}
		}
		dsn = "file:" + opts.Path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	store := &Store{
		db:     db,
		path:   opts.Path,
		logger: logger,
	}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	// Initialize embedding index if provider is configured
	if opts.EmbeddingProvider != nil {
		idx, err := embeddings.NewIndex(db, embeddings.IndexConfig{
			Provider:  opts.EmbeddingProvider,
			BatchSize: opts.EmbeddingConfig.BatchSize,
			Workers:   opts.EmbeddingConfig.Workers,
			QueueSize: opts.EmbeddingConfig.QueueSize,
			Logger:    logger,
		})
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("init embedding index: %w", err)
		}
		store.embeddingIndex = idx
		store.hybridSearcher = NewHybridSearcher(store, idx, HybridConfig{
			Alpha:  opts.EmbeddingConfig.HybridAlpha,
			Logger: logger,
		})
	}

	return store, nil
}

// initSchema creates the database schema if it doesn't exist.
func (s *Store) initSchema() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(schemaSQL)
	if err != nil {
		return fmt.Errorf("execute schema: %w", err)
	}

	return nil
}

// Close closes the database connection and embedding index.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Close embedding index first
	if s.embeddingIndex != nil {
		if err := s.embeddingIndex.Close(); err != nil {
			s.logger.Error("close embedding index", "error", err)
		}
	}

	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// DB returns the underlying database connection for advanced queries.
// Use with caution - prefer using the Store methods.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Path returns the database file path.
func (s *Store) Path() string {
	return s.path
}

// Stats returns database statistics.
type Stats struct {
	NodeCount      int64            `json:"node_count"`
	HyperedgeCount int64            `json:"hyperedge_count"`
	NodesByTier    map[string]int64 `json:"nodes_by_tier"`
	NodesByType    map[string]int64 `json:"nodes_by_type"`
}

// Stats returns current database statistics.
func (s *Store) Stats(ctx context.Context) (*Stats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := &Stats{
		NodesByTier: make(map[string]int64),
		NodesByType: make(map[string]int64),
	}

	// Total nodes
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM nodes").Scan(&stats.NodeCount)
	if err != nil {
		return nil, fmt.Errorf("count nodes: %w", err)
	}

	// Total hyperedges
	err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM hyperedges").Scan(&stats.HyperedgeCount)
	if err != nil {
		return nil, fmt.Errorf("count hyperedges: %w", err)
	}

	// Nodes by tier
	rows, err := s.db.QueryContext(ctx, "SELECT tier, COUNT(*) FROM nodes GROUP BY tier")
	if err != nil {
		return nil, fmt.Errorf("count by tier: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tier string
		var count int64
		if err := rows.Scan(&tier, &count); err != nil {
			return nil, fmt.Errorf("scan tier count: %w", err)
		}
		stats.NodesByTier[tier] = count
	}

	// Nodes by type
	rows, err = s.db.QueryContext(ctx, "SELECT type, COUNT(*) FROM nodes GROUP BY type")
	if err != nil {
		return nil, fmt.Errorf("count by type: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var nodeType string
		var count int64
		if err := rows.Scan(&nodeType, &count); err != nil {
			return nil, fmt.Errorf("scan type count: %w", err)
		}
		stats.NodesByType[nodeType] = count
	}

	return stats, nil
}

// BeginTx starts a new transaction.
func (s *Store) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, nil)
}

// WithTx executes a function within a transaction.
// If the function returns an error, the transaction is rolled back.
// Otherwise, the transaction is committed.
func (s *Store) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("rollback failed: %v (original error: %w)", rbErr, err)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// Search performs hybrid keyword + semantic search if embeddings are enabled,
// otherwise falls back to keyword-only search.
func (s *Store) Search(ctx context.Context, query string, opts SearchOptions) ([]*SearchResult, error) {
	if s.hybridSearcher != nil {
		return s.hybridSearcher.Search(ctx, query, opts)
	}
	return s.SearchByContent(ctx, query, opts)
}

// HasEmbeddings returns true if embedding search is enabled.
func (s *Store) HasEmbeddings() bool {
	return s.embeddingIndex != nil
}

// EmbeddingIndex returns the embedding index, or nil if not configured.
func (s *Store) EmbeddingIndex() *embeddings.Index {
	return s.embeddingIndex
}

// QueueEmbedding queues a node for background embedding.
// Does nothing if embeddings are not configured.
func (s *Store) QueueEmbedding(nodeID, content string) {
	if s.embeddingIndex != nil && content != "" {
		s.embeddingIndex.IndexAsync(nodeID, content)
	}
}

// EmbedSync embeds a node synchronously, waiting for completion.
// Returns nil if embeddings are not configured.
func (s *Store) EmbedSync(ctx context.Context, nodeID, content string) error {
	if s.embeddingIndex == nil {
		return nil
	}
	return s.embeddingIndex.IndexSync(ctx, nodeID, content)
}
