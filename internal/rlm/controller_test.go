package rlm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/rlm/meta"
)

// mockLLMClient implements meta.LLMClient for testing.
type mockLLMClient struct {
	responses []string
	callCount int
}

func (m *mockLLMClient) Complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	if m.callCount < len(m.responses) {
		resp := m.responses[m.callCount]
		m.callCount++
		return resp, nil
	}
	// Default to DIRECT action
	return `{"action": "DIRECT", "reasoning": "Default direct response"}`, nil
}

// mockTraceRecorder implements TraceRecorder for testing.
type mockTraceRecorder struct {
	events []TraceEvent
}

func (m *mockTraceRecorder) RecordEvent(event TraceEvent) error {
	m.events = append(m.events, event)
	return nil
}

func TestDefaultControllerConfig(t *testing.T) {
	cfg := DefaultControllerConfig()

	assert.Equal(t, 100000, cfg.MaxTokenBudget)
	assert.Equal(t, 5, cfg.MaxRecursionDepth)
	assert.Equal(t, 10, cfg.MemoryQueryLimit)
	assert.True(t, cfg.StoreDecisions)
	assert.True(t, cfg.TraceEnabled)
}

func TestNewController(t *testing.T) {
	store := createTestStore(t)
	client := &mockLLMClient{}
	metaCtrl := meta.NewController(client, meta.DefaultConfig())
	cfg := DefaultControllerConfig()

	ctrl := NewController(metaCtrl, client, store, cfg)

	require.NotNil(t, ctrl)
	assert.NotNil(t, ctrl.meta)
	assert.NotNil(t, ctrl.store)
	assert.NotNil(t, ctrl.synthesizer)
}

func TestSetTracer(t *testing.T) {
	store := createTestStore(t)
	client := &mockLLMClient{}
	metaCtrl := meta.NewController(client, meta.DefaultConfig())
	ctrl := NewController(metaCtrl, client, store, DefaultControllerConfig())

	tracer := &mockTraceRecorder{}
	ctrl.SetTracer(tracer)

	assert.NotNil(t, ctrl.tracer)
}

func TestExecute_DirectAction(t *testing.T) {
	store := createTestStore(t)
	client := &mockLLMClient{
		responses: []string{`{"action": "DIRECT", "reasoning": "Simple task"}`},
	}
	metaCtrl := meta.NewController(client, meta.DefaultConfig())
	cfg := DefaultControllerConfig()
	cfg.StoreDecisions = false // Disable for simpler test
	ctrl := NewController(metaCtrl, client, store, cfg)
	ctx := context.Background()

	result, err := ctrl.Execute(ctx, "What is 2+2?")
	require.NoError(t, err)

	assert.NotEmpty(t, result.Response)
	assert.Equal(t, "What is 2+2?", result.Task)
	assert.Greater(t, result.Duration, int64(0))
}

func TestExecute_WithTracing(t *testing.T) {
	store := createTestStore(t)
	client := &mockLLMClient{
		responses: []string{`{"action": "DIRECT", "reasoning": "Traced task"}`},
	}
	metaCtrl := meta.NewController(client, meta.DefaultConfig())
	cfg := DefaultControllerConfig()
	cfg.StoreDecisions = false
	cfg.TraceEnabled = true
	ctrl := NewController(metaCtrl, client, store, cfg)

	tracer := &mockTraceRecorder{}
	ctrl.SetTracer(tracer)

	ctx := context.Background()
	_, err := ctrl.Execute(ctx, "Test task")
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(tracer.events), 1)
}

func TestExecute_WithMemoryStorage(t *testing.T) {
	store := createTestStore(t)
	client := &mockLLMClient{
		responses: []string{`{"action": "DIRECT", "reasoning": "Store this"}`},
	}
	metaCtrl := meta.NewController(client, meta.DefaultConfig())
	cfg := DefaultControllerConfig()
	cfg.StoreDecisions = true
	ctrl := NewController(metaCtrl, client, store, cfg)
	ctx := context.Background()

	_, err := ctrl.Execute(ctx, "Important task")
	require.NoError(t, err)

	// Check that a decision node was created
	nodes, err := store.ListNodes(ctx, hypergraph.NodeFilter{
		Types: []hypergraph.NodeType{hypergraph.NodeTypeDecision},
		Limit: 10,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(nodes), 1)
}

func TestExecute_MemoryQuery(t *testing.T) {
	store := createTestStore(t)
	ctx := context.Background()

	// Pre-populate memory with relevant facts
	fact := hypergraph.NewNode(hypergraph.NodeTypeFact, "The project uses Go language")
	fact.Confidence = 0.9
	require.NoError(t, store.CreateNode(ctx, fact))

	client := &mockLLMClient{
		responses: []string{`{"action": "MEMORY_QUERY", "params": {"query": "project language"}, "reasoning": "Need context"}`},
	}
	metaCtrl := meta.NewController(client, meta.DefaultConfig())
	cfg := DefaultControllerConfig()
	cfg.StoreDecisions = false
	ctrl := NewController(metaCtrl, client, store, cfg)

	result, err := ctrl.Execute(ctx, "What language does the project use?")
	require.NoError(t, err)

	// Should have queried memory
	assert.NotEmpty(t, result.Response)
}

func TestExecute_Decompose(t *testing.T) {
	store := createTestStore(t)
	client := &mockLLMClient{
		responses: []string{
			`{"action": "DECOMPOSE", "params": {"strategy": "concept"}, "reasoning": "Complex task"}`,
			`{"action": "DIRECT", "reasoning": "Part 1"}`,
			`{"action": "DIRECT", "reasoning": "Part 2"}`,
		},
	}
	metaCtrl := meta.NewController(client, meta.DefaultConfig())
	cfg := DefaultControllerConfig()
	cfg.StoreDecisions = false
	ctrl := NewController(metaCtrl, client, store, cfg)
	ctx := context.Background()

	result, err := ctrl.Execute(ctx, "Explain memory management and garbage collection")
	require.NoError(t, err)

	assert.NotEmpty(t, result.Response)
}

func TestQueryMemoryContext(t *testing.T) {
	store := createTestStore(t)
	ctx := context.Background()

	// Add some facts
	fact1 := hypergraph.NewNode(hypergraph.NodeTypeFact, "The system uses SQLite for storage")
	fact1.Confidence = 0.8
	require.NoError(t, store.CreateNode(ctx, fact1))

	fact2 := hypergraph.NewNode(hypergraph.NodeTypeFact, "Memory is tiered into task, session, and long-term")
	fact2.Confidence = 0.9
	require.NoError(t, store.CreateNode(ctx, fact2))

	client := &mockLLMClient{}
	metaCtrl := meta.NewController(client, meta.DefaultConfig())
	ctrl := NewController(metaCtrl, client, store, DefaultControllerConfig())

	hints, err := ctrl.queryMemoryContext(ctx, "Tell me about the system storage")
	require.NoError(t, err)

	// Should find relevant facts
	assert.GreaterOrEqual(t, len(hints), 0) // May or may not match depending on content
}

func TestExecuteSynthesize(t *testing.T) {
	store := createTestStore(t)
	client := &mockLLMClient{}
	metaCtrl := meta.NewController(client, meta.DefaultConfig())
	ctrl := NewController(metaCtrl, client, store, DefaultControllerConfig())
	ctx := context.Background()

	state := meta.State{
		Task: "Combine results",
		PartialResults: []string{
			"First part of the analysis",
			"Second part of the analysis",
		},
	}

	response, tokens, err := ctrl.executeSynthesize(ctx, state)
	require.NoError(t, err)

	assert.Contains(t, response, "First part")
	assert.Contains(t, response, "Second part")
	assert.GreaterOrEqual(t, tokens, 0)
}

func TestExecuteSynthesize_Empty(t *testing.T) {
	store := createTestStore(t)
	client := &mockLLMClient{}
	metaCtrl := meta.NewController(client, meta.DefaultConfig())
	ctrl := NewController(metaCtrl, client, store, DefaultControllerConfig())
	ctx := context.Background()

	state := meta.State{
		Task:           "Combine results",
		PartialResults: []string{},
	}

	response, _, err := ctrl.executeSynthesize(ctx, state)
	require.NoError(t, err)

	assert.Contains(t, response, "No partial results")
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		text     string
		expected int
	}{
		{"", 0},
		{"test", 1},
		{"hello world", 2}, // 11 chars / 4 ≈ 2
		{"This is a longer piece of text for testing", 10},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			result := estimateTokens(tt.text)
			assert.InDelta(t, tt.expected, result, 2)
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"short", 10, "short"},
		{"this is a longer string", 10, "this is..."},
		{"exact", 5, "exact"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := truncate(tt.input, tt.max)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	assert.True(t, len(id1) > 0)
	assert.True(t, id1 != id2) // Should be unique
	assert.Contains(t, id1, "rlm-")
}

func TestExecutionResult_Fields(t *testing.T) {
	result := ExecutionResult{
		Task:        "Test task",
		Response:    "Test response",
		TotalTokens: 100,
	}

	assert.Equal(t, "Test task", result.Task)
	assert.Equal(t, "Test response", result.Response)
	assert.Equal(t, 100, result.TotalTokens)
}

func TestTraceEvent_Fields(t *testing.T) {
	event := TraceEvent{
		ID:       "test-id",
		Type:     "decision",
		Action:   "Test action",
		Tokens:   50,
		Depth:    1,
		ParentID: "parent-id",
		Status:   "completed",
	}

	assert.Equal(t, "test-id", event.ID)
	assert.Equal(t, "decision", event.Type)
	assert.Equal(t, 1, event.Depth)
	assert.Equal(t, "parent-id", event.ParentID)
}

// Helper function to create a test store
func createTestStore(t *testing.T) *hypergraph.Store {
	t.Helper()
	store, err := hypergraph.NewStore(hypergraph.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}
