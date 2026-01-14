package hypergraph

import (
	"context"
	"testing"

	"github.com/rand/recurse/internal/memory/embeddings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHybridSearcher_RRF(t *testing.T) {
	// Create a mock hybrid searcher just to test the RRF algorithm
	searcher := &HybridSearcher{
		alpha: 0.7,
		k:     60,
	}

	keyword := []*SearchResult{
		{Node: &Node{ID: "a"}, Score: 1.0},
		{Node: &Node{ID: "b"}, Score: 0.8},
		{Node: &Node{ID: "c"}, Score: 0.6},
	}

	semantic := []embeddings.SearchResult{
		{NodeID: "b", Similarity: 0.95},
		{NodeID: "d", Similarity: 0.90},
		{NodeID: "a", Similarity: 0.85},
	}

	results := searcher.reciprocalRankFusion(keyword, semantic, 4)

	// "b" should rank highest (appears in both lists highly)
	require.NotEmpty(t, results)
	assert.Equal(t, "b", results[0].nodeID)

	// All 4 unique IDs should be present
	ids := make(map[string]bool)
	for _, r := range results {
		ids[r.nodeID] = true
	}
	assert.True(t, ids["a"])
	assert.True(t, ids["b"])
	assert.True(t, ids["c"])
	assert.True(t, ids["d"])
}

func TestHybridSearcher_RRF_KeywordOnly(t *testing.T) {
	searcher := &HybridSearcher{
		alpha: 0.0, // Keyword only
		k:     60,
	}

	keyword := []*SearchResult{
		{Node: &Node{ID: "a"}, Score: 1.0},
		{Node: &Node{ID: "b"}, Score: 0.8},
	}

	semantic := []embeddings.SearchResult{
		{NodeID: "c", Similarity: 0.95},
		{NodeID: "d", Similarity: 0.90},
	}

	results := searcher.reciprocalRankFusion(keyword, semantic, 4)

	// With alpha=0, keyword results should dominate
	require.NotEmpty(t, results)
	assert.Equal(t, "a", results[0].nodeID)
	assert.Equal(t, "b", results[1].nodeID)
}

func TestHybridSearcher_RRF_SemanticOnly(t *testing.T) {
	searcher := &HybridSearcher{
		alpha: 1.0, // Semantic only
		k:     60,
	}

	keyword := []*SearchResult{
		{Node: &Node{ID: "a"}, Score: 1.0},
		{Node: &Node{ID: "b"}, Score: 0.8},
	}

	semantic := []embeddings.SearchResult{
		{NodeID: "c", Similarity: 0.95},
		{NodeID: "d", Similarity: 0.90},
	}

	results := searcher.reciprocalRankFusion(keyword, semantic, 4)

	// With alpha=1, semantic results should dominate
	require.NotEmpty(t, results)
	assert.Equal(t, "c", results[0].nodeID)
	assert.Equal(t, "d", results[1].nodeID)
}

func TestHybridSearcher_MatchesFilters(t *testing.T) {
	searcher := &HybridSearcher{}

	node := &Node{
		ID:         "test",
		Type:       NodeTypeFact,
		Tier:       TierSession,
		Subtype:    "file",
		Confidence: 0.8,
	}

	tests := []struct {
		name    string
		opts    SearchOptions
		matches bool
	}{
		{
			name:    "no filters",
			opts:    SearchOptions{},
			matches: true,
		},
		{
			name:    "matching type",
			opts:    SearchOptions{Types: []NodeType{NodeTypeFact}},
			matches: true,
		},
		{
			name:    "non-matching type",
			opts:    SearchOptions{Types: []NodeType{NodeTypeEntity}},
			matches: false,
		},
		{
			name:    "matching tier",
			opts:    SearchOptions{Tiers: []Tier{TierSession}},
			matches: true,
		},
		{
			name:    "non-matching tier",
			opts:    SearchOptions{Tiers: []Tier{TierLongterm}},
			matches: false,
		},
		{
			name:    "matching subtype",
			opts:    SearchOptions{Subtypes: []string{"file"}},
			matches: true,
		},
		{
			name:    "non-matching subtype",
			opts:    SearchOptions{Subtypes: []string{"function"}},
			matches: false,
		},
		{
			name:    "confidence above threshold",
			opts:    SearchOptions{MinConfidence: 0.5},
			matches: true,
		},
		{
			name:    "confidence below threshold",
			opts:    SearchOptions{MinConfidence: 0.9},
			matches: false,
		},
		{
			name: "multiple filters all match",
			opts: SearchOptions{
				Types:         []NodeType{NodeTypeFact},
				Tiers:         []Tier{TierSession},
				MinConfidence: 0.5,
			},
			matches: true,
		},
		{
			name: "multiple filters one fails",
			opts: SearchOptions{
				Types:         []NodeType{NodeTypeFact},
				Tiers:         []Tier{TierLongterm}, // doesn't match
				MinConfidence: 0.5,
			},
			matches: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := searcher.matchesFilters(node, tt.opts)
			assert.Equal(t, tt.matches, result)
		})
	}
}

func TestHybridSearcher_SetAlpha(t *testing.T) {
	searcher := &HybridSearcher{alpha: 0.7}

	// Normal values
	searcher.SetAlpha(0.5)
	assert.Equal(t, 0.5, searcher.Alpha())

	// Clamp to 0
	searcher.SetAlpha(-1.0)
	assert.Equal(t, 0.0, searcher.Alpha())

	// Clamp to 1
	searcher.SetAlpha(2.0)
	assert.Equal(t, 1.0, searcher.Alpha())
}

// mockEmbeddingIndex is a minimal mock for testing hybrid search
type mockEmbeddingIndex struct {
	results []embeddings.SearchResult
	err     error
}

func (m *mockEmbeddingIndex) Search(ctx context.Context, query string, limit int) ([]embeddings.SearchResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.results, nil
}

func TestHybridSearcher_Integration(t *testing.T) {
	// Create an in-memory store
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Add some test nodes
	nodes := []*Node{
		{ID: "node-1", Type: NodeTypeFact, Content: "Go programming language", Tier: TierSession, Confidence: 0.9},
		{ID: "node-2", Type: NodeTypeFact, Content: "Golang error handling", Tier: TierSession, Confidence: 0.8},
		{ID: "node-3", Type: NodeTypeFact, Content: "Python programming", Tier: TierSession, Confidence: 0.7},
	}

	for _, node := range nodes {
		require.NoError(t, store.CreateNode(ctx, node))
	}

	// Without embeddings, Search should fall back to keyword search
	results, err := store.Search(ctx, "Go", SearchOptions{Limit: 10})
	require.NoError(t, err)
	require.NotEmpty(t, results)

	// Should find the Go-related nodes
	foundGo := false
	for _, r := range results {
		if r.Node.ID == "node-1" || r.Node.ID == "node-2" {
			foundGo = true
			break
		}
	}
	assert.True(t, foundGo, "should find Go-related nodes")
}
