package evolution

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

func TestDefaultDecayConfig(t *testing.T) {
	cfg := DefaultDecayConfig()

	assert.Equal(t, time.Hour*24*7, cfg.HalfLife)
	assert.Equal(t, 0.1, cfg.AccessBoost)
	assert.Equal(t, 0.3, cfg.ArchiveThreshold)
	assert.Equal(t, 0.1, cfg.PruneThreshold)
	assert.Equal(t, time.Hour*24, cfg.MinRetention)
	assert.Contains(t, cfg.ExcludeTiers, hypergraph.TierTask)
}

func TestNewDecayer(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultDecayConfig()

	d := NewDecayer(store, cfg)

	require.NotNil(t, d)
	assert.Equal(t, store, d.store)
	assert.Equal(t, cfg, d.config)
}

func TestCalculateDecay(t *testing.T) {
	cfg := DefaultDecayConfig()
	cfg.HalfLife = time.Hour // 1 hour half-life for easier testing
	d := &Decayer{config: cfg}

	tests := []struct {
		name     string
		elapsed  time.Duration
		expected float64
		delta    float64
	}{
		{
			name:     "no time elapsed",
			elapsed:  0,
			expected: 1.0,
			delta:    0.001,
		},
		{
			name:     "one half-life",
			elapsed:  time.Hour,
			expected: 0.5,
			delta:    0.001,
		},
		{
			name:     "two half-lives",
			elapsed:  time.Hour * 2,
			expected: 0.25,
			delta:    0.001,
		},
		{
			name:     "half a half-life",
			elapsed:  time.Minute * 30,
			expected: 0.707, // sqrt(0.5)
			delta:    0.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := d.calculateDecay(tt.elapsed)
			assert.InDelta(t, tt.expected, result, tt.delta)
		})
	}
}

func TestCalculateAmplification(t *testing.T) {
	cfg := DefaultDecayConfig()
	d := &Decayer{config: cfg}

	tests := []struct {
		name        string
		accessCount int
		minExpected float64
	}{
		{
			name:        "zero access",
			accessCount: 0,
			minExpected: 1.0,
		},
		{
			name:        "one access",
			accessCount: 1,
			minExpected: 1.0,
		},
		{
			name:        "many accesses",
			accessCount: 100,
			minExpected: 1.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := d.calculateAmplification(tt.accessCount)
			assert.GreaterOrEqual(t, result, tt.minExpected)
		})
	}
}

func TestApplyDecay(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultDecayConfig()
	cfg.HalfLife = time.Millisecond // Very short for testing
	cfg.ExcludeTiers = nil          // Don't exclude any tiers
	d := NewDecayer(store, cfg)
	ctx := context.Background()

	// Create a node with old last_accessed time
	node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Old fact")
	node.Tier = hypergraph.TierSession
	node.Confidence = 1.0
	oldTime := time.Now().Add(-time.Hour)
	node.LastAccessed = &oldTime
	require.NoError(t, store.CreateNode(ctx, node))

	// Wait a bit to ensure decay
	time.Sleep(time.Millisecond * 10)

	result, err := d.ApplyDecay(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, result.NodesProcessed)
	assert.Equal(t, 1, result.NodesDecayed)

	// Verify confidence decreased
	updated, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)
	assert.Less(t, updated.Confidence, 1.0)
}

func TestApplyDecay_ExcludesTiers(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultDecayConfig()
	cfg.HalfLife = time.Millisecond
	cfg.ExcludeTiers = []hypergraph.Tier{hypergraph.TierTask}
	d := NewDecayer(store, cfg)
	ctx := context.Background()

	// Create task tier node (should be excluded)
	node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Task fact")
	node.Tier = hypergraph.TierTask
	node.Confidence = 1.0
	oldTime := time.Now().Add(-time.Hour)
	node.LastAccessed = &oldTime
	require.NoError(t, store.CreateNode(ctx, node))

	time.Sleep(time.Millisecond * 10)

	result, err := d.ApplyDecay(ctx)
	require.NoError(t, err)

	assert.Equal(t, 0, result.NodesProcessed) // Task tier excluded
	assert.Equal(t, 0, result.NodesDecayed)

	// Verify confidence unchanged
	updated, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)
	assert.Equal(t, 1.0, updated.Confidence)
}

func TestArchiveLowConfidence(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultDecayConfig()
	cfg.ArchiveThreshold = 0.5
	cfg.MinRetention = 0 // Disable retention for test
	cfg.ExcludeTiers = nil
	d := NewDecayer(store, cfg)
	ctx := context.Background()

	// Create low confidence node
	node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Low confidence fact")
	node.Tier = hypergraph.TierSession
	node.Confidence = 0.2 // Below threshold
	node.CreatedAt = time.Now().Add(-time.Hour)
	require.NoError(t, store.CreateNode(ctx, node))

	result, err := d.ArchiveLowConfidence(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, result.NodesArchived)

	// Verify node is archived
	updated, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)
	assert.Equal(t, hypergraph.TierArchive, updated.Tier)
}

func TestArchiveLowConfidence_RespectsMinRetention(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultDecayConfig()
	cfg.ArchiveThreshold = 0.5
	cfg.MinRetention = time.Hour * 24 // 1 day retention
	cfg.ExcludeTiers = nil
	d := NewDecayer(store, cfg)
	ctx := context.Background()

	// Create recent low confidence node
	node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Recent low confidence")
	node.Tier = hypergraph.TierSession
	node.Confidence = 0.2
	// CreatedAt is set to now by NewNode, so it's within retention period
	require.NoError(t, store.CreateNode(ctx, node))

	result, err := d.ArchiveLowConfidence(ctx)
	require.NoError(t, err)

	assert.Equal(t, 0, result.NodesArchived) // Should not archive due to retention

	// Verify node is still in session tier
	updated, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)
	assert.Equal(t, hypergraph.TierSession, updated.Tier)
}

func TestPruneArchived(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultDecayConfig()
	cfg.PruneThreshold = 0.2
	d := NewDecayer(store, cfg)
	ctx := context.Background()

	// Create archived node below prune threshold
	node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Prune me")
	node.Tier = hypergraph.TierArchive
	node.Confidence = 0.05 // Below prune threshold
	require.NoError(t, store.CreateNode(ctx, node))

	result, err := d.PruneArchived(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, result.NodesPruned)

	// Verify node is deleted
	_, err = store.GetNode(ctx, node.ID)
	assert.Error(t, err)
}

func TestPruneArchived_KeepsAboveThreshold(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultDecayConfig()
	cfg.PruneThreshold = 0.1
	d := NewDecayer(store, cfg)
	ctx := context.Background()

	// Create archived node above prune threshold
	node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Keep me")
	node.Tier = hypergraph.TierArchive
	node.Confidence = 0.25 // Above prune threshold
	require.NoError(t, store.CreateNode(ctx, node))

	result, err := d.PruneArchived(ctx)
	require.NoError(t, err)

	assert.Equal(t, 0, result.NodesPruned)

	// Verify node still exists
	_, err = store.GetNode(ctx, node.ID)
	assert.NoError(t, err)
}

func TestRunFullCycle(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultDecayConfig()
	cfg.HalfLife = time.Millisecond
	cfg.ArchiveThreshold = 0.5
	cfg.PruneThreshold = 0.1
	cfg.MinRetention = 0
	cfg.ExcludeTiers = nil
	d := NewDecayer(store, cfg)
	ctx := context.Background()

	// Create node that will decay, be archived, then pruned
	node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Full cycle node")
	node.Tier = hypergraph.TierSession
	node.Confidence = 0.15 // Will be archived but not pruned initially
	oldTime := time.Now().Add(-time.Hour)
	node.LastAccessed = &oldTime
	node.CreatedAt = oldTime
	require.NoError(t, store.CreateNode(ctx, node))

	time.Sleep(time.Millisecond * 10)

	result, err := d.RunFullCycle(ctx)
	require.NoError(t, err)

	assert.Greater(t, result.Duration, time.Duration(0))
	// The node should have gone through the cycle
	assert.GreaterOrEqual(t, result.NodesProcessed, 1)
}

func TestRecordAccess(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultDecayConfig()
	cfg.AccessBoost = 0.2
	d := NewDecayer(store, cfg)
	ctx := context.Background()

	// Create node with low confidence
	node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Boost me")
	node.Tier = hypergraph.TierSession
	node.Confidence = 0.5
	require.NoError(t, store.CreateNode(ctx, node))

	err := d.RecordAccess(ctx, node.ID)
	require.NoError(t, err)

	// Verify confidence increased
	updated, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)
	assert.Equal(t, 0.7, updated.Confidence) // 0.5 + 0.2
}

func TestRecordAccess_CapsAtOne(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultDecayConfig()
	cfg.AccessBoost = 0.5
	d := NewDecayer(store, cfg)
	ctx := context.Background()

	// Create node with high confidence
	node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Already high")
	node.Tier = hypergraph.TierSession
	node.Confidence = 0.9
	require.NoError(t, store.CreateNode(ctx, node))

	err := d.RecordAccess(ctx, node.ID)
	require.NoError(t, err)

	// Verify confidence capped at 1.0
	updated, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)
	assert.Equal(t, 1.0, updated.Confidence)
}

func TestRestoreFromArchive(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultDecayConfig()
	cfg.ArchiveThreshold = 0.3
	d := NewDecayer(store, cfg)
	ctx := context.Background()

	// Create archived node
	node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Restore me")
	node.Tier = hypergraph.TierArchive
	node.Confidence = 0.1
	require.NoError(t, store.CreateNode(ctx, node))

	err := d.RestoreFromArchive(ctx, node.ID)
	require.NoError(t, err)

	// Verify node is restored to longterm with threshold confidence
	updated, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)
	assert.Equal(t, hypergraph.TierLongterm, updated.Tier)
	assert.Equal(t, 0.3, updated.Confidence) // Reset to threshold
}

func TestRestoreFromArchive_NotArchived(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultDecayConfig()
	d := NewDecayer(store, cfg)
	ctx := context.Background()

	// Create non-archived node
	node := hypergraph.NewNode(hypergraph.NodeTypeFact, "Not archived")
	node.Tier = hypergraph.TierSession
	require.NoError(t, store.CreateNode(ctx, node))

	err := d.RestoreFromArchive(ctx, node.ID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not archived")
}

func TestGetDecayStats(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultDecayConfig()
	cfg.ArchiveThreshold = 0.3
	d := NewDecayer(store, cfg)
	ctx := context.Background()

	// Create nodes with various confidences
	highConf := hypergraph.NewNode(hypergraph.NodeTypeFact, "High confidence")
	highConf.Confidence = 0.95
	require.NoError(t, store.CreateNode(ctx, highConf))

	lowConf := hypergraph.NewNode(hypergraph.NodeTypeFact, "Low confidence")
	lowConf.Confidence = 0.2 // Below archive threshold but not archived
	require.NoError(t, store.CreateNode(ctx, lowConf))

	archived := hypergraph.NewNode(hypergraph.NodeTypeFact, "Archived")
	archived.Tier = hypergraph.TierArchive
	archived.Confidence = 0.15
	require.NoError(t, store.CreateNode(ctx, archived))

	stats, err := d.GetDecayStats(ctx)
	require.NoError(t, err)

	assert.Equal(t, 3, stats.TotalNodes)
	assert.Equal(t, 1, stats.ArchivedNodes)
	assert.Equal(t, 1, stats.AtRiskNodes) // lowConf is at risk
	assert.Greater(t, stats.AverageConfidence, 0.0)
	assert.NotEmpty(t, stats.ConfidenceBuckets)
}

func TestConfidenceBucket(t *testing.T) {
	tests := []struct {
		confidence float64
		expected   string
	}{
		{0.95, "high (90-100%)"},
		{0.85, "good (70-90%)"},
		{0.55, "medium (50-70%)"},
		{0.35, "low (30-50%)"},
		{0.15, "critical (<30%)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := confidenceBucket(tt.confidence)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDecayResult_Duration(t *testing.T) {
	store := createTestStore(t)
	cfg := DefaultDecayConfig()
	d := NewDecayer(store, cfg)
	ctx := context.Background()

	result, err := d.ApplyDecay(ctx)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, result.Duration, time.Duration(0))
}
