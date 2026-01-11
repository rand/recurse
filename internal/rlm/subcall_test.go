package rlm

import (
	"context"
	"testing"

	"github.com/rand/recurse/internal/rlm/meta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// subCallMockClient is a test double for LLMClient.
type subCallMockClient struct {
	response string
	err      error
	calls    []string
}

func (m *subCallMockClient) Complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	m.calls = append(m.calls, prompt)
	return m.response, m.err
}

func TestNewSubCallRouter(t *testing.T) {
	router := NewSubCallRouter(SubCallConfig{})
	assert.NotNil(t, router)
	assert.Equal(t, 5, router.maxDepth)
	assert.Equal(t, 100000, router.budgetLimit)
	assert.NotEmpty(t, router.models)
}

func TestSubCallRouter_Call(t *testing.T) {
	client := &subCallMockClient{response: "Test response"}
	router := NewSubCallRouter(SubCallConfig{
		Client:   client,
		MaxDepth: 5,
	})

	resp := router.Call(context.Background(), SubCallRequest{
		Prompt:  "Summarize this",
		Context: "Some test content",
		Model:   "fast",
	})

	assert.Empty(t, resp.Error)
	assert.Equal(t, "Test response", resp.Response)
	assert.NotEmpty(t, resp.ModelUsed)
	assert.Greater(t, resp.TokensUsed, 0)
}

func TestSubCallRouter_Call_NoClient(t *testing.T) {
	router := NewSubCallRouter(SubCallConfig{})

	resp := router.Call(context.Background(), SubCallRequest{
		Prompt: "Test",
	})

	assert.Contains(t, resp.Error, "not configured")
}

func TestSubCallRouter_Call_MaxDepthExceeded(t *testing.T) {
	client := &subCallMockClient{response: "Test"}
	router := NewSubCallRouter(SubCallConfig{
		Client:   client,
		MaxDepth: 2,
	})

	// Simulate already at max depth
	router.currentDepth = 2

	resp := router.Call(context.Background(), SubCallRequest{
		Prompt: "Test",
	})

	assert.Contains(t, resp.Error, "max recursion depth exceeded")
}

func TestSubCallRouter_BatchCall(t *testing.T) {
	client := &subCallMockClient{response: "Response"}
	router := NewSubCallRouter(SubCallConfig{
		Client: client,
	})

	requests := []SubCallRequest{
		{Prompt: "Task 1", Context: "Content 1"},
		{Prompt: "Task 2", Context: "Content 2"},
		{Prompt: "Task 3", Context: "Content 3"},
	}

	responses := router.BatchCall(context.Background(), requests)

	assert.Len(t, responses, 3)
	for _, resp := range responses {
		assert.Empty(t, resp.Error)
		assert.Equal(t, "Response", resp.Response)
	}
}

func TestSubCallRouter_SelectModel_ExplicitTier(t *testing.T) {
	router := NewSubCallRouter(SubCallConfig{})

	tests := []struct {
		tierHint     string
		expectedTier meta.ModelTier
	}{
		{"fast", meta.TierFast},
		{"balanced", meta.TierBalanced},
		{"powerful", meta.TierPowerful},
		{"reasoning", meta.TierReasoning},
	}

	for _, tt := range tests {
		t.Run(tt.tierHint, func(t *testing.T) {
			model := router.selectModel(context.Background(), SubCallRequest{
				Model: tt.tierHint,
			})
			require.NotNil(t, model)
			assert.Equal(t, tt.expectedTier, model.Tier)
		})
	}
}

func TestSubCallRouter_Stats(t *testing.T) {
	client := &subCallMockClient{response: "Test"}
	router := NewSubCallRouter(SubCallConfig{
		Client: client,
	})

	// Make some calls
	router.Call(context.Background(), SubCallRequest{Prompt: "Test 1", Model: "fast"})
	router.Call(context.Background(), SubCallRequest{Prompt: "Test 2", Model: "fast"})
	router.Call(context.Background(), SubCallRequest{Prompt: "Test 3", Model: "balanced"})

	stats := router.Stats()
	assert.Equal(t, int64(3), stats.TotalCalls)
	assert.Greater(t, stats.TotalTokens, int64(0))
}

func TestSubCallRouter_ResetStats(t *testing.T) {
	client := &subCallMockClient{response: "Test"}
	router := NewSubCallRouter(SubCallConfig{
		Client: client,
	})

	router.Call(context.Background(), SubCallRequest{Prompt: "Test"})
	stats := router.Stats()
	assert.Equal(t, int64(1), stats.TotalCalls)

	router.ResetStats()
	stats = router.Stats()
	assert.Equal(t, int64(0), stats.TotalCalls)
}

func TestSubCallRouter_IsConfigured(t *testing.T) {
	router := NewSubCallRouter(SubCallConfig{})
	assert.False(t, router.IsConfigured())

	router.SetClient(&subCallMockClient{})
	assert.True(t, router.IsConfigured())
}

func TestSubCallRequest_Fields(t *testing.T) {
	req := SubCallRequest{
		Prompt:    "Test prompt",
		Context:   "Test context",
		Model:     "fast",
		Depth:     2,
		Budget:    5000,
		MaxTokens: 500,
	}

	assert.Equal(t, "Test prompt", req.Prompt)
	assert.Equal(t, "Test context", req.Context)
	assert.Equal(t, "fast", req.Model)
	assert.Equal(t, 2, req.Depth)
	assert.Equal(t, 5000, req.Budget)
	assert.Equal(t, 500, req.MaxTokens)
}

func TestSubCallResponse_Fields(t *testing.T) {
	resp := SubCallResponse{
		Response:   "Test response",
		ModelUsed:  "anthropic/claude-haiku-4.5",
		TokensUsed: 100,
		Cost:       0.001,
	}

	assert.Equal(t, "Test response", resp.Response)
	assert.Equal(t, "anthropic/claude-haiku-4.5", resp.ModelUsed)
	assert.Equal(t, 100, resp.TokensUsed)
	assert.Equal(t, 0.001, resp.Cost)
}

func TestSubCallStats_ToJSON(t *testing.T) {
	stats := SubCallStats{
		TotalCalls:  10,
		TotalTokens: 5000,
		TotalCost:   0.05,
	}

	json := stats.ToJSON()
	assert.Contains(t, json, "\"total_calls\": 10")
	assert.Contains(t, json, "\"total_tokens\": 5000")
}

func TestBuildPrompt(t *testing.T) {
	router := NewSubCallRouter(SubCallConfig{})

	req := SubCallRequest{
		Prompt:  "Summarize this",
		Context: "Long content here",
		Depth:   2,
		Budget:  5000,
	}

	prompt := router.buildPrompt(req)

	assert.Contains(t, prompt, "Recursion depth: 2")
	assert.Contains(t, prompt, "Budget remaining: 5000")
	assert.Contains(t, prompt, "## Task")
	assert.Contains(t, prompt, "Summarize this")
	assert.Contains(t, prompt, "## Context")
	assert.Contains(t, prompt, "Long content here")
}
