package evolution

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

func TestDefaultConsolidationConfig(t *testing.T) {
	cfg := DefaultConsolidationConfig()

	assert.Equal(t, 3, cfg.MinNodes)
	assert.Equal(t, 0.85, cfg.SimilarityThreshold)
	assert.True(t, cfg.PreserveSourceLinks)
	assert.Equal(t, 1000, cfg.MaxSummaryLength)
}

func TestNewConsolidator(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultConsolidationConfig()

	c := NewConsolidator(store, cfg)

	require.NotNil(t, c)
	assert.Equal(t, store, c.store)
	assert.Equal(t, cfg, c.config)
}

func TestConsolidate_BelowMinNodes(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultConsolidationConfig()
	cfg.MinNodes = 5
	c := NewConsolidator(store, cfg)
	ctx := context.Background()

	// Add only 2 nodes (below MinNodes threshold)
	node1 := hypergraph.NewNode(hypergraph.NodeTypeFact, "Fact 1")
	node1.Tier = hypergraph.TierTask
	require.NoError(t, store.CreateNode(ctx, node1))

	node2 := hypergraph.NewNode(hypergraph.NodeTypeFact, "Fact 2")
	node2.Tier = hypergraph.TierTask
	require.NoError(t, store.CreateNode(ctx, node2))

	result, err := c.Consolidate(ctx, hypergraph.TierTask, hypergraph.TierSession)
	require.NoError(t, err)

	assert.Equal(t, 2, result.NodesProcessed)
	assert.Equal(t, 0, result.NodesMerged)
	assert.Equal(t, 0, result.SummariesCreated)
}

func TestConsolidate_WithDuplicates(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultConsolidationConfig()
	cfg.MinNodes = 2
	c := NewConsolidator(store, cfg)
	ctx := context.Background()

	// Add nodes with duplicate content (different case/whitespace)
	node1 := hypergraph.NewNode(hypergraph.NodeTypeFact, "User prefers dark mode")
	node1.Tier = hypergraph.TierTask
	node1.AccessCount = 5
	require.NoError(t, store.CreateNode(ctx, node1))

	node2 := hypergraph.NewNode(hypergraph.NodeTypeFact, "user prefers dark mode")
	node2.Tier = hypergraph.TierTask
	node2.AccessCount = 3
	require.NoError(t, store.CreateNode(ctx, node2))

	node3 := hypergraph.NewNode(hypergraph.NodeTypeFact, "Different fact entirely")
	node3.Tier = hypergraph.TierTask
	require.NoError(t, store.CreateNode(ctx, node3))

	result, err := c.Consolidate(ctx, hypergraph.TierTask, hypergraph.TierSession)
	require.NoError(t, err)

	assert.Equal(t, 3, result.NodesProcessed)
	assert.Equal(t, 1, result.NodesMerged) // One duplicate merged

	// Verify the surviving node has combined access count
	// Re-fetch nodes in session tier (where they were promoted)
	nodes, err := store.ListNodes(ctx, hypergraph.NodeFilter{
		Tiers: []hypergraph.Tier{hypergraph.TierSession},
		Limit: 100,
	})
	require.NoError(t, err)

	// Count non-summary fact nodes (summaries have subtype="summary")
	factCount := 0
	var mergedNode *hypergraph.Node
	for _, n := range nodes {
		if n.Type == hypergraph.NodeTypeFact && n.Subtype != "summary" {
			factCount++
			if normalizeContent(n.Content) == "user prefers dark mode" {
				mergedNode = n
			}
		}
	}
	assert.Equal(t, 2, factCount) // 2 nodes remain after deduplication (one merged away)
	if mergedNode != nil {
		assert.Equal(t, 8, mergedNode.AccessCount) // 5 + 3
	}
}

func TestConsolidate_CreatesSummaries(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultConsolidationConfig()
	cfg.MinNodes = 3
	cfg.PreserveSourceLinks = true
	c := NewConsolidator(store, cfg)
	ctx := context.Background()

	// Add enough facts of the same subtype to trigger summary creation
	for i := 0; i < 4; i++ {
		node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Fact content "+string(rune('A'+i)))
		node.Tier = hypergraph.TierTask
		node.Subtype = "preference"
		require.NoError(t, store.CreateNode(ctx, node))
	}

	result, err := c.Consolidate(ctx, hypergraph.TierTask, hypergraph.TierSession)
	require.NoError(t, err)

	assert.Equal(t, 4, result.NodesProcessed)
	assert.GreaterOrEqual(t, result.SummariesCreated, 1)

	// Verify summary node exists with correct tier
	nodes, err := store.ListNodes(ctx, hypergraph.NodeFilter{
		Tiers: []hypergraph.Tier{hypergraph.TierSession},
		Limit: 100,
	})
	require.NoError(t, err)

	var summary *hypergraph.Node
	for _, n := range nodes {
		if n.Subtype == "summary" {
			summary = n
			break
		}
	}

	require.NotNil(t, summary, "should create summary node")
	assert.Contains(t, summary.Content, "Summary of")
}

func TestConsolidate_StrengthensEdges(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultConsolidationConfig()
	cfg.MinNodes = 2
	c := NewConsolidator(store, cfg)
	ctx := context.Background()

	// Create nodes
	node1 := hypergraph.NewNode(hypergraph.NodeTypeFact, "Node 1")
	node1.Tier = hypergraph.TierTask
	require.NoError(t, store.CreateNode(ctx, node1))

	node2 := hypergraph.NewNode(hypergraph.NodeTypeFact, "Node 2")
	node2.Tier = hypergraph.TierTask
	require.NoError(t, store.CreateNode(ctx, node2))

	// Create edge connecting them
	edge := hypergraph.NewHyperedge(hypergraph.HyperedgeRelation, "related")
	edge.Weight = 1.0
	require.NoError(t, store.CreateHyperedge(ctx, edge))
	require.NoError(t, store.AddMember(ctx, hypergraph.Membership{
		HyperedgeID: edge.ID,
		NodeID:      node1.ID,
		Role:        hypergraph.RoleSubject,
		Position:    0,
	}))
	require.NoError(t, store.AddMember(ctx, hypergraph.Membership{
		HyperedgeID: edge.ID,
		NodeID:      node2.ID,
		Role:        hypergraph.RoleObject,
		Position:    1,
	}))

	result, err := c.Consolidate(ctx, hypergraph.TierTask, hypergraph.TierSession)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, result.EdgesStrengthened, 1)

	// Verify edge weight increased
	updatedEdge, err := store.GetHyperedge(ctx, edge.ID)
	require.NoError(t, err)
	assert.Greater(t, updatedEdge.Weight, 1.0)
}

func TestConsolidate_PromotesNodes(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultConsolidationConfig()
	cfg.MinNodes = 1
	c := NewConsolidator(store, cfg)
	ctx := context.Background()

	// Create task-tier node
	node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Promotable fact")
	node.Tier = hypergraph.TierTask
	require.NoError(t, store.CreateNode(ctx, node))

	_, err := c.Consolidate(ctx, hypergraph.TierTask, hypergraph.TierSession)
	require.NoError(t, err)

	// Verify node was promoted
	promoted, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)
	assert.Equal(t, hypergraph.TierSession, promoted.Tier)
}

func TestDeduplicateNodes(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultConsolidationConfig()
	c := NewConsolidator(store, cfg)
	ctx := context.Background()

	nodes := []*hypergraph.Node{
		{ID: "1", Content: "Hello World", Type: hypergraph.NodeTypeFact, Confidence: 0.8, AccessCount: 1},
		{ID: "2", Content: "hello world", Type: hypergraph.NodeTypeFact, Confidence: 0.9, AccessCount: 2},
		{ID: "3", Content: "Different content", Type: hypergraph.NodeTypeFact, Confidence: 0.7, AccessCount: 1},
	}

	// Create nodes in store
	for _, n := range nodes {
		require.NoError(t, store.CreateNode(ctx, n))
	}

	merged, err := c.deduplicateNodes(ctx, nodes)
	require.NoError(t, err)

	assert.Equal(t, 1, merged) // One duplicate merged
}

func TestMergeNodes(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultConsolidationConfig()
	c := NewConsolidator(store, cfg)
	ctx := context.Background()

	target := &hypergraph.Node{
		ID:          "target",
		Content:     "Target content",
		Type:        hypergraph.NodeTypeFact,
		Confidence:  0.7,
		AccessCount: 5,
	}
	source := &hypergraph.Node{
		ID:          "source",
		Content:     "Source content",
		Type:        hypergraph.NodeTypeFact,
		Confidence:  0.9,
		AccessCount: 3,
	}

	require.NoError(t, store.CreateNode(ctx, target))
	require.NoError(t, store.CreateNode(ctx, source))

	err := c.mergeNodes(ctx, target, source)
	require.NoError(t, err)

	// Verify target was updated
	updated, err := store.GetNode(ctx, target.ID)
	require.NoError(t, err)
	assert.Equal(t, 8, updated.AccessCount)      // 5 + 3
	assert.Equal(t, 0.9, updated.Confidence)     // Higher confidence kept

	// Verify source was deleted
	_, err = store.GetNode(ctx, source.ID)
	assert.Error(t, err) // Should not exist
}

func TestGroupRelatedNodes(t *testing.T) {
	c := &Consolidator{config: DefaultConsolidationConfig()}

	nodes := []*hypergraph.Node{
		{ID: "1", Subtype: "preference"},
		{ID: "2", Subtype: "preference"},
		{ID: "3", Subtype: "context"},
		{ID: "4", Subtype: ""},
		{ID: "5", Subtype: "preference"},
	}

	groups := c.groupRelatedNodes(nodes)

	// Should have 3 groups: preference, context, default
	assert.Len(t, groups, 3)

	// Find preference group
	var prefCount int
	for _, group := range groups {
		for _, n := range group {
			if n.Subtype == "preference" {
				prefCount++
			}
		}
	}
	assert.Equal(t, 3, prefCount)
}

func TestGenerateSummary(t *testing.T) {
	cfg := DefaultConsolidationConfig()
	cfg.MaxSummaryLength = 500
	c := &Consolidator{config: cfg}

	nodes := []*hypergraph.Node{
		{ID: "1", Content: "First point"},
		{ID: "2", Content: "Second point"},
		{ID: "3", Content: "Third point"},
	}

	summary := c.generateSummary(nodes)

	assert.Contains(t, summary, "Summary of 3 items")
	assert.Contains(t, summary, "First point")
	assert.Contains(t, summary, "Second point")
	assert.Contains(t, summary, "Third point")
}

func TestGenerateSummary_Empty(t *testing.T) {
	c := &Consolidator{config: DefaultConsolidationConfig()}

	summary := c.generateSummary([]*hypergraph.Node{})
	assert.Empty(t, summary)
}

func TestGenerateSummary_Truncation(t *testing.T) {
	cfg := DefaultConsolidationConfig()
	cfg.MaxSummaryLength = 100
	c := &Consolidator{config: cfg}

	// Create many nodes with unique content to exceed max length
	var nodes []*hypergraph.Node
	for i := 0; i < 20; i++ {
		nodes = append(nodes, &hypergraph.Node{
			ID:      fmt.Sprintf("node-%d", i),
			Content: fmt.Sprintf("Content point number %d with extra text", i),
		})
	}

	summary := c.generateSummary(nodes)
	assert.LessOrEqual(t, len(summary), 100)
	assert.Contains(t, summary, "...") // Should be truncated
}

func TestGenerateSummary_LimitsTen(t *testing.T) {
	c := &Consolidator{config: DefaultConsolidationConfig()}

	// Create more than 10 nodes
	var nodes []*hypergraph.Node
	for i := 0; i < 15; i++ {
		nodes = append(nodes, &hypergraph.Node{
			ID:      string(rune('a' + i)),
			Content: "Point " + string(rune('A'+i)),
		})
	}

	summary := c.generateSummary(nodes)
	assert.Contains(t, summary, "and 5 more")
}

func TestAverageConfidence(t *testing.T) {
	c := &Consolidator{}

	tests := []struct {
		name     string
		nodes    []*hypergraph.Node
		expected float64
	}{
		{
			name:     "empty",
			nodes:    []*hypergraph.Node{},
			expected: 0,
		},
		{
			name: "single",
			nodes: []*hypergraph.Node{
				{Confidence: 0.8},
			},
			expected: 0.8,
		},
		{
			name: "multiple",
			nodes: []*hypergraph.Node{
				{Confidence: 0.6},
				{Confidence: 0.8},
				{Confidence: 1.0},
			},
			expected: 0.8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			avg := c.averageConfidence(tt.nodes)
			assert.InDelta(t, tt.expected, avg, 0.001)
		})
	}
}

func TestGroupByType(t *testing.T) {
	nodes := []*hypergraph.Node{
		{ID: "1", Type: hypergraph.NodeTypeFact},
		{ID: "2", Type: hypergraph.NodeTypeFact},
		{ID: "3", Type: hypergraph.NodeTypeEntity},
		{ID: "4", Type: hypergraph.NodeTypeDecision},
		{ID: "5", Type: hypergraph.NodeTypeFact},
	}

	groups := groupByType(nodes)

	assert.Len(t, groups[hypergraph.NodeTypeFact], 3)
	assert.Len(t, groups[hypergraph.NodeTypeEntity], 1)
	assert.Len(t, groups[hypergraph.NodeTypeDecision], 1)
}

func TestNormalizeContent(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello World", "hello world"},
		{"  multiple   spaces  ", "multiple spaces"},
		{"UPPERCASE", "uppercase"},
		{"mixed  CASE  content", "mixed case content"},
		{"tabs\tand\nnewlines", "tabs and newlines"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeContent(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStrengthenEdges_CapsAtTen(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultConsolidationConfig()
	c := NewConsolidator(store, cfg)
	ctx := context.Background()

	// Create nodes
	node1 := hypergraph.NewNode(hypergraph.NodeTypeFact, "Node 1")
	node1.Tier = hypergraph.TierTask
	require.NoError(t, store.CreateNode(ctx, node1))

	node2 := hypergraph.NewNode(hypergraph.NodeTypeFact, "Node 2")
	node2.Tier = hypergraph.TierTask
	require.NoError(t, store.CreateNode(ctx, node2))

	// Create edge with high weight
	edge := hypergraph.NewHyperedge(hypergraph.HyperedgeRelation, "related")
	edge.Weight = 9.5
	require.NoError(t, store.CreateHyperedge(ctx, edge))
	require.NoError(t, store.AddMember(ctx, hypergraph.Membership{
		HyperedgeID: edge.ID,
		NodeID:      node1.ID,
		Role:        hypergraph.RoleSubject,
		Position:    0,
	}))
	require.NoError(t, store.AddMember(ctx, hypergraph.Membership{
		HyperedgeID: edge.ID,
		NodeID:      node2.ID,
		Role:        hypergraph.RoleObject,
		Position:    1,
	}))

	_, err := c.strengthenEdges(ctx, hypergraph.TierTask)
	require.NoError(t, err)

	// Verify edge weight is capped at 10
	updatedEdge, err := store.GetHyperedge(ctx, edge.ID)
	require.NoError(t, err)
	assert.Equal(t, 10.0, updatedEdge.Weight)
}

// Helper function to create a test store
func createTestStore(t *testing.T) *hypergraph.Store {
	t.Helper()
	store, err := hypergraph.NewStore(hypergraph.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}
