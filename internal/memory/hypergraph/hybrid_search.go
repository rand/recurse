package hypergraph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"sort"
	"time"

	"github.com/rand/recurse/internal/memory/embeddings"
)

const (
	defaultAlpha       = 0.7  // Weight for semantic vs keyword (0=keyword, 1=semantic)
	defaultRRFConstant = 60   // Standard RRF constant
)

// RetrievalOutcome represents the outcome of a single node retrieval.
// This is used for meta-evolution tracking.
type RetrievalOutcome struct {
	QueryHash      string    // Hash of the query for grouping
	QueryType      string    // computational, retrieval, analytical, transformational
	NodeID         string    // Retrieved node ID
	NodeType       string    // Type of the retrieved node
	NodeSubtype    string    // Subtype if any
	RelevanceScore float64   // 0.0-1.0, from search score
	WasUsed        bool      // Did the retrieved content get used?
	ContextTokens  int       // Token count of the context
	LatencyMs      int       // Query latency in milliseconds
	Timestamp      time.Time // When this outcome was recorded
}

// OutcomeRecorder records retrieval outcomes for meta-evolution.
type OutcomeRecorder interface {
	RecordOutcome(ctx context.Context, outcome RetrievalOutcome) error
}

// HybridSearcher combines keyword and semantic search using Reciprocal Rank Fusion.
type HybridSearcher struct {
	store           *Store
	index           *embeddings.Index
	alpha           float64 // Weight for semantic search
	k               int     // RRF constant
	logger          *slog.Logger
	metrics         *embeddings.EmbeddingMetrics
	outcomeRecorder OutcomeRecorder
}

// HybridConfig configures the hybrid searcher.
type HybridConfig struct {
	Alpha           float64 // Semantic weight (0=keyword only, 1=semantic only, default: 0.7)
	K               int     // RRF constant (default: 60)
	Logger          *slog.Logger
	Metrics         *embeddings.EmbeddingMetrics
	OutcomeRecorder OutcomeRecorder // Optional: records outcomes for meta-evolution
}

// NewHybridSearcher creates a new hybrid searcher.
func NewHybridSearcher(store *Store, index *embeddings.Index, cfg HybridConfig) *HybridSearcher {
	if cfg.Alpha <= 0 {
		cfg.Alpha = defaultAlpha
	}
	if cfg.K <= 0 {
		cfg.K = defaultRRFConstant
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &HybridSearcher{
		store:           store,
		index:           index,
		alpha:           cfg.Alpha,
		k:               cfg.K,
		logger:          cfg.Logger,
		metrics:         cfg.Metrics,
		outcomeRecorder: cfg.OutcomeRecorder,
	}
}

// SetOutcomeRecorder sets the outcome recorder for meta-evolution tracking.
func (h *HybridSearcher) SetOutcomeRecorder(recorder OutcomeRecorder) {
	h.outcomeRecorder = recorder
}

// Search performs hybrid keyword + semantic search.
func (h *HybridSearcher) Search(
	ctx context.Context,
	query string,
	opts SearchOptions,
) ([]*SearchResult, error) {
	start := time.Now()
	var latencyMs int
	defer func() {
		latencyMs = int(time.Since(start).Milliseconds())
		if h.metrics != nil {
			h.metrics.RecordHybridSearch(time.Since(start))
		}
	}()

	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	// Fetch more candidates for fusion
	candidateLimit := limit * 3

	// 1. Keyword search
	keywordOpts := SearchOptions{
		Types:         opts.Types,
		Tiers:         opts.Tiers,
		Subtypes:      opts.Subtypes,
		MinConfidence: opts.MinConfidence,
		Limit:         candidateLimit,
	}
	keywordResults, err := h.store.SearchByContent(ctx, query, keywordOpts)
	if err != nil {
		return nil, err
	}

	// 2. Semantic search
	semanticResults, err := h.index.Search(ctx, query, candidateLimit)
	if err != nil {
		// Fall back to keyword-only on embedding failure
		h.logger.Warn("semantic search failed, falling back to keyword",
			"error", err)
		if len(keywordResults) > limit {
			return keywordResults[:limit], nil
		}
		return keywordResults, nil
	}

	// 3. Reciprocal Rank Fusion
	fused := h.reciprocalRankFusion(keywordResults, semanticResults, limit)

	// 4. Hydrate nodes if needed (semantic results may only have IDs)
	results, err := h.hydrateResults(ctx, fused, opts)
	if err != nil {
		return nil, err
	}

	// 5. Record outcomes for meta-evolution
	h.recordOutcomes(ctx, query, results, latencyMs, opts.QueryType)

	return results, nil
}

// reciprocalRankFusion combines two ranked lists using RRF.
// RRF(d) = Σ 1/(k + rank(d)) for each list
func (h *HybridSearcher) reciprocalRankFusion(
	keyword []*SearchResult,
	semantic []embeddings.SearchResult,
	limit int,
) []rankedResult {
	scores := make(map[string]float64)
	nodes := make(map[string]*Node)

	// Score keyword results (weighted by 1-alpha)
	for i, r := range keyword {
		id := r.Node.ID
		scores[id] += (1 - h.alpha) / float64(h.k+i+1)
		nodes[id] = r.Node
	}

	// Score semantic results (weighted by alpha)
	for i, r := range semantic {
		id := r.NodeID
		scores[id] += h.alpha / float64(h.k+i+1)
		// Don't overwrite node if already set from keyword results
	}

	// Sort by combined score
	var all []rankedResult
	for id, score := range scores {
		all = append(all, rankedResult{
			nodeID: id,
			score:  score,
			node:   nodes[id], // May be nil for semantic-only results
		})
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].score > all[j].score
	})

	// Take top limit
	if len(all) > limit {
		all = all[:limit]
	}

	return all
}

type rankedResult struct {
	nodeID string
	score  float64
	node   *Node
}

// hydrateResults ensures all results have full Node data.
func (h *HybridSearcher) hydrateResults(
	ctx context.Context,
	ranked []rankedResult,
	opts SearchOptions,
) ([]*SearchResult, error) {
	results := make([]*SearchResult, 0, len(ranked))

	for _, r := range ranked {
		var node *Node
		if r.node != nil {
			node = r.node
		} else {
			// Fetch node for semantic-only results
			var err error
			node, err = h.store.GetNode(ctx, r.nodeID)
			if err != nil {
				h.logger.Debug("failed to fetch node",
					"node_id", r.nodeID,
					"error", err)
				continue
			}
		}

		// Apply filters if needed
		if !h.matchesFilters(node, opts) {
			continue
		}

		results = append(results, &SearchResult{
			Node:  node,
			Score: r.score,
		})
	}

	return results, nil
}

// matchesFilters checks if a node matches the search options.
func (h *HybridSearcher) matchesFilters(node *Node, opts SearchOptions) bool {
	// Type filter
	if len(opts.Types) > 0 {
		found := false
		for _, t := range opts.Types {
			if node.Type == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Tier filter
	if len(opts.Tiers) > 0 {
		found := false
		for _, t := range opts.Tiers {
			if node.Tier == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Subtype filter
	if len(opts.Subtypes) > 0 {
		found := false
		for _, s := range opts.Subtypes {
			if node.Subtype == s {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Confidence filter
	if opts.MinConfidence > 0 && node.Confidence < opts.MinConfidence {
		return false
	}

	return true
}

// SetAlpha adjusts the semantic weight at runtime.
func (h *HybridSearcher) SetAlpha(alpha float64) {
	if alpha < 0 {
		alpha = 0
	}
	if alpha > 1 {
		alpha = 1
	}
	h.alpha = alpha
}

// Alpha returns the current semantic weight.
func (h *HybridSearcher) Alpha() float64 {
	return h.alpha
}

// hashQuery creates a short hash of a query for outcome grouping.
func hashQuery(query string) string {
	h := sha256.Sum256([]byte(query))
	return hex.EncodeToString(h[:8]) // First 8 bytes = 16 hex chars
}

// recordOutcomes records retrieval outcomes for meta-evolution tracking.
func (h *HybridSearcher) recordOutcomes(ctx context.Context, query string, results []*SearchResult, latencyMs int, queryType string) {
	if h.outcomeRecorder == nil || len(results) == 0 {
		return
	}

	queryHash := hashQuery(query)
	if queryType == "" {
		queryType = "retrieval" // Default query type
	}

	for _, result := range results {
		outcome := RetrievalOutcome{
			Timestamp:      time.Now(),
			QueryHash:      queryHash,
			QueryType:      queryType,
			NodeID:         result.Node.ID,
			NodeType:       string(result.Node.Type),
			NodeSubtype:    result.Node.Subtype,
			RelevanceScore: result.Score,
			WasUsed:        false, // Will be updated via feedback
			LatencyMs:      latencyMs,
		}

		if err := h.outcomeRecorder.RecordOutcome(ctx, outcome); err != nil {
			h.logger.Debug("failed to record outcome",
				"node_id", result.Node.ID,
				"error", err)
		}
	}
}
