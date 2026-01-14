package learning

import (
	"math"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// Property-based tests for ContinuousLearner.

// TestProperty_AdjustmentsBounded verifies adjustments never exceed MaxAdjustment.
func TestProperty_AdjustmentsBounded(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxAdj := rapid.Float64Range(0.1, 1.0).Draw(t, "maxAdjustment")
		learningRate := rapid.Float64Range(0.01, 0.5).Draw(t, "learningRate")

		cfg := LearnerConfig{
			MaxAdjustment:   maxAdj,
			LearningRate:    learningRate,
			MinObservations: 1,
			DecayRate:       0.95,
		}

		learner, err := NewContinuousLearner(cfg)
		if err != nil {
			t.Fatalf("failed to create learner: %v", err)
		}
		defer learner.Close()

		// Record random outcomes
		numOutcomes := rapid.IntRange(1, 100).Draw(t, "numOutcomes")
		for i := 0; i < numOutcomes; i++ {
			outcome := ExecutionOutcome{
				QueryFeatures: QueryFeatures{
					Category:   "test",
					Complexity: rapid.Float64Range(0.0, 1.0).Draw(t, "complexity"),
				},
				StrategyUsed: "chain",
				ModelUsed:    "model",
				Success:      rapid.Bool().Draw(t, "success"),
				QualityScore: rapid.Float64Range(0.0, 1.0).Draw(t, "quality"),
				Cost:         rapid.Float64Range(0.0, 0.1).Draw(t, "cost"),
				LatencyMS:    int64(rapid.IntRange(100, 10000).Draw(t, "latency")),
			}
			learner.RecordOutcome(outcome)
		}

		// Verify all adjustments are bounded
		stats := learner.Stats()
		for key, adj := range stats.RoutingAdjustments {
			if adj < -maxAdj || adj > maxAdj {
				t.Errorf("routing adjustment %s = %f exceeds bounds [-%f, %f]",
					key, adj, maxAdj, maxAdj)
			}
		}
		for key, pref := range stats.StrategyPreferences {
			if pref < -maxAdj || pref > maxAdj {
				t.Errorf("strategy preference %s = %f exceeds bounds [-%f, %f]",
					key, pref, maxAdj, maxAdj)
			}
		}
	})
}

// TestProperty_SuccessReinforcesRouting verifies successful outcomes increase adjustments.
func TestProperty_SuccessReinforcesRouting(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := LearnerConfig{
			MaxAdjustment:   0.5,
			LearningRate:    0.2,
			MinObservations: 1,
			DecayRate:       1.0, // No decay for this test
		}

		learner, err := NewContinuousLearner(cfg)
		if err != nil {
			t.Fatalf("failed to create learner: %v", err)
		}
		defer learner.Close()

		category := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "category")
		model := rapid.StringMatching(`model-[a-z]`).Draw(t, "model")

		// Record multiple successful outcomes with high quality
		numSuccess := rapid.IntRange(3, 10).Draw(t, "numSuccess")
		for i := 0; i < numSuccess; i++ {
			outcome := ExecutionOutcome{
				QueryFeatures: QueryFeatures{
					Category:   category,
					Complexity: 0.5,
				},
				StrategyUsed: "chain",
				ModelUsed:    model,
				Success:      true,
				QualityScore: rapid.Float64Range(0.7, 1.0).Draw(t, "quality"),
			}
			learner.RecordOutcome(outcome)
		}

		// Adjustment should be positive
		adj := learner.GetRoutingAdjustment(category, model)
		if adj <= 0 {
			t.Errorf("multiple high-quality successes should result in positive adjustment, got %f", adj)
		}
	})
}

// TestProperty_FailureDiscouragesRouting verifies failed outcomes decrease adjustments.
func TestProperty_FailureDiscouragesRouting(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := LearnerConfig{
			MaxAdjustment:   0.5,
			LearningRate:    0.2,
			MinObservations: 1,
			DecayRate:       1.0, // No decay for this test
		}

		learner, err := NewContinuousLearner(cfg)
		if err != nil {
			t.Fatalf("failed to create learner: %v", err)
		}
		defer learner.Close()

		category := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "category")
		model := rapid.StringMatching(`model-[a-z]`).Draw(t, "model")

		// Record multiple failed outcomes with low quality
		numFailures := rapid.IntRange(3, 10).Draw(t, "numFailures")
		for i := 0; i < numFailures; i++ {
			outcome := ExecutionOutcome{
				QueryFeatures: QueryFeatures{
					Category:   category,
					Complexity: 0.5,
				},
				StrategyUsed: "chain",
				ModelUsed:    model,
				Success:      false,
				QualityScore: rapid.Float64Range(0.0, 0.3).Draw(t, "quality"),
			}
			learner.RecordOutcome(outcome)
		}

		// Adjustment should be negative
		adj := learner.GetRoutingAdjustment(category, model)
		if adj >= 0 {
			t.Errorf("multiple failures should result in negative adjustment, got %f", adj)
		}
	})
}

// TestProperty_ObservationCountsAccurate verifies observation counts match recordings.
func TestProperty_ObservationCountsAccurate(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		learner, err := NewContinuousLearner(DefaultLearnerConfig())
		if err != nil {
			t.Fatalf("failed to create learner: %v", err)
		}
		defer learner.Close()

		category := "test"
		model := "model"
		strategy := "chain"

		numOutcomes := rapid.IntRange(1, 50).Draw(t, "numOutcomes")
		for i := 0; i < numOutcomes; i++ {
			outcome := ExecutionOutcome{
				QueryFeatures: QueryFeatures{Category: category},
				StrategyUsed:  strategy,
				ModelUsed:     model,
				Success:       rapid.Bool().Draw(t, "success"),
				QualityScore:  rapid.Float64Range(0.0, 1.0).Draw(t, "quality"),
			}
			learner.RecordOutcome(outcome)
		}

		stats := learner.Stats()
		routingKey := category + ":" + model
		strategyKey := "strat:" + category + ":" + strategy

		if stats.ObservationCounts[routingKey] != numOutcomes {
			t.Errorf("routing observation count %d != expected %d",
				stats.ObservationCounts[routingKey], numOutcomes)
		}
		if stats.ObservationCounts[strategyKey] != numOutcomes {
			t.Errorf("strategy observation count %d != expected %d",
				stats.ObservationCounts[strategyKey], numOutcomes)
		}
	})
}

// TestProperty_RewardBounded verifies computed rewards are in [-1, 1].
func TestProperty_RewardBounded(t *testing.T) {
	learner, _ := NewContinuousLearner(DefaultLearnerConfig())
	defer learner.Close()

	rapid.Check(t, func(t *rapid.T) {
		outcome := ExecutionOutcome{
			QueryFeatures: QueryFeatures{
				Category:          rapid.StringMatching(`[a-z]+`).Draw(t, "category"),
				Complexity:        rapid.Float64Range(0.0, 1.0).Draw(t, "complexity"),
				EstimatedTokens:   rapid.IntRange(0, 10000).Draw(t, "tokens"),
				RequiresReasoning: rapid.Bool().Draw(t, "reasoning"),
				RequiresCode:      rapid.Bool().Draw(t, "code"),
			},
			Success:      rapid.Bool().Draw(t, "success"),
			QualityScore: rapid.Float64Range(0.0, 1.0).Draw(t, "quality"),
			Cost:         rapid.Float64Range(0.0, 1.0).Draw(t, "cost"),
			LatencyMS:    int64(rapid.IntRange(0, 100000).Draw(t, "latency")),
		}

		reward := learner.computeReward(outcome)

		if reward < -1.0 || reward > 1.0 {
			t.Errorf("reward %f out of bounds [-1, 1]", reward)
		}
	})
}

// TestProperty_DecayReducesAdjustment verifies time decay reduces adjustment magnitude.
func TestProperty_DecayReducesAdjustment(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		decayRate := rapid.Float64Range(0.5, 0.99).Draw(t, "decayRate")
		initialAdj := rapid.Float64Range(0.1, 0.5).Draw(t, "initialAdj")
		ageDays := rapid.Float64Range(1.0, 30.0).Draw(t, "ageDays")

		cfg := LearnerConfig{
			DecayRate:       decayRate,
			MinObservations: 1,
			MaxAdjustment:   1.0,
			LearningRate:    0.1,
		}

		learner, err := NewContinuousLearner(cfg)
		if err != nil {
			t.Fatalf("failed to create learner: %v", err)
		}
		defer learner.Close()

		age := time.Duration(ageDays*24) * time.Hour
		decayed := learner.applyDecay(initialAdj, age)

		// Decayed value should be less than original (for positive values)
		if decayed >= initialAdj {
			t.Errorf("decayed value %f should be less than original %f after %f days",
				decayed, initialAdj, ageDays)
		}

		// Decayed value should be positive (for positive input)
		if decayed <= 0 {
			t.Errorf("decayed value %f should remain positive", decayed)
		}

		// Verify decay formula: expected = initial * decay^days
		// Use larger tolerance due to floating point precision in time calculations
		expected := initialAdj * math.Pow(decayRate, ageDays)
		if math.Abs(decayed-expected) > 0.01 {
			t.Errorf("decayed value %f != expected %f (decay^days)", decayed, expected)
		}
	})
}

// TestProperty_BestModelDeterministic verifies BestModelForQueryType is deterministic.
func TestProperty_BestModelDeterministic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := LearnerConfig{
			MinObservations: 1,
			MaxAdjustment:   0.5,
			LearningRate:    0.2,
			DecayRate:       1.0, // No decay
		}

		learner, err := NewContinuousLearner(cfg)
		if err != nil {
			t.Fatalf("failed to create learner: %v", err)
		}
		defer learner.Close()

		category := "code"
		models := []string{"model-a", "model-b", "model-c"}

		// Train with varying quality per model
		for _, model := range models {
			quality := rapid.Float64Range(0.3, 0.9).Draw(t, "quality_"+model)
			for i := 0; i < 5; i++ {
				learner.RecordOutcome(ExecutionOutcome{
					QueryFeatures: QueryFeatures{Category: category},
					StrategyUsed:  "chain",
					ModelUsed:     model,
					Success:       quality > 0.5,
					QualityScore:  quality,
				})
			}
		}

		// Multiple calls should return same result
		best1 := learner.BestModelForQueryType(category, models)
		best2 := learner.BestModelForQueryType(category, models)
		best3 := learner.BestModelForQueryType(category, models)

		if best1 != best2 || best2 != best3 {
			t.Errorf("BestModelForQueryType not deterministic: %s, %s, %s", best1, best2, best3)
		}
	})
}

// Note: Persistence is tested in TestContinuousLearner_Persistence (unit test).
// Property-based testing of async persistence is inherently flaky.

// TestProperty_ResetClearsAll verifies Reset clears all state.
func TestProperty_ResetClearsAll(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		learner, err := NewContinuousLearner(DefaultLearnerConfig())
		if err != nil {
			t.Fatalf("failed to create learner: %v", err)
		}
		defer learner.Close()

		// Record random outcomes
		numOutcomes := rapid.IntRange(5, 50).Draw(t, "numOutcomes")
		for i := 0; i < numOutcomes; i++ {
			learner.RecordOutcome(ExecutionOutcome{
				QueryFeatures: QueryFeatures{
					Category: rapid.StringMatching(`[a-z]+`).Draw(t, "category"),
				},
				StrategyUsed: rapid.StringMatching(`[a-z]+`).Draw(t, "strategy"),
				ModelUsed:    rapid.StringMatching(`model-[a-z]`).Draw(t, "model"),
				Success:      rapid.Bool().Draw(t, "success"),
				QualityScore: rapid.Float64Range(0.0, 1.0).Draw(t, "quality"),
			})
		}

		// Verify state is populated
		statsBefore := learner.Stats()
		if len(statsBefore.RoutingAdjustments) == 0 {
			t.Skip("no adjustments recorded")
		}

		// Reset
		if err := learner.Reset(); err != nil {
			t.Fatalf("reset failed: %v", err)
		}

		// Verify state is cleared
		statsAfter := learner.Stats()
		if len(statsAfter.RoutingAdjustments) != 0 {
			t.Errorf("routing adjustments not cleared: %d remaining",
				len(statsAfter.RoutingAdjustments))
		}
		if len(statsAfter.StrategyPreferences) != 0 {
			t.Errorf("strategy preferences not cleared: %d remaining",
				len(statsAfter.StrategyPreferences))
		}
		if len(statsAfter.ObservationCounts) != 0 {
			t.Errorf("observation counts not cleared: %d remaining",
				len(statsAfter.ObservationCounts))
		}
	})
}

// TestProperty_ConcurrentSafety verifies concurrent access doesn't cause races.
func TestProperty_ConcurrentSafety(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		learner, err := NewContinuousLearner(DefaultLearnerConfig())
		if err != nil {
			t.Fatalf("failed to create learner: %v", err)
		}
		defer learner.Close()

		numWriters := rapid.IntRange(2, 5).Draw(t, "numWriters")
		numReaders := rapid.IntRange(2, 5).Draw(t, "numReaders")
		opsPerGoroutine := rapid.IntRange(10, 50).Draw(t, "opsPerGoroutine")

		done := make(chan struct{})

		// Start writers
		for w := 0; w < numWriters; w++ {
			go func() {
				for i := 0; i < opsPerGoroutine; i++ {
					learner.RecordOutcome(ExecutionOutcome{
						QueryFeatures: QueryFeatures{Category: "test"},
						StrategyUsed:  "chain",
						ModelUsed:     "model",
						Success:       i%2 == 0,
						QualityScore:  0.5,
					})
				}
				done <- struct{}{}
			}()
		}

		// Start readers
		for r := 0; r < numReaders; r++ {
			go func() {
				for i := 0; i < opsPerGoroutine; i++ {
					_ = learner.GetRoutingAdjustment("test", "model")
					_ = learner.GetStrategyPreference("test", "chain")
					_ = learner.Stats()
					_ = learner.BestModelForQueryType("test", []string{"model"})
				}
				done <- struct{}{}
			}()
		}

		// Wait for all goroutines
		for i := 0; i < numWriters+numReaders; i++ {
			<-done
		}

		// If we get here without panic/deadlock, test passes
	})
}
