package tools

import (
	"context"
	"encoding/json"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/memory/tiers"
)

func newTestTaskMemory(t *testing.T) *tiers.TaskMemory {
	t.Helper()
	store, err := hypergraph.NewStore(hypergraph.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return tiers.NewTaskMemory(store, tiers.DefaultTaskConfig())
}

func TestMemoryStoreTool_Fact(t *testing.T) {
	tm := newTestTaskMemory(t)
	tool := NewMemoryStoreTool(tm)
	ctx := context.Background()

	resp, err := callTool(ctx, tool, MemoryStoreParams{
		Type:       "fact",
		Content:    "The sky is blue",
		Confidence: 0.9,
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "Stored fact")
}

func TestMemoryStoreTool_Entity(t *testing.T) {
	tm := newTestTaskMemory(t)
	tool := NewMemoryStoreTool(tm)
	ctx := context.Background()

	resp, err := callTool(ctx, tool, MemoryStoreParams{
		Type:    "entity",
		Content: "main.go",
		Subtype: "file",
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "Stored entity")
}

func TestMemoryStoreTool_Snippet(t *testing.T) {
	tm := newTestTaskMemory(t)
	tool := NewMemoryStoreTool(tm)
	ctx := context.Background()

	resp, err := callTool(ctx, tool, MemoryStoreParams{
		Type:    "snippet",
		Content: "func main() {}",
		File:    "main.go",
		Line:    1,
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "Stored snippet")
}

func TestMemoryStoreTool_Decision(t *testing.T) {
	tm := newTestTaskMemory(t)
	tool := NewMemoryStoreTool(tm)
	ctx := context.Background()

	resp, err := callTool(ctx, tool, MemoryStoreParams{
		Type:         "decision",
		Content:      "Use SQLite",
		Rationale:    "Simple and embedded",
		Alternatives: []string{"PostgreSQL", "MongoDB"},
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "Stored decision")
}

func TestMemoryStoreTool_Experience(t *testing.T) {
	tm := newTestTaskMemory(t)
	tool := NewMemoryStoreTool(tm)
	ctx := context.Background()

	resp, err := callTool(ctx, tool, MemoryStoreParams{
		Type:    "experience",
		Content: "Tried caching",
		Outcome: "Improved performance by 50%",
		Success: true,
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "Stored experience")
}

func TestMemoryStoreTool_Deduplication(t *testing.T) {
	tm := newTestTaskMemory(t)
	tool := NewMemoryStoreTool(tm)
	ctx := context.Background()

	// Store same fact twice
	_, err := callTool(ctx, tool, MemoryStoreParams{
		Type:    "fact",
		Content: "Duplicate fact",
	})
	require.NoError(t, err)

	resp, err := callTool(ctx, tool, MemoryStoreParams{
		Type:    "fact",
		Content: "Duplicate fact",
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "Found existing")
}

func TestMemoryStoreTool_InvalidType(t *testing.T) {
	tm := newTestTaskMemory(t)
	tool := NewMemoryStoreTool(tm)
	ctx := context.Background()

	resp, err := callTool(ctx, tool, MemoryStoreParams{
		Type:    "invalid",
		Content: "test",
	})
	require.NoError(t, err)
	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "unknown memory type")
}

func TestMemoryQueryTool_Search(t *testing.T) {
	tm := newTestTaskMemory(t)
	storeTool := NewMemoryStoreTool(tm)
	queryTool := NewMemoryQueryTool(tm)
	ctx := context.Background()

	// Store some facts
	callTool(ctx, storeTool, MemoryStoreParams{Type: "fact", Content: "The quick brown fox"})
	callTool(ctx, storeTool, MemoryStoreParams{Type: "fact", Content: "A lazy dog"})
	callTool(ctx, storeTool, MemoryStoreParams{Type: "fact", Content: "The fox is quick"})

	// Search for "fox"
	resp, err := callTool(ctx, queryTool, MemoryQueryParams{Query: "fox", Limit: 10})
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "Found 2 nodes")
}

func TestMemoryQueryTool_Recent(t *testing.T) {
	tm := newTestTaskMemory(t)
	storeTool := NewMemoryStoreTool(tm)
	queryTool := NewMemoryQueryTool(tm)
	ctx := context.Background()

	// Store some items
	callTool(ctx, storeTool, MemoryStoreParams{Type: "fact", Content: "Fact 1"})
	callTool(ctx, storeTool, MemoryStoreParams{Type: "entity", Content: "Entity 1", Subtype: "test"})

	// Get recent
	resp, err := callTool(ctx, queryTool, MemoryQueryParams{Recent: true, Limit: 10})
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "Found 2 nodes")
	assert.Contains(t, resp.Content, "(recent)")
}

func TestMemoryQueryTool_NoResults(t *testing.T) {
	tm := newTestTaskMemory(t)
	queryTool := NewMemoryQueryTool(tm)
	ctx := context.Background()

	resp, err := callTool(ctx, queryTool, MemoryQueryParams{Query: "nonexistent"})
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "(no results)")
}

func TestMemoryRelateTool(t *testing.T) {
	tm := newTestTaskMemory(t)
	storeTool := NewMemoryStoreTool(tm)
	relateTool := NewMemoryRelateTool(tm)
	ctx := context.Background()

	// Store two entities
	resp1, _ := callTool(ctx, storeTool, MemoryStoreParams{Type: "entity", Content: "main.go", Subtype: "file"})
	resp2, _ := callTool(ctx, storeTool, MemoryStoreParams{Type: "entity", Content: "func main()", Subtype: "function"})

	// Extract IDs from metadata
	meta1 := parseStoreResult(t, resp1)
	meta2 := parseStoreResult(t, resp2)

	// Create relationship
	resp, err := callTool(ctx, relateTool, MemoryRelateParams{
		Label:     "contains",
		SubjectID: meta1.NodeID,
		ObjectID:  meta2.NodeID,
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "Created relationship")
	assert.Contains(t, resp.Content, "contains")
}

func TestMemoryRelateTool_MissingParams(t *testing.T) {
	tm := newTestTaskMemory(t)
	tool := NewMemoryRelateTool(tm)
	ctx := context.Background()

	resp, err := callTool(ctx, tool, MemoryRelateParams{
		Label: "contains",
		// Missing subject_id and object_id
	})
	require.NoError(t, err)
	assert.True(t, resp.IsError)
}

func TestMemoryQueryTool_RelatedTo(t *testing.T) {
	tm := newTestTaskMemory(t)
	storeTool := NewMemoryStoreTool(tm)
	relateTool := NewMemoryRelateTool(tm)
	queryTool := NewMemoryQueryTool(tm)
	ctx := context.Background()

	// Create a small graph
	resp1, _ := callTool(ctx, storeTool, MemoryStoreParams{Type: "entity", Content: "A", Subtype: "test"})
	resp2, _ := callTool(ctx, storeTool, MemoryStoreParams{Type: "entity", Content: "B", Subtype: "test"})
	resp3, _ := callTool(ctx, storeTool, MemoryStoreParams{Type: "entity", Content: "C", Subtype: "test"})

	id1 := parseStoreResult(t, resp1).NodeID
	id2 := parseStoreResult(t, resp2).NodeID
	id3 := parseStoreResult(t, resp3).NodeID

	// A -> B -> C
	callTool(ctx, relateTool, MemoryRelateParams{Label: "connects", SubjectID: id1, ObjectID: id2})
	callTool(ctx, relateTool, MemoryRelateParams{Label: "connects", SubjectID: id2, ObjectID: id3})

	// Query related to A
	resp, err := callTool(ctx, queryTool, MemoryQueryParams{RelatedTo: id1, Depth: 2})
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "related to")
}

// parseStoreResult extracts MemoryStoreResult from a tool response's JSON metadata.
func parseStoreResult(t *testing.T, resp fantasy.ToolResponse) MemoryStoreResult {
	t.Helper()
	var result MemoryStoreResult
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &result))
	return result
}

// Helper to call a tool with typed params
func callTool[T any](ctx context.Context, tool fantasy.AgentTool, params T) (fantasy.ToolResponse, error) {
	input, err := json.Marshal(params)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	return tool.Run(ctx, fantasy.ToolCall{Input: string(input)})
}
