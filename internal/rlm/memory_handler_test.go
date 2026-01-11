package rlm

import (
	"context"
	"testing"

	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryCallbackHandler_NilStore(t *testing.T) {
	handler := NewMemoryCallbackHandler(nil)

	// All operations should return empty results without error
	nodes, err := handler.MemoryQuery("test", 10)
	require.NoError(t, err)
	assert.Empty(t, nodes)

	id, err := handler.MemoryAddFact("test fact", 0.9)
	require.NoError(t, err)
	assert.Empty(t, id)

	id, err = handler.MemoryAddExperience("test", "outcome", true)
	require.NoError(t, err)
	assert.Empty(t, id)

	nodes, err = handler.MemoryGetContext(10)
	require.NoError(t, err)
	assert.Empty(t, nodes)

	id, err = handler.MemoryRelate("test", "a", "b")
	require.NoError(t, err)
	assert.Empty(t, id)
}

func TestMemoryCallbackHandler_WithStore(t *testing.T) {
	// Create in-memory store
	store, err := hypergraph.NewStore(hypergraph.Options{})
	require.NoError(t, err)
	defer store.Close()

	handler := NewMemoryCallbackHandler(store)

	// Add a fact
	factID, err := handler.MemoryAddFact("Go is a statically typed language", 0.95)
	require.NoError(t, err)
	assert.NotEmpty(t, factID)

	// Add an experience
	expID, err := handler.MemoryAddExperience(
		"Used map_reduce for large file",
		"Successfully processed in 4 chunks",
		true,
	)
	require.NoError(t, err)
	assert.NotEmpty(t, expID)

	// Query for facts
	nodes, err := handler.MemoryQuery("Go", 10)
	require.NoError(t, err)
	assert.NotEmpty(t, nodes)
	assert.Contains(t, nodes[0].Content, "Go")

	// Get context
	ctx, err := handler.MemoryGetContext(10)
	require.NoError(t, err)
	assert.NotEmpty(t, ctx)

	// Create relationship
	edgeID, err := handler.MemoryRelate("relates_to", factID, expID)
	require.NoError(t, err)
	assert.NotEmpty(t, edgeID)
}

func TestMemoryCallbackHandler_WithContext(t *testing.T) {
	handler := NewMemoryCallbackHandler(nil)
	ctx := context.WithValue(context.Background(), "key", "value")

	newHandler := handler.WithContext(ctx)
	assert.NotNil(t, newHandler)
	assert.Equal(t, ctx, newHandler.ctx)
}
