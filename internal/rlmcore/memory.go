package rlmcore

import (
	"context"
	"fmt"
)

// MemoryStore wraps the rlm-core SqliteMemoryStore.
// This provides a hypergraph-based memory system with:
// - Multi-relation edges (hyperedges)
// - Temporal decay
// - Semantic similarity search
// - Cross-session persistence
type MemoryStore struct {
	// handle is the opaque pointer to the Rust MemoryStore
	// handle unsafe.Pointer
}

// NewMemoryStore creates a new rlm-core backed memory store.
func NewMemoryStore(dbPath string) (*MemoryStore, error) {
	if !Available() {
		return nil, fmt.Errorf("rlm-core not available: set RLM_USE_CORE=true and ensure library is built")
	}
	// TODO: Call C.rlm_memory_store_new(dbPath)
	return nil, fmt.Errorf("not implemented: awaiting FFI")
}

// Store adds a memory entry to the hypergraph.
func (m *MemoryStore) Store(ctx context.Context, content string, metadata map[string]any) error {
	// TODO: Call C.rlm_memory_store_add()
	return fmt.Errorf("not implemented")
}

// Query retrieves memories by semantic similarity.
func (m *MemoryStore) Query(ctx context.Context, query string, limit int) ([]Memory, error) {
	// TODO: Call C.rlm_memory_store_query()
	return nil, fmt.Errorf("not implemented")
}

// Close releases the memory store resources.
func (m *MemoryStore) Close() error {
	// TODO: Call C.rlm_memory_store_free()
	return nil
}

// Memory represents a retrieved memory entry.
type Memory struct {
	ID         string
	Content    string
	Metadata   map[string]any
	Similarity float64
	CreatedAt  int64
}
