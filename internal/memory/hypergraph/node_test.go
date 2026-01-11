package hypergraph

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewNode(t *testing.T) {
	node := NewNode(NodeTypeFact, "test content")

	assert.NotEmpty(t, node.ID)
	assert.Equal(t, NodeTypeFact, node.Type)
	assert.Equal(t, "test content", node.Content)
	assert.Equal(t, TierTask, node.Tier)
	assert.Equal(t, 1.0, node.Confidence)
	assert.False(t, node.CreatedAt.IsZero())
	assert.False(t, node.UpdatedAt.IsZero())
}

func TestStore_CreateNode(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	node := NewNode(NodeTypeEntity, "test.go")
	node.Subtype = "file"

	err = store.CreateNode(ctx, node)
	require.NoError(t, err)

	// Verify node was created
	got, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)

	assert.Equal(t, node.ID, got.ID)
	assert.Equal(t, NodeTypeEntity, got.Type)
	assert.Equal(t, "file", got.Subtype)
	assert.Equal(t, "test.go", got.Content)
	assert.Equal(t, TierTask, got.Tier)
}

func TestStore_CreateNode_WithProvenance(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	prov := Provenance{
		File:       "internal/memory/node.go",
		Line:       42,
		CommitHash: "abc123",
		Source:     "agent",
	}
	provJSON, _ := json.Marshal(prov)

	node := NewNode(NodeTypeSnippet, "func foo() {}")
	node.Provenance = provJSON

	err = store.CreateNode(ctx, node)
	require.NoError(t, err)

	got, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)

	var gotProv Provenance
	err = json.Unmarshal(got.Provenance, &gotProv)
	require.NoError(t, err)

	assert.Equal(t, prov.File, gotProv.File)
	assert.Equal(t, prov.Line, gotProv.Line)
	assert.Equal(t, prov.CommitHash, gotProv.CommitHash)
}

func TestStore_UpdateNode(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	node := NewNode(NodeTypeFact, "original")
	err = store.CreateNode(ctx, node)
	require.NoError(t, err)

	// Update the node
	node.Content = "updated"
	node.Tier = TierSession
	node.Confidence = 0.8

	err = store.UpdateNode(ctx, node)
	require.NoError(t, err)

	// Verify update
	got, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)

	assert.Equal(t, "updated", got.Content)
	assert.Equal(t, TierSession, got.Tier)
	assert.Equal(t, 0.8, got.Confidence)
}

func TestStore_UpdateNode_NotFound(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	node := NewNode(NodeTypeFact, "test")
	node.ID = "nonexistent"

	err = store.UpdateNode(ctx, node)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestStore_DeleteNode(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	node := NewNode(NodeTypeFact, "to be deleted")
	err = store.CreateNode(ctx, node)
	require.NoError(t, err)

	err = store.DeleteNode(ctx, node.ID)
	require.NoError(t, err)

	// Verify deletion
	_, err = store.GetNode(ctx, node.ID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestStore_DeleteNode_NotFound(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	err = store.DeleteNode(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestStore_IncrementAccess(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	node := NewNode(NodeTypeFact, "test")
	err = store.CreateNode(ctx, node)
	require.NoError(t, err)

	assert.Equal(t, 0, node.AccessCount)

	// Increment access
	err = store.IncrementAccess(ctx, node.ID)
	require.NoError(t, err)

	err = store.IncrementAccess(ctx, node.ID)
	require.NoError(t, err)

	got, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)

	assert.Equal(t, 2, got.AccessCount)
	assert.NotNil(t, got.LastAccessed)
}

func TestStore_ListNodes_NoFilter(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create test nodes
	for i := 0; i < 5; i++ {
		node := NewNode(NodeTypeFact, "test")
		err = store.CreateNode(ctx, node)
		require.NoError(t, err)
	}

	nodes, err := store.ListNodes(ctx, NodeFilter{})
	require.NoError(t, err)
	assert.Len(t, nodes, 5)
}

func TestStore_ListNodes_ByType(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create mixed nodes
	types := []NodeType{NodeTypeFact, NodeTypeFact, NodeTypeEntity, NodeTypeSnippet}
	for _, nt := range types {
		node := NewNode(nt, "test")
		err = store.CreateNode(ctx, node)
		require.NoError(t, err)
	}

	nodes, err := store.ListNodes(ctx, NodeFilter{Types: []NodeType{NodeTypeFact}})
	require.NoError(t, err)
	assert.Len(t, nodes, 2)

	for _, n := range nodes {
		assert.Equal(t, NodeTypeFact, n.Type)
	}
}

func TestStore_ListNodes_ByTier(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create nodes in different tiers
	tiers := []Tier{TierTask, TierTask, TierSession, TierLongterm}
	for _, tier := range tiers {
		node := NewNode(NodeTypeFact, "test")
		node.Tier = tier
		err = store.CreateNode(ctx, node)
		require.NoError(t, err)
	}

	nodes, err := store.ListNodes(ctx, NodeFilter{Tiers: []Tier{TierTask}})
	require.NoError(t, err)
	assert.Len(t, nodes, 2)
}

func TestStore_ListNodes_ByConfidence(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create nodes with different confidence levels
	confidences := []float64{0.3, 0.5, 0.7, 0.9}
	for _, c := range confidences {
		node := NewNode(NodeTypeFact, "test")
		node.Confidence = c
		err = store.CreateNode(ctx, node)
		require.NoError(t, err)
	}

	nodes, err := store.ListNodes(ctx, NodeFilter{MinConfidence: 0.6})
	require.NoError(t, err)
	assert.Len(t, nodes, 2)

	for _, n := range nodes {
		assert.GreaterOrEqual(t, n.Confidence, 0.6)
	}
}

func TestStore_ListNodes_WithPagination(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create 10 nodes
	for i := 0; i < 10; i++ {
		node := NewNode(NodeTypeFact, "test")
		err = store.CreateNode(ctx, node)
		require.NoError(t, err)
	}

	// Get first page
	page1, err := store.ListNodes(ctx, NodeFilter{Limit: 3})
	require.NoError(t, err)
	assert.Len(t, page1, 3)

	// Get second page
	page2, err := store.ListNodes(ctx, NodeFilter{Limit: 3, Offset: 3})
	require.NoError(t, err)
	assert.Len(t, page2, 3)

	// Ensure different nodes
	assert.NotEqual(t, page1[0].ID, page2[0].ID)
}

func TestStore_CountNodes(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create mixed nodes
	for i := 0; i < 3; i++ {
		node := NewNode(NodeTypeFact, "test")
		err = store.CreateNode(ctx, node)
		require.NoError(t, err)
	}
	for i := 0; i < 2; i++ {
		node := NewNode(NodeTypeEntity, "test")
		err = store.CreateNode(ctx, node)
		require.NoError(t, err)
	}

	// Count all
	count, err := store.CountNodes(ctx, NodeFilter{})
	require.NoError(t, err)
	assert.Equal(t, int64(5), count)

	// Count by type
	count, err = store.CountNodes(ctx, NodeFilter{Types: []NodeType{NodeTypeFact}})
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}
