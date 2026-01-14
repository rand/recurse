package routing

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/rlm/learning"
	"github.com/rand/recurse/internal/rlm/meta"
)

func TestNewLearnedRouter(t *testing.T) {
	router := NewLearnedRouter(DefaultRouterConfig())

	assert.NotNil(t, router)
	assert.Greater(t, len(router.models), 0)
	assert.NotNil(t, router.modelsByTier)
	assert.Equal(t, 0.6, router.confidenceThreshold)
}

func TestNewLearnedRouter_CustomConfig(t *testing.T) {
	cfg := RouterConfig{
		ConfidenceThreshold: 0.8,
		CostSensitivity:     0.3,
		Models: []meta.ModelSpec{
			{ID: "test-model", Tier: meta.TierFast},
		},
	}

	router := NewLearnedRouter(cfg)

	assert.Equal(t, 0.8, router.confidenceThreshold)
	assert.Equal(t, 0.3, router.costSensitivity)
	assert.Len(t, router.models, 1)
}

func TestLearnedRouter_Route_BasicSelection(t *testing.T) {
	router := NewLearnedRouter(DefaultRouterConfig())
	ctx := context.Background()

	decision := router.Route(ctx, "Write a simple function", "code", 0.5)

	require.NotNil(t, decision)
	assert.NotNil(t, decision.Model)
	assert.Greater(t, decision.Score, 0.0)
	assert.NotEmpty(t, decision.Reason)
}

func TestLearnedRouter_Route_CostSensitive(t *testing.T) {
	router := NewLearnedRouter(DefaultRouterConfig())
	ctx := context.Background()

	// High cost sensitivity should prefer cheaper models
	cheapDecision := router.Route(ctx, "Simple task", "general", 0.9)
	expensiveDecision := router.Route(ctx, "Simple task", "general", 0.1)

	require.NotNil(t, cheapDecision.Model)
	require.NotNil(t, expensiveDecision.Model)

	// Cheap decision should pick fast tier
	assert.Equal(t, meta.TierFast, cheapDecision.Model.Tier)
}

func TestLearnedRouter_Route_ReasoningTask(t *testing.T) {
	router := NewLearnedRouter(DefaultRouterConfig())
	ctx := context.Background()

	decision := router.Route(ctx, "Prove this theorem mathematically", "reasoning", 0.3)

	require.NotNil(t, decision.Model)
	// Should pick reasoning tier for math tasks
	assert.Equal(t, meta.TierReasoning, decision.Model.Tier)
}

func TestLearnedRouter_Route_ComplexTask(t *testing.T) {
	router := NewLearnedRouter(DefaultRouterConfig())
	ctx := context.Background()

	decision := router.Route(ctx, "Analyze and refactor this complex architecture", "analysis", 0.3)

	require.NotNil(t, decision.Model)
	// Should pick powerful tier for complex analysis
	assert.Equal(t, meta.TierPowerful, decision.Model.Tier)
}

func TestLearnedRouter_Route_WithLearner(t *testing.T) {
	learner, err := learning.NewContinuousLearner(learning.LearnerConfig{
		MinObservations: 1,
		LearningRate:    0.2,
		MaxAdjustment:   0.5,
		DecayRate:       1.0,
	})
	require.NoError(t, err)
	defer learner.Close()

	// Train the learner to prefer a specific model
	for i := 0; i < 5; i++ {
		learner.RecordOutcome(learning.ExecutionOutcome{
			QueryFeatures: learning.QueryFeatures{Category: "code"},
			StrategyUsed:  "direct",
			ModelUsed:     "anthropic/claude-haiku-4.5",
			Success:       true,
			QualityScore:  0.95,
		})
	}

	router := NewLearnedRouter(RouterConfig{
		Learner:         learner,
		CostSensitivity: 0.5,
	})

	ctx := context.Background()
	// Use high cost sensitivity to ensure fast tier selection (where haiku lives)
	decision := router.Route(ctx, "Simple code task", "code", 0.9)

	require.NotNil(t, decision.Model)
	// High cost sensitivity should pick fast tier, and haiku should be there
	assert.Equal(t, meta.TierFast, decision.Model.Tier)
	// The learned adjustment should have been applied
	adj := learner.GetRoutingAdjustment("code", decision.Model.ID)
	// If the model is haiku, it should have positive adjustment
	if decision.Model.ID == "anthropic/claude-haiku-4.5" {
		assert.Greater(t, adj, 0.0)
	}
}

func TestLearnedRouter_SelectModel_Interface(t *testing.T) {
	router := NewLearnedRouter(DefaultRouterConfig())
	ctx := context.Background()

	// Test that it implements meta.ModelSelector
	var _ meta.ModelSelector = router

	model := router.SelectModel(ctx, "Simple task", 10000, 0)
	assert.NotNil(t, model)
}

func TestLearnedRouter_SelectModel_BudgetAware(t *testing.T) {
	router := NewLearnedRouter(DefaultRouterConfig())
	ctx := context.Background()

	// Low budget should prefer cheaper models
	lowBudgetModel := router.SelectModel(ctx, "Task", 500, 0)
	highBudgetModel := router.SelectModel(ctx, "Task", 50000, 0)

	require.NotNil(t, lowBudgetModel)
	require.NotNil(t, highBudgetModel)

	// Low budget should pick fast tier
	assert.Equal(t, meta.TierFast, lowBudgetModel.Tier)
}

func TestLearnedRouter_SelectModel_DepthAware(t *testing.T) {
	router := NewLearnedRouter(DefaultRouterConfig())
	ctx := context.Background()

	// High depth should prefer cheaper models
	lowDepthModel := router.SelectModel(ctx, "Task", 10000, 1)
	highDepthModel := router.SelectModel(ctx, "Task", 10000, 5)

	require.NotNil(t, lowDepthModel)
	require.NotNil(t, highDepthModel)

	// High depth should pick fast tier
	assert.Equal(t, meta.TierFast, highDepthModel.Tier)
}

func TestLearnedRouter_CascadeRoute_Success(t *testing.T) {
	router := NewLearnedRouter(RouterConfig{
		ConfidenceThreshold: 0.7,
	})
	ctx := context.Background()

	// Mock executor that returns high confidence on first try
	executor := func(ctx context.Context, model *meta.ModelSpec, query string) (string, float64, float64, int64, error) {
		return "Success response", 0.9, 0.001, 100, nil
	}

	result, err := router.CascadeRoute(ctx, "Test query", "code", executor)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Success response", result.Response)
	assert.NotNil(t, result.FinalModel)
	assert.Equal(t, 1, result.Attempts)
	assert.False(t, result.Escalated)
}

func TestLearnedRouter_CascadeRoute_Escalation(t *testing.T) {
	router := NewLearnedRouter(RouterConfig{
		ConfidenceThreshold: 0.7,
	})
	ctx := context.Background()

	attempts := 0
	// Mock executor that returns low confidence initially, then high
	executor := func(ctx context.Context, model *meta.ModelSpec, query string) (string, float64, float64, int64, error) {
		attempts++
		if model.Tier == meta.TierFast {
			return "Low confidence", 0.4, 0.001, 50, nil
		}
		return "High confidence response", 0.9, 0.01, 200, nil
	}

	result, err := router.CascadeRoute(ctx, "Complex query", "analysis", executor)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "High confidence response", result.Response)
	assert.True(t, result.Escalated)
	assert.Greater(t, result.Attempts, 1)
}

func TestLearnedRouter_CascadeRoute_AllFail(t *testing.T) {
	router := NewLearnedRouter(RouterConfig{
		ConfidenceThreshold: 0.99, // Impossible to meet
	})
	ctx := context.Background()

	// Mock executor that always returns low confidence
	executor := func(ctx context.Context, model *meta.ModelSpec, query string) (string, float64, float64, int64, error) {
		return "Low confidence", 0.5, 0.001, 100, nil
	}

	result, err := router.CascadeRoute(ctx, "Query", "general", executor)

	// Should still return a result (last attempt)
	if result != nil {
		assert.NotEmpty(t, result.Response)
	} else {
		assert.Error(t, err)
	}
}

func TestLearnedRouter_CascadeRoute_Error(t *testing.T) {
	router := NewLearnedRouter(RouterConfig{
		ConfidenceThreshold: 0.7,
	})
	ctx := context.Background()

	attempts := 0
	// Mock executor that fails on first tier, succeeds on second
	executor := func(ctx context.Context, model *meta.ModelSpec, query string) (string, float64, float64, int64, error) {
		attempts++
		if model.Tier == meta.TierFast {
			return "", 0, 0, 0, errors.New("model error")
		}
		return "Success after error", 0.85, 0.01, 200, nil
	}

	result, err := router.CascadeRoute(ctx, "Query", "code", executor)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Success after error", result.Response)
	assert.True(t, result.Escalated)
}

func TestLearnedRouter_CascadeRoute_WithLearner(t *testing.T) {
	learner, err := learning.NewContinuousLearner(learning.DefaultLearnerConfig())
	require.NoError(t, err)
	defer learner.Close()

	router := NewLearnedRouter(RouterConfig{
		Learner:             learner,
		ConfidenceThreshold: 0.7,
	})
	ctx := context.Background()

	executor := func(ctx context.Context, model *meta.ModelSpec, query string) (string, float64, float64, int64, error) {
		return "Response", 0.85, 0.01, 100, nil
	}

	result, err := router.CascadeRoute(ctx, "Test query", "code", executor)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Learner should have recorded the outcome
	stats := learner.Stats()
	assert.NotEmpty(t, stats.RoutingAdjustments)
}

func TestLearnedRouter_Stats(t *testing.T) {
	router := NewLearnedRouter(DefaultRouterConfig())
	ctx := context.Background()

	// Make some routing decisions
	for i := 0; i < 5; i++ {
		router.Route(ctx, "Query", "code", 0.5)
	}

	stats := router.Stats()

	assert.Equal(t, int64(5), stats.TotalRoutes)
	assert.NotEmpty(t, stats.RoutesByModel)
}

func TestRouterStats_EscalationRate(t *testing.T) {
	tests := []struct {
		routes      int64
		escalations int64
		expected    float64
	}{
		{0, 0, 0},
		{10, 2, 20.0},
		{100, 15, 15.0},
		{50, 0, 0},
	}

	for _, tt := range tests {
		stats := RouterStats{
			TotalRoutes:      tt.routes,
			TotalEscalations: tt.escalations,
		}
		assert.InDelta(t, tt.expected, stats.EscalationRate(), 0.001)
	}
}

func TestClassifyQuery(t *testing.T) {
	tests := []struct {
		query    string
		expected string
	}{
		{"Write a function", "code"},
		{"Implement this feature", "code"},
		{"Fix the bug in main.go", "code"},
		{"Analyze this architecture", "analysis"},
		{"Explain how this works", "analysis"},
		{"Prove this theorem", "reasoning"},
		{"Calculate the result", "reasoning"},
		{"Hello, how are you?", "general"},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			result := classifyQuery(tt.query)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTierName(t *testing.T) {
	tests := []struct {
		tier     meta.ModelTier
		expected string
	}{
		{meta.TierFast, "fast"},
		{meta.TierBalanced, "balanced"},
		{meta.TierPowerful, "powerful"},
		{meta.TierReasoning, "reasoning"},
		{meta.ModelTier(99), "unknown"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tierName(tt.tier))
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		value, min, max, expected float64
	}{
		{0.5, 0.0, 1.0, 0.5},
		{-0.5, 0.0, 1.0, 0.0},
		{1.5, 0.0, 1.0, 1.0},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, clamp(tt.value, tt.min, tt.max))
	}
}

func TestDefaultRouterConfig(t *testing.T) {
	cfg := DefaultRouterConfig()

	assert.Equal(t, 0.6, cfg.ConfidenceThreshold)
	assert.Equal(t, 0.5, cfg.CostSensitivity)
	assert.Len(t, cfg.CascadeOrder, 4)
}

// Benchmarks

func BenchmarkRoute(b *testing.B) {
	router := NewLearnedRouter(DefaultRouterConfig())
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		router.Route(ctx, "Write a function to process data", "code", 0.5)
	}
}

func BenchmarkRouteWithLearner(b *testing.B) {
	learner, _ := learning.NewContinuousLearner(learning.DefaultLearnerConfig())
	defer learner.Close()

	router := NewLearnedRouter(RouterConfig{Learner: learner})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		router.Route(ctx, "Write a function to process data", "code", 0.5)
	}
}

func BenchmarkSelectModel(b *testing.B) {
	router := NewLearnedRouter(DefaultRouterConfig())
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		router.SelectModel(ctx, "Process this task", 10000, 2)
	}
}
