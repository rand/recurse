package rlm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
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

// =============================================================================
// Property-Based Tests for Callback Handlers
// =============================================================================

// TestProperty_HandleLLMCallNeverPanics verifies handler never panics on any input.
func TestProperty_HandleLLMCallNeverPanics(t *testing.T) {
	client := &subCallMockClient{response: "response"}
	router := NewSubCallRouter(SubCallConfig{Client: client})
	handler := NewREPLCallbackHandler(router)

	rapid.Check(t, func(t *rapid.T) {
		prompt := rapid.String().Draw(t, "prompt")
		context := rapid.String().Draw(t, "context")
		model := rapid.SampledFrom([]string{"fast", "balanced", "powerful", "reasoning", "auto", ""}).Draw(t, "model")

		// Should never panic
		result, err := handler.HandleLLMCall(prompt, context, model)
		assert.NoError(t, err)
		assert.NotEmpty(t, result)
	})
}

// TestProperty_HandleLLMBatchLengthMatchesInput verifies batch results match input length.
func TestProperty_HandleLLMBatchLengthMatchesInput(t *testing.T) {
	client := &subCallMockClient{response: "response"}
	router := NewSubCallRouter(SubCallConfig{Client: client})
	handler := NewREPLCallbackHandler(router)

	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 10).Draw(t, "batch_size")
		prompts := make([]string, n)
		contexts := make([]string, n)

		for i := 0; i < n; i++ {
			prompts[i] = rapid.String().Draw(t, "prompt")
			contexts[i] = rapid.String().Draw(t, "context")
		}

		results, err := handler.HandleLLMBatch(prompts, contexts, "auto")
		assert.NoError(t, err)
		assert.Equal(t, n, len(results), "Result count should match input count")
	})
}

// TestProperty_WithContextPreservesRouter verifies context chaining preserves router.
func TestProperty_WithContextPreservesRouter(t *testing.T) {
	client := &subCallMockClient{response: "test"}
	router := NewSubCallRouter(SubCallConfig{Client: client})
	handler := NewREPLCallbackHandler(router)

	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()

		newHandler := handler.WithContext(ctx)
		assert.Equal(t, handler.router, newHandler.router, "Router should be preserved")
	})
}

// TestProperty_WithDepthSetsCorrectValue verifies depth setting.
func TestProperty_WithDepthSetsCorrectValue(t *testing.T) {
	handler := NewREPLCallbackHandler(nil)

	rapid.Check(t, func(t *rapid.T) {
		depth := rapid.IntRange(0, 100).Draw(t, "depth")

		newHandler := handler.WithDepth(depth)
		assert.Equal(t, depth, newHandler.depth)
	})
}

// TestProperty_WithBudgetSetsCorrectValue verifies budget setting.
func TestProperty_WithBudgetSetsCorrectValue(t *testing.T) {
	handler := NewREPLCallbackHandler(nil)

	rapid.Check(t, func(t *rapid.T) {
		budget := rapid.IntRange(0, 1000000).Draw(t, "budget")

		newHandler := handler.WithBudget(budget)
		assert.Equal(t, budget, newHandler.budget)
	})
}

// TestProperty_NilRouterReturnsEmptyResults verifies graceful handling of nil router.
func TestProperty_NilRouterReturnsEmptyResults(t *testing.T) {
	handler := NewREPLCallbackHandler(nil)

	rapid.Check(t, func(t *rapid.T) {
		prompt := rapid.String().Draw(t, "prompt")
		context := rapid.String().Draw(t, "context")

		result, err := handler.HandleLLMCall(prompt, context, "auto")
		assert.NoError(t, err)
		assert.Empty(t, result, "Nil router should return empty result")
	})
}

// TestProperty_ChainedMethodsAreIndependent verifies method chaining creates new instances.
func TestProperty_ChainedMethodsAreIndependent(t *testing.T) {
	handler := NewREPLCallbackHandler(nil)
	defaultBudget := handler.budget // Capture default budget (100000)

	rapid.Check(t, func(t *rapid.T) {
		depth1 := rapid.IntRange(0, 50).Draw(t, "depth1")
		depth2 := rapid.IntRange(51, 100).Draw(t, "depth2")
		budget := rapid.IntRange(1000, 10000).Draw(t, "budget")

		h1 := handler.WithDepth(depth1)
		h2 := handler.WithDepth(depth2)
		h3 := h1.WithBudget(budget)

		assert.Equal(t, depth1, h1.depth)
		assert.Equal(t, depth2, h2.depth)
		assert.Equal(t, 0, handler.depth, "Original should be unchanged")
		assert.Equal(t, budget, h3.budget)
		assert.Equal(t, defaultBudget, h1.budget, "First chain should preserve default budget")
		assert.Equal(t, depth1, h3.depth, "Budget chain should preserve depth from h1")
	})
}
