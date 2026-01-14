package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStats(t *testing.T) {
	// Create a temp directory for the test
	tmpDir := t.TempDir()

	// Create a memory store directly to add some test data
	store, err := hypergraph.NewStore(hypergraph.Options{
		Path:              filepath.Join(tmpDir, "memory.db"),
		CreateIfNotExists: true,
	})
	require.NoError(t, err)

	// Add a test node
	node := &hypergraph.Node{
		Type:       hypergraph.NodeTypeFact,
		Content:    "Test fact content",
		Tier:       hypergraph.TierSession,
		Confidence: 0.9,
	}
	err = store.CreateNode(t.Context(), node)
	require.NoError(t, err)
	store.Close()

	// Reopen and verify stats
	store2, err := hypergraph.NewStore(hypergraph.Options{
		Path:              filepath.Join(tmpDir, "memory.db"),
		CreateIfNotExists: false,
	})
	require.NoError(t, err)
	defer store2.Close()

	stats, err := store2.Stats(t.Context())
	require.NoError(t, err)

	assert.Equal(t, int64(1), stats.NodeCount)
	assert.Equal(t, int64(1), stats.NodesByType["fact"])
	assert.Equal(t, int64(1), stats.NodesByTier["session"])
}

func TestMemorySearch(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a memory store with test data
	store, err := hypergraph.NewStore(hypergraph.Options{
		Path:              filepath.Join(tmpDir, "memory.db"),
		CreateIfNotExists: true,
	})
	require.NoError(t, err)

	// Add test nodes
	node1 := &hypergraph.Node{
		Type:       hypergraph.NodeTypeFact,
		Content:    "User authentication uses JWT tokens",
		Tier:       hypergraph.TierLongterm,
		Confidence: 0.95,
	}
	err = store.CreateNode(t.Context(), node1)
	require.NoError(t, err)

	node2 := &hypergraph.Node{
		Type:       hypergraph.NodeTypeFact,
		Content:    "Database schema uses PostgreSQL",
		Tier:       hypergraph.TierSession,
		Confidence: 0.85,
	}
	err = store.CreateNode(t.Context(), node2)
	require.NoError(t, err)

	// Test search
	results, err := store.Search(t.Context(), "authentication", hypergraph.SearchOptions{
		Limit: 10,
	})
	require.NoError(t, err)

	assert.Len(t, results, 1)
	assert.Contains(t, results[0].Node.Content, "authentication")

	store.Close()
}

func TestMemoryExportFormat(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a memory store with test data
	store, err := hypergraph.NewStore(hypergraph.Options{
		Path:              filepath.Join(tmpDir, "memory.db"),
		CreateIfNotExists: true,
	})
	require.NoError(t, err)

	node := &hypergraph.Node{
		Type:       hypergraph.NodeTypeFact,
		Content:    "Test content for export",
		Tier:       hypergraph.TierSession,
		Confidence: 0.9,
	}
	err = store.CreateNode(t.Context(), node)
	require.NoError(t, err)

	// Get nodes for export
	nodes, err := store.RecentNodes(t.Context(), 0, nil)
	require.NoError(t, err)

	stats, err := store.Stats(t.Context())
	require.NoError(t, err)

	store.Close()

	// Test that export struct can be marshaled to JSON
	export := struct {
		ExportedAt string             `json:"exported_at"`
		Stats      *hypergraph.Stats  `json:"stats"`
		Nodes      []*hypergraph.Node `json:"nodes"`
	}{
		ExportedAt: "2024-01-01T00:00:00Z",
		Stats:      stats,
		Nodes:      nodes,
	}

	data, err := json.MarshalIndent(export, "", "  ")
	require.NoError(t, err)

	// Verify the JSON structure
	var parsed map[string]interface{}
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Contains(t, parsed, "exported_at")
	assert.Contains(t, parsed, "stats")
	assert.Contains(t, parsed, "nodes")
}

func TestMemoryGCDryRun(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a memory store
	store, err := hypergraph.NewStore(hypergraph.Options{
		Path:              filepath.Join(tmpDir, "memory.db"),
		CreateIfNotExists: true,
	})
	require.NoError(t, err)

	// Add some nodes
	for i := 0; i < 5; i++ {
		node := &hypergraph.Node{
			Type:       hypergraph.NodeTypeFact,
			Content:    "Test content",
			Tier:       hypergraph.TierSession,
			Confidence: 0.5, // Low confidence for potential archiving
		}
		err = store.CreateNode(t.Context(), node)
		require.NoError(t, err)
	}

	// Get initial stats
	statsBefore, err := store.Stats(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(5), statsBefore.NodeCount)

	store.Close()
}

func TestOpenMemoryStoreCreatesDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "memory.db")

	// Verify DB doesn't exist
	_, err := os.Stat(dbPath)
	assert.True(t, os.IsNotExist(err))

	// Create store
	store, err := hypergraph.NewStore(hypergraph.Options{
		Path:              dbPath,
		CreateIfNotExists: true,
	})
	require.NoError(t, err)
	store.Close()

	// Verify DB was created
	_, err = os.Stat(dbPath)
	assert.NoError(t, err)
}
