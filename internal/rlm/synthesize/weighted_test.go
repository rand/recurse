package synthesize

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfidenceLevel_String(t *testing.T) {
	tests := []struct {
		level    ConfidenceLevel
		expected string
	}{
		{ConfidenceVeryLow, "very_low"},
		{ConfidenceLow, "low"},
		{ConfidenceMedium, "medium"},
		{ConfidenceHigh, "high"},
		{ConfidenceLevel(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.level.String())
		})
	}
}

func TestConfidence_Weighted(t *testing.T) {
	tests := []struct {
		name       string
		confidence Confidence
		expected   float64
	}{
		{
			name:       "no components uses score",
			confidence: Confidence{Score: 0.7},
			expected:   0.7,
		},
		{
			name: "weighted average of components",
			confidence: Confidence{
				Score: 0.5,
				Components: []ConfidenceComponent{
					{Factor: "a", Score: 0.8, Weight: 0.5},
					{Factor: "b", Score: 0.6, Weight: 0.3},
					{Factor: "c", Score: 0.4, Weight: 0.2},
				},
			},
			expected: (0.8*0.5 + 0.6*0.3 + 0.4*0.2) / (0.5 + 0.3 + 0.2),
		},
		{
			name: "zero weights uses score",
			confidence: Confidence{
				Score: 0.6,
				Components: []ConfidenceComponent{
					{Factor: "a", Score: 0.8, Weight: 0},
					{Factor: "b", Score: 0.4, Weight: 0},
				},
			},
			expected: 0.6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.InDelta(t, tt.expected, tt.confidence.Weighted(), 0.001)
		})
	}
}

func TestConfidence_Level(t *testing.T) {
	tests := []struct {
		score    float64
		expected ConfidenceLevel
	}{
		{0.95, ConfidenceHigh},
		{0.90, ConfidenceHigh},
		{0.85, ConfidenceMedium},
		{0.70, ConfidenceMedium},
		{0.65, ConfidenceLow},
		{0.50, ConfidenceLow},
		{0.45, ConfidenceVeryLow},
		{0.20, ConfidenceVeryLow},
	}

	for _, tt := range tests {
		t.Run(tt.expected.String(), func(t *testing.T) {
			conf := Confidence{Score: tt.score}
			assert.Equal(t, tt.expected, conf.Level())
		})
	}
}

func TestHeuristicScorer_Score_ErrorResult(t *testing.T) {
	scorer := NewHeuristicScorer()
	ctx := context.Background()

	result := &SubCallResult{
		ID:       "1",
		Response: "some content",
		Error:    "failed to process",
	}

	conf, err := scorer.Score(ctx, result)
	require.NoError(t, err)
	assert.Equal(t, 0.0, conf.Score)
}

func TestHeuristicScorer_Score_ShortResponse(t *testing.T) {
	scorer := NewHeuristicScorer()
	ctx := context.Background()

	result := &SubCallResult{
		ID:       "1",
		Response: "Yes",
	}

	conf, err := scorer.Score(ctx, result)
	require.NoError(t, err)
	assert.Less(t, conf.Weighted(), 0.6) // Short responses get lower confidence
}

func TestHeuristicScorer_Score_WellStructuredResponse(t *testing.T) {
	scorer := NewHeuristicScorer()
	ctx := context.Background()

	result := &SubCallResult{
		ID: "1",
		Response: `## Overview

This is a well-structured response with multiple sections.

- First point
- Second point
- Third point

` + "```go" + `
func example() {
    return nil
}
` + "```" + `

The code above demonstrates the pattern.`,
	}

	conf, err := scorer.Score(ctx, result)
	require.NoError(t, err)
	assert.Greater(t, conf.Weighted(), 0.6) // Well-structured gets higher confidence
}

func TestHeuristicScorer_Score_HedgingResponse(t *testing.T) {
	scorer := NewHeuristicScorer()
	ctx := context.Background()

	// Moderate hedging is appropriate
	result := &SubCallResult{
		ID:       "1",
		Response: "I think this might work, but it seems like there could be edge cases. Perhaps we should consider alternatives.",
	}

	conf, err := scorer.Score(ctx, result)
	require.NoError(t, err)
	// Heavy hedging reduces confidence
	hasHedgingComponent := false
	for _, c := range conf.Components {
		if c.Factor == "hedging" {
			hasHedgingComponent = true
			assert.Less(t, c.Score, 0.8) // Heavy hedging gets lower score
		}
	}
	assert.True(t, hasHedgingComponent)
}

func TestHeuristicScorer_Score_SpecificResponse(t *testing.T) {
	scorer := NewHeuristicScorer()
	ctx := context.Background()

	result := &SubCallResult{
		ID:       "1",
		Response: "The function in main.go at line 42 returns nil when the input is empty. The handleRequest() function calls validate() first.",
	}

	conf, err := scorer.Score(ctx, result)
	require.NoError(t, err)

	// Find specificity component
	var specificityScore float64
	for _, c := range conf.Components {
		if c.Factor == "specificity" {
			specificityScore = c.Score
		}
	}
	assert.Greater(t, specificityScore, 0.6) // Specific content gets higher score
}

func TestNormalizeWeights_SumsToOne(t *testing.T) {
	results := []*ScoredResult{
		{Confidence: Confidence{Score: 0.9}},
		{Confidence: Confidence{Score: 0.7}},
		{Confidence: Confidence{Score: 0.5}},
	}

	weights := NormalizeWeights(results)
	require.Len(t, weights, 3)

	sum := weights[0] + weights[1] + weights[2]
	assert.InDelta(t, 1.0, sum, 0.001)
}

func TestNormalizeWeights_HigherConfidenceGetsMoreWeight(t *testing.T) {
	results := []*ScoredResult{
		{Confidence: Confidence{Score: 0.9}},
		{Confidence: Confidence{Score: 0.3}},
	}

	weights := NormalizeWeights(results)
	require.Len(t, weights, 2)

	assert.Greater(t, weights[0], weights[1])
}

func TestNormalizeWeights_Empty(t *testing.T) {
	weights := NormalizeWeights(nil)
	assert.Nil(t, weights)
}

func TestNormalizeWeights_AllZero(t *testing.T) {
	results := []*ScoredResult{
		{Confidence: Confidence{Score: 0}},
		{Confidence: Confidence{Score: 0}},
	}

	weights := NormalizeWeights(results)
	require.Len(t, weights, 2)

	// Equal weights when all zero
	assert.InDelta(t, 0.5, weights[0], 0.001)
	assert.InDelta(t, 0.5, weights[1], 0.001)
}

func TestScoreResults(t *testing.T) {
	ctx := context.Background()

	results := []SubCallResult{
		{ID: "1", Response: "Short"},
		{ID: "2", Response: "This is a much longer and more detailed response that should score higher on the length metric."},
		{ID: "3", Response: "", Error: "failed"},
	}

	scored, err := ScoreResults(ctx, results)
	require.NoError(t, err)
	require.Len(t, scored, 3)

	// Results should be sorted by confidence (descending)
	for i := 0; i < len(scored)-1; i++ {
		assert.GreaterOrEqual(t, scored[i].Confidence.Weighted(), scored[i+1].Confidence.Weighted())
	}

	// Error result should have lowest confidence
	assert.Equal(t, 0.0, scored[len(scored)-1].Confidence.Weighted())
}

func TestWeightedSynthesizer_FilterByConfidence(t *testing.T) {
	synth := &WeightedSynthesizer{
		config: WeightedSynthesisConfig{MinConfidence: 0.5},
	}

	results := []*ScoredResult{
		{Confidence: Confidence{Score: 0.8}},
		{Confidence: Confidence{Score: 0.3}}, // Below threshold
		{Confidence: Confidence{Score: 0.6}},
		{Confidence: Confidence{Score: 0.4}}, // Below threshold
	}

	filtered, filteredCount := synth.filterByConfidence(results)

	assert.Len(t, filtered, 2)
	assert.Equal(t, 2, filteredCount)
}

func TestWeightedSynthesizer_GenerateWarnings(t *testing.T) {
	synth := &WeightedSynthesizer{}

	tests := []struct {
		name           string
		results        []*ScoredResult
		filteredCount  int
		overallConf    float64
		expectWarnings int
	}{
		{
			name: "low confidence warning",
			results: []*ScoredResult{
				{Confidence: Confidence{Score: 0.4}},
			},
			filteredCount:  0,
			overallConf:    0.4,
			expectWarnings: 2, // low confidence + single source
		},
		{
			name: "filtered sources warning",
			results: []*ScoredResult{
				{Confidence: Confidence{Score: 0.8}},
				{Confidence: Confidence{Score: 0.7}},
			},
			filteredCount:  3,
			overallConf:    0.75,
			expectWarnings: 1, // filtered sources
		},
		{
			name: "high variance warning",
			results: []*ScoredResult{
				{Confidence: Confidence{Score: 0.95}},
				{Confidence: Confidence{Score: 0.45}},
			},
			filteredCount:  0,
			overallConf:    0.7,
			expectWarnings: 1, // high variance
		},
		{
			name: "no warnings",
			results: []*ScoredResult{
				{Confidence: Confidence{Score: 0.8}},
				{Confidence: Confidence{Score: 0.75}},
			},
			filteredCount:  0,
			overallConf:    0.77,
			expectWarnings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := synth.generateWarnings(tt.results, tt.filteredCount, tt.overallConf)
			assert.Len(t, warnings, tt.expectWarnings)
		})
	}
}

func TestWeightedSynthesizer_ComputeOverallConfidence(t *testing.T) {
	synth := &WeightedSynthesizer{}

	results := []*ScoredResult{
		{Confidence: Confidence{Score: 0.9}},
		{Confidence: Confidence{Score: 0.6}},
	}
	weights := []float64{0.7, 0.3}

	overall := synth.computeOverallConfidence(results, weights)

	expected := 0.9*0.7 + 0.6*0.3
	assert.InDelta(t, expected, overall, 0.001)
}

func TestWeightedSynthesizer_ComputeOverallConfidence_Empty(t *testing.T) {
	synth := &WeightedSynthesizer{}

	overall := synth.computeOverallConfidence(nil, nil)
	assert.Equal(t, 0.0, overall)
}

func TestDefaultWeightedConfig(t *testing.T) {
	cfg := DefaultWeightedConfig()

	assert.Equal(t, 0.3, cfg.MinConfidence)
	assert.False(t, cfg.IncludeProvenance)
	assert.False(t, cfg.ShowConfidenceScores)
}

func TestScoredResult_Weight(t *testing.T) {
	result := &ScoredResult{
		Confidence: Confidence{Score: 0.75},
	}

	assert.Equal(t, 0.75, result.Weight())
}

// Benchmarks

func BenchmarkHeuristicScorer(b *testing.B) {
	scorer := NewHeuristicScorer()
	ctx := context.Background()
	result := &SubCallResult{
		ID: "1",
		Response: `## Analysis

This is a detailed response with multiple sections and good structure.

- Point one with specific details
- Point two referencing main.go
- Point three with numbers: 42, 100, 256

The function handleRequest() processes the input correctly.`,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = scorer.Score(ctx, result)
	}
}

func BenchmarkNormalizeWeights(b *testing.B) {
	results := []*ScoredResult{
		{Confidence: Confidence{Score: 0.9}},
		{Confidence: Confidence{Score: 0.7}},
		{Confidence: Confidence{Score: 0.5}},
		{Confidence: Confidence{Score: 0.3}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NormalizeWeights(results)
	}
}

func BenchmarkScoreResults(b *testing.B) {
	ctx := context.Background()
	results := []SubCallResult{
		{ID: "1", Response: "Short response"},
		{ID: "2", Response: "Medium length response with some details about the implementation."},
		{ID: "3", Response: "Longer response with code blocks and structure.\n\n- Item 1\n- Item 2\n\n```go\nfunc example() {}\n```"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ScoreResults(ctx, results)
	}
}
