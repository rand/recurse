// Package hypergraph provides a SQLite-backed hypergraph memory store.
package hypergraph

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

//go:embed schema.sql
var schemaSQL string

// Store manages the hypergraph memory database.
type Store struct {
	db   *sql.DB
	mu   sync.RWMutex
	path string
}

// Options configures the hypergraph store.
type Options struct {
	// Path to the SQLite database file.
	// If empty, uses in-memory database.
	Path string

	// CreateIfNotExists creates the database file if it doesn't exist.
	CreateIfNotExists bool
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

	store := &Store{
		db:   db,
		path: opts.Path,
	}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
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

// Close closes the database connection.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

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
