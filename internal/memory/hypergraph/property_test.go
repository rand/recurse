package hypergraph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// TestProperty_NodeCreateGetRoundtrip verifies nodes survive create/get cycle.
func TestProperty_NodeCreateGetRoundtrip(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		nodeType := rapid.SampledFrom([]NodeType{
			NodeTypeFact, NodeTypeEntity, NodeTypeSnippet, NodeTypeDecision, NodeTypeExperience,
		}).Draw(t, "type")
		content := rapid.String().Draw(t, "content")
		subtype := rapid.String().Draw(t, "subtype")
		confidence := rapid.Float64Range(0, 1).Draw(t, "confidence")
		tier := rapid.SampledFrom([]Tier{
			TierTask, TierSession, TierLongterm, TierArchive,
		}).Draw(t, "tier")

		node := NewNode(nodeType, content)
		node.Subtype = subtype
		node.Confidence = confidence
		node.Tier = tier

		err := store.CreateNode(ctx, node)
		require.NoError(t, err)

		got, err := store.GetNode(ctx, node.ID)
		require.NoError(t, err)
		require.NotNil(t, got)

		assert.Equal(t, node.ID, got.ID)
		assert.Equal(t, node.Type, got.Type)
		assert.Equal(t, node.Subtype, got.Subtype)
		assert.Equal(t, node.Content, got.Content)
		assert.Equal(t, node.Tier, got.Tier)
		assert.InDelta(t, node.Confidence, got.Confidence, 0.0001)
	})
}

// TestProperty_NodeUpdatePreservesID verifies updates don't change node identity.
func TestProperty_NodeUpdatePreservesID(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		// Create initial node
		node := NewNode(NodeTypeFact, "initial content")
		err := store.CreateNode(ctx, node)
		require.NoError(t, err)

		originalID := node.ID
		originalCreatedAt := node.CreatedAt

		// Update with random values
		node.Content = rapid.String().Draw(t, "new_content")
		node.Confidence = rapid.Float64Range(0, 1).Draw(t, "new_confidence")
		node.Tier = rapid.SampledFrom([]Tier{
			TierTask, TierSession, TierLongterm,
		}).Draw(t, "new_tier")

		err = store.UpdateNode(ctx, node)
		require.NoError(t, err)

		got, err := store.GetNode(ctx, originalID)
		require.NoError(t, err)

		assert.Equal(t, originalID, got.ID)
		assert.Equal(t, originalCreatedAt.Unix(), got.CreatedAt.Unix())
		assert.Equal(t, node.Content, got.Content)
		assert.InDelta(t, node.Confidence, got.Confidence, 0.0001)
	})
}

// TestProperty_IncrementAccessAlwaysIncreases verifies access count always goes up.
func TestProperty_IncrementAccessAlwaysIncreases(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		node := NewNode(NodeTypeFact, "test")
		err := store.CreateNode(ctx, node)
		require.NoError(t, err)

		increments := rapid.IntRange(1, 10).Draw(t, "increments")

		for i := 0; i < increments; i++ {
			err := store.IncrementAccess(ctx, node.ID)
			require.NoError(t, err)
		}

		got, err := store.GetNode(ctx, node.ID)
		require.NoError(t, err)

		// Access count should be at least the number of increments
		// (starts at 0, so should be exactly equal)
		assert.Equal(t, increments, got.AccessCount)
	})
}

// TestProperty_ListNodesFilterConsistency verifies filter results are consistent.
func TestProperty_ListNodesFilterConsistency(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create a mix of nodes
	types := []NodeType{NodeTypeFact, NodeTypeEntity, NodeTypeSnippet}
	tiers := []Tier{TierTask, TierSession, TierLongterm}

	for _, nodeType := range types {
		for _, tier := range tiers {
			node := NewNode(nodeType, "content")
			node.Tier = tier
			node.Confidence = 0.8
			err := store.CreateNode(ctx, node)
			require.NoError(t, err)
		}
	}

	rapid.Check(t, func(t *rapid.T) {
		// Random filter
		var filterTypes []NodeType
		if rapid.Bool().Draw(t, "has_type_filter") {
			filterTypes = []NodeType{rapid.SampledFrom(types).Draw(t, "filter_type")}
		}

		var filterTiers []Tier
		if rapid.Bool().Draw(t, "has_tier_filter") {
			filterTiers = []Tier{rapid.SampledFrom(tiers).Draw(t, "filter_tier")}
		}

		nodes, err := store.ListNodes(ctx, NodeFilter{
			Types: filterTypes,
			Tiers: filterTiers,
		})
		require.NoError(t, err)

		// Verify all returned nodes match the filter
		for _, node := range nodes {
			if len(filterTypes) > 0 {
				assert.Contains(t, filterTypes, node.Type)
			}
			if len(filterTiers) > 0 {
				assert.Contains(t, filterTiers, node.Tier)
			}
		}
	})
}

// TestProperty_HyperedgeCreateGetRoundtrip verifies hyperedges survive create/get cycle.
func TestProperty_HyperedgeCreateGetRoundtrip(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		edgeType := rapid.SampledFrom([]HyperedgeType{
			HyperedgeRelation, HyperedgeComposition, HyperedgeCausation,
			HyperedgeContext, HyperedgeSpawns, HyperedgeConsiders,
		}).Draw(t, "edge_type")
		label := rapid.String().Draw(t, "label")
		// Use range 0.01-1.0 since 0 gets defaulted to 1.0 in CreateHyperedge
		weight := rapid.Float64Range(0.01, 1).Draw(t, "weight")

		edge := NewHyperedge(edgeType, label)
		edge.Weight = weight

		err := store.CreateHyperedge(ctx, edge)
		require.NoError(t, err)

		got, err := store.GetHyperedge(ctx, edge.ID)
		require.NoError(t, err)
		require.NotNil(t, got)

		assert.Equal(t, edge.ID, got.ID)
		assert.Equal(t, edgeType, got.Type)
		assert.Equal(t, label, got.Label)
		assert.InDelta(t, weight, got.Weight, 0.0001)
	})
}

// TestProperty_DeleteNodeRemovesFromStore verifies deleted nodes are gone.
func TestProperty_DeleteNodeRemovesFromStore(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		// Use non-empty content to ensure node is valid
		content := rapid.StringMatching(`[a-zA-Z0-9]{1,50}`).Draw(t, "content")
		node := NewNode(NodeTypeFact, content)
		err := store.CreateNode(ctx, node)
		require.NoError(t, err)

		// Verify exists
		got, err := store.GetNode(ctx, node.ID)
		require.NoError(t, err)
		require.NotNil(t, got)

		// Delete
		err = store.DeleteNode(ctx, node.ID)
		require.NoError(t, err)

		// Verify gone - GetNode returns error for not found
		got, err = store.GetNode(ctx, node.ID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

// TestProperty_EvolutionLogPersists verifies evolution entries are persisted.
func TestProperty_EvolutionLogPersists(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		operation := rapid.SampledFrom([]EvolutionOperation{
			EvolutionCreate, EvolutionConsolidate, EvolutionPromote,
			EvolutionDecay, EvolutionPrune, EvolutionArchive,
		}).Draw(t, "operation")

		fromTier := rapid.SampledFrom([]Tier{
			TierTask, TierSession, TierLongterm, "",
		}).Draw(t, "from_tier")

		toTier := rapid.SampledFrom([]Tier{
			TierSession, TierLongterm, TierArchive, "",
		}).Draw(t, "to_tier")

		reasoning := rapid.String().Draw(t, "reasoning")

		entry := &EvolutionEntry{
			Operation: operation,
			FromTier:  fromTier,
			ToTier:    toTier,
			Reasoning: reasoning,
		}

		err := store.RecordEvolution(ctx, entry)
		require.NoError(t, err)
		assert.NotZero(t, entry.ID)

		// Retrieve and verify
		entries, err := store.ListEvolutionLog(ctx, EvolutionFilter{
			Operations: []EvolutionOperation{operation},
			Limit:      100,
		})
		require.NoError(t, err)

		found := false
		for _, e := range entries {
			if e.ID == entry.ID {
				found = true
				assert.Equal(t, operation, e.Operation)
				assert.Equal(t, reasoning, e.Reasoning)
				break
			}
		}
		assert.True(t, found, "entry should be in evolution log")
	})
}

// TestProperty_ConcurrentNodeCreation verifies concurrent creates are safe.
func TestProperty_ConcurrentNodeCreation(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		numGoroutines := rapid.IntRange(2, 5).Draw(t, "num_goroutines")
		nodesPerGoroutine := rapid.IntRange(1, 5).Draw(t, "nodes_per")

		done := make(chan error, numGoroutines*nodesPerGoroutine)

		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				for j := 0; j < nodesPerGoroutine; j++ {
					node := NewNode(NodeTypeFact, "concurrent test")
					done <- store.CreateNode(ctx, node)
				}
			}(i)
		}

		// Collect results
		for i := 0; i < numGoroutines*nodesPerGoroutine; i++ {
			err := <-done
			assert.NoError(t, err)
		}
	})
}

// TestProperty_SearchExcludesArchived verifies search never returns archived nodes.
func TestProperty_SearchExcludesArchived(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create nodes in all tiers with same content
	keyword := "searchable_keyword"
	for _, tier := range []Tier{TierTask, TierSession, TierLongterm, TierArchive} {
		node := NewNode(NodeTypeFact, keyword+" content")
		node.Tier = tier
		err := store.CreateNode(ctx, node)
		require.NoError(t, err)
	}

	rapid.Check(t, func(t *rapid.T) {
		results, err := store.SearchByContent(ctx, keyword, SearchOptions{})
		require.NoError(t, err)

		for _, result := range results {
			assert.NotEqual(t, TierArchive, result.Node.Tier,
				"search should never return archived nodes")
		}
	})
}
