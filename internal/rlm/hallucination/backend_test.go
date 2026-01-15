package hallucination

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLLMClient implements LLMCompleter for testing.
type mockLLMClient struct {
	responses []string
	callCount int
	err       error
}

func (m *mockLLMClient) Complete(_ context.Context, _ string, _ int) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if m.callCount < len(m.responses) {
		resp := m.responses[m.callCount]
		m.callCount++
		return resp, nil
	}
	m.callCount++
	return "YES", nil
}

// mockLogprobsClient implements LogprobsCompleter for testing.
type mockLogprobsClient struct {
	mockLLMClient
	logprobs map[string]float64
}

func (m *mockLogprobsClient) CompleteWithLogprobs(_ context.Context, _ string, _ int) (string, map[string]float64, error) {
	if m.err != nil {
		return "", nil, m.err
	}
	return "YES", m.logprobs, nil
}

func TestMockBackend(t *testing.T) {
	t.Run("fixed probability", func(t *testing.T) {
		backend := NewMockBackend(0.75)
		ctx := context.Background()

		prob, err := backend.EstimateProbability(ctx, "test claim", "test context")

		require.NoError(t, err)
		assert.Equal(t, 0.75, prob)
		assert.Equal(t, 1, backend.CallCount())

		lastClaim, lastCtx := backend.LastCall()
		assert.Equal(t, "test claim", lastClaim)
		assert.Equal(t, "test context", lastCtx)
	})

	t.Run("custom handler", func(t *testing.T) {
		backend := NewMockBackendWithHandler(func(claim, context string) float64 {
			if strings.Contains(claim, "true") {
				return 0.9
			}
			return 0.1
		})
		ctx := context.Background()

		prob1, _ := backend.EstimateProbability(ctx, "this is true", "context")
		prob2, _ := backend.EstimateProbability(ctx, "this is false", "context")

		assert.Equal(t, 0.9, prob1)
		assert.Equal(t, 0.1, prob2)
	})

	t.Run("batch estimate", func(t *testing.T) {
		backend := NewMockBackend(0.8)
		ctx := context.Background()

		probs, err := backend.BatchEstimate(ctx, []string{"claim1", "claim2", "claim3"}, "context")

		require.NoError(t, err)
		require.Len(t, probs, 3)
		for _, p := range probs {
			assert.Equal(t, 0.8, p)
		}
		assert.Equal(t, 3, backend.CallCount())
	})
}

func TestSelfVerifyBackend_Sampling(t *testing.T) {
	t.Run("all yes responses", func(t *testing.T) {
		client := &mockLLMClient{
			responses: []string{"YES", "YES", "YES", "YES", "YES"},
		}
		backend := NewSelfVerifyBackend(client, 5*time.Second, 5)
		ctx := context.Background()

		prob, err := backend.EstimateProbability(ctx, "test claim", "test context")

		require.NoError(t, err)
		assert.Equal(t, 1.0, prob)
	})

	t.Run("all no responses", func(t *testing.T) {
		client := &mockLLMClient{
			responses: []string{"NO", "NO", "NO", "NO", "NO"},
		}
		backend := NewSelfVerifyBackend(client, 5*time.Second, 5)
		ctx := context.Background()

		prob, err := backend.EstimateProbability(ctx, "test claim", "test context")

		require.NoError(t, err)
		assert.Equal(t, 0.0, prob)
	})

	t.Run("mixed responses", func(t *testing.T) {
		client := &mockLLMClient{
			responses: []string{"YES", "NO", "YES", "NO", "YES"},
		}
		backend := NewSelfVerifyBackend(client, 5*time.Second, 5)
		ctx := context.Background()

		prob, err := backend.EstimateProbability(ctx, "test claim", "test context")

		require.NoError(t, err)
		assert.Equal(t, 0.6, prob) // 3 yes out of 5
	})

	t.Run("handles errors gracefully", func(t *testing.T) {
		client := &mockLLMClient{
			err: errors.New("API error"),
		}
		backend := NewSelfVerifyBackend(client, 5*time.Second, 3)
		ctx := context.Background()

		prob, err := backend.EstimateProbability(ctx, "test claim", "test context")

		// Should return neutral probability on all errors
		require.NoError(t, err)
		assert.Equal(t, 0.0, prob) // All samples failed
	})
}

func TestSelfVerifyBackend_WithLogprobs(t *testing.T) {
	t.Run("uses logprobs when available", func(t *testing.T) {
		client := &mockLogprobsClient{
			logprobs: map[string]float64{
				"YES": -0.1, // High probability
				"NO":  -2.5, // Low probability
			},
		}
		backend := NewSelfVerifyBackend(client, 5*time.Second, 5)
		ctx := context.Background()

		prob, err := backend.EstimateProbability(ctx, "test claim", "test context")

		require.NoError(t, err)
		assert.Greater(t, prob, 0.5, "YES should have higher probability")
	})
}

func TestHaikuBackend(t *testing.T) {
	t.Run("sampling estimation", func(t *testing.T) {
		client := &mockLLMClient{
			responses: []string{"YES", "YES", "NO"},
		}
		backend := NewHaikuBackend(client, 3*time.Second, 3)
		ctx := context.Background()

		prob, err := backend.EstimateProbability(ctx, "test claim", "test context")

		require.NoError(t, err)
		assert.InDelta(t, 0.667, prob, 0.01) // 2 yes out of 3
	})

	t.Run("batch estimate", func(t *testing.T) {
		client := &mockLLMClient{
			responses: []string{"YES", "YES", "YES", "NO", "NO", "NO"},
		}
		backend := NewHaikuBackend(client, 3*time.Second, 3)
		ctx := context.Background()

		probs, err := backend.BatchEstimate(ctx, []string{"claim1", "claim2"}, "context")

		require.NoError(t, err)
		require.Len(t, probs, 2)
		assert.Equal(t, 1.0, probs[0]) // First 3: all YES
		assert.Equal(t, 0.0, probs[1]) // Next 3: all NO
	})
}

func TestCachingBackend(t *testing.T) {
	t.Run("caches results", func(t *testing.T) {
		mock := NewMockBackend(0.8)
		backend := NewCachingBackend(mock, 5*time.Minute, 100)
		ctx := context.Background()

		// First call
		prob1, err := backend.EstimateProbability(ctx, "claim", "context")
		require.NoError(t, err)
		assert.Equal(t, 0.8, prob1)
		assert.Equal(t, 1, mock.CallCount())

		// Second call - should be cached
		prob2, err := backend.EstimateProbability(ctx, "claim", "context")
		require.NoError(t, err)
		assert.Equal(t, 0.8, prob2)
		assert.Equal(t, 1, mock.CallCount()) // No additional calls
	})

	t.Run("different claims not cached together", func(t *testing.T) {
		mock := NewMockBackend(0.8)
		backend := NewCachingBackend(mock, 5*time.Minute, 100)
		ctx := context.Background()

		backend.EstimateProbability(ctx, "claim1", "context")
		backend.EstimateProbability(ctx, "claim2", "context")

		assert.Equal(t, 2, mock.CallCount())
	})

	t.Run("different contexts not cached together", func(t *testing.T) {
		mock := NewMockBackend(0.8)
		backend := NewCachingBackend(mock, 5*time.Minute, 100)
		ctx := context.Background()

		backend.EstimateProbability(ctx, "claim", "context1")
		backend.EstimateProbability(ctx, "claim", "context2")

		assert.Equal(t, 2, mock.CallCount())
	})

	t.Run("batch uses cache", func(t *testing.T) {
		mock := NewMockBackend(0.8)
		backend := NewCachingBackend(mock, 5*time.Minute, 100)
		ctx := context.Background()

		// Pre-populate cache
		backend.EstimateProbability(ctx, "claim1", "context")
		backend.EstimateProbability(ctx, "claim2", "context")

		// Batch should use cache
		probs, err := backend.BatchEstimate(ctx, []string{"claim1", "claim2", "claim3"}, "context")
		require.NoError(t, err)
		require.Len(t, probs, 3)

		// Only claim3 should cause new call
		assert.Equal(t, 3, mock.CallCount())
	})

	t.Run("evicts old entries at capacity", func(t *testing.T) {
		mock := NewMockBackend(0.8)
		backend := NewCachingBackend(mock, 5*time.Minute, 3) // Very small cache
		ctx := context.Background()

		// Fill cache
		backend.EstimateProbability(ctx, "claim1", "context")
		backend.EstimateProbability(ctx, "claim2", "context")
		backend.EstimateProbability(ctx, "claim3", "context")
		assert.Equal(t, 3, mock.CallCount())

		// Add fourth entry, evicting first
		backend.EstimateProbability(ctx, "claim4", "context")
		assert.Equal(t, 4, mock.CallCount())

		// claim1 should be evicted and re-fetched
		backend.EstimateProbability(ctx, "claim1", "context")
		assert.Equal(t, 5, mock.CallCount())

		// claim2 might be evicted too
		size, _, _ := backend.CacheStats()
		assert.LessOrEqual(t, size, 3)
	})

	t.Run("clear cache", func(t *testing.T) {
		mock := NewMockBackend(0.8)
		backend := NewCachingBackend(mock, 5*time.Minute, 100)
		ctx := context.Background()

		backend.EstimateProbability(ctx, "claim", "context")
		assert.Equal(t, 1, mock.CallCount())

		backend.ClearCache()

		backend.EstimateProbability(ctx, "claim", "context")
		assert.Equal(t, 2, mock.CallCount()) // Cache was cleared
	})
}

func TestNewBackend(t *testing.T) {
	client := &mockLLMClient{}

	t.Run("creates self backend", func(t *testing.T) {
		cfg := BackendConfig{Type: BackendTypeSelf, CacheTTL: 0}
		backend, err := NewBackend(cfg, client)

		require.NoError(t, err)
		assert.Equal(t, "self", backend.Name())
	})

	t.Run("creates haiku backend", func(t *testing.T) {
		cfg := BackendConfig{Type: BackendTypeHaiku, CacheTTL: 0}
		backend, err := NewBackend(cfg, client)

		require.NoError(t, err)
		assert.Equal(t, "haiku", backend.Name())
	})

	t.Run("creates mock backend", func(t *testing.T) {
		cfg := BackendConfig{Type: BackendTypeMock, CacheTTL: 0}
		backend, err := NewBackend(cfg, client)

		require.NoError(t, err)
		assert.Equal(t, "mock", backend.Name())
	})

	t.Run("wraps with cache when TTL > 0", func(t *testing.T) {
		cfg := BackendConfig{Type: BackendTypeMock, CacheTTL: 5 * time.Minute}
		backend, err := NewBackend(cfg, client)

		require.NoError(t, err)
		assert.Contains(t, backend.Name(), "cached")
	})

	t.Run("errors on unknown type", func(t *testing.T) {
		cfg := BackendConfig{Type: "unknown"}
		_, err := NewBackend(cfg, client)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown backend type")
	})
}

func TestBuildVerificationPrompt(t *testing.T) {
	prompt := BuildVerificationPrompt("The sky is blue", "Scientific evidence about colors")

	assert.Contains(t, prompt, "Scientific evidence about colors")
	assert.Contains(t, prompt, "The sky is blue")
	assert.Contains(t, prompt, "YES or NO")
}

func TestIsYesResponse(t *testing.T) {
	tests := []struct {
		response string
		expected bool
	}{
		{"YES", true},
		{"yes", true},
		{"Yes", true},
		{"Y", true},
		{"y", true},
		{"true", true},
		{"TRUE", true},
		{"Yes, that is correct", true},
		{"NO", false},
		{"no", false},
		{"No", false},
		{"N", false},
		{"false", false},
		{"FALSE", false},
		{"No, that is incorrect", false},
		{"", false},
		{"maybe", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.response, func(t *testing.T) {
			result := isYesResponse(tt.response)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractProbabilityFromLogprobs(t *testing.T) {
	t.Run("yes higher probability", func(t *testing.T) {
		logprobs := map[string]float64{
			"YES": -0.1,
			"NO":  -3.0,
		}
		prob := extractProbabilityFromLogprobs(logprobs)
		assert.Greater(t, prob, 0.9, "YES should dominate")
	})

	t.Run("no higher probability", func(t *testing.T) {
		logprobs := map[string]float64{
			"YES": -3.0,
			"NO":  -0.1,
		}
		prob := extractProbabilityFromLogprobs(logprobs)
		assert.Less(t, prob, 0.1, "NO should dominate")
	})

	t.Run("equal probabilities", func(t *testing.T) {
		logprobs := map[string]float64{
			"YES": -1.0,
			"NO":  -1.0,
		}
		prob := extractProbabilityFromLogprobs(logprobs)
		assert.InDelta(t, 0.5, prob, 0.01)
	})

	t.Run("empty logprobs", func(t *testing.T) {
		prob := extractProbabilityFromLogprobs(map[string]float64{})
		assert.Equal(t, 0.5, prob)
	})

	t.Run("handles lowercase tokens", func(t *testing.T) {
		logprobs := map[string]float64{
			"yes": -0.1,
			"no":  -3.0,
		}
		prob := extractProbabilityFromLogprobs(logprobs)
		assert.Greater(t, prob, 0.5)
	})
}

func TestDefaultBackendConfig(t *testing.T) {
	cfg := DefaultBackendConfig()

	assert.Equal(t, BackendTypeSelf, cfg.Type)
	assert.Equal(t, 5*time.Second, cfg.Timeout)
	assert.Equal(t, 15*time.Minute, cfg.CacheTTL)
	assert.Equal(t, 10, cfg.MaxBatchSize)
	assert.True(t, cfg.SamplingFallback)
	assert.Equal(t, 5, cfg.SamplingCount)
}
