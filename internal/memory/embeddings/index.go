package embeddings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// Index manages embedding storage and search.
type Index struct {
	db         *sql.DB
	provider   Provider
	batchSize  int
	background chan indexRequest
	done       chan struct{}
	wg         sync.WaitGroup
	mu         sync.RWMutex
	logger     *slog.Logger
	metrics    *EmbeddingMetrics
}

// IndexConfig configures the embedding index.
type IndexConfig struct {
	Provider  Provider // Required: embedding provider
	BatchSize int      // Texts to embed per batch (default: 32)
	Workers   int      // Background workers (default: 2)
	QueueSize int      // Background queue depth (default: 1000)
	Logger    *slog.Logger
	Metrics   *EmbeddingMetrics // Optional: metrics collector
}

type indexRequest struct {
	nodeID  string
	content string
	done    chan error // Optional: for sync indexing
}

// NewIndex creates a new embedding index.
func NewIndex(db *sql.DB, cfg IndexConfig) (*Index, error) {
	if cfg.Provider == nil {
		return nil, errors.New("embedding provider required")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 32
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 2
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1000
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	idx := &Index{
		db:         db,
		provider:   cfg.Provider,
		batchSize:  cfg.BatchSize,
		background: make(chan indexRequest, cfg.QueueSize),
		done:       make(chan struct{}),
		logger:     cfg.Logger,
		metrics:    cfg.Metrics,
	}

	// Initialize schema
	if err := idx.initSchema(); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}

	// Start background workers
	for i := 0; i < cfg.Workers; i++ {
		idx.wg.Add(1)
		go idx.worker()
	}

	return idx, nil
}

func (idx *Index) initSchema() error {
	// Store embeddings as BLOBs in a regular table
	// This works without sqlite-vec extension
	_, err := idx.db.Exec(`
		CREATE TABLE IF NOT EXISTS node_embeddings (
			node_id TEXT PRIMARY KEY,
			embedding BLOB NOT NULL,
			model TEXT NOT NULL,
			dimensions INTEGER NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	// Index for faster lookups
	_, err = idx.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_node_embeddings_model
		ON node_embeddings(model)
	`)
	if err != nil {
		return fmt.Errorf("create index: %w", err)
	}

	return nil
}

// IndexAsync queues a node for background embedding.
func (idx *Index) IndexAsync(nodeID, content string) {
	select {
	case idx.background <- indexRequest{nodeID: nodeID, content: content}:
	default:
		idx.logger.Warn("embedding queue full, dropping request",
			"node_id", nodeID)
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

// HasEmbedding checks if a node has an embedding.
func (idx *Index) HasEmbedding(nodeID string) bool {
	var count int
	err := idx.db.QueryRow(`
		SELECT COUNT(*) FROM node_embeddings WHERE node_id = ?
	`, nodeID).Scan(&count)
	return err == nil && count > 0
}

// GetEmbedding retrieves the embedding for a node.
func (idx *Index) GetEmbedding(ctx context.Context, nodeID string) (Vector, error) {
	var blob []byte
	err := idx.db.QueryRowContext(ctx, `
		SELECT embedding FROM node_embeddings WHERE node_id = ?
	`, nodeID).Scan(&blob)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get embedding: %w", err)
	}
	return VectorFromBytes(blob), nil
}

// SearchResult represents a search match.
type SearchResult struct {
	NodeID     string
	Similarity float32
}

// Search finds nodes similar to the query.
func (idx *Index) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	start := time.Now()
	defer func() {
		if idx.metrics != nil {
			idx.metrics.RecordSearch(time.Since(start))
		}
	}()

	if limit <= 0 {
		limit = 20
	}

	// Embed the query
	vectors, err := idx.provider.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vectors) == 0 {
		return nil, nil
	}
	queryVec := vectors[0]

	return idx.SearchByVector(ctx, queryVec, limit)
}

// SearchByVector finds nodes similar to the given vector.
func (idx *Index) SearchByVector(ctx context.Context, queryVec Vector, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}

	// Load all embeddings and compute similarity
	// This is O(n) but works without sqlite-vec
	// For production scale, use sqlite-vec or pgvector
	rows, err := idx.db.QueryContext(ctx, `
		SELECT node_id, embedding FROM node_embeddings
	`)
	if err != nil {
		return nil, fmt.Errorf("query embeddings: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var nodeID string
		var blob []byte
		if err := rows.Scan(&nodeID, &blob); err != nil {
			continue
		}

		vec := VectorFromBytes(blob)
		similarity := queryVec.Similarity(vec)

		results = append(results, SearchResult{
			NodeID:     nodeID,
			Similarity: similarity,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan rows: %w", err)
	}

	// Sort by similarity descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	// Take top limit
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// Delete removes an embedding.
func (idx *Index) Delete(ctx context.Context, nodeID string) error {
	_, err := idx.db.ExecContext(ctx, `
		DELETE FROM node_embeddings WHERE node_id = ?
	`, nodeID)
	return err
}

// Count returns the number of indexed embeddings.
func (idx *Index) Count(ctx context.Context) (int, error) {
	var count int
	err := idx.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM node_embeddings
	`).Scan(&count)
	return count, err
}

// QueueDepth returns the number of pending embedding requests.
func (idx *Index) QueueDepth() int {
	return len(idx.background)
}

// Metrics returns the metrics collector.
func (idx *Index) Metrics() *EmbeddingMetrics {
	return idx.metrics
}

// Close stops background workers and closes the index.
func (idx *Index) Close() error {
	close(idx.done)
	idx.wg.Wait()
	return nil
}

func (idx *Index) worker() {
	defer idx.wg.Done()

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

		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		vectors, err := idx.provider.Embed(ctx, texts)
		cancel()
		duration := time.Since(start)

		// Record metrics
		if idx.metrics != nil {
			if err != nil {
				idx.metrics.RecordEmbedError()
			} else {
				idx.metrics.RecordEmbed(duration, len(texts))
			}
			idx.metrics.SetQueueDepth(len(idx.background))
		}

		for i, req := range batch {
			if err != nil {
				idx.logger.Error("batch embed failed",
					"error", err,
					"node_id", req.nodeID)
				if req.done != nil {
					req.done <- err
				}
				continue
			}

			if storeErr := idx.storeVector(req.nodeID, vectors[i]); storeErr != nil {
				idx.logger.Error("store vector failed",
					"error", storeErr,
					"node_id", req.nodeID)
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
		case <-idx.done:
			flush()
			return

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

func (idx *Index) storeVector(nodeID string, vec Vector) error {
	_, err := idx.db.Exec(`
		INSERT OR REPLACE INTO node_embeddings (node_id, embedding, model, dimensions, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, nodeID, vec.ToBytes(), idx.provider.Model(), len(vec))
	return err
}
