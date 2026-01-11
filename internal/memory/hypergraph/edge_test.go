package hypergraph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHyperedge(t *testing.T) {
	edge := NewHyperedge(HyperedgeRelation, "contains")

	assert.NotEmpty(t, edge.ID)
	assert.Equal(t, HyperedgeRelation, edge.Type)
	assert.Equal(t, "contains", edge.Label)
	assert.Equal(t, 1.0, edge.Weight)
	assert.False(t, edge.CreatedAt.IsZero())
}

func TestStore_CreateHyperedge(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	edge := NewHyperedge(HyperedgeRelation, "calls")

	err = store.CreateHyperedge(ctx, edge)
	require.NoError(t, err)

	got, err := store.GetHyperedge(ctx, edge.ID)
	require.NoError(t, err)

	assert.Equal(t, edge.ID, got.ID)
	assert.Equal(t, HyperedgeRelation, got.Type)
	assert.Equal(t, "calls", got.Label)
}

func TestStore_UpdateHyperedge(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	edge := NewHyperedge(HyperedgeRelation, "original")
	err = store.CreateHyperedge(ctx, edge)
	require.NoError(t, err)

	edge.Label = "updated"
	edge.Weight = 2.5

	err = store.UpdateHyperedge(ctx, edge)
	require.NoError(t, err)

	got, err := store.GetHyperedge(ctx, edge.ID)
	require.NoError(t, err)

	assert.Equal(t, "updated", got.Label)
	assert.Equal(t, 2.5, got.Weight)
}

func TestStore_DeleteHyperedge(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	edge := NewHyperedge(HyperedgeRelation, "to delete")
	err = store.CreateHyperedge(ctx, edge)
	require.NoError(t, err)

	err = store.DeleteHyperedge(ctx, edge.ID)
	require.NoError(t, err)

	_, err = store.GetHyperedge(ctx, edge.ID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestStore_Membership(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create nodes
	node1 := NewNode(NodeTypeEntity, "file.go")
	node2 := NewNode(NodeTypeEntity, "func main")
	err = store.CreateNode(ctx, node1)
	require.NoError(t, err)
	err = store.CreateNode(ctx, node2)
	require.NoError(t, err)

	// Create hyperedge
	edge := NewHyperedge(HyperedgeRelation, "contains")
	err = store.CreateHyperedge(ctx, edge)
	require.NoError(t, err)

	// Add members
	err = store.AddMember(ctx, Membership{
		HyperedgeID: edge.ID,
		NodeID:      node1.ID,
		Role:        RoleSubject,
		Position:    0,
	})
	require.NoError(t, err)

	err = store.AddMember(ctx, Membership{
		HyperedgeID: edge.ID,
		NodeID:      node2.ID,
		Role:        RoleObject,
		Position:    1,
	})
	require.NoError(t, err)

	// Get members
	members, err := store.GetMembers(ctx, edge.ID)
	require.NoError(t, err)
	assert.Len(t, members, 2)

	assert.Equal(t, node1.ID, members[0].NodeID)
	assert.Equal(t, RoleSubject, members[0].Role)
	assert.Equal(t, node2.ID, members[1].NodeID)
	assert.Equal(t, RoleObject, members[1].Role)
}

func TestStore_RemoveMember(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create node and hyperedge
	node := NewNode(NodeTypeEntity, "test")
	err = store.CreateNode(ctx, node)
	require.NoError(t, err)

	edge := NewHyperedge(HyperedgeRelation, "test")
	err = store.CreateHyperedge(ctx, edge)
	require.NoError(t, err)

	// Add member
	err = store.AddMember(ctx, Membership{
		HyperedgeID: edge.ID,
		NodeID:      node.ID,
		Role:        RoleSubject,
	})
	require.NoError(t, err)

	// Remove member
	err = store.RemoveMember(ctx, edge.ID, node.ID, RoleSubject)
	require.NoError(t, err)

	// Verify removed
	members, err := store.GetMembers(ctx, edge.ID)
	require.NoError(t, err)
	assert.Len(t, members, 0)
}

func TestStore_GetMemberNodes(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create nodes
	node1 := NewNode(NodeTypeEntity, "node1")
	node2 := NewNode(NodeTypeEntity, "node2")
	err = store.CreateNode(ctx, node1)
	require.NoError(t, err)
	err = store.CreateNode(ctx, node2)
	require.NoError(t, err)

	// Create hyperedge with members
	edge := NewHyperedge(HyperedgeComposition, "group")
	err = store.CreateHyperedge(ctx, edge)
	require.NoError(t, err)

	err = store.AddMember(ctx, Membership{HyperedgeID: edge.ID, NodeID: node1.ID, Role: RoleParticipant, Position: 0})
	require.NoError(t, err)
	err = store.AddMember(ctx, Membership{HyperedgeID: edge.ID, NodeID: node2.ID, Role: RoleParticipant, Position: 1})
	require.NoError(t, err)

	// Get member nodes
	nodes, err := store.GetMemberNodes(ctx, edge.ID)
	require.NoError(t, err)
	assert.Len(t, nodes, 2)

	assert.Equal(t, "node1", nodes[0].Content)
	assert.Equal(t, "node2", nodes[1].Content)
}

func TestStore_GetNodeHyperedges(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create a node
	node := NewNode(NodeTypeEntity, "shared node")
	err = store.CreateNode(ctx, node)
	require.NoError(t, err)

	// Create multiple hyperedges containing the node
	edge1 := NewHyperedge(HyperedgeRelation, "relation1")
	edge2 := NewHyperedge(HyperedgeRelation, "relation2")
	err = store.CreateHyperedge(ctx, edge1)
	require.NoError(t, err)
	err = store.CreateHyperedge(ctx, edge2)
	require.NoError(t, err)

	err = store.AddMember(ctx, Membership{HyperedgeID: edge1.ID, NodeID: node.ID, Role: RoleSubject})
	require.NoError(t, err)
	err = store.AddMember(ctx, Membership{HyperedgeID: edge2.ID, NodeID: node.ID, Role: RoleObject})
	require.NoError(t, err)

	// Get all hyperedges for the node
	edges, err := store.GetNodeHyperedges(ctx, node.ID)
	require.NoError(t, err)
	assert.Len(t, edges, 2)
}

func TestStore_ListHyperedges(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create hyperedges of different types
	types := []HyperedgeType{HyperedgeRelation, HyperedgeRelation, HyperedgeCausation, HyperedgeContext}
	for _, et := range types {
		edge := NewHyperedge(et, "test")
		err = store.CreateHyperedge(ctx, edge)
		require.NoError(t, err)
	}

	// List all
	edges, err := store.ListHyperedges(ctx, HyperedgeFilter{})
	require.NoError(t, err)
	assert.Len(t, edges, 4)

	// List by type
	edges, err = store.ListHyperedges(ctx, HyperedgeFilter{Types: []HyperedgeType{HyperedgeRelation}})
	require.NoError(t, err)
	assert.Len(t, edges, 2)
}

func TestStore_ListHyperedges_ByWeight(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create hyperedges with different weights
	weights := []float64{0.5, 1.0, 1.5, 2.0}
	for _, w := range weights {
		edge := NewHyperedge(HyperedgeRelation, "test")
		edge.Weight = w
		err = store.CreateHyperedge(ctx, edge)
		require.NoError(t, err)
	}

	// Filter by minimum weight
	edges, err := store.ListHyperedges(ctx, HyperedgeFilter{MinWeight: 1.5})
	require.NoError(t, err)
	assert.Len(t, edges, 2)

	for _, e := range edges {
		assert.GreaterOrEqual(t, e.Weight, 1.5)
	}
}

func TestStore_CreateRelation(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create nodes
	node1 := NewNode(NodeTypeEntity, "file.go")
	node2 := NewNode(NodeTypeEntity, "func main")
	err = store.CreateNode(ctx, node1)
	require.NoError(t, err)
	err = store.CreateNode(ctx, node2)
	require.NoError(t, err)

	// Create relation
	edge, err := store.CreateRelation(ctx, "contains", node1.ID, node2.ID)
	require.NoError(t, err)

	assert.NotEmpty(t, edge.ID)
	assert.Equal(t, HyperedgeRelation, edge.Type)
	assert.Equal(t, "contains", edge.Label)

	// Verify members
	members, err := store.GetMembers(ctx, edge.ID)
	require.NoError(t, err)
	assert.Len(t, members, 2)

	assert.Equal(t, node1.ID, members[0].NodeID)
	assert.Equal(t, RoleSubject, members[0].Role)
	assert.Equal(t, node2.ID, members[1].NodeID)
	assert.Equal(t, RoleObject, members[1].Role)
}

func TestStore_CascadeDeleteHyperedge(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create node and hyperedge with membership
	node := NewNode(NodeTypeEntity, "test")
	err = store.CreateNode(ctx, node)
	require.NoError(t, err)

	edge := NewHyperedge(HyperedgeRelation, "test")
	err = store.CreateHyperedge(ctx, edge)
	require.NoError(t, err)

	err = store.AddMember(ctx, Membership{HyperedgeID: edge.ID, NodeID: node.ID, Role: RoleSubject})
	require.NoError(t, err)

	// Delete hyperedge - membership should be cascade deleted
	err = store.DeleteHyperedge(ctx, edge.ID)
	require.NoError(t, err)

	// Verify membership is gone (querying directly since GetMembers returns empty for nonexistent edge)
	var count int
	err = store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM membership WHERE hyperedge_id = ?", edge.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
