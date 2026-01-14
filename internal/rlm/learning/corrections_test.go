package learning

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCorrectionLearner(t *testing.T) {
	learner, err := NewContinuousLearner(DefaultLearnerConfig())
	require.NoError(t, err)
	defer learner.Close()

	cl := NewCorrectionLearner(learner, DefaultCorrectionLearnerConfig())

	assert.NotNil(t, cl)
	assert.NotNil(t, cl.corrections)
	assert.NotNil(t, cl.patterns)
	assert.Equal(t, 1000, cl.config.MaxCorrections)
	assert.Equal(t, 3, cl.config.PatternThreshold)
}

func TestNewCorrectionLearner_CustomConfig(t *testing.T) {
	cfg := CorrectionLearnerConfig{
		MaxCorrections:   500,
		PatternThreshold: 5,
	}

	cl := NewCorrectionLearner(nil, cfg)

	assert.Equal(t, 500, cl.config.MaxCorrections)
	assert.Equal(t, 5, cl.config.PatternThreshold)
}

func TestNewCorrectionLearner_InvalidConfig(t *testing.T) {
	cfg := CorrectionLearnerConfig{
		MaxCorrections:   0,
		PatternThreshold: 0,
	}

	cl := NewCorrectionLearner(nil, cfg)

	// Should default to sensible values
	assert.Equal(t, 1000, cl.config.MaxCorrections)
	assert.Equal(t, 3, cl.config.PatternThreshold)
}

func TestCorrectionLearner_RecordCorrection(t *testing.T) {
	learner, err := NewContinuousLearner(LearnerConfig{
		MinObservations: 1,
		LearningRate:    0.2,
		MaxAdjustment:   0.5,
		DecayRate:       1.0,
	})
	require.NoError(t, err)
	defer learner.Close()

	cl := NewCorrectionLearner(learner, DefaultCorrectionLearnerConfig())
	ctx := context.Background()

	correction := UserCorrection{
		Query:        "Write a function to sort an array",
		RLMOutput:    "def sort(arr): return arr", // Incomplete
		Correction:   "def sort(arr): return sorted(arr)",
		Type:         CorrectionOutput,
		Severity:     0.7,
		ModelUsed:    "claude-haiku",
		StrategyUsed: "direct",
		QueryType:    "code",
	}

	cl.RecordCorrection(ctx, correction)

	stats := cl.Stats()
	assert.Equal(t, 1, stats.TotalCorrections)
	assert.Equal(t, 1, stats.CorrectionsByType[CorrectionOutput])
	assert.Equal(t, 1, stats.CorrectionsByModel["claude-haiku"])
}

func TestCorrectionLearner_RecordCorrection_SetsTimestamp(t *testing.T) {
	cl := NewCorrectionLearner(nil, DefaultCorrectionLearnerConfig())
	ctx := context.Background()

	before := time.Now()
	correction := UserCorrection{
		Query:      "Test query",
		Correction: "Fixed",
		Type:       CorrectionOutput,
	}

	cl.RecordCorrection(ctx, correction)
	after := time.Now()

	// Check that timestamp was set
	cl.mu.RLock()
	recorded := cl.corrections[0]
	cl.mu.RUnlock()

	assert.False(t, recorded.Timestamp.IsZero())
	assert.True(t, recorded.Timestamp.After(before) || recorded.Timestamp.Equal(before))
	assert.True(t, recorded.Timestamp.Before(after) || recorded.Timestamp.Equal(after))
}

func TestCorrectionLearner_RecordCorrection_TrimsToMax(t *testing.T) {
	cfg := CorrectionLearnerConfig{
		MaxCorrections:   5,
		PatternThreshold: 3,
	}
	cl := NewCorrectionLearner(nil, cfg)
	ctx := context.Background()

	// Record more than max
	for i := 0; i < 10; i++ {
		cl.RecordCorrection(ctx, UserCorrection{
			Query:      "Query",
			Correction: "Fixed",
			Type:       CorrectionOutput,
		})
	}

	stats := cl.Stats()
	assert.Equal(t, 5, stats.TotalCorrections)
}

func TestCorrectionLearner_PatternDetection(t *testing.T) {
	cl := NewCorrectionLearner(nil, CorrectionLearnerConfig{
		MaxCorrections:   100,
		PatternThreshold: 2,
	})
	ctx := context.Background()

	// Record similar corrections to create a pattern
	for i := 0; i < 3; i++ {
		cl.RecordCorrection(ctx, UserCorrection{
			Query:        "Sort the array using quicksort",
			Correction:   "Fixed sort implementation",
			Type:         CorrectionOutput,
			Severity:     0.6,
			ModelUsed:    "claude-haiku",
			StrategyUsed: "direct",
		})
	}

	patterns := cl.GetPatterns(CorrectionOutput)
	require.NotEmpty(t, patterns)

	pattern := patterns[0]
	assert.Equal(t, CorrectionOutput, pattern.Type)
	assert.GreaterOrEqual(t, pattern.Frequency, 1)
	assert.Contains(t, pattern.Keywords, "sort")
}

func TestCorrectionLearner_GetAllPatterns(t *testing.T) {
	cl := NewCorrectionLearner(nil, DefaultCorrectionLearnerConfig())
	ctx := context.Background()

	// Record corrections of different types
	types := []CorrectionType{
		CorrectionClassifier,
		CorrectionExecution,
		CorrectionRouting,
		CorrectionOutput,
		CorrectionStyle,
	}

	for _, corrType := range types {
		cl.RecordCorrection(ctx, UserCorrection{
			Query:      "Test query for " + string(corrType),
			Correction: "Fixed",
			Type:       corrType,
		})
	}

	allPatterns := cl.GetAllPatterns()
	assert.Len(t, allPatterns, 5)
}

func TestCorrectionLearner_GetSignificantPatterns(t *testing.T) {
	cl := NewCorrectionLearner(nil, CorrectionLearnerConfig{
		MaxCorrections:   100,
		PatternThreshold: 3,
	})
	ctx := context.Background()

	// Record below threshold
	cl.RecordCorrection(ctx, UserCorrection{
		Query:      "Infrequent query",
		Correction: "Fixed",
		Type:       CorrectionStyle,
	})

	// Record above threshold
	for i := 0; i < 5; i++ {
		cl.RecordCorrection(ctx, UserCorrection{
			Query:      "Frequent sorting query",
			Correction: "Fixed",
			Type:       CorrectionOutput,
		})
	}

	significant := cl.GetSignificantPatterns()
	assert.Len(t, significant, 1)
	assert.Equal(t, CorrectionOutput, significant[0].Type)
}

func TestCorrectionLearner_SuggestAdjustments(t *testing.T) {
	cl := NewCorrectionLearner(nil, CorrectionLearnerConfig{
		MaxCorrections:   100,
		PatternThreshold: 2,
	})
	ctx := context.Background()

	// Record enough corrections to create a pattern
	for i := 0; i < 5; i++ {
		cl.RecordCorrection(ctx, UserCorrection{
			Query:        "Write sorting function",
			Correction:   "Fixed",
			Type:         CorrectionOutput,
			Severity:     0.8,
			ModelUsed:    "claude-haiku",
			StrategyUsed: "direct",
		})
	}

	adjustments := cl.SuggestAdjustments()
	require.NotEmpty(t, adjustments)

	// Should suggest routing and/or strategy adjustments
	var hasRouting, hasStrategy bool
	for _, adj := range adjustments {
		if adj.Type == AdjustmentRouting {
			hasRouting = true
			assert.Equal(t, "claude-haiku", adj.Target)
			assert.Less(t, adj.Change, 0.0) // Negative adjustment
		}
		if adj.Type == AdjustmentStrategy {
			hasStrategy = true
			assert.Equal(t, "direct", adj.Target)
		}
	}
	assert.True(t, hasRouting || hasStrategy)
}

func TestCorrectionLearner_SuggestAdjustments_BelowThreshold(t *testing.T) {
	cl := NewCorrectionLearner(nil, CorrectionLearnerConfig{
		MaxCorrections:   100,
		PatternThreshold: 10, // High threshold
	})
	ctx := context.Background()

	// Only a few corrections
	cl.RecordCorrection(ctx, UserCorrection{
		Query:        "Test query",
		Correction:   "Fixed",
		Type:         CorrectionOutput,
		ModelUsed:    "test-model",
		StrategyUsed: "test-strategy",
	})

	adjustments := cl.SuggestAdjustments()
	assert.Empty(t, adjustments)
}

func TestCorrectionLearner_Stats(t *testing.T) {
	cl := NewCorrectionLearner(nil, DefaultCorrectionLearnerConfig())
	ctx := context.Background()

	corrections := []UserCorrection{
		{Query: "Q1", Correction: "C1", Type: CorrectionOutput, Severity: 0.5, ModelUsed: "model-a"},
		{Query: "Q2", Correction: "C2", Type: CorrectionOutput, Severity: 0.7, ModelUsed: "model-a"},
		{Query: "Q3", Correction: "C3", Type: CorrectionRouting, Severity: 0.9, ModelUsed: "model-b"},
	}

	for _, c := range corrections {
		cl.RecordCorrection(ctx, c)
	}

	stats := cl.Stats()

	assert.Equal(t, 3, stats.TotalCorrections)
	assert.Equal(t, 2, stats.CorrectionsByType[CorrectionOutput])
	assert.Equal(t, 1, stats.CorrectionsByType[CorrectionRouting])
	assert.Equal(t, 2, stats.CorrectionsByModel["model-a"])
	assert.Equal(t, 1, stats.CorrectionsByModel["model-b"])
	assert.InDelta(t, 0.7, stats.AverageSeverity, 0.01) // (0.5 + 0.7 + 0.9) / 3
}

func TestCorrectionLearner_Stats_Empty(t *testing.T) {
	cl := NewCorrectionLearner(nil, DefaultCorrectionLearnerConfig())

	stats := cl.Stats()

	assert.Equal(t, 0, stats.TotalCorrections)
	assert.Equal(t, 0.0, stats.AverageSeverity)
}

func TestCorrectionStats_ToJSON(t *testing.T) {
	stats := CorrectionStats{
		TotalCorrections:   5,
		CorrectionsByType:  map[CorrectionType]int{CorrectionOutput: 3, CorrectionStyle: 2},
		CorrectionsByModel: map[string]int{"model-a": 5},
		AverageSeverity:    0.6,
		PatternCount:       2,
	}

	json := stats.ToJSON()
	assert.Contains(t, json, "total_corrections")
	assert.Contains(t, json, "5")
	assert.Contains(t, json, "average_severity")
}

func TestCorrectionLearner_Reset(t *testing.T) {
	cl := NewCorrectionLearner(nil, DefaultCorrectionLearnerConfig())
	ctx := context.Background()

	// Add some corrections
	for i := 0; i < 5; i++ {
		cl.RecordCorrection(ctx, UserCorrection{
			Query:      "Test query",
			Correction: "Fixed",
			Type:       CorrectionOutput,
		})
	}

	statsBefore := cl.Stats()
	assert.Equal(t, 5, statsBefore.TotalCorrections)

	cl.Reset()

	statsAfter := cl.Stats()
	assert.Equal(t, 0, statsAfter.TotalCorrections)
	assert.Empty(t, cl.GetAllPatterns())
}

func TestCorrectionLearner_IntegrationWithLearner(t *testing.T) {
	learner, err := NewContinuousLearner(LearnerConfig{
		MinObservations: 1,
		LearningRate:    0.2,
		MaxAdjustment:   0.5,
		DecayRate:       1.0,
	})
	require.NoError(t, err)
	defer learner.Close()

	cl := NewCorrectionLearner(learner, DefaultCorrectionLearnerConfig())
	ctx := context.Background()

	// Record a high-severity correction
	cl.RecordCorrection(ctx, UserCorrection{
		Query:        "Sort array",
		RLMOutput:    "Wrong answer",
		Correction:   "Correct answer",
		Type:         CorrectionOutput,
		Severity:     0.9,
		ModelUsed:    "test-model",
		StrategyUsed: "direct",
		QueryType:    "code",
	})

	// The learner should have recorded a negative outcome
	adj := learner.GetRoutingAdjustment("code", "test-model")
	assert.Less(t, adj, 0.0) // Should be negative due to low quality score
}

// Test helper functions

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{
			"Write a function to sort an array",
			[]string{"write", "function", "sort", "array"},
		},
		{
			"The quick brown fox jumps over the lazy dog",
			[]string{"quick", "brown", "fox", "jumps", "over", "lazy", "dog"},
		},
		{
			"a b c d", // All too short
			[]string{},
		},
		{
			"", // Empty
			[]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := extractKeywords(tt.input)
			for _, expected := range tt.expected {
				assert.Contains(t, result, expected)
			}
		})
	}
}

func TestExtractKeywords_RemovesPunctuation(t *testing.T) {
	result := extractKeywords("Hello, World! How are you?")
	assert.Contains(t, result, "hello")
	assert.Contains(t, result, "world")
	// Should not contain punctuation
	for _, kw := range result {
		assert.NotContains(t, kw, ",")
		assert.NotContains(t, kw, "!")
		assert.NotContains(t, kw, "?")
	}
}

func TestExtractKeywords_LimitsTo10(t *testing.T) {
	longText := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron"
	result := extractKeywords(longText)
	assert.LessOrEqual(t, len(result), 10)
}

func TestKeywordOverlap(t *testing.T) {
	tests := []struct {
		a        []string
		b        []string
		expected float64
	}{
		{
			[]string{"a", "b", "c"},
			[]string{"a", "b", "c"},
			1.0, // Perfect overlap
		},
		{
			[]string{"a", "b"},
			[]string{"c", "d"},
			0.0, // No overlap
		},
		{
			[]string{"a", "b", "c"},
			[]string{"b", "c", "d"},
			0.5, // 2 matches / 4 union
		},
		{
			[]string{},
			[]string{"a"},
			0.0, // Empty set
		},
		{
			[]string{"a"},
			[]string{},
			0.0, // Empty set
		},
	}

	for _, tt := range tests {
		result := keywordOverlap(tt.a, tt.b)
		assert.InDelta(t, tt.expected, result, 0.01)
	}
}

func TestGeneratePatternDescription(t *testing.T) {
	tests := []struct {
		corrType CorrectionType
		expected string
	}{
		{CorrectionClassifier, "query classification errors"},
		{CorrectionExecution, "execution strategy failures"},
		{CorrectionRouting, "model routing issues"},
		{CorrectionOutput, "output content errors"},
		{CorrectionStyle, "style/format issues"},
		{CorrectionType("unknown"), "general corrections"},
	}

	for _, tt := range tests {
		correction := UserCorrection{Type: tt.corrType}
		result := generatePatternDescription(correction)
		assert.Equal(t, tt.expected, result)
	}
}

func TestContainsString(t *testing.T) {
	slice := []string{"apple", "banana", "cherry"}

	assert.True(t, containsString(slice, "apple"))
	assert.True(t, containsString(slice, "banana"))
	assert.True(t, containsString(slice, "cherry"))
	assert.False(t, containsString(slice, "date"))
	assert.False(t, containsString([]string{}, "any"))
}

func TestCorrectionPattern_AverageSeverityCalculation(t *testing.T) {
	cl := NewCorrectionLearner(nil, CorrectionLearnerConfig{
		MaxCorrections:   100,
		PatternThreshold: 1,
	})
	ctx := context.Background()

	severities := []float64{0.5, 0.7, 0.9}
	for _, sev := range severities {
		cl.RecordCorrection(ctx, UserCorrection{
			Query:      "Same query pattern",
			Correction: "Fixed",
			Type:       CorrectionOutput,
			Severity:   sev,
		})
	}

	patterns := cl.GetPatterns(CorrectionOutput)
	require.Len(t, patterns, 1)

	expectedAvg := (0.5 + 0.7 + 0.9) / 3.0
	assert.InDelta(t, expectedAvg, patterns[0].AverageSeverity, 0.01)
}

func TestCorrectionPattern_TracksAffectedModels(t *testing.T) {
	cl := NewCorrectionLearner(nil, DefaultCorrectionLearnerConfig())
	ctx := context.Background()

	// Record corrections from different models
	models := []string{"model-a", "model-a", "model-b"}
	for _, model := range models {
		cl.RecordCorrection(ctx, UserCorrection{
			Query:      "Same query",
			Correction: "Fixed",
			Type:       CorrectionOutput,
			ModelUsed:  model,
		})
	}

	patterns := cl.GetPatterns(CorrectionOutput)
	require.Len(t, patterns, 1)

	assert.Equal(t, 2, patterns[0].AffectedModels["model-a"])
	assert.Equal(t, 1, patterns[0].AffectedModels["model-b"])
}

func TestCorrectionPattern_TracksAffectedStrategies(t *testing.T) {
	cl := NewCorrectionLearner(nil, DefaultCorrectionLearnerConfig())
	ctx := context.Background()

	strategies := []string{"direct", "direct", "chain"}
	for _, strategy := range strategies {
		cl.RecordCorrection(ctx, UserCorrection{
			Query:        "Same query",
			Correction:   "Fixed",
			Type:         CorrectionOutput,
			StrategyUsed: strategy,
		})
	}

	patterns := cl.GetPatterns(CorrectionOutput)
	require.Len(t, patterns, 1)

	assert.Equal(t, 2, patterns[0].AffectedStrategies["direct"])
	assert.Equal(t, 1, patterns[0].AffectedStrategies["chain"])
}

func TestCorrectionPattern_LimitsKeywords(t *testing.T) {
	cl := NewCorrectionLearner(nil, DefaultCorrectionLearnerConfig())
	ctx := context.Background()

	// Generate many unique keywords
	for i := 0; i < 30; i++ {
		cl.RecordCorrection(ctx, UserCorrection{
			Query:      "keyword" + string(rune('a'+i%26)) + " unique term" + string(rune('0'+i)),
			Correction: "Fixed",
			Type:       CorrectionOutput,
		})
	}

	patterns := cl.GetPatterns(CorrectionOutput)
	require.NotEmpty(t, patterns)

	// Keywords should be limited to 20
	assert.LessOrEqual(t, len(patterns[0].Keywords), 20)
}

// Benchmarks

func BenchmarkRecordCorrection(b *testing.B) {
	cl := NewCorrectionLearner(nil, DefaultCorrectionLearnerConfig())
	ctx := context.Background()

	correction := UserCorrection{
		Query:        "Write a function to sort an array",
		Correction:   "Fixed implementation",
		Type:         CorrectionOutput,
		Severity:     0.5,
		ModelUsed:    "test-model",
		StrategyUsed: "direct",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cl.RecordCorrection(ctx, correction)
	}
}

func BenchmarkSuggestAdjustments(b *testing.B) {
	cl := NewCorrectionLearner(nil, CorrectionLearnerConfig{
		MaxCorrections:   1000,
		PatternThreshold: 3,
	})
	ctx := context.Background()

	// Pre-populate with corrections
	for i := 0; i < 100; i++ {
		cl.RecordCorrection(ctx, UserCorrection{
			Query:        "Sort array query",
			Correction:   "Fixed",
			Type:         CorrectionOutput,
			Severity:     0.7,
			ModelUsed:    "model-a",
			StrategyUsed: "direct",
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cl.SuggestAdjustments()
	}
}

func BenchmarkExtractKeywords(b *testing.B) {
	text := "Write a function to implement quicksort algorithm for sorting arrays efficiently"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractKeywords(text)
	}
}
