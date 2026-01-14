package hypergraph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// backendTestSuite runs the same tests against any Backend implementation.
func runBackendTests(t *testing.T, newBackend func() Backend) {
	t.Run("CreateNode", func(t *testing.T) {
		backend := newBackend()
		defer backend.Close()
		ctx := context.Background()

		node := NewNode(NodeTypeEntity, "test content")
		err := backend.CreateNode(ctx, node)
		require.NoError(t, err)
		assert.NotEmpty(t, node.ID)

		retrieved, err := backend.GetNode(ctx, node.ID)
		require.NoError(t, err)
		assert.Equal(t, node.ID, retrieved.ID)
		assert.Equal(t, "test content", retrieved.Content)
		assert.Equal(t, NodeTypeEntity, retrieved.Type)
	})

	t.Run("GetNode_NotFound", func(t *testing.T) {
		backend := newBackend()
		defer backend.Close()
		ctx := context.Background()

		_, err := backend.GetNode(ctx, "nonexistent")
		require.Error(t, err)
		assert.True(t, IsNotFound(err))
	})

	t.Run("UpdateNode", func(t *testing.T) {
		backend := newBackend()
		defer backend.Close()
		ctx := context.Background()

		node := NewNode(NodeTypeEntity, "original")
		require.NoError(t, backend.CreateNode(ctx, node))

		node.Content = "updated"
		require.NoError(t, backend.UpdateNode(ctx, node))

		retrieved, err := backend.GetNode(ctx, node.ID)
		require.NoError(t, err)
		assert.Equal(t, "updated", retrieved.Content)
	})

	t.Run("DeleteNode", func(t *testing.T) {
		backend := newBackend()
		defer backend.Close()
		ctx := context.Background()

		node := NewNode(NodeTypeEntity, "to delete")
		require.NoError(t, backend.CreateNode(ctx, node))

		require.NoError(t, backend.DeleteNode(ctx, node.ID))

		_, err := backend.GetNode(ctx, node.ID)
		require.Error(t, err)
		assert.True(t, IsNotFound(err))
	})

	t.Run("ListNodes", func(t *testing.T) {
		backend := newBackend()
		defer backend.Close()
		ctx := context.Background()

		// Create nodes of different types
		for i := 0; i < 3; i++ {
			require.NoError(t, backend.CreateNode(ctx, NewNode(NodeTypeEntity, "entity")))
		}
		for i := 0; i < 2; i++ {
			require.NoError(t, backend.CreateNode(ctx, NewNode(NodeTypeFact, "fact")))
		}

		// List all
		nodes, err := backend.ListNodes(ctx, NodeFilter{})
		require.NoError(t, err)
		assert.Len(t, nodes, 5)

		// Filter by type
		nodes, err = backend.ListNodes(ctx, NodeFilter{Types: []NodeType{NodeTypeEntity}})
		require.NoError(t, err)
		assert.Len(t, nodes, 3)

		// With limit
		nodes, err = backend.ListNodes(ctx, NodeFilter{Limit: 2})
		require.NoError(t, err)
		assert.Len(t, nodes, 2)
	})

	t.Run("CountNodes", func(t *testing.T) {
		backend := newBackend()
		defer backend.Close()
		ctx := context.Background()

		for i := 0; i < 5; i++ {
			require.NoError(t, backend.CreateNode(ctx, NewNode(NodeTypeEntity, "test")))
		}

		count, err := backend.CountNodes(ctx, NodeFilter{})
		require.NoError(t, err)
		assert.Equal(t, int64(5), count)
	})

	t.Run("IncrementAccess", func(t *testing.T) {
		backend := newBackend()
		defer backend.Close()
		ctx := context.Background()

		node := NewNode(NodeTypeEntity, "test")
		require.NoError(t, backend.CreateNode(ctx, node))

		require.NoError(t, backend.IncrementAccess(ctx, node.ID))
		require.NoError(t, backend.IncrementAccess(ctx, node.ID))

		retrieved, err := backend.GetNode(ctx, node.ID)
		require.NoError(t, err)
		assert.Equal(t, 2, retrieved.AccessCount)
		assert.NotNil(t, retrieved.LastAccessed)
	})

	t.Run("CreateHyperedge", func(t *testing.T) {
		backend := newBackend()
		defer backend.Close()
		ctx := context.Background()

		edge := NewHyperedge(HyperedgeRelation, "test relation")
		require.NoError(t, backend.CreateHyperedge(ctx, edge))
		assert.NotEmpty(t, edge.ID)

		retrieved, err := backend.GetHyperedge(ctx, edge.ID)
		require.NoError(t, err)
		assert.Equal(t, "test relation", retrieved.Label)
	})

	t.Run("UpdateHyperedge", func(t *testing.T) {
		backend := newBackend()
		defer backend.Close()
		ctx := context.Background()

		edge := NewHyperedge(HyperedgeRelation, "original")
		require.NoError(t, backend.CreateHyperedge(ctx, edge))

		edge.Label = "updated"
		require.NoError(t, backend.UpdateHyperedge(ctx, edge))

		retrieved, err := backend.GetHyperedge(ctx, edge.ID)
		require.NoError(t, err)
		assert.Equal(t, "updated", retrieved.Label)
	})

	t.Run("DeleteHyperedge", func(t *testing.T) {
		backend := newBackend()
		defer backend.Close()
		ctx := context.Background()

		edge := NewHyperedge(HyperedgeRelation, "to delete")
		require.NoError(t, backend.CreateHyperedge(ctx, edge))

		require.NoError(t, backend.DeleteHyperedge(ctx, edge.ID))

		_, err := backend.GetHyperedge(ctx, edge.ID)
		require.Error(t, err)
		assert.True(t, IsNotFound(err))
	})

	t.Run("Membership", func(t *testing.T) {
		backend := newBackend()
		defer backend.Close()
		ctx := context.Background()

		// Create nodes and edge
		node1 := NewNode(NodeTypeEntity, "subject")
		node2 := NewNode(NodeTypeEntity, "object")
		require.NoError(t, backend.CreateNode(ctx, node1))
		require.NoError(t, backend.CreateNode(ctx, node2))

		edge := NewHyperedge(HyperedgeRelation, "connects")
		require.NoError(t, backend.CreateHyperedge(ctx, edge))

		// Add members
		require.NoError(t, backend.AddMember(ctx, Membership{
			HyperedgeID: edge.ID,
			NodeID:      node1.ID,
			Role:        RoleSubject,
			Position:    0,
		}))
		require.NoError(t, backend.AddMember(ctx, Membership{
			HyperedgeID: edge.ID,
			NodeID:      node2.ID,
			Role:        RoleObject,
			Position:    1,
		}))

		// Get members
		members, err := backend.GetMembers(ctx, edge.ID)
		require.NoError(t, err)
		assert.Len(t, members, 2)

		// Get member nodes
		nodes, err := backend.GetMemberNodes(ctx, edge.ID)
		require.NoError(t, err)
		assert.Len(t, nodes, 2)

		// Get node hyperedges
		edges, err := backend.GetNodeHyperedges(ctx, node1.ID)
		require.NoError(t, err)
		assert.Len(t, edges, 1)

		// Remove member
		require.NoError(t, backend.RemoveMember(ctx, edge.ID, node1.ID, RoleSubject))
		members, err = backend.GetMembers(ctx, edge.ID)
		require.NoError(t, err)
		assert.Len(t, members, 1)
	})

	t.Run("SearchByContent", func(t *testing.T) {
		backend := newBackend()
		defer backend.Close()
		ctx := context.Background()

		node1 := NewNode(NodeTypeEntity, "hello world")
		node2 := NewNode(NodeTypeEntity, "goodbye world")
		node3 := NewNode(NodeTypeEntity, "hello hello")
		require.NoError(t, backend.CreateNode(ctx, node1))
		require.NoError(t, backend.CreateNode(ctx, node2))
		require.NoError(t, backend.CreateNode(ctx, node3))

		results, err := backend.SearchByContent(ctx, "hello", SearchOptions{})
		require.NoError(t, err)
		assert.Len(t, results, 2)

		results, err = backend.SearchByContent(ctx, "world", SearchOptions{})
		require.NoError(t, err)
		assert.Len(t, results, 2)

		results, err = backend.SearchByContent(ctx, "goodbye", SearchOptions{})
		require.NoError(t, err)
		assert.Len(t, results, 1)
	})

	t.Run("GetConnected", func(t *testing.T) {
		backend := newBackend()
		defer backend.Close()
		ctx := context.Background()

		// Create a simple graph: A -> B -> C
		nodeA := NewNode(NodeTypeEntity, "A")
		nodeB := NewNode(NodeTypeEntity, "B")
		nodeC := NewNode(NodeTypeEntity, "C")
		require.NoError(t, backend.CreateNode(ctx, nodeA))
		require.NoError(t, backend.CreateNode(ctx, nodeB))
		require.NoError(t, backend.CreateNode(ctx, nodeC))

		edge1 := NewHyperedge(HyperedgeRelation, "A to B")
		edge2 := NewHyperedge(HyperedgeRelation, "B to C")
		require.NoError(t, backend.CreateHyperedge(ctx, edge1))
		require.NoError(t, backend.CreateHyperedge(ctx, edge2))

		// A -> B
		require.NoError(t, backend.AddMember(ctx, Membership{HyperedgeID: edge1.ID, NodeID: nodeA.ID, Role: RoleSubject, Position: 0}))
		require.NoError(t, backend.AddMember(ctx, Membership{HyperedgeID: edge1.ID, NodeID: nodeB.ID, Role: RoleObject, Position: 1}))

		// B -> C
		require.NoError(t, backend.AddMember(ctx, Membership{HyperedgeID: edge2.ID, NodeID: nodeB.ID, Role: RoleSubject, Position: 0}))
		require.NoError(t, backend.AddMember(ctx, Membership{HyperedgeID: edge2.ID, NodeID: nodeC.ID, Role: RoleObject, Position: 1}))

		// Get immediate connections from A
		connected, err := backend.GetConnected(ctx, nodeA.ID, TraversalOptions{MaxDepth: 1})
		require.NoError(t, err)
		assert.Len(t, connected, 1)
		assert.Equal(t, nodeB.ID, connected[0].Node.ID)

		// Get connections with depth 2
		connected, err = backend.GetConnected(ctx, nodeA.ID, TraversalOptions{MaxDepth: 2})
		require.NoError(t, err)
		assert.Len(t, connected, 2)
	})

	t.Run("RecentNodes", func(t *testing.T) {
		backend := newBackend()
		defer backend.Close()
		ctx := context.Background()

		node1 := NewNode(NodeTypeEntity, "first")
		node2 := NewNode(NodeTypeEntity, "second")
		require.NoError(t, backend.CreateNode(ctx, node1))
		require.NoError(t, backend.CreateNode(ctx, node2))

		// Access node1 multiple times to ensure it's more recent
		require.NoError(t, backend.IncrementAccess(ctx, node1.ID))
		require.NoError(t, backend.IncrementAccess(ctx, node1.ID))

		nodes, err := backend.RecentNodes(ctx, 10, nil)
		require.NoError(t, err)
		assert.Len(t, nodes, 2)

		// Verify both nodes are returned (order may vary by backend)
		ids := []string{nodes[0].ID, nodes[1].ID}
		assert.Contains(t, ids, node1.ID)
		assert.Contains(t, ids, node2.ID)
	})

	t.Run("Stats", func(t *testing.T) {
		backend := newBackend()
		defer backend.Close()
		ctx := context.Background()

		for i := 0; i < 3; i++ {
			require.NoError(t, backend.CreateNode(ctx, NewNode(NodeTypeEntity, "entity")))
		}
		require.NoError(t, backend.CreateHyperedge(ctx, NewHyperedge(HyperedgeRelation, "edge")))

		stats, err := backend.Stats(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(3), stats.NodeCount)
		assert.Equal(t, int64(1), stats.HyperedgeCount)
		assert.Equal(t, int64(3), stats.NodesByType[string(NodeTypeEntity)])
	})
}

func TestInMemoryBackend(t *testing.T) {
	runBackendTests(t, func() Backend {
		return NewInMemoryBackend()
	})
}

func TestSQLiteBackend(t *testing.T) {
	runBackendTests(t, func() Backend {
		backend, err := NewSQLiteBackend(SQLiteBackendOptions{})
		if err != nil {
			t.Fatalf("failed to create SQLite backend: %v", err)
		}
		return backend
	})
}

func TestIsNotFound(t *testing.T) {
	err := &ErrNotFound{Entity: "node", ID: "123"}
	assert.True(t, IsNotFound(err))
	assert.Equal(t, "node not found: 123", err.Error())

	assert.False(t, IsNotFound(nil))
	assert.False(t, IsNotFound(assert.AnError))
}

// Benchmarks

func BenchmarkInMemoryBackend_CreateNode(b *testing.B) {
	backend := NewInMemoryBackend()
	defer backend.Close()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		backend.CreateNode(ctx, NewNode(NodeTypeEntity, "test content"))
	}
}

func BenchmarkSQLiteBackend_CreateNode(b *testing.B) {
	backend, _ := NewSQLiteBackend(SQLiteBackendOptions{})
	defer backend.Close()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		backend.CreateNode(ctx, NewNode(NodeTypeEntity, "test content"))
	}
}

func BenchmarkInMemoryBackend_SearchByContent(b *testing.B) {
	backend := NewInMemoryBackend()
	defer backend.Close()
	ctx := context.Background()

	// Pre-populate
	for i := 0; i < 1000; i++ {
		backend.CreateNode(ctx, NewNode(NodeTypeEntity, "hello world content"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		backend.SearchByContent(ctx, "world", SearchOptions{Limit: 10})
	}
}

func BenchmarkSQLiteBackend_SearchByContent(b *testing.B) {
	backend, _ := NewSQLiteBackend(SQLiteBackendOptions{})
	defer backend.Close()
	ctx := context.Background()

	// Pre-populate
	for i := 0; i < 1000; i++ {
		backend.CreateNode(ctx, NewNode(NodeTypeEntity, "hello world content"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		backend.SearchByContent(ctx, "world", SearchOptions{Limit: 10})
	}
}
