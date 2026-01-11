package hypergraph

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestGraph(t *testing.T, store *Store) (nodes []*Node, edges []*Hyperedge) {
	t.Helper()
	ctx := context.Background()

	// Create nodes
	// A -> B -> C (linear chain)
	// A -> D (branch)
	nodeA := NewNode(NodeTypeEntity, "node A content")
	nodeA.Subtype = "file"
	nodeB := NewNode(NodeTypeEntity, "node B content")
	nodeB.Subtype = "function"
	nodeC := NewNode(NodeTypeFact, "node C is a fact")
	nodeD := NewNode(NodeTypeSnippet, "node D snippet content")

	for _, n := range []*Node{nodeA, nodeB, nodeC, nodeD} {
		err := store.CreateNode(ctx, n)
		require.NoError(t, err)
	}

	// Create edges
	edgeAB, err := store.CreateRelation(ctx, "contains", nodeA.ID, nodeB.ID)
	require.NoError(t, err)

	edgeBC, err := store.CreateRelation(ctx, "calls", nodeB.ID, nodeC.ID)
	require.NoError(t, err)

	edgeAD, err := store.CreateRelation(ctx, "references", nodeA.ID, nodeD.ID)
	require.NoError(t, err)

	return []*Node{nodeA, nodeB, nodeC, nodeD}, []*Hyperedge{edgeAB, edgeBC, edgeAD}
}

func TestStore_SearchByContent(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create test nodes
	node1 := NewNode(NodeTypeFact, "The quick brown fox jumps")
	node2 := NewNode(NodeTypeFact, "A lazy dog sleeps")
	node3 := NewNode(NodeTypeEntity, "Fox class definition")

	for _, n := range []*Node{node1, node2, node3} {
		err := store.CreateNode(ctx, n)
		require.NoError(t, err)
	}

	// Search for "fox"
	results, err := store.SearchByContent(ctx, "fox", SearchOptions{})
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Search with type filter
	results, err = store.SearchByContent(ctx, "fox", SearchOptions{Types: []NodeType{NodeTypeFact}})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, NodeTypeFact, results[0].Node.Type)
}

func TestStore_SearchByContent_WithLimit(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create many nodes with same content
	for i := 0; i < 10; i++ {
		node := NewNode(NodeTypeFact, "matching content here")
		err := store.CreateNode(ctx, node)
		require.NoError(t, err)
	}

	results, err := store.SearchByContent(ctx, "matching", SearchOptions{Limit: 5})
	require.NoError(t, err)
	assert.Len(t, results, 5)
}

func TestStore_SearchByContent_ExcludesArchived(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create active and archived nodes
	activeNode := NewNode(NodeTypeFact, "searchable content")
	archivedNode := NewNode(NodeTypeFact, "searchable content")
	archivedNode.Tier = TierArchive

	err = store.CreateNode(ctx, activeNode)
	require.NoError(t, err)
	err = store.CreateNode(ctx, archivedNode)
	require.NoError(t, err)

	results, err := store.SearchByContent(ctx, "searchable", SearchOptions{})
	require.NoError(t, err)
	assert.Len(t, results, 1) // Only active node
	assert.Equal(t, TierTask, results[0].Node.Tier)
}

func TestStore_GetConnected_Immediate(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	nodes, _ := setupTestGraph(t, store)
	ctx := context.Background()

	// Get nodes connected to A (should be B and D)
	connected, err := store.GetConnected(ctx, nodes[0].ID, TraversalOptions{
		Direction: TraverseBoth,
		MaxDepth:  1,
	})
	require.NoError(t, err)
	assert.Len(t, connected, 2)

	// Verify we got B and D
	connectedIDs := make(map[string]bool)
	for _, c := range connected {
		connectedIDs[c.Node.ID] = true
	}
	assert.True(t, connectedIDs[nodes[1].ID]) // B
	assert.True(t, connectedIDs[nodes[3].ID]) // D
}

func TestStore_GetConnected_MultiHop(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	nodes, _ := setupTestGraph(t, store)
	ctx := context.Background()

	// Get nodes within 2 hops of A (should be B, C, D)
	connected, err := store.GetConnected(ctx, nodes[0].ID, TraversalOptions{
		Direction: TraverseBoth,
		MaxDepth:  2,
	})
	require.NoError(t, err)
	assert.Len(t, connected, 3)

	// Verify depths
	depths := make(map[string]int)
	for _, c := range connected {
		depths[c.Node.ID] = c.Depth
	}
	assert.Equal(t, 1, depths[nodes[1].ID]) // B at depth 1
	assert.Equal(t, 1, depths[nodes[3].ID]) // D at depth 1
	assert.Equal(t, 2, depths[nodes[2].ID]) // C at depth 2
}

func TestStore_GetConnected_Outgoing(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	nodes, _ := setupTestGraph(t, store)
	ctx := context.Background()

	// Get outgoing connections from A
	connected, err := store.GetConnected(ctx, nodes[0].ID, TraversalOptions{
		Direction: TraverseOutgoing,
		MaxDepth:  1,
	})
	require.NoError(t, err)

	// A is subject in edges to B and D
	connectedIDs := make(map[string]bool)
	for _, c := range connected {
		connectedIDs[c.Node.ID] = true
	}
	assert.True(t, connectedIDs[nodes[1].ID]) // B
	assert.True(t, connectedIDs[nodes[3].ID]) // D
}

func TestStore_GetConnected_Incoming(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	nodes, _ := setupTestGraph(t, store)
	ctx := context.Background()

	// Get incoming connections to B (should be A)
	connected, err := store.GetConnected(ctx, nodes[1].ID, TraversalOptions{
		Direction: TraverseIncoming,
		MaxDepth:  1,
	})
	require.NoError(t, err)

	// B is object in edge from A
	assert.Len(t, connected, 1)
	assert.Equal(t, nodes[0].ID, connected[0].Node.ID) // A
}

func TestStore_GetConnected_WithEdgeTypeFilter(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create nodes
	nodeA := NewNode(NodeTypeEntity, "A")
	nodeB := NewNode(NodeTypeEntity, "B")
	nodeC := NewNode(NodeTypeEntity, "C")
	for _, n := range []*Node{nodeA, nodeB, nodeC} {
		err := store.CreateNode(ctx, n)
		require.NoError(t, err)
	}

	// Create different edge types
	edge1 := NewHyperedge(HyperedgeRelation, "rel")
	edge2 := NewHyperedge(HyperedgeCausation, "cause")
	store.CreateHyperedge(ctx, edge1)
	store.CreateHyperedge(ctx, edge2)

	store.AddMember(ctx, Membership{HyperedgeID: edge1.ID, NodeID: nodeA.ID, Role: RoleSubject})
	store.AddMember(ctx, Membership{HyperedgeID: edge1.ID, NodeID: nodeB.ID, Role: RoleObject})
	store.AddMember(ctx, Membership{HyperedgeID: edge2.ID, NodeID: nodeA.ID, Role: RoleSubject})
	store.AddMember(ctx, Membership{HyperedgeID: edge2.ID, NodeID: nodeC.ID, Role: RoleObject})

	// Filter by relation type only
	connected, err := store.GetConnected(ctx, nodeA.ID, TraversalOptions{
		EdgeTypes: []HyperedgeType{HyperedgeRelation},
		MaxDepth:  1,
	})
	require.NoError(t, err)
	assert.Len(t, connected, 1)
	assert.Equal(t, nodeB.ID, connected[0].Node.ID)
}

func TestStore_GetConnected_IncludeEdge(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	nodes, _ := setupTestGraph(t, store)
	ctx := context.Background()

	connected, err := store.GetConnected(ctx, nodes[0].ID, TraversalOptions{
		MaxDepth:    1,
		IncludeEdge: true,
	})
	require.NoError(t, err)

	for _, c := range connected {
		assert.NotNil(t, c.Edge, "edge should be included")
		assert.NotEmpty(t, c.Edge.ID)
	}
}

func TestStore_GetSubgraph(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	nodes, edges := setupTestGraph(t, store)
	ctx := context.Background()

	// Get subgraph around node A with depth 1
	subgraph, err := store.GetSubgraph(ctx, []string{nodes[0].ID}, 1)
	require.NoError(t, err)

	// Should include A, B, D (immediate neighbors)
	assert.GreaterOrEqual(t, len(subgraph.Nodes), 3)
	assert.GreaterOrEqual(t, len(subgraph.Hyperedges), 2)

	// Verify node A is included
	found := false
	for _, n := range subgraph.Nodes {
		if n.ID == nodes[0].ID {
			found = true
			break
		}
	}
	assert.True(t, found)

	// Get subgraph with depth 2 (should include C)
	subgraph, err = store.GetSubgraph(ctx, []string{nodes[0].ID}, 2)
	require.NoError(t, err)
	assert.Len(t, subgraph.Nodes, 4)        // All nodes
	assert.Len(t, subgraph.Hyperedges, len(edges)) // All edges
}

func TestStore_RecentNodes(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create nodes with delay between them for distinct timestamps
	var createdNodes []*Node
	for i := 0; i < 3; i++ {
		node := NewNode(NodeTypeFact, "test")
		err := store.CreateNode(ctx, node)
		require.NoError(t, err)
		createdNodes = append(createdNodes, node)
	}

	// Wait to ensure next access has a clearly different timestamp (1+ second for SQLite text comparison)
	time.Sleep(1100 * time.Millisecond)

	// Access the first created node
	targetNode := createdNodes[0]
	err = store.IncrementAccess(ctx, targetNode.ID)
	require.NoError(t, err)

	// Reload the target node to verify last_accessed was set
	reloaded, err := store.GetNode(ctx, targetNode.ID)
	require.NoError(t, err)
	require.NotNil(t, reloaded.LastAccessed, "last_accessed should be set after IncrementAccess")

	// Get recent nodes
	recent, err := store.RecentNodes(ctx, 10, nil)
	require.NoError(t, err)
	assert.Len(t, recent, 3)

	// The accessed node should be first because its last_accessed is 1+ second after others' updated_at
	assert.Equal(t, targetNode.ID, recent[0].ID)
	assert.NotNil(t, recent[0].LastAccessed)
	assert.Equal(t, 1, recent[0].AccessCount)
}

func TestStore_RecentNodes_ByTier(t *testing.T) {
	store, err := NewStore(Options{})
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create nodes in different tiers
	taskNode := NewNode(NodeTypeFact, "task")
	taskNode.Tier = TierTask

	sessionNode := NewNode(NodeTypeFact, "session")
	sessionNode.Tier = TierSession

	store.CreateNode(ctx, taskNode)
	store.CreateNode(ctx, sessionNode)

	// Get only task tier
	recent, err := store.RecentNodes(ctx, 10, []Tier{TierTask})
	require.NoError(t, err)
	assert.Len(t, recent, 1)
	assert.Equal(t, TierTask, recent[0].Tier)
}
