package rlm

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// classifierMockLLMClient implements meta.LLMClient for testing.
type classifierMockLLMClient struct {
	response string
	err      error
}

func (m *classifierMockLLMClient) Complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func TestLLMClassifier_Classify_Computational(t *testing.T) {
	client := &classifierMockLLMClient{
		response: `{"task_type": "computational", "confidence": 0.9, "reasoning": "Query asks to count occurrences"}`,
	}
	classifier := NewLLMClassifier(client)

	result, err := classifier.Classify(context.Background(), "What is the total?", nil)
	require.NoError(t, err)

	assert.Equal(t, TaskTypeComputational, result.Type)
	assert.Equal(t, 0.9, result.Confidence)
	assert.Contains(t, result.Signals, "source:llm_fallback")
}

func TestLLMClassifier_Classify_Retrieval(t *testing.T) {
	client := &classifierMockLLMClient{
		response: `{"task_type": "retrieval", "confidence": 0.85, "reasoning": "Looking for specific code"}`,
	}
	classifier := NewLLMClassifier(client)

	result, err := classifier.Classify(context.Background(), "What is the secret code?", nil)
	require.NoError(t, err)

	assert.Equal(t, TaskTypeRetrieval, result.Type)
	assert.Equal(t, 0.85, result.Confidence)
}

func TestLLMClassifier_Classify_WithHint(t *testing.T) {
	client := &classifierMockLLMClient{
		response: `{"task_type": "analytical", "confidence": 0.8, "reasoning": "Relationship query"}`,
	}
	classifier := NewLLMClassifier(client)

	hint := &Classification{
		Type:       TaskTypeAnalytical,
		Confidence: 0.5,
		Signals:    []string{"keyword:related"},
	}

	result, err := classifier.Classify(context.Background(), "Are these related?", hint)
	require.NoError(t, err)

	assert.Equal(t, TaskTypeAnalytical, result.Type)
	assert.Contains(t, result.Signals, "rule_hint:analytical@50%")
}

func TestLLMClassifier_Classify_ParsesMarkdownJSON(t *testing.T) {
	// LLMs sometimes wrap JSON in markdown code blocks
	client := &classifierMockLLMClient{
		response: "```json\n{\"task_type\": \"computational\", \"confidence\": 0.9, \"reasoning\": \"test\"}\n```",
	}
	classifier := NewLLMClassifier(client)

	result, err := classifier.Classify(context.Background(), "count items", nil)
	require.NoError(t, err)

	assert.Equal(t, TaskTypeComputational, result.Type)
}

func TestLLMClassifier_Classify_HandlesMalformedJSON(t *testing.T) {
	client := &classifierMockLLMClient{
		response: "I think this is a computational task",
	}
	classifier := NewLLMClassifier(client)

	result, err := classifier.Classify(context.Background(), "count items", nil)
	assert.Error(t, err)
	assert.Equal(t, TaskTypeUnknown, result.Type)
}

func TestLLMClassifier_Classify_HandlesLLMError(t *testing.T) {
	client := &classifierMockLLMClient{
		err: errors.New("API error"),
	}
	classifier := NewLLMClassifier(client)

	result, err := classifier.Classify(context.Background(), "count items", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM classification failed")
	assert.Equal(t, TaskTypeUnknown, result.Type)
}

func TestLLMClassifier_Classify_NilClient(t *testing.T) {
	classifier := NewLLMClassifier(nil)

	result, err := classifier.Classify(context.Background(), "count items", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM client not available")
	assert.Equal(t, TaskTypeUnknown, result.Type)
}

func TestLLMClassifier_Cache(t *testing.T) {
	callCount := 0
	client := &classifierMockLLMClient{
		response: `{"task_type": "computational", "confidence": 0.9, "reasoning": "test"}`,
	}
	// Wrap to count calls
	countingClient := &countingMockClient{
		inner:     client,
		callCount: &callCount,
	}

	classifier := NewLLMClassifier(countingClient)

	// First call
	result1, err := classifier.Classify(context.Background(), "What is the total?", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount)

	// Second call with same query - should use cache
	result2, err := classifier.Classify(context.Background(), "What is the total?", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount) // No additional call

	assert.Equal(t, result1.Type, result2.Type)
	assert.Contains(t, result2.Signals, "source:cache")
}

func TestLLMClassifier_Cache_NormalizesQuery(t *testing.T) {
	callCount := 0
	client := &classifierMockLLMClient{
		response: `{"task_type": "computational", "confidence": 0.9, "reasoning": "test"}`,
	}
	countingClient := &countingMockClient{
		inner:     client,
		callCount: &callCount,
	}

	classifier := NewLLMClassifier(countingClient)

	// First call
	_, _ = classifier.Classify(context.Background(), "What is the total?", nil)
	assert.Equal(t, 1, callCount)

	// Same query with different case - should use cache
	_, _ = classifier.Classify(context.Background(), "WHAT IS THE TOTAL?", nil)
	assert.Equal(t, 1, callCount)

	// Same query with whitespace - should use cache
	_, _ = classifier.Classify(context.Background(), "  what is the total?  ", nil)
	assert.Equal(t, 1, callCount)
}

func TestLLMClassifier_ClearCache(t *testing.T) {
	callCount := 0
	client := &classifierMockLLMClient{
		response: `{"task_type": "computational", "confidence": 0.9, "reasoning": "test"}`,
	}
	countingClient := &countingMockClient{
		inner:     client,
		callCount: &callCount,
	}

	classifier := NewLLMClassifier(countingClient)

	// First call
	_, _ = classifier.Classify(context.Background(), "What is the total?", nil)
	assert.Equal(t, 1, callCount)

	// Clear cache
	classifier.ClearCache()

	// Should call LLM again
	_, _ = classifier.Classify(context.Background(), "What is the total?", nil)
	assert.Equal(t, 2, callCount)
}

func TestLLMClassifier_MapTaskType(t *testing.T) {
	classifier := NewLLMClassifier(nil)

	tests := []struct {
		input    string
		expected TaskType
	}{
		{"computational", TaskTypeComputational},
		{"COMPUTATIONAL", TaskTypeComputational},
		{"compute", TaskTypeComputational},
		{"retrieval", TaskTypeRetrieval},
		{"retrieve", TaskTypeRetrieval},
		{"lookup", TaskTypeRetrieval},
		{"find", TaskTypeRetrieval},
		{"analytical", TaskTypeAnalytical},
		{"analysis", TaskTypeAnalytical},
		{"reasoning", TaskTypeAnalytical},
		{"transformational", TaskTypeTransformational},
		{"transform", TaskTypeTransformational},
		{"unknown_type", TaskTypeUnknown},
		{"", TaskTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := classifier.mapTaskType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLLMClassifier_ConfidenceBounds(t *testing.T) {
	tests := []struct {
		name     string
		response string
		expected float64
	}{
		{
			name:     "confidence above 1 is capped",
			response: `{"task_type": "computational", "confidence": 1.5, "reasoning": "test"}`,
			expected: 1.0,
		},
		{
			name:     "negative confidence is floored",
			response: `{"task_type": "computational", "confidence": -0.5, "reasoning": "test"}`,
			expected: 0.0,
		},
		{
			name:     "normal confidence preserved",
			response: `{"task_type": "computational", "confidence": 0.75, "reasoning": "test"}`,
			expected: 0.75,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &classifierMockLLMClient{response: tt.response}
			classifier := NewLLMClassifier(client)

			result, err := classifier.Classify(context.Background(), "test", nil)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result.Confidence)
		})
	}
}

// countingMockClient wraps a mock client and counts calls.
type countingMockClient struct {
	inner     *classifierMockLLMClient
	callCount *int
}

func (c *countingMockClient) Complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	*c.callCount++
	return c.inner.Complete(ctx, prompt, maxTokens)
}
