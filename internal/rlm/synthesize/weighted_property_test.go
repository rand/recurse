package synthesize

import (
	"context"
	"math"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// Property-based tests for confidence-weighted synthesis.

// TestProperty_WeightsSumToOne verifies normalized weights always sum to 1.0.
func TestProperty_WeightsSumToOne(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numResults := rapid.IntRange(1, 20).Draw(t, "numResults")

		results := make([]*ScoredResult, numResults)
		for i := 0; i < numResults; i++ {
			score := rapid.Float64Range(0.0, 1.0).Draw(t, "score")
			results[i] = &ScoredResult{
				Confidence: Confidence{Score: score},
			}
		}

		weights := NormalizeWeights(results)

		if len(weights) != numResults {
			t.Fatalf("expected %d weights, got %d", numResults, len(weights))
		}

		sum := 0.0
		for _, w := range weights {
			sum += w
		}

		if math.Abs(sum-1.0) > 0.0001 {
			t.Errorf("weights sum to %f, expected 1.0", sum)
		}
	})
}

// TestProperty_WeightsNonNegative verifies all weights are non-negative.
func TestProperty_WeightsNonNegative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numResults := rapid.IntRange(1, 20).Draw(t, "numResults")

		results := make([]*ScoredResult, numResults)
		for i := 0; i < numResults; i++ {
			score := rapid.Float64Range(0.0, 1.0).Draw(t, "score")
			results[i] = &ScoredResult{
				Confidence: Confidence{Score: score},
			}
		}

		weights := NormalizeWeights(results)

		for i, w := range weights {
			if w < 0 {
				t.Errorf("weight %d is negative: %f", i, w)
			}
		}
	})
}

// TestProperty_HigherConfidenceHigherWeight verifies higher confidence results get higher weight.
func TestProperty_HigherConfidenceHigherWeight(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lowScore := rapid.Float64Range(0.1, 0.4).Draw(t, "lowScore")
		highScore := rapid.Float64Range(0.6, 0.9).Draw(t, "highScore")

		results := []*ScoredResult{
			{Confidence: Confidence{Score: highScore}},
			{Confidence: Confidence{Score: lowScore}},
		}

		weights := NormalizeWeights(results)

		if weights[0] <= weights[1] {
			t.Errorf("high confidence (%.2f) weight %.4f should be > low confidence (%.2f) weight %.4f",
				highScore, weights[0], lowScore, weights[1])
		}
	})
}

// TestProperty_ConfidenceScoreInRange verifies confidence scores stay in [0, 1].
func TestProperty_ConfidenceScoreInRange(t *testing.T) {
	scorer := NewHeuristicScorer()
	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		// Generate random response content
		words := rapid.IntRange(1, 200).Draw(t, "words")
		var sb strings.Builder
		for i := 0; i < words; i++ {
			word := rapid.StringMatching(`[a-zA-Z]{1,15}`).Draw(t, "word")
			sb.WriteString(word)
			sb.WriteString(" ")
		}

		result := &SubCallResult{
			ID:       "test",
			Response: sb.String(),
		}

		conf, err := scorer.Score(ctx, result)
		if err != nil {
			t.Fatalf("scorer error: %v", err)
		}

		if conf.Score < 0.0 || conf.Score > 1.0 {
			t.Errorf("confidence score %f out of range [0, 1]", conf.Score)
		}

		if conf.Weighted() < 0.0 || conf.Weighted() > 1.0 {
			t.Errorf("weighted confidence %f out of range [0, 1]", conf.Weighted())
		}
	})
}

// TestProperty_ComponentScoresInRange verifies all component scores are in [0, 1].
func TestProperty_ComponentScoresInRange(t *testing.T) {
	scorer := NewHeuristicScorer()
	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		// Generate various response patterns
		hasLists := rapid.Bool().Draw(t, "hasLists")
		hasCode := rapid.Bool().Draw(t, "hasCode")
		hasHedging := rapid.Bool().Draw(t, "hasHedging")

		var sb strings.Builder
		sb.WriteString("This is a response. ")

		if hasLists {
			sb.WriteString("\n- Item one\n- Item two\n")
		}
		if hasCode {
			sb.WriteString("\n```go\nfunc example() {}\n```\n")
		}
		if hasHedging {
			sb.WriteString("I think this might work, perhaps. ")
		}

		result := &SubCallResult{
			ID:       "test",
			Response: sb.String(),
		}

		conf, err := scorer.Score(ctx, result)
		if err != nil {
			t.Fatalf("scorer error: %v", err)
		}

		for _, comp := range conf.Components {
			if comp.Score < 0.0 || comp.Score > 1.0 {
				t.Errorf("component %s score %f out of range [0, 1]",
					comp.Factor, comp.Score)
			}
			if comp.Weight < 0.0 || comp.Weight > 1.0 {
				t.Errorf("component %s weight %f out of range [0, 1]",
					comp.Factor, comp.Weight)
			}
		}
	})
}

// TestProperty_FilteredResultsHaveLowConfidence verifies filtered results are low confidence.
func TestProperty_FilteredResultsHaveLowConfidence(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		minConfidence := rapid.Float64Range(0.3, 0.7).Draw(t, "minConfidence")
		numResults := rapid.IntRange(2, 10).Draw(t, "numResults")

		synth := &WeightedSynthesizer{
			config: WeightedSynthesisConfig{MinConfidence: minConfidence},
		}

		results := make([]*ScoredResult, numResults)
		for i := 0; i < numResults; i++ {
			score := rapid.Float64Range(0.0, 1.0).Draw(t, "score")
			results[i] = &ScoredResult{
				SubCallResult: SubCallResult{ID: string(rune('a' + i))},
				Confidence:    Confidence{Score: score},
			}
		}

		filtered, filteredCount := synth.filterByConfidence(results)

		// Verify all filtered results have confidence >= threshold
		for _, r := range filtered {
			if r.Confidence.Weighted() < minConfidence {
				t.Errorf("filtered result has confidence %f < threshold %f",
					r.Confidence.Weighted(), minConfidence)
			}
		}

		// Verify count is correct
		expectedFiltered := 0
		for _, r := range results {
			if r.Confidence.Weighted() < minConfidence {
				expectedFiltered++
			}
		}

		if filteredCount != expectedFiltered {
			t.Errorf("filtered count %d != expected %d", filteredCount, expectedFiltered)
		}
	})
}

// TestProperty_OverallConfidenceInRange verifies overall confidence is in [0, 1].
func TestProperty_OverallConfidenceInRange(t *testing.T) {
	synth := &WeightedSynthesizer{}

	rapid.Check(t, func(t *rapid.T) {
		numResults := rapid.IntRange(1, 10).Draw(t, "numResults")

		results := make([]*ScoredResult, numResults)
		for i := 0; i < numResults; i++ {
			score := rapid.Float64Range(0.0, 1.0).Draw(t, "score")
			results[i] = &ScoredResult{
				Confidence: Confidence{Score: score},
			}
		}

		weights := NormalizeWeights(results)
		overall := synth.computeOverallConfidence(results, weights)

		if overall < 0.0 || overall > 1.0 {
			t.Errorf("overall confidence %f out of range [0, 1]", overall)
		}
	})
}

// TestProperty_ConfidenceLevelConsistency verifies level matches score range.
func TestProperty_ConfidenceLevelConsistency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		score := rapid.Float64Range(0.0, 1.0).Draw(t, "score")
		conf := Confidence{Score: score}
		level := conf.Level()

		switch {
		case score >= 0.9:
			if level != ConfidenceHigh {
				t.Errorf("score %.2f should be ConfidenceHigh, got %s", score, level)
			}
		case score >= 0.7:
			if level != ConfidenceMedium {
				t.Errorf("score %.2f should be ConfidenceMedium, got %s", score, level)
			}
		case score >= 0.5:
			if level != ConfidenceLow {
				t.Errorf("score %.2f should be ConfidenceLow, got %s", score, level)
			}
		default:
			if level != ConfidenceVeryLow {
				t.Errorf("score %.2f should be ConfidenceVeryLow, got %s", score, level)
			}
		}
	})
}

// TestProperty_ScoreResultsPreservesAll verifies ScoreResults doesn't lose results.
func TestProperty_ScoreResultsPreservesAll(t *testing.T) {
	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		numResults := rapid.IntRange(1, 20).Draw(t, "numResults")

		results := make([]SubCallResult, numResults)
		for i := 0; i < numResults; i++ {
			results[i] = SubCallResult{
				ID:       string(rune('a' + i)),
				Response: rapid.StringMatching(`[a-zA-Z ]{10,100}`).Draw(t, "response"),
			}
		}

		scored, err := ScoreResults(ctx, results)
		if err != nil {
			t.Fatalf("ScoreResults error: %v", err)
		}

		if len(scored) != numResults {
			t.Errorf("ScoreResults returned %d results, expected %d", len(scored), numResults)
		}

		// Verify all original IDs are present
		ids := make(map[string]bool)
		for _, r := range scored {
			ids[r.ID] = true
		}

		for _, r := range results {
			if !ids[r.ID] {
				t.Errorf("original result ID %s not found in scored results", r.ID)
			}
		}
	})
}

// TestProperty_ScoreResultsSortedByConfidence verifies results are sorted descending.
func TestProperty_ScoreResultsSortedByConfidence(t *testing.T) {
	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		numResults := rapid.IntRange(2, 10).Draw(t, "numResults")

		results := make([]SubCallResult, numResults)
		for i := 0; i < numResults; i++ {
			results[i] = SubCallResult{
				ID:       string(rune('a' + i)),
				Response: rapid.StringMatching(`[a-zA-Z ]{10,200}`).Draw(t, "response"),
			}
		}

		scored, err := ScoreResults(ctx, results)
		if err != nil {
			t.Fatalf("ScoreResults error: %v", err)
		}

		// Verify sorted descending by confidence
		for i := 0; i < len(scored)-1; i++ {
			if scored[i].Confidence.Weighted() < scored[i+1].Confidence.Weighted() {
				t.Errorf("results not sorted: index %d confidence %.4f < index %d confidence %.4f",
					i, scored[i].Confidence.Weighted(), i+1, scored[i+1].Confidence.Weighted())
			}
		}
	})
}

// TestProperty_WeightedAverageCorrect verifies weighted average computation.
func TestProperty_WeightedAverageCorrect(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numComponents := rapid.IntRange(1, 5).Draw(t, "numComponents")

		components := make([]ConfidenceComponent, numComponents)
		var expectedSum, expectedWeightSum float64

		for i := 0; i < numComponents; i++ {
			score := rapid.Float64Range(0.0, 1.0).Draw(t, "score")
			weight := rapid.Float64Range(0.1, 1.0).Draw(t, "weight")
			components[i] = ConfidenceComponent{
				Factor: string(rune('a' + i)),
				Score:  score,
				Weight: weight,
			}
			expectedSum += score * weight
			expectedWeightSum += weight
		}

		conf := Confidence{
			Score:      0.5, // Should be ignored when components present
			Components: components,
		}

		expected := expectedSum / expectedWeightSum
		actual := conf.Weighted()

		if math.Abs(expected-actual) > 0.0001 {
			t.Errorf("weighted average %.6f != expected %.6f", actual, expected)
		}
	})
}

// TestProperty_EmptyComponentsUsesScore verifies empty components falls back to score.
func TestProperty_EmptyComponentsUsesScore(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		score := rapid.Float64Range(0.0, 1.0).Draw(t, "score")

		conf := Confidence{
			Score:      score,
			Components: nil,
		}

		if conf.Weighted() != score {
			t.Errorf("empty components should use score: got %f, expected %f",
				conf.Weighted(), score)
		}

		conf.Components = []ConfidenceComponent{}
		if conf.Weighted() != score {
			t.Errorf("empty slice should use score: got %f, expected %f",
				conf.Weighted(), score)
		}
	})
}
