package routing

import (
	"context"
	"testing"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) (*Store, *hypergraph.Store) {
	t.Helper()
	graph, err := hypergraph.NewStore(hypergraph.Options{
		Path:              "", // In-memory
		CreateIfNotExists: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { graph.Close() })

	return NewStore(graph), graph
}

func TestCategoryClassifier_Classify(t *testing.T) {
	classifier := NewCategoryClassifier(ClassifierConfig{CacheSize: 100})

	tests := []struct {
		name         string
		query        string
		wantCategory TaskCategory
		minConfidence float64
	}{
		{
			name:          "simple question - multiple simple patterns",
			query:         "What is the meaning of life? Define it. Tell me what's important.",
			wantCategory:  CategorySimple,
			minConfidence: 0.5,
		},
		{
			name:          "coding request with code keywords",
			query:         "Write code to implement a function that debugs the class method",
			wantCategory:  CategoryCoding,
			minConfidence: 0.5,
		},
		{
			name:          "reasoning task with prove",
			query:         "Prove the theorem and derive the conclusion step by step using logic",
			wantCategory:  CategoryReasoning,
			minConfidence: 0.5,
		},
		{
			name:          "creative writing",
			query:         "Write a story and create an imaginative poem for me",
			wantCategory:  CategoryCreative,
			minConfidence: 0.5,
		},
		{
			name:          "analysis request",
			query:         "Analyze and evaluate this, then summarize and compare the results",
			wantCategory:  CategoryAnalysis,
			minConfidence: 0.5,
		},
		{
			name:          "code block detection",
			query:         "Fix this bug:\n```go\nfunc main() {\n  fmt.Println(\"hello\")\n}\n```",
			wantCategory:  CategoryCoding,
			minConfidence: 0.9,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			category, confidence := classifier.Classify(ctx, tt.query)
			assert.Equal(t, tt.wantCategory, category)
			assert.GreaterOrEqual(t, confidence, tt.minConfidence)
		})
	}
}

func TestCategoryClassifier_Cache(t *testing.T) {
	classifier := NewCategoryClassifier(ClassifierConfig{CacheSize: 100})
	ctx := context.Background()

	query := "What is the capital of France?"

	// First call
	cat1, conf1 := classifier.Classify(ctx, query)

	// Second call should hit cache
	cat2, conf2 := classifier.Classify(ctx, query)

	assert.Equal(t, cat1, cat2)
	assert.Equal(t, conf1, conf2)

	stats := classifier.Stats()
	assert.Equal(t, int64(2), stats.TotalCalls)
	assert.Equal(t, int64(1), stats.CacheHits)
}

func TestFeatureExtractor_Extract(t *testing.T) {
	classifier := NewCategoryClassifier(ClassifierConfig{})
	extractor := NewFeatureExtractor(classifier, ExtractorConfig{})
	ctx := context.Background()

	t.Run("detects code", func(t *testing.T) {
		query := "```go\nfunc main() {}\n```"
		features := extractor.Extract(ctx, query, 0, 0)
		assert.True(t, features.HasCode)
		assert.Contains(t, features.Languages, "go")
	})

	t.Run("detects math", func(t *testing.T) {
		query := "Solve the equation: ∫ x² dx"
		features := extractor.Extract(ctx, query, 0, 0)
		assert.True(t, features.HasMath)
	})

	t.Run("estimates tokens", func(t *testing.T) {
		query := "This is a test query with some words"
		features := extractor.Extract(ctx, query, 0, 0)
		assert.Greater(t, features.TokenCount, 0)
	})

	t.Run("estimates depth", func(t *testing.T) {
		simple := "What is 2+2?"
		complex := "Design an architecture for a distributed system with step by step explanation"

		simpleFeatures := extractor.Extract(ctx, simple, 0, 0)
		complexFeatures := extractor.Extract(ctx, complex, 0, 0)

		assert.Less(t, simpleFeatures.EstimatedDepth, complexFeatures.EstimatedDepth)
	})

	t.Run("detects ambiguity", func(t *testing.T) {
		clear := "In file src/main.go, fix the bug on line 42"
		vague := "Fix it"

		clearFeatures := extractor.Extract(ctx, clear, 0, 0)
		vagueFeatures := extractor.Extract(ctx, vague, 0, 0)

		assert.Less(t, clearFeatures.Ambiguity, vagueFeatures.Ambiguity)
	})
}

func TestRouter_Route(t *testing.T) {
	profiles := []*ModelProfile{
		{
			ID:             "fast-model",
			Provider:       "test",
			Name:           "Fast Model",
			MaxTokens:      4096,
			ContextWindow:  8192,
			InputCostPer1M: 0.5,
			OutputCostPer1M: 1.0,
			MedianLatency:  500 * time.Millisecond,
			SuccessRate:    0.9,
			CategoryScores: map[TaskCategory]float64{
				CategorySimple: 0.9,
				CategoryCoding: 0.6,
			},
		},
		{
			ID:              "powerful-model",
			Provider:        "test",
			Name:            "Powerful Model",
			MaxTokens:       8192,
			ContextWindow:   128000,
			InputCostPer1M:  10.0,
			OutputCostPer1M: 30.0,
			MedianLatency:   2 * time.Second,
			SuccessRate:     0.95,
			CategoryScores: map[TaskCategory]float64{
				CategorySimple:    0.95,
				CategoryCoding:    0.95,
				CategoryReasoning: 0.98,
			},
		},
	}

	classifier := NewCategoryClassifier(ClassifierConfig{})
	extractor := NewFeatureExtractor(classifier, ExtractorConfig{})
	router := NewRouter(profiles, extractor, ScoringRouterConfig{
		DefaultModel:  "fast-model",
		MinConfidence: 0.7,
	})

	ctx := context.Background()

	t.Run("routes simple query", func(t *testing.T) {
		result, err := router.Route(ctx, "What is the capital of France?", nil)
		require.NoError(t, err)
		assert.NotEmpty(t, result.ModelID)
		assert.Greater(t, result.Score, 0.0)
	})

	t.Run("respects cost constraint", func(t *testing.T) {
		result, err := router.Route(ctx, "Complex coding task", &RoutingConstraints{
			MaxCostPerRequest: 0.001, // Very low budget
		})
		require.NoError(t, err)
		assert.Equal(t, "fast-model", result.ModelID)
	})

	t.Run("respects latency constraint", func(t *testing.T) {
		result, err := router.Route(ctx, "Quick question", &RoutingConstraints{
			MaxLatency: 1 * time.Second,
		})
		require.NoError(t, err)
		assert.Equal(t, "fast-model", result.ModelID)
	})

	t.Run("respects excluded models", func(t *testing.T) {
		result, err := router.Route(ctx, "Any query", &RoutingConstraints{
			ExcludedModels: []string{"fast-model"},
		})
		require.NoError(t, err)
		assert.Equal(t, "powerful-model", result.ModelID)
	})

	t.Run("provides alternatives", func(t *testing.T) {
		result, err := router.Route(ctx, "Some query", nil)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(result.Alternatives), 1)
	})
}

func TestStore_Profiles(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	profile := &ModelProfile{
		ID:              "test-model",
		Provider:        "anthropic",
		Name:            "Claude",
		MaxTokens:       4096,
		ContextWindow:   200000,
		InputCostPer1M:  3.0,
		OutputCostPer1M: 15.0,
		SuccessRate:     0.95,
		UpdatedAt:       time.Now(),
	}

	// Save profile
	err := store.SaveProfile(ctx, profile)
	require.NoError(t, err)

	// Get profile
	got, err := store.GetProfile(ctx, "test-model")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, profile.ID, got.ID)
	assert.Equal(t, profile.Provider, got.Provider)

	// List profiles
	profiles, err := store.ListProfiles(ctx)
	require.NoError(t, err)
	assert.Len(t, profiles, 1)

	// Delete profile
	err = store.DeleteProfile(ctx, "test-model")
	require.NoError(t, err)

	got, err = store.GetProfile(ctx, "test-model")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestStore_History(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	entry := &RoutingHistoryEntry{
		Timestamp: time.Now(),
		Task:      "Test task",
		Features: &TaskFeatures{
			Category:           CategoryCoding,
			CategoryConfidence: 0.9,
		},
		ModelUsed: "test-model",
		Score:     0.85,
		Outcome:   OutcomeSuccess,
		LatencyMS: 500,
		TokensUsed: 1000,
		Cost:      0.01,
	}

	// Record history
	err := store.RecordHistory(ctx, entry)
	require.NoError(t, err)
	assert.NotEmpty(t, entry.ID)

	// Get history
	entries, err := store.GetRecentHistory(ctx, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "test-model", entries[0].ModelUsed)

	// Get by model
	entries, err = store.GetHistoryByModel(ctx, "test-model", 10)
	require.NoError(t, err)
	assert.Len(t, entries, 1)

	// Get by category
	entries, err = store.GetHistoryByCategory(ctx, CategoryCoding, 10)
	require.NoError(t, err)
	assert.Len(t, entries, 1)

	// Stats
	stats, err := store.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.HistoryCount)
	assert.Equal(t, 1, stats.SuccessCount)
}

func TestLearner_RecordOutcome(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Create initial profiles
	profiles := []*ModelProfile{
		{
			ID:          "test-model",
			Provider:    "test",
			SuccessRate: 0.5,
			CategoryScores: map[TaskCategory]float64{
				CategoryCoding: 0.5,
			},
			UpdatedAt: time.Now(),
		},
	}

	classifier := NewCategoryClassifier(ClassifierConfig{})
	extractor := NewFeatureExtractor(classifier, ExtractorConfig{})
	router := NewRouter(profiles, extractor, ScoringRouterConfig{})

	learner := NewLearner(store, router, LearnerConfig{
		BatchSize: 1, // Flush immediately for testing
	})

	// Record successful outcome
	entry := &RoutingHistoryEntry{
		Timestamp: time.Now(),
		ModelUsed: "test-model",
		Features: &TaskFeatures{
			Category: CategoryCoding,
		},
		Outcome:   OutcomeSuccess,
		LatencyMS: 500,
	}

	err := learner.RecordOutcome(ctx, entry)
	require.NoError(t, err)

	// Check that profile was updated
	profile := router.GetProfile("test-model")
	require.NotNil(t, profile)
	// Success should improve the category score
	assert.Greater(t, profile.GetCategoryScore(CategoryCoding), 0.5)
}

func TestLearner_LearnFromFeedback(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	profiles := []*ModelProfile{
		{ID: "test-model", CategoryScores: make(map[TaskCategory]float64), UpdatedAt: time.Now()},
	}

	classifier := NewCategoryClassifier(ClassifierConfig{})
	extractor := NewFeatureExtractor(classifier, ExtractorConfig{})
	router := NewRouter(profiles, extractor, ScoringRouterConfig{})

	learner := NewLearner(store, router, LearnerConfig{BatchSize: 1})

	// Good feedback
	err := learner.LearnFromFeedback(ctx, "test-model", CategoryCoding, 5)
	require.NoError(t, err)

	// History should be recorded
	entries, err := store.GetRecentHistory(ctx, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(entries), 1)
}

func TestScoringWeights_Normalize(t *testing.T) {
	weights := ScoringWeights{
		Quality:    0.8,
		Cost:       0.4,
		Latency:    0.2,
		Historical: 0.6,
	}

	weights.Normalize()

	total := weights.Quality + weights.Cost + weights.Latency + weights.Historical
	assert.InDelta(t, 1.0, total, 0.001)
}

func TestModelProfile_CategoryScore(t *testing.T) {
	profile := &ModelProfile{
		ID:             "test",
		CategoryScores: make(map[TaskCategory]float64),
	}

	// Default score
	assert.Equal(t, 0.5, profile.GetCategoryScore(CategoryCoding))

	// Set score
	profile.SetCategoryScore(CategoryCoding, 0.9)
	assert.Equal(t, 0.9, profile.GetCategoryScore(CategoryCoding))

	// Clamping
	profile.SetCategoryScore(CategoryCoding, 1.5)
	assert.Equal(t, 1.0, profile.GetCategoryScore(CategoryCoding))

	profile.SetCategoryScore(CategoryCoding, -0.5)
	assert.Equal(t, 0.0, profile.GetCategoryScore(CategoryCoding))
}

func TestRoutingOutcome_Weight(t *testing.T) {
	assert.Equal(t, 1.0, OutcomeSuccess.OutcomeWeight())
	assert.Equal(t, 0.5, OutcomeCorrected.OutcomeWeight())
	assert.Equal(t, 0.0, OutcomeFailed.OutcomeWeight())
}
