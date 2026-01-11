package memory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

// mockProvider implements MemoryProvider for testing.
type mockProvider struct {
	nodes []*hypergraph.Node
	stats MemoryStats
}

func (m *mockProvider) GetRecent(limit int) ([]*hypergraph.Node, error) {
	if limit > len(m.nodes) {
		return m.nodes, nil
	}
	return m.nodes[:limit], nil
}

func (m *mockProvider) Search(query string, limit int) ([]*hypergraph.Node, error) {
	return m.nodes, nil
}

func (m *mockProvider) GetStats() (MemoryStats, error) {
	return m.stats, nil
}

func TestNewMemoryDialog(t *testing.T) {
	provider := &mockProvider{
		nodes: []*hypergraph.Node{
			hypergraph.NewNode(hypergraph.NodeTypeFact, "Test fact"),
			hypergraph.NewNode(hypergraph.NodeTypeEntity, "Test entity"),
		},
		stats: MemoryStats{
			TotalNodes: 2,
			ByType: map[hypergraph.NodeType]int{
				hypergraph.NodeTypeFact:   1,
				hypergraph.NodeTypeEntity: 1,
			},
		},
	}

	dialog := NewMemoryDialog(provider)
	require.NotNil(t, dialog)
	assert.Equal(t, MemoryDialogID, dialog.ID())
}

func TestMemoryDialog_NilProvider(t *testing.T) {
	// Should handle nil provider gracefully
	dialog := NewMemoryDialog(nil)
	require.NotNil(t, dialog)

	// Init should not panic
	cmd := dialog.Init()
	assert.NotNil(t, cmd)
}

func TestFormatNodeLabel(t *testing.T) {
	tests := []struct {
		name     string
		nodeType hypergraph.NodeType
		content  string
		wantIcon string
	}{
		{"fact", hypergraph.NodeTypeFact, "Test", "[F]"},
		{"entity", hypergraph.NodeTypeEntity, "Test", "[E]"},
		{"snippet", hypergraph.NodeTypeSnippet, "Test", "[S]"},
		{"decision", hypergraph.NodeTypeDecision, "Test", "[D]"},
		{"experience", hypergraph.NodeTypeExperience, "Test", "[X]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := hypergraph.NewNode(tt.nodeType, tt.content)
			label := formatNodeLabel(node)
			assert.Contains(t, label, tt.wantIcon)
			assert.Contains(t, label, tt.content)
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"this is a very long string", 10, "this is..."},
		{"with\nnewlines", 20, "with newlines"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.max)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNodesToItems(t *testing.T) {
	nodes := []*hypergraph.Node{
		hypergraph.NewNode(hypergraph.NodeTypeFact, "Fact 1"),
		hypergraph.NewNode(hypergraph.NodeTypeEntity, "Entity 1"),
	}

	items := nodesToItems(nodes)
	assert.Len(t, items, 2)
}
