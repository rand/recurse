package learning

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewContinuousLearner(t *testing.T) {
	learner, err := NewContinuousLearner(DefaultLearnerConfig())
	require.NoError(t, err)
	defer learner.Close()

	assert.NotNil(t, learner)
	assert.NotNil(t, learner.routingAdjustments)
	assert.NotNil(t, learner.strategyPreferences)
}

func TestNewContinuousLearner_WithPersistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "learning.db")

	cfg := DefaultLearnerConfig()
	cfg.DBPath = dbPath

	learner, err := NewContinuousLearner(cfg)
	require.NoError(t, err)
	defer learner.Close()

	assert.NotNil(t, learner.db)
	assert.FileExists(t, dbPath)
}

func TestContinuousLearner_RecordOutcome_Success(t *testing.T) {
	learner, err := NewContinuousLearner(DefaultLearnerConfig())
	require.NoError(t, err)
	defer learner.Close()

	outcome := ExecutionOutcome{
		Query: "Write a Go function",
		QueryFeatures: QueryFeatures{
			Category:      "code",
			Complexity:    0.5,
			RequiresCode:  true,
			Domain:        "go",
		},
		StrategyUsed: "chain",
		ModelUsed:    "claude-3-haiku",
		Success:      true,
		QualityScore: 0.8,
		Cost:         0.001,
		LatencyMS:    1500,
		Timestamp:    time.Now(),
	}

	learner.RecordOutcome(outcome)

	stats := learner.Stats()
	assert.Contains(t, stats.RoutingAdjustments, "code:claude-3-haiku")
	assert.Contains(t, stats.StrategyPreferences, "code:chain")
	assert.Equal(t, 1, stats.ObservationCounts["code:claude-3-haiku"])
}

func TestContinuousLearner_RecordOutcome_Failure(t *testing.T) {
	learner, err := NewContinuousLearner(DefaultLearnerConfig())
	require.NoError(t, err)
	defer learner.Close()

	outcome := ExecutionOutcome{
		Query: "Complex analysis",
		QueryFeatures: QueryFeatures{
			Category:          "analysis",
			Complexity:        0.8,
			RequiresReasoning: true,
		},
		StrategyUsed: "chain",
		ModelUsed:    "claude-3-haiku",
		Success:      false,
		QualityScore: 0.2,
		Cost:         0.002,
		LatencyMS:    5000,
		Timestamp:    time.Now(),
	}

	learner.RecordOutcome(outcome)

	stats := learner.Stats()
	// Failed outcome should result in negative adjustment
	assert.Less(t, stats.RoutingAdjustments["analysis:claude-3-haiku"], 0.0)
}

func TestContinuousLearner_GetRoutingAdjustment_InsufficientObservations(t *testing.T) {
	cfg := DefaultLearnerConfig()
	cfg.MinObservations = 3

	learner, err := NewContinuousLearner(cfg)
	require.NoError(t, err)
	defer learner.Close()

	// Record fewer outcomes than MinObservations
	outcome := ExecutionOutcome{
		QueryFeatures: QueryFeatures{Category: "code"},
		StrategyUsed:  "chain",
		ModelUsed:     "claude-3-haiku",
		Success:       true,
		QualityScore:  0.9,
	}
	learner.RecordOutcome(outcome)

	// Should return 0 due to insufficient observations
	adj := learner.GetRoutingAdjustment("code", "claude-3-haiku")
	assert.Equal(t, 0.0, adj)
}

func TestContinuousLearner_GetRoutingAdjustment_SufficientObservations(t *testing.T) {
	cfg := DefaultLearnerConfig()
	cfg.MinObservations = 3

	learner, err := NewContinuousLearner(cfg)
	require.NoError(t, err)
	defer learner.Close()

	// Record enough successful outcomes
	for i := 0; i < 5; i++ {
		outcome := ExecutionOutcome{
			QueryFeatures: QueryFeatures{Category: "code"},
			StrategyUsed:  "chain",
			ModelUsed:     "claude-3-haiku",
			Success:       true,
			QualityScore:  0.85,
		}
		learner.RecordOutcome(outcome)
	}

	// Should return positive adjustment
	adj := learner.GetRoutingAdjustment("code", "claude-3-haiku")
	assert.Greater(t, adj, 0.0)
}

func TestContinuousLearner_GetStrategyPreference(t *testing.T) {
	cfg := DefaultLearnerConfig()
	cfg.MinObservations = 2

	learner, err := NewContinuousLearner(cfg)
	require.NoError(t, err)
	defer learner.Close()

	// Record outcomes with different strategies
	for i := 0; i < 3; i++ {
		learner.RecordOutcome(ExecutionOutcome{
			QueryFeatures: QueryFeatures{Category: "reasoning"},
			StrategyUsed:  "tree",
			ModelUsed:     "claude-3-opus",
			Success:       true,
			QualityScore:  0.9,
		})
	}

	for i := 0; i < 3; i++ {
		learner.RecordOutcome(ExecutionOutcome{
			QueryFeatures: QueryFeatures{Category: "reasoning"},
			StrategyUsed:  "chain",
			ModelUsed:     "claude-3-opus",
			Success:       false,
			QualityScore:  0.3,
		})
	}

	// Tree should be preferred over chain for reasoning
	treePref := learner.GetStrategyPreference("reasoning", "tree")
	chainPref := learner.GetStrategyPreference("reasoning", "chain")
	assert.Greater(t, treePref, chainPref)
}

func TestContinuousLearner_BestModelForQueryType(t *testing.T) {
	cfg := DefaultLearnerConfig()
	cfg.MinObservations = 2

	learner, err := NewContinuousLearner(cfg)
	require.NoError(t, err)
	defer learner.Close()

	// Train with different models
	for i := 0; i < 3; i++ {
		learner.RecordOutcome(ExecutionOutcome{
			QueryFeatures: QueryFeatures{Category: "code"},
			StrategyUsed:  "chain",
			ModelUsed:     "claude-3-opus",
			Success:       true,
			QualityScore:  0.95,
		})
	}

	for i := 0; i < 3; i++ {
		learner.RecordOutcome(ExecutionOutcome{
			QueryFeatures: QueryFeatures{Category: "code"},
			StrategyUsed:  "chain",
			ModelUsed:     "claude-3-haiku",
			Success:       true,
			QualityScore:  0.7,
		})
	}

	candidates := []string{"claude-3-opus", "claude-3-haiku", "claude-3-sonnet"}
	best := learner.BestModelForQueryType("code", candidates)
	assert.Equal(t, "claude-3-opus", best)
}

func TestContinuousLearner_BestModelForQueryType_NoData(t *testing.T) {
	learner, err := NewContinuousLearner(DefaultLearnerConfig())
	require.NoError(t, err)
	defer learner.Close()

	candidates := []string{"claude-3-opus", "claude-3-haiku"}
	best := learner.BestModelForQueryType("unknown", candidates)
	assert.Empty(t, best) // No data, should return empty
}

func TestContinuousLearner_BestStrategyForQueryType(t *testing.T) {
	cfg := DefaultLearnerConfig()
	cfg.MinObservations = 2

	learner, err := NewContinuousLearner(cfg)
	require.NoError(t, err)
	defer learner.Close()

	// LATS works well for complex tasks
	for i := 0; i < 3; i++ {
		learner.RecordOutcome(ExecutionOutcome{
			QueryFeatures: QueryFeatures{Category: "complex", Complexity: 0.9},
			StrategyUsed:  "lats",
			ModelUsed:     "claude-3-opus",
			Success:       true,
			QualityScore:  0.92,
		})
	}

	// Chain doesn't work as well
	for i := 0; i < 3; i++ {
		learner.RecordOutcome(ExecutionOutcome{
			QueryFeatures: QueryFeatures{Category: "complex", Complexity: 0.9},
			StrategyUsed:  "chain",
			ModelUsed:     "claude-3-opus",
			Success:       false,
			QualityScore:  0.4,
		})
	}

	candidates := []string{"chain", "tree", "lats"}
	best := learner.BestStrategyForQueryType("complex", candidates)
	assert.Equal(t, "lats", best)
}

func TestContinuousLearner_MaxAdjustmentBound(t *testing.T) {
	cfg := DefaultLearnerConfig()
	cfg.MaxAdjustment = 0.5
	cfg.LearningRate = 0.5 // Higher learning rate for faster adjustment

	learner, err := NewContinuousLearner(cfg)
	require.NoError(t, err)
	defer learner.Close()

	// Record many successful outcomes
	for i := 0; i < 20; i++ {
		learner.RecordOutcome(ExecutionOutcome{
			QueryFeatures: QueryFeatures{Category: "test"},
			StrategyUsed:  "chain",
			ModelUsed:     "model-a",
			Success:       true,
			QualityScore:  1.0,
		})
	}

	stats := learner.Stats()
	adj := stats.RoutingAdjustments["test:model-a"]
	assert.LessOrEqual(t, adj, cfg.MaxAdjustment)
	assert.GreaterOrEqual(t, adj, -cfg.MaxAdjustment)
}

func TestContinuousLearner_Reset(t *testing.T) {
	learner, err := NewContinuousLearner(DefaultLearnerConfig())
	require.NoError(t, err)
	defer learner.Close()

	// Record some outcomes
	learner.RecordOutcome(ExecutionOutcome{
		QueryFeatures: QueryFeatures{Category: "code"},
		StrategyUsed:  "chain",
		ModelUsed:     "model-a",
		Success:       true,
		QualityScore:  0.8,
	})

	stats := learner.Stats()
	assert.NotEmpty(t, stats.RoutingAdjustments)

	// Reset
	err = learner.Reset()
	require.NoError(t, err)

	stats = learner.Stats()
	assert.Empty(t, stats.RoutingAdjustments)
	assert.Empty(t, stats.StrategyPreferences)
	assert.Empty(t, stats.ObservationCounts)
}

func TestContinuousLearner_Persistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "learning.db")

	// Create learner and record outcomes
	cfg := DefaultLearnerConfig()
	cfg.DBPath = dbPath
	cfg.MinObservations = 1

	learner1, err := NewContinuousLearner(cfg)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		learner1.RecordOutcome(ExecutionOutcome{
			QueryFeatures: QueryFeatures{Category: "code"},
			StrategyUsed:  "chain",
			ModelUsed:     "model-a",
			Success:       true,
			QualityScore:  0.85,
		})
	}

	// Wait for async persistence
	time.Sleep(100 * time.Millisecond)
	learner1.Close()

	// Create new learner with same DB
	learner2, err := NewContinuousLearner(cfg)
	require.NoError(t, err)
	defer learner2.Close()

	// Should have loaded previous state
	adj := learner2.GetRoutingAdjustment("code", "model-a")
	assert.Greater(t, adj, 0.0)
}

func TestContinuousLearner_DecayOverTime(t *testing.T) {
	cfg := DefaultLearnerConfig()
	cfg.DecayRate = 0.5 // Aggressive decay for testing

	learner, err := NewContinuousLearner(cfg)
	require.NoError(t, err)
	defer learner.Close()

	// Manually set up state with old timestamp
	learner.mu.Lock()
	learner.routingAdjustments["test:model"] = 0.4
	learner.observationCounts["test:model"] = 10
	learner.lastObservation["test:model"] = time.Now().Add(-48 * time.Hour) // 2 days ago
	learner.mu.Unlock()

	// Decay should reduce the adjustment
	adj := learner.GetRoutingAdjustment("test", "model")
	assert.Less(t, adj, 0.4)
	assert.Greater(t, adj, 0.0) // But still positive
}

func TestComputeReward(t *testing.T) {
	learner, _ := NewContinuousLearner(DefaultLearnerConfig())
	defer learner.Close()

	tests := []struct {
		name         string
		outcome      ExecutionOutcome
		expectSign   string // "positive", "negative", "near_zero"
	}{
		{
			name: "high quality success",
			outcome: ExecutionOutcome{
				Success:       true,
				QualityScore:  0.95,
				QueryFeatures: QueryFeatures{Complexity: 0.5},
				Cost:          0.001,
				LatencyMS:     500,
			},
			expectSign: "positive",
		},
		{
			name: "low quality failure",
			outcome: ExecutionOutcome{
				Success:       false,
				QualityScore:  0.2,
				QueryFeatures: QueryFeatures{Complexity: 0.5},
				Cost:          0.01,
				LatencyMS:     10000,
			},
			expectSign: "negative",
		},
		{
			name: "mediocre success",
			outcome: ExecutionOutcome{
				Success:       true,
				QualityScore:  0.5,
				QueryFeatures: QueryFeatures{Complexity: 0.5},
				Cost:          0.005,
				LatencyMS:     2000,
			},
			expectSign: "positive", // Success still pushes it positive
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reward := learner.computeReward(tt.outcome)

			switch tt.expectSign {
			case "positive":
				assert.Greater(t, reward, 0.0)
			case "negative":
				assert.Less(t, reward, 0.0)
			case "near_zero":
				assert.InDelta(t, 0.0, reward, 0.1)
			}

			// Reward should always be bounded
			assert.LessOrEqual(t, reward, 1.0)
			assert.GreaterOrEqual(t, reward, -1.0)
		})
	}
}

func TestQueryFeatures_JSON(t *testing.T) {
	features := QueryFeatures{
		Category:          "code",
		Complexity:        0.7,
		EstimatedTokens:   2000,
		RequiresReasoning: true,
		RequiresCode:      true,
		Domain:            "go",
	}

	outcome := ExecutionOutcome{
		Query:         "test",
		QueryFeatures: features,
		StrategyUsed:  "chain",
		ModelUsed:     "model",
		Success:       true,
	}

	stats := LearnerStats{
		RoutingAdjustments: map[string]float64{"code:model": 0.5},
	}

	json := stats.ToJSON()
	assert.Contains(t, json, "routing_adjustments")
	assert.Contains(t, json, "code:model")
	assert.NotEmpty(t, outcome.Query)
}

func TestClamp(t *testing.T) {
	tests := []struct {
		value, min, max, expected float64
	}{
		{0.5, 0.0, 1.0, 0.5},   // Within range
		{-0.5, 0.0, 1.0, 0.0},  // Below min
		{1.5, 0.0, 1.0, 1.0},   // Above max
		{-1.0, -0.5, 0.5, -0.5}, // Below min with negative range
		{1.0, -0.5, 0.5, 0.5},   // Above max with negative range
	}

	for _, tt := range tests {
		result := clamp(tt.value, tt.min, tt.max)
		assert.Equal(t, tt.expected, result)
	}
}

func TestContinuousLearner_ConcurrentAccess(t *testing.T) {
	learner, err := NewContinuousLearner(DefaultLearnerConfig())
	require.NoError(t, err)
	defer learner.Close()

	done := make(chan struct{})

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			learner.RecordOutcome(ExecutionOutcome{
				QueryFeatures: QueryFeatures{Category: "code"},
				StrategyUsed:  "chain",
				ModelUsed:     "model",
				Success:       i%2 == 0,
				QualityScore:  float64(i%100) / 100.0,
			})
		}
		done <- struct{}{}
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = learner.GetRoutingAdjustment("code", "model")
			_ = learner.GetStrategyPreference("code", "chain")
			_ = learner.Stats()
		}
		done <- struct{}{}
	}()

	// Wait for both
	<-done
	<-done
}

func TestDefaultLearnerConfig(t *testing.T) {
	cfg := DefaultLearnerConfig()

	assert.Equal(t, 0.1, cfg.LearningRate)
	assert.Equal(t, 0.95, cfg.DecayRate)
	assert.Equal(t, 3, cfg.MinObservations)
	assert.Equal(t, 0.5, cfg.MaxAdjustment)
	assert.Empty(t, cfg.DBPath)
}

// Benchmarks

func BenchmarkRecordOutcome(b *testing.B) {
	learner, _ := NewContinuousLearner(DefaultLearnerConfig())
	defer learner.Close()

	outcome := ExecutionOutcome{
		Query: "Write a function",
		QueryFeatures: QueryFeatures{
			Category:   "code",
			Complexity: 0.5,
		},
		StrategyUsed: "chain",
		ModelUsed:    "model",
		Success:      true,
		QualityScore: 0.8,
		Cost:         0.001,
		LatencyMS:    1000,
		Timestamp:    time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		learner.RecordOutcome(outcome)
	}
}

func BenchmarkGetRoutingAdjustment(b *testing.B) {
	cfg := DefaultLearnerConfig()
	cfg.MinObservations = 1
	learner, _ := NewContinuousLearner(cfg)
	defer learner.Close()

	// Pre-populate with data
	for i := 0; i < 100; i++ {
		learner.RecordOutcome(ExecutionOutcome{
			QueryFeatures: QueryFeatures{Category: "code"},
			StrategyUsed:  "chain",
			ModelUsed:     "model",
			Success:       true,
			QualityScore:  0.8,
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = learner.GetRoutingAdjustment("code", "model")
	}
}

func BenchmarkBestModelForQueryType(b *testing.B) {
	cfg := DefaultLearnerConfig()
	cfg.MinObservations = 1
	learner, _ := NewContinuousLearner(cfg)
	defer learner.Close()

	// Pre-populate with data for multiple models
	models := []string{"model-a", "model-b", "model-c", "model-d"}
	for _, model := range models {
		for i := 0; i < 10; i++ {
			learner.RecordOutcome(ExecutionOutcome{
				QueryFeatures: QueryFeatures{Category: "code"},
				StrategyUsed:  "chain",
				ModelUsed:     model,
				Success:       true,
				QualityScore:  0.7 + float64(i%3)*0.1,
			})
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = learner.BestModelForQueryType("code", models)
	}
}
