// Package compress provides context compression for the RLM system.
package compress

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rand/recurse/internal/memory/embeddings"
)

// Manager orchestrates context compression for RLM calls.
// It manages multiple compressors, allocates budget across chunks,
// and provides unified metrics and caching.
type Manager struct {
	base         *ContextCompressor
	hierarchical *HierarchicalCompressor
	incremental  *IncrementalCompressor
	embedder     embeddings.Provider

	defaultOpts Options

	mu    sync.Mutex
	stats ManagerStats
}

// ManagerConfig configures the compression manager.
type ManagerConfig struct {
	// Base compressor configuration.
	Base Config

	// Hierarchical compressor levels.
	HierarchicalLevels []float64

	// Incremental compressor settings.
	CacheSize       int
	CacheTTL        time.Duration
	ChangeThreshold float64

	// Embedder for query relevance scoring (optional).
	Embedder embeddings.Provider

	// Default compression options.
	DefaultOptions Options
}

// DefaultManagerConfig returns sensible defaults.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		Base:               DefaultConfig(),
		HierarchicalLevels: []float64{0.5, 0.25, 0.125},
		CacheSize:          1000,
		CacheTTL:           time.Hour,
		ChangeThreshold:    0.1,
		DefaultOptions:     DefaultOptions(),
	}
}

// NewManager creates a new compression manager.
func NewManager(cfg ManagerConfig) *Manager {
	baseCompressor := NewContextCompressor(cfg.Base)

	return &Manager{
		base: baseCompressor,
		hierarchical: NewHierarchicalCompressor(HierarchicalConfig{
			Base:   cfg.Base,
			Levels: cfg.HierarchicalLevels,
		}),
		incremental: NewIncrementalCompressor(IncrementalConfig{
			Base:            cfg.Base,
			MaxCacheSize:    cfg.CacheSize,
			CacheTTL:        cfg.CacheTTL,
			ChangeThreshold: cfg.ChangeThreshold,
		}),
		embedder:    cfg.Embedder,
		defaultOpts: cfg.DefaultOptions,
		stats: ManagerStats{
			ByMethod:   make(map[Method]int64),
			ByChunkType: make(map[string]int64),
		},
	}
}

// ManagerStats tracks compression manager statistics.
type ManagerStats struct {
	TotalCompressions   int64
	TotalChunksProcessed int64
	TotalTokensSaved    int64
	TotalTokensOriginal int64
	CacheHits           int64
	CacheMisses         int64
	AvgRatio            float64
	ByMethod            map[Method]int64
	ByChunkType         map[string]int64
}

// ContextChunk represents a piece of context to be compressed.
type ContextChunk struct {
	// ID is a unique identifier for caching purposes.
	ID string

	// Content is the text content to compress.
	Content string

	// Type categorizes the chunk (e.g., "file", "search", "memory", "tool").
	Type string

	// Relevance is a score from 0-1 indicating importance relative to query.
	// Higher relevance chunks get more token budget.
	Relevance float64
}

// PreparedContext is the result of context preparation.
type PreparedContext struct {
	// Content is the prepared (potentially compressed) context string.
	Content string

	// OriginalTokens is the total token count before compression.
	OriginalTokens int

	// CompressedTokens is the token count after compression.
	CompressedTokens int

	// Ratio is the overall compression ratio.
	Ratio float64

	// ChunkResults contains per-chunk compression results.
	ChunkResults []*ChunkResult

	// Duration is how long preparation took.
	Duration time.Duration
}

// ChunkResult contains the compression result for a single chunk.
type ChunkResult struct {
	ID               string
	Type             string
	OriginalTokens   int
	CompressedTokens int
	Ratio            float64
	Method           Method
	Cached           bool
	Allocated        int // Token budget allocated to this chunk
}

// PrepareContext compresses multiple chunks to fit within a token budget.
// It allocates budget based on relevance scores and concatenates results.
func (m *Manager) PrepareContext(ctx context.Context, chunks []ContextChunk, query string, tokenBudget int) (*PreparedContext, error) {
	start := time.Now()

	if len(chunks) == 0 {
		return &PreparedContext{
			Content:  "",
			Duration: time.Since(start),
		}, nil
	}

	// Calculate original token counts
	originalTokens := 0
	chunkTokens := make([]int, len(chunks))
	for i, chunk := range chunks {
		tokens := estimateTokens(chunk.Content)
		chunkTokens[i] = tokens
		originalTokens += tokens
	}

	// If already under budget, no compression needed
	if originalTokens <= tokenBudget {
		var content strings.Builder
		results := make([]*ChunkResult, len(chunks))
		for i, chunk := range chunks {
			if i > 0 {
				content.WriteString("\n\n")
			}
			content.WriteString(chunk.Content)
			results[i] = &ChunkResult{
				ID:               chunk.ID,
				Type:             chunk.Type,
				OriginalTokens:   chunkTokens[i],
				CompressedTokens: chunkTokens[i],
				Ratio:            1.0,
				Method:           MethodPassthrough,
				Allocated:        chunkTokens[i],
			}
		}
		return &PreparedContext{
			Content:          content.String(),
			OriginalTokens:   originalTokens,
			CompressedTokens: originalTokens,
			Ratio:            1.0,
			ChunkResults:     results,
			Duration:         time.Since(start),
		}, nil
	}

	// Score chunks by relevance (use embeddings if available)
	relevanceScores := m.scoreChunkRelevance(ctx, chunks, query)

	// Allocate budget proportionally to relevance
	allocations := m.allocateBudget(chunks, relevanceScores, chunkTokens, tokenBudget)

	// Compress each chunk to its allocation
	results := make([]*ChunkResult, len(chunks))
	compressedChunks := make([]string, len(chunks))

	for i, chunk := range chunks {
		allocation := allocations[i]

		opts := m.defaultOpts
		opts.TargetTokens = allocation
		opts.QueryContext = query

		// Use incremental compressor for caching benefits
		result, err := m.incremental.Compress(ctx, chunk.ID, chunk.Content, opts)
		if err != nil {
			return nil, fmt.Errorf("compressing chunk %s: %w", chunk.ID, err)
		}

		compressedChunks[i] = result.Compressed
		results[i] = &ChunkResult{
			ID:               chunk.ID,
			Type:             chunk.Type,
			OriginalTokens:   chunkTokens[i],
			CompressedTokens: result.CompressedTokens,
			Ratio:            result.Ratio,
			Method:           result.Method,
			Cached:           result.Method == MethodPassthrough && result.CompressedTokens == chunkTokens[i],
			Allocated:        allocation,
		}
	}

	// Concatenate compressed chunks
	var content strings.Builder
	compressedTokens := 0
	for i, compressed := range compressedChunks {
		if i > 0 {
			content.WriteString("\n\n")
			compressedTokens += 2 // Approximate tokens for separator
		}
		content.WriteString(compressed)
		compressedTokens += results[i].CompressedTokens
	}

	prepared := &PreparedContext{
		Content:          content.String(),
		OriginalTokens:   originalTokens,
		CompressedTokens: compressedTokens,
		Ratio:            float64(compressedTokens) / float64(originalTokens),
		ChunkResults:     results,
		Duration:         time.Since(start),
	}

	// Update stats
	m.updateStats(prepared)

	return prepared, nil
}

// CompressChunk compresses a single chunk with the given options.
func (m *Manager) CompressChunk(ctx context.Context, chunk ContextChunk, opts Options) (*Result, error) {
	return m.incremental.Compress(ctx, chunk.ID, chunk.Content, opts)
}

// CompressHierarchical creates multi-level compression for large content.
func (m *Manager) CompressHierarchical(ctx context.Context, content string, opts Options) (*HierarchicalResult, error) {
	return m.hierarchical.Compress(ctx, content, opts)
}

// scoreChunkRelevance scores chunks based on their relevance to the query.
func (m *Manager) scoreChunkRelevance(ctx context.Context, chunks []ContextChunk, query string) []float64 {
	scores := make([]float64, len(chunks))

	// Start with provided relevance scores
	for i, chunk := range chunks {
		scores[i] = chunk.Relevance
		if scores[i] <= 0 {
			scores[i] = 0.5 // Default relevance
		}
	}

	// Enhance with embedding similarity if available
	if m.embedder != nil && query != "" {
		queryVecs, err := m.embedder.Embed(ctx, []string{query})
		if err == nil && len(queryVecs) > 0 {
			queryVec := queryVecs[0]

			// Get first 100 chars of each chunk for embedding
			snippets := make([]string, len(chunks))
			for i, chunk := range chunks {
				if len(chunk.Content) > 200 {
					snippets[i] = chunk.Content[:200]
				} else {
					snippets[i] = chunk.Content
				}
			}

			chunkVecs, err := m.embedder.Embed(ctx, snippets)
			if err == nil && len(chunkVecs) == len(chunks) {
				for i, chunkVec := range chunkVecs {
					similarity := float64(queryVec.Similarity(chunkVec))
					// Blend provided relevance with computed similarity
					scores[i] = (scores[i] + similarity) / 2
				}
			}
		}
	}

	// Normalize scores to sum to 1
	total := 0.0
	for _, s := range scores {
		total += s
	}
	if total > 0 {
		for i := range scores {
			scores[i] /= total
		}
	}

	return scores
}

// allocateBudget distributes token budget across chunks based on relevance.
func (m *Manager) allocateBudget(chunks []ContextChunk, relevance []float64, originalTokens []int, totalBudget int) []int {
	n := len(chunks)
	allocations := make([]int, n)

	// Calculate relevance-weighted ideal allocations
	for i := range chunks {
		ideal := int(float64(totalBudget) * relevance[i])
		// Don't allocate more than original size
		if ideal > originalTokens[i] {
			ideal = originalTokens[i]
		}
		allocations[i] = ideal
	}

	// Ensure minimum allocation for each chunk
	minAllocation := 50 // At least 50 tokens per chunk
	for i := range allocations {
		if allocations[i] < minAllocation && originalTokens[i] >= minAllocation {
			allocations[i] = minAllocation
		}
	}

	// Redistribute any excess budget
	allocated := 0
	for _, a := range allocations {
		allocated += a
	}

	if allocated < totalBudget {
		// Extra budget available - give to highest relevance chunks
		extra := totalBudget - allocated

		// Sort by relevance (descending)
		type indexedRelevance struct {
			index     int
			relevance float64
			headroom  int // Room to grow
		}
		ranked := make([]indexedRelevance, n)
		for i := range chunks {
			ranked[i] = indexedRelevance{
				index:     i,
				relevance: relevance[i],
				headroom:  originalTokens[i] - allocations[i],
			}
		}
		sort.Slice(ranked, func(i, j int) bool {
			return ranked[i].relevance > ranked[j].relevance
		})

		// Distribute extra to chunks with headroom
		for _, r := range ranked {
			if extra <= 0 {
				break
			}
			if r.headroom > 0 {
				give := r.headroom
				if give > extra {
					give = extra
				}
				allocations[r.index] += give
				extra -= give
			}
		}
	}

	return allocations
}

func (m *Manager) updateStats(prepared *PreparedContext) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stats.TotalCompressions++
	m.stats.TotalChunksProcessed += int64(len(prepared.ChunkResults))
	m.stats.TotalTokensOriginal += int64(prepared.OriginalTokens)
	m.stats.TotalTokensSaved += int64(prepared.OriginalTokens - prepared.CompressedTokens)

	// Update running average ratio
	n := float64(m.stats.TotalCompressions)
	m.stats.AvgRatio = m.stats.AvgRatio*(n-1)/n + prepared.Ratio/n

	// Track by method and type
	for _, result := range prepared.ChunkResults {
		m.stats.ByMethod[result.Method]++
		if result.Type != "" {
			m.stats.ByChunkType[result.Type]++
		}
		if result.Cached {
			m.stats.CacheHits++
		} else {
			m.stats.CacheMisses++
		}
	}
}

// Stats returns current compression manager statistics.
func (m *Manager) Stats() ManagerStats {
	m.mu.Lock()
	defer m.mu.Unlock()

	s := m.stats
	s.ByMethod = make(map[Method]int64)
	for k, v := range m.stats.ByMethod {
		s.ByMethod[k] = v
	}
	s.ByChunkType = make(map[string]int64)
	for k, v := range m.stats.ByChunkType {
		s.ByChunkType[k] = v
	}
	return s
}

// CacheStats returns cache statistics from the incremental compressor.
func (m *Manager) CacheStats() CacheStats {
	return m.incremental.Stats()
}

// InvalidateCache clears the compression cache.
func (m *Manager) InvalidateCache() {
	m.incremental.InvalidateAll()
}

// InvalidateCacheEntry removes a specific entry from the cache.
func (m *Manager) InvalidateCacheEntry(id string) {
	m.incremental.Invalidate(id)
}

// Base returns the underlying base compressor.
func (m *Manager) Base() *ContextCompressor {
	return m.base
}

// Hierarchical returns the hierarchical compressor.
func (m *Manager) Hierarchical() *HierarchicalCompressor {
	return m.hierarchical
}

// Incremental returns the incremental compressor.
func (m *Manager) Incremental() *IncrementalCompressor {
	return m.incremental
}
