package tiers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

func newTestStore(t *testing.T) *hypergraph.Store {
	t.Helper()
	store, err := hypergraph.NewStore(hypergraph.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func TestNewTaskMemory(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())

	assert.NotNil(t, tm)
	assert.Equal(t, 1000, tm.Config().MaxNodes)
	assert.True(t, tm.Config().AutoConsolidate)
}

func TestTaskMemory_AddFact(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())
	ctx := context.Background()

	fact, err := tm.AddFact(ctx, "The sky is blue", 0.9)
	require.NoError(t, err)

	assert.NotEmpty(t, fact.ID)
	assert.Equal(t, hypergraph.NodeTypeFact, fact.Type)
	assert.Equal(t, hypergraph.TierTask, fact.Tier)
	assert.Equal(t, 0.9, fact.Confidence)
}

func TestTaskMemory_AddFact_Deduplication(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())
	ctx := context.Background()

	// Add the same fact twice
	fact1, err := tm.AddFact(ctx, "Water boils at 100C", 1.0)
	require.NoError(t, err)

	fact2, err := tm.AddFact(ctx, "Water boils at 100C", 1.0)
	require.NoError(t, err)

	// Should return the same node (deduplicated)
	assert.Equal(t, fact1.ID, fact2.ID)

	// Should only have one fact in memory
	facts, err := tm.GetFacts(ctx)
	require.NoError(t, err)
	assert.Len(t, facts, 1)
}

func TestTaskMemory_AddSnippet(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())
	ctx := context.Background()

	snippet, err := tm.AddSnippet(ctx, "func main() {}", "main.go", 1)
	require.NoError(t, err)

	assert.NotEmpty(t, snippet.ID)
	assert.Equal(t, hypergraph.NodeTypeSnippet, snippet.Type)
	assert.Equal(t, hypergraph.TierTask, snippet.Tier)
	assert.NotNil(t, snippet.Provenance)
}

func TestTaskMemory_AddEntity(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())
	ctx := context.Background()

	entity, err := tm.AddEntity(ctx, "main.go", "file")
	require.NoError(t, err)

	assert.NotEmpty(t, entity.ID)
	assert.Equal(t, hypergraph.NodeTypeEntity, entity.Type)
	assert.Equal(t, "file", entity.Subtype)
	assert.Equal(t, hypergraph.TierTask, entity.Tier)
}

func TestTaskMemory_AddEntity_Deduplication(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())
	ctx := context.Background()

	entity1, err := tm.AddEntity(ctx, "main.go", "file")
	require.NoError(t, err)

	entity2, err := tm.AddEntity(ctx, "main.go", "file")
	require.NoError(t, err)

	assert.Equal(t, entity1.ID, entity2.ID)
}

func TestTaskMemory_AddDecision(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())
	ctx := context.Background()

	decision, err := tm.AddDecision(ctx,
		"Use SQLite for storage",
		"Simple, embedded, sufficient for our needs",
		[]string{"PostgreSQL", "MongoDB"},
	)
	require.NoError(t, err)

	assert.NotEmpty(t, decision.ID)
	assert.Equal(t, hypergraph.NodeTypeDecision, decision.Type)
	assert.NotNil(t, decision.Metadata)
}

func TestTaskMemory_AddExperience(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())
	ctx := context.Background()

	// Add successful experience
	exp1, err := tm.AddExperience(ctx,
		"Used BFS for graph traversal",
		"Successfully found all connected nodes",
		true,
	)
	require.NoError(t, err)
	assert.Equal(t, 1.0, exp1.Confidence)

	// Add failed experience
	exp2, err := tm.AddExperience(ctx,
		"Tried DFS first",
		"Stack overflow on deep graphs",
		false,
	)
	require.NoError(t, err)
	assert.Equal(t, 0.5, exp2.Confidence)
}

func TestTaskMemory_Relate(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())
	ctx := context.Background()

	file, err := tm.AddEntity(ctx, "main.go", "file")
	require.NoError(t, err)

	function, err := tm.AddEntity(ctx, "func main()", "function")
	require.NoError(t, err)

	edge, err := tm.Relate(ctx, "contains", file.ID, function.ID)
	require.NoError(t, err)

	assert.NotEmpty(t, edge.ID)
	assert.Equal(t, "contains", edge.Label)
}

func TestTaskMemory_GetContext(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())
	ctx := context.Background()

	// Add some nodes
	tm.AddFact(ctx, "Fact 1", 1.0)
	tm.AddFact(ctx, "Fact 2", 1.0)
	tm.AddEntity(ctx, "Entity 1", "test")

	// Get context
	nodes, err := tm.GetContext(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, nodes, 3)
}

func TestTaskMemory_GetRelated(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())
	ctx := context.Background()

	// Create a small graph
	node1, _ := tm.AddEntity(ctx, "A", "test")
	node2, _ := tm.AddEntity(ctx, "B", "test")
	node3, _ := tm.AddEntity(ctx, "C", "test")

	tm.Relate(ctx, "connects", node1.ID, node2.ID)
	tm.Relate(ctx, "connects", node2.ID, node3.ID)

	// Get nodes related to node1
	related, err := tm.GetRelated(ctx, node1.ID, 2)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(related), 1)
}

func TestTaskMemory_Search(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())
	ctx := context.Background()

	tm.AddFact(ctx, "The quick brown fox", 1.0)
	tm.AddFact(ctx, "A lazy dog", 1.0)
	tm.AddFact(ctx, "The fox is quick", 1.0)

	results, err := tm.Search(ctx, "fox", 10)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestTaskMemory_Clear(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())
	ctx := context.Background()

	// Add nodes
	tm.AddFact(ctx, "Fact 1", 1.0)
	tm.AddFact(ctx, "Fact 2", 1.0)

	// Verify nodes exist
	facts, _ := tm.GetFacts(ctx)
	assert.Len(t, facts, 2)

	// Clear
	err := tm.Clear(ctx)
	require.NoError(t, err)

	// Verify cleared
	facts, _ = tm.GetFacts(ctx)
	assert.Len(t, facts, 0)
}

func TestTaskMemory_Stats(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())
	ctx := context.Background()

	tm.AddFact(ctx, "Fact 1", 1.0)
	tm.AddFact(ctx, "Fact 2", 1.0)
	tm.AddEntity(ctx, "Entity 1", "test")
	tm.AddSnippet(ctx, "code", "test.go", 1)
	tm.AddDecision(ctx, "Decision", "Reason", nil)

	stats, err := tm.Stats(ctx)
	require.NoError(t, err)

	assert.Equal(t, 5, stats.TotalNodes)
	assert.Equal(t, 2, stats.Facts)
	assert.Equal(t, 1, stats.Entities)
	assert.Equal(t, 1, stats.Snippets)
	assert.Equal(t, 1, stats.Decisions)
	assert.Equal(t, 1000, stats.MaxNodes)
	assert.NotEmpty(t, stats.TaskID)
}

func TestTaskMemory_StartTask(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())
	ctx := context.Background()

	err := tm.StartTask(ctx, "Implement feature X")
	require.NoError(t, err)

	// Verify task node was created
	nodes, err := store.ListNodes(ctx, hypergraph.NodeFilter{
		Types:    []hypergraph.NodeType{hypergraph.NodeTypeEntity},
		Subtypes: []string{"task"},
	})
	require.NoError(t, err)
	assert.Len(t, nodes, 1)
	assert.Equal(t, "Implement feature X", nodes[0].Content)
}

func TestTaskMemory_Capacity(t *testing.T) {
	store := newTestStore(t)
	config := TaskMemoryConfig{
		MaxNodes:        5,
		AutoConsolidate: false,
	}
	tm := NewTaskMemory(store, config)
	ctx := context.Background()

	// Fill to capacity
	for i := 0; i < 5; i++ {
		_, err := tm.AddFact(ctx, "Fact", 1.0)
		require.NoError(t, err)
	}

	// Adding one more should return capacity error
	_, err := tm.AddFact(ctx, "One more", 1.0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "capacity")
}

// mockFactVerifier implements FactVerifier for testing
type mockFactVerifier struct {
	enabled            bool
	allowAll           bool
	rejectAll          bool
	adjustedConfidence float64
	returnError        error
}

func (m *mockFactVerifier) VerifyFact(ctx context.Context, content string, evidence string, confidence float64) (bool, float64, error) {
	if m.returnError != nil {
		return false, 0, m.returnError
	}
	if m.rejectAll {
		return false, 0, nil
	}
	if m.allowAll || m.adjustedConfidence > 0 {
		return true, m.adjustedConfidence, nil
	}
	return true, confidence, nil
}

func (m *mockFactVerifier) Enabled() bool {
	return m.enabled
}

func TestTaskMemory_FactVerifier_AllowsValidFacts(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())
	ctx := context.Background()

	// Set up a verifier that allows facts with adjusted confidence
	verifier := &mockFactVerifier{
		enabled:            true,
		adjustedConfidence: 0.85,
	}
	tm.SetFactVerifier(verifier)

	fact, err := tm.AddFactWithEvidence(ctx, "Verified fact", 0.9, "supporting evidence")
	require.NoError(t, err)

	assert.NotNil(t, fact)
	assert.Equal(t, 0.85, fact.Confidence, "should use adjusted confidence")
}

func TestTaskMemory_FactVerifier_RejectsFacts(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())
	ctx := context.Background()

	// Set up a verifier that rejects all facts
	verifier := &mockFactVerifier{
		enabled:   true,
		rejectAll: true,
	}
	tm.SetFactVerifier(verifier)

	fact, err := tm.AddFactWithEvidence(ctx, "Invalid fact", 0.9, "contradicting evidence")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rejected")
	assert.Nil(t, fact)
}

func TestTaskMemory_FactVerifier_DisabledPassesThrough(t *testing.T) {
	store := newTestStore(t)
	tm := NewTaskMemory(store, DefaultTaskConfig())
	ctx := context.Background()

	// Set up a disabled verifier
	verifier := &mockFactVerifier{
		enabled:   false,
		rejectAll: true, // Would reject if enabled
	}
	tm.SetFactVerifier(verifier)

	fact, err := tm.AddFact(ctx, "Fact without verification", 0.9)
	require.NoError(t, err)

	assert.NotNil(t, fact)
	assert.Equal(t, 0.9, fact.Confidence, "should use original confidence when disabled")
}

func TestTaskMemory_FactVerifier_RequireEvidence(t *testing.T) {
	store := newTestStore(t)
	config := DefaultTaskConfig()
	config.RequireEvidence = true
	tm := NewTaskMemory(store, config)
	ctx := context.Background()

	verifier := &mockFactVerifier{
		enabled:            true,
		adjustedConfidence: 0.8,
	}
	tm.SetFactVerifier(verifier)

	// AddFact without evidence should fail when RequireEvidence is true
	fact, err := tm.AddFact(ctx, "Fact without evidence", 0.9)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "evidence required")
	assert.Nil(t, fact)

	// AddFactWithEvidence should work
	fact, err = tm.AddFactWithEvidence(ctx, "Fact with evidence", 0.9, "some evidence")
	require.NoError(t, err)
	assert.NotNil(t, fact)
}
