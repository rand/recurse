//go:build integration

package meta

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenRouterIntegration(t *testing.T) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}

	client, err := NewOpenRouterClient(OpenRouterConfig{
		APIKey: apiKey,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test a simple completion
	t.Run("simple_completion", func(t *testing.T) {
		resp, err := client.Complete(ctx, "What is 2+2? Answer with just the number.", 50)
		require.NoError(t, err)
		assert.Contains(t, resp, "4")
		t.Logf("Response: %s", resp)
	})

	// Test model selection with different contexts
	t.Run("model_selection", func(t *testing.T) {
		selector := &AdaptiveSelector{models: DefaultModels()}

		// Low budget should select fast tier
		spec := selector.SelectModel(ctx, "simple task", 500, 0)
		require.NotNil(t, spec)
		assert.Equal(t, TierFast, spec.Tier)
		t.Logf("Low budget selected: %s (tier: %d)", spec.Name, spec.Tier)

		// Math task should select reasoning tier
		spec = selector.SelectModel(ctx, "prove this theorem mathematically", 10000, 0)
		require.NotNil(t, spec)
		assert.Equal(t, TierReasoning, spec.Tier)
		t.Logf("Math task selected: %s (tier: %d)", spec.Name, spec.Tier)

		// Complex task should select powerful tier
		spec = selector.SelectModel(ctx, "analyze and refactor this codebase", 10000, 0)
		require.NotNil(t, spec)
		assert.Equal(t, TierPowerful, spec.Tier)
		t.Logf("Complex task selected: %s (tier: %d)", spec.Name, spec.Tier)
	})
}

func TestOpenRouterMetaControllerIntegration(t *testing.T) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}

	client, err := NewOpenRouterClient(OpenRouterConfig{
		APIKey: apiKey,
	})
	require.NoError(t, err)

	// Create meta-controller with OpenRouter client
	ctrl := NewController(client, DefaultConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test orchestration decision
	t.Run("orchestration_decision", func(t *testing.T) {
		state := State{
			Task:           "Explain how to implement a binary search tree",
			ContextTokens:  1000,
			BudgetRemain:   5000,
			RecursionDepth: 0,
			MaxDepth:       5,
		}

		decision, err := ctrl.Decide(ctx, state)
		require.NoError(t, err)
		require.NotNil(t, decision)

		t.Logf("Decision: %s", decision.Action)
		t.Logf("Reasoning: %s", decision.Reasoning)

		// Should be a valid action
		validActions := []Action{ActionDirect, ActionDecompose, ActionMemoryQuery, ActionSubcall, ActionSynthesize}
		assert.Contains(t, validActions, decision.Action)
	})
}
