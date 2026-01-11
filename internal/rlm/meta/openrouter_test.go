package meta

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultModels(t *testing.T) {
	models := DefaultModels()
	assert.NotEmpty(t, models)

	// Verify we have models in each tier
	tierCounts := make(map[ModelTier]int)
	for _, m := range models {
		tierCounts[m.Tier]++
	}

	assert.Greater(t, tierCounts[TierFast], 0, "should have fast tier models")
	assert.Greater(t, tierCounts[TierBalanced], 0, "should have balanced tier models")
	assert.Greater(t, tierCounts[TierPowerful], 0, "should have powerful tier models")
	assert.Greater(t, tierCounts[TierReasoning], 0, "should have reasoning tier models")
}

func TestAdaptiveSelector_SelectModel(t *testing.T) {
	models := DefaultModels()
	selector := &AdaptiveSelector{models: models}
	ctx := context.Background()

	tests := []struct {
		name         string
		task         string
		budget       int
		depth        int
		expectedTier ModelTier
	}{
		{
			name:         "low budget uses fast tier",
			task:         "simple task",
			budget:       500,
			depth:        0,
			expectedTier: TierFast,
		},
		{
			name:         "moderate budget uses balanced tier",
			task:         "moderate task",
			budget:       3000,
			depth:        0,
			expectedTier: TierBalanced,
		},
		{
			name:         "high depth uses fast tier",
			task:         "simple task",
			budget:       10000,
			depth:        4,
			expectedTier: TierFast,
		},
		{
			name:         "reasoning keywords use reasoning tier",
			task:         "prove this theorem mathematically",
			budget:       10000,
			depth:        0,
			expectedTier: TierReasoning,
		},
		{
			name:         "complex keywords with budget use powerful tier",
			task:         "analyze and refactor this code",
			budget:       10000,
			depth:        0,
			expectedTier: TierPowerful,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := selector.SelectModel(ctx, tt.task, tt.budget, tt.depth)
			require.NotNil(t, spec)
			assert.Equal(t, tt.expectedTier, spec.Tier, "expected tier %v, got %v for model %s", tt.expectedTier, spec.Tier, spec.ID)
		})
	}
}

func TestAdaptiveSelector_DetermineTier(t *testing.T) {
	selector := &AdaptiveSelector{models: DefaultModels()}

	tests := []struct {
		name         string
		task         string
		budget       int
		depth        int
		expectedTier ModelTier
	}{
		{"very low budget", "any task", 100, 0, TierFast},
		{"low budget", "any task", 800, 0, TierFast},
		{"moderate budget", "any task", 2000, 0, TierBalanced},
		{"high budget", "any task", 8000, 0, TierBalanced},
		{"depth 2", "any task", 10000, 2, TierBalanced},
		{"depth 3", "any task", 10000, 3, TierFast},
		{"depth 4", "any task", 10000, 4, TierFast},
		{"math task", "calculate the derivative", 10000, 0, TierReasoning},
		{"logic task", "prove by induction", 10000, 0, TierReasoning},
		{"analyze task", "analyze this code", 10000, 0, TierPowerful},
		{"analyze low budget", "analyze this code", 2000, 0, TierBalanced},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tier := selector.determineTier(tt.task, tt.budget, tt.depth)
			assert.Equal(t, tt.expectedTier, tier)
		})
	}
}

func TestExtractContext(t *testing.T) {
	tests := []struct {
		name           string
		prompt         string
		expectedBudget int
		expectedDepth  int
	}{
		{
			name:           "no context",
			prompt:         "simple prompt",
			expectedBudget: 10000,
			expectedDepth:  0,
		},
		{
			name:           "with budget",
			prompt:         "Task: do something\nBudget remaining: 5000 tokens",
			expectedBudget: 5000,
			expectedDepth:  0,
		},
		{
			name:           "with depth",
			prompt:         "Task: do something\nRecursion depth: 3/5",
			expectedBudget: 10000,
			expectedDepth:  3,
		},
		{
			name:           "with both",
			prompt:         "Task: do something\nBudget remaining: 2500 tokens\nRecursion depth: 2/5",
			expectedBudget: 2500,
			expectedDepth:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget, depth := extractContext(tt.prompt)
			assert.Equal(t, tt.expectedBudget, budget)
			assert.Equal(t, tt.expectedDepth, depth)
		})
	}
}

func TestNewOpenRouterClient_NoAPIKey(t *testing.T) {
	// Ensure env var is not set
	t.Setenv("OPENROUTER_API_KEY", "")

	_, err := NewOpenRouterClient(OpenRouterConfig{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "API key")
}

func TestModelSpec_Fields(t *testing.T) {
	spec := ModelSpec{
		ID:          "test/model",
		Name:        "Test Model",
		Tier:        TierBalanced,
		InputCost:   1.0,
		OutputCost:  2.0,
		ContextSize: 100000,
		Strengths:   []string{"testing", "example"},
	}

	assert.Equal(t, "test/model", spec.ID)
	assert.Equal(t, "Test Model", spec.Name)
	assert.Equal(t, TierBalanced, spec.Tier)
	assert.Equal(t, 1.0, spec.InputCost)
	assert.Equal(t, 2.0, spec.OutputCost)
	assert.Equal(t, 100000, spec.ContextSize)
	assert.Contains(t, spec.Strengths, "testing")
}
