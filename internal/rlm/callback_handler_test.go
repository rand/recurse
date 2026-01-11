package rlm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestREPLCallbackHandler_HandleLLMCall(t *testing.T) {
	client := &subCallMockClient{response: "Test response from LLM"}
	router := NewSubCallRouter(SubCallConfig{
		Client: client,
	})

	handler := NewREPLCallbackHandler(router)

	result, err := handler.HandleLLMCall("Summarize this", "Some context", "fast")
	require.NoError(t, err)
	assert.Equal(t, "Test response from LLM", result)

	// Verify the router received the call
	stats := router.Stats()
	assert.Equal(t, int64(1), stats.TotalCalls)
}

func TestREPLCallbackHandler_HandleLLMCall_NilRouter(t *testing.T) {
	handler := NewREPLCallbackHandler(nil)

	result, err := handler.HandleLLMCall("Test", "Context", "auto")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestREPLCallbackHandler_HandleLLMBatch(t *testing.T) {
	client := &subCallMockClient{response: "Batch response"}
	router := NewSubCallRouter(SubCallConfig{
		Client: client,
	})

	handler := NewREPLCallbackHandler(router)

	prompts := []string{"Prompt 1", "Prompt 2", "Prompt 3"}
	contexts := []string{"Context 1", "Context 2", "Context 3"}

	results, err := handler.HandleLLMBatch(prompts, contexts, "fast")
	require.NoError(t, err)
	assert.Len(t, results, 3)
	for _, r := range results {
		assert.Equal(t, "Batch response", r)
	}

	stats := router.Stats()
	assert.Equal(t, int64(3), stats.TotalCalls)
}

func TestREPLCallbackHandler_HandleLLMBatch_NilRouter(t *testing.T) {
	handler := NewREPLCallbackHandler(nil)

	results, err := handler.HandleLLMBatch([]string{"P1", "P2"}, []string{"C1", "C2"}, "auto")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestREPLCallbackHandler_WithContext(t *testing.T) {
	handler := NewREPLCallbackHandler(nil)
	ctx := context.WithValue(context.Background(), "key", "value")

	newHandler := handler.WithContext(ctx)
	assert.NotNil(t, newHandler)
	assert.Equal(t, ctx, newHandler.ctx)
}

func TestREPLCallbackHandler_WithDepth(t *testing.T) {
	handler := NewREPLCallbackHandler(nil)

	newHandler := handler.WithDepth(3)
	assert.NotNil(t, newHandler)
	assert.Equal(t, 3, newHandler.depth)
}

func TestREPLCallbackHandler_WithBudget(t *testing.T) {
	handler := NewREPLCallbackHandler(nil)

	newHandler := handler.WithBudget(50000)
	assert.NotNil(t, newHandler)
	assert.Equal(t, 50000, newHandler.budget)
}

func TestCallbackError(t *testing.T) {
	err := &CallbackError{Message: "test error"}
	assert.Equal(t, "test error", err.Error())
}
