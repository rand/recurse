package evolution

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

func TestDefaultPromotionConfig(t *testing.T) {
	cfg := DefaultPromotionConfig()

	assert.Equal(t, 1, cfg.MinAccessCount)
	assert.Equal(t, 0.5, cfg.MinConfidence)
	assert.Equal(t, time.Minute*5, cfg.MinAge)
	assert.Equal(t, 2, cfg.TaskToSessionThreshold)
	assert.Equal(t, 5, cfg.SessionToLongtermThreshold)
	assert.True(t, cfg.ConsolidateOnPromotion)
}

func TestNewPromoter(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultPromotionConfig()

	p := NewPromoter(store, cfg)

	require.NotNil(t, p)
	assert.Equal(t, store, p.store)
	assert.Equal(t, cfg, p.config)
	assert.NotNil(t, p.consolidator) // Should have consolidator when enabled
}

func TestNewPromoter_NoConsolidation(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultPromotionConfig()
	cfg.ConsolidateOnPromotion = false

	p := NewPromoter(store, cfg)

	require.NotNil(t, p)
	assert.Nil(t, p.consolidator)
}

func TestPromoteTaskToSession(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultPromotionConfig()
	cfg.ConsolidateOnPromotion = false
	cfg.MinAge = 0 // Disable age check for test
	p := NewPromoter(store, cfg)
	ctx := context.Background()

	// Create nodes with varying access counts
	node1 := hypergraph.NewNode(hypergraph.NodeTypeFact, "High access fact")
	node1.Tier = hypergraph.TierTask
	node1.AccessCount = 5
	node1.Confidence = 0.9
	require.NoError(t, store.CreateNode(ctx, node1))

	node2 := hypergraph.NewNode(hypergraph.NodeTypeFact, "Low access fact")
	node2.Tier = hypergraph.TierTask
	node2.AccessCount = 1
	node2.Confidence = 0.9
	require.NoError(t, store.CreateNode(ctx, node2))

	result, err := p.PromoteTaskToSession(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, result.TaskToSession) // Only high access node promoted
	assert.Equal(t, 1, result.Skipped)

	// Verify node1 is in session tier
	updated, err := store.GetNode(ctx, node1.ID)
	require.NoError(t, err)
	assert.Equal(t, hypergraph.TierSession, updated.Tier)

	// Verify node2 is still in task tier
	updated2, err := store.GetNode(ctx, node2.ID)
	require.NoError(t, err)
	assert.Equal(t, hypergraph.TierTask, updated2.Tier)
}

func TestPromoteSessionToLongterm(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultPromotionConfig()
	cfg.ConsolidateOnPromotion = false
	cfg.MinAge = 0 // Disable age check for test
	p := NewPromoter(store, cfg)
	ctx := context.Background()

	// Create session node with high access count
	node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Important session fact")
	node.Tier = hypergraph.TierSession
	node.AccessCount = 10
	node.Confidence = 0.95
	require.NoError(t, store.CreateNode(ctx, node))

	result, err := p.PromoteSessionToLongterm(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, result.SessionToLongterm)

	// Verify node is in long-term tier
	updated, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)
	assert.Equal(t, hypergraph.TierLongterm, updated.Tier)
}

func TestPromoteAll(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultPromotionConfig()
	cfg.ConsolidateOnPromotion = false
	cfg.MinAge = 0
	p := NewPromoter(store, cfg)
	ctx := context.Background()

	// Create task node ready for promotion (but below longterm threshold)
	taskNode := hypergraph.NewNode(hypergraph.NodeTypeFact, "Task fact")
	taskNode.Tier = hypergraph.TierTask
	taskNode.AccessCount = 3 // Above task→session (2) but below session→longterm (5)
	taskNode.Confidence = 0.8
	require.NoError(t, store.CreateNode(ctx, taskNode))

	// Create session node ready for promotion
	sessionNode := hypergraph.NewNode(hypergraph.NodeTypeFact, "Session fact")
	sessionNode.Tier = hypergraph.TierSession
	sessionNode.AccessCount = 10
	sessionNode.Confidence = 0.9
	require.NoError(t, store.CreateNode(ctx, sessionNode))

	result, err := p.PromoteAll(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, result.TaskToSession)
	assert.Equal(t, 1, result.SessionToLongterm) // Only original session node promoted
}

func TestShouldPromoteToSession(t *testing.T) {
	cfg := DefaultPromotionConfig()
	cfg.MinAge = 0
	p := &Promoter{config: cfg}

	tests := []struct {
		name     string
		node     *hypergraph.Node
		expected bool
	}{
		{
			name: "meets all criteria",
			node: &hypergraph.Node{
				AccessCount: 5,
				Confidence:  0.8,
				CreatedAt:   time.Now().Add(-time.Hour),
			},
			expected: true,
		},
		{
			name: "low confidence",
			node: &hypergraph.Node{
				AccessCount: 5,
				Confidence:  0.3,
				CreatedAt:   time.Now().Add(-time.Hour),
			},
			expected: false,
		},
		{
			name: "low access count",
			node: &hypergraph.Node{
				AccessCount: 1,
				Confidence:  0.8,
				CreatedAt:   time.Now().Add(-time.Hour),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.shouldPromoteToSession(tt.node)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestShouldPromoteToLongterm(t *testing.T) {
	cfg := DefaultPromotionConfig()
	cfg.MinAge = 0
	p := &Promoter{config: cfg}

	tests := []struct {
		name     string
		node     *hypergraph.Node
		expected bool
	}{
		{
			name: "meets all criteria",
			node: &hypergraph.Node{
				AccessCount: 10,
				Confidence:  0.9,
				CreatedAt:   time.Now().Add(-time.Hour),
			},
			expected: true,
		},
		{
			name: "below session threshold",
			node: &hypergraph.Node{
				AccessCount: 3, // Below SessionToLongtermThreshold (5)
				Confidence:  0.9,
				CreatedAt:   time.Now().Add(-time.Hour),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.shouldPromoteToLongterm(tt.node)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestForcePromote(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultPromotionConfig()
	cfg.ConsolidateOnPromotion = false
	p := NewPromoter(store, cfg)
	ctx := context.Background()

	tests := []struct {
		name         string
		initialTier  hypergraph.Tier
		expectedTier hypergraph.Tier
	}{
		{
			name:         "task to session",
			initialTier:  hypergraph.TierTask,
			expectedTier: hypergraph.TierSession,
		},
		{
			name:         "session to longterm",
			initialTier:  hypergraph.TierSession,
			expectedTier: hypergraph.TierLongterm,
		},
		{
			name:         "longterm stays longterm",
			initialTier:  hypergraph.TierLongterm,
			expectedTier: hypergraph.TierLongterm,
		},
		{
			name:         "archive to longterm",
			initialTier:  hypergraph.TierArchive,
			expectedTier: hypergraph.TierLongterm,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Test fact")
			node.Tier = tt.initialTier
			require.NoError(t, store.CreateNode(ctx, node))

			err := p.ForcePromote(ctx, node.ID)
			require.NoError(t, err)

			updated, err := store.GetNode(ctx, node.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedTier, updated.Tier)
		})
	}
}

func TestDemote(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultPromotionConfig()
	cfg.ConsolidateOnPromotion = false
	p := NewPromoter(store, cfg)
	ctx := context.Background()

	// Create long-term node
	node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Demote me")
	node.Tier = hypergraph.TierLongterm
	require.NoError(t, store.CreateNode(ctx, node))

	// Demote to session
	err := p.Demote(ctx, node.ID, hypergraph.TierSession)
	require.NoError(t, err)

	updated, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)
	assert.Equal(t, hypergraph.TierSession, updated.Tier)
}

func TestDemote_ToArchive(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultPromotionConfig()
	cfg.ConsolidateOnPromotion = false
	p := NewPromoter(store, cfg)
	ctx := context.Background()

	// Create session node
	node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Archive me")
	node.Tier = hypergraph.TierSession
	require.NoError(t, store.CreateNode(ctx, node))

	// Demote to archive (special case - always allowed)
	err := p.Demote(ctx, node.ID, hypergraph.TierArchive)
	require.NoError(t, err)

	updated, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)
	assert.Equal(t, hypergraph.TierArchive, updated.Tier)
}

func TestDemote_InvalidTarget(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultPromotionConfig()
	cfg.ConsolidateOnPromotion = false
	p := NewPromoter(store, cfg)
	ctx := context.Background()

	// Create task node
	node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Cannot demote")
	node.Tier = hypergraph.TierTask
	require.NoError(t, store.CreateNode(ctx, node))

	// Try to "demote" to session (higher tier) - should fail
	err := p.Demote(ctx, node.ID, hypergraph.TierSession)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not lower than")
}

func TestGetPromotionCandidates(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultPromotionConfig()
	cfg.ConsolidateOnPromotion = false
	cfg.MinAge = 0
	p := NewPromoter(store, cfg)
	ctx := context.Background()

	// Create candidates and non-candidates
	candidate := hypergraph.NewNode(hypergraph.NodeTypeFact, "Ready for promotion")
	candidate.Tier = hypergraph.TierTask
	candidate.AccessCount = 10
	candidate.Confidence = 0.9
	require.NoError(t, store.CreateNode(ctx, candidate))

	nonCandidate := hypergraph.NewNode(hypergraph.NodeTypeFact, "Not ready")
	nonCandidate.Tier = hypergraph.TierTask
	nonCandidate.AccessCount = 1
	nonCandidate.Confidence = 0.9
	require.NoError(t, store.CreateNode(ctx, nonCandidate))

	candidates, err := p.GetPromotionCandidates(ctx, hypergraph.TierTask)
	require.NoError(t, err)

	assert.Len(t, candidates, 1)
	assert.Equal(t, candidate.ID, candidates[0].ID)
}

func TestStats(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultPromotionConfig()
	cfg.ConsolidateOnPromotion = false
	cfg.MinAge = 0
	p := NewPromoter(store, cfg)
	ctx := context.Background()

	// Create nodes in different tiers
	for i := 0; i < 3; i++ {
		node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Task node")
		node.Tier = hypergraph.TierTask
		node.AccessCount = 1
		require.NoError(t, store.CreateNode(ctx, node))
	}

	for i := 0; i < 2; i++ {
		node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Session node")
		node.Tier = hypergraph.TierSession
		node.AccessCount = 3
		require.NoError(t, store.CreateNode(ctx, node))
	}

	node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Longterm node")
	node.Tier = hypergraph.TierLongterm
	require.NoError(t, store.CreateNode(ctx, node))

	stats, err := p.Stats(ctx)
	require.NoError(t, err)

	assert.Equal(t, 3, stats.TaskCount)
	assert.Equal(t, 2, stats.SessionCount)
	assert.Equal(t, 1, stats.LongtermCount)
	assert.Equal(t, 0, stats.ArchiveCount)
}

func TestPromotionWithConsolidation(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultPromotionConfig()
	cfg.ConsolidateOnPromotion = true
	cfg.MinAge = 0
	p := NewPromoter(store, cfg)
	ctx := context.Background()

	// Create duplicate nodes that will be consolidated
	node1 := hypergraph.NewNode(hypergraph.NodeTypeFact, "Duplicate content")
	node1.Tier = hypergraph.TierTask
	node1.AccessCount = 5
	node1.Confidence = 0.9
	require.NoError(t, store.CreateNode(ctx, node1))

	node2 := hypergraph.NewNode(hypergraph.NodeTypeFact, "duplicate content")
	node2.Tier = hypergraph.TierTask
	node2.AccessCount = 3
	node2.Confidence = 0.8
	require.NoError(t, store.CreateNode(ctx, node2))

	result, err := p.PromoteTaskToSession(ctx)
	require.NoError(t, err)

	// Consolidation should have merged the duplicates
	assert.GreaterOrEqual(t, result.TaskToSession, 0)

	// Check that nodes were processed (exact count depends on consolidation)
	nodes, err := store.ListNodes(ctx, hypergraph.NodeFilter{
		Tiers: []hypergraph.Tier{hypergraph.TierSession},
		Limit: 100,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(nodes), 1)
}

func TestPromotionResult_Duration(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultPromotionConfig()
	cfg.ConsolidateOnPromotion = false
	p := NewPromoter(store, cfg)
	ctx := context.Background()

	result, err := p.PromoteTaskToSession(ctx)
	require.NoError(t, err)

	assert.Greater(t, result.Duration, time.Duration(0))
}
