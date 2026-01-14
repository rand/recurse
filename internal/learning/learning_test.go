package learning

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

func TestStore_StoreFact(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	fact := &LearnedFact{
		Content:    "Go uses if err != nil for error handling",
		Domain:     "go",
		Source:     SourceExplicit,
		Confidence: 0.95,
	}

	err := store.StoreFact(ctx, fact)
	require.NoError(t, err)
	assert.NotEmpty(t, fact.ID)

	// Retrieve
	got, err := store.GetFact(ctx, fact.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, fact.Content, got.Content)
	assert.Equal(t, fact.Domain, got.Domain)
	assert.Equal(t, SourceExplicit, got.Source)
}

func TestStore_SearchFacts(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Store multiple facts
	facts := []*LearnedFact{
		{Content: "Go error handling uses if err != nil", Domain: "go", Source: SourceInferred, Confidence: 0.8},
		{Content: "Python uses try/except for errors", Domain: "python", Source: SourceInferred, Confidence: 0.8},
		{Content: "Go interfaces are implicit", Domain: "go", Source: SourceInferred, Confidence: 0.9},
	}

	for _, f := range facts {
		require.NoError(t, store.StoreFact(ctx, f))
	}

	// Search
	results, err := store.SearchFacts(ctx, "error handling", 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1) // Should find at least one match
}

func TestStore_StorePattern(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	pattern := &LearnedPattern{
		Name:        "Table-Driven Tests",
		PatternType: PatternTypeCode,
		Trigger:     "Go test with multiple cases",
		Template:    "tests := []struct{...}{{...}}\nfor _, tt := range tests {...}",
		Domains:     []string{"go"},
		SuccessRate: 0.9,
	}

	err := store.StorePattern(ctx, pattern)
	require.NoError(t, err)
	assert.NotEmpty(t, pattern.ID)

	// List patterns
	patterns, err := store.ListPatterns(ctx, PatternTypeCode, 10)
	require.NoError(t, err)
	assert.Len(t, patterns, 1)
	assert.Equal(t, "Table-Driven Tests", patterns[0].Name)
}

func TestStore_StorePreference(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	pref := &UserPreference{
		Key:        "coding_style",
		Value:      "functional",
		Scope:      ScopeGlobal,
		Source:     SourceExplicit,
		Confidence: 1.0,
	}

	err := store.StorePreference(ctx, pref)
	require.NoError(t, err)
	assert.NotEmpty(t, pref.ID)

	// Get by key
	got, err := store.GetPreferenceByKey(ctx, "coding_style", ScopeGlobal, "")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "functional", got.Value)
}

func TestStore_StoreConstraint(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	constraint := &LearnedConstraint{
		ConstraintType: ConstraintAvoid,
		Description:    "Avoid using panic for error handling",
		Correction:     "Return errors instead",
		Domain:         "go",
		Severity:       0.9,
		Source:         SourceCorrection,
	}

	err := store.StoreConstraint(ctx, constraint)
	require.NoError(t, err)
	assert.NotEmpty(t, constraint.ID)

	// List constraints
	constraints, err := store.ListConstraints(ctx, "go", 0)
	require.NoError(t, err)
	assert.Len(t, constraints, 1)
	assert.Equal(t, ConstraintAvoid, constraints[0].ConstraintType)
}

func TestExtractor_ProcessSuccessSignal(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	extractor := NewExtractor(store, ExtractorConfig{})

	signal := NewSuccessSignal(SignalContext{
		SessionID: "sess-1",
		TaskID:    "task-1",
		Query:     "Implement error handling",
		Output:    "if err != nil { return err }",
	}, 0.9)
	signal.Domain = "go"

	err := extractor.ProcessSignal(ctx, signal)
	require.NoError(t, err)

	// Check a fact was stored
	facts, err := store.ListFacts(ctx, "go", 0, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(facts), 1)
}

func TestExtractor_ProcessCorrectionSignal(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	// Use low minConfidence to accept correction signals (confidence = 1.0 - severity)
	extractor := NewExtractor(store, ExtractorConfig{
		MinConfidence: 0.1,
	})

	signal := NewCorrectionSignal(SignalContext{
		SessionID: "sess-1",
		Query:     "Handle the error",
		Output:    "panic(err)",
	}, CorrectionDetails{
		OriginalOutput:  "panic(err)",
		CorrectedOutput: "return fmt.Errorf(...)",
		CorrectionType:  "code",
		Severity:        0.3, // Low severity = high confidence (0.7)
	})
	signal.Domain = "go"

	err := extractor.ProcessSignal(ctx, signal)
	require.NoError(t, err)

	// Check a constraint was stored
	constraints, err := store.ListConstraints(ctx, "", 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(constraints), 1, "should have at least one constraint")
	assert.Equal(t, ConstraintAvoid, constraints[0].ConstraintType)
}

func TestExtractor_ProcessPreferenceSignal(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	extractor := NewExtractor(store, ExtractorConfig{})

	signal := NewPreferenceSignal(SignalContext{
		SessionID: "sess-1",
	}, PreferenceDetails{
		Key:      "test_framework",
		Value:    "pytest",
		Scope:    ScopeDomain,
		ScopeValue: "python",
		Explicit: true,
	})

	err := extractor.ProcessSignal(ctx, signal)
	require.NoError(t, err)

	// Check preference was stored
	pref, err := store.GetPreferenceByKey(ctx, "test_framework", ScopeDomain, "python")
	require.NoError(t, err)
	require.NotNil(t, pref)
	assert.Equal(t, "pytest", pref.Value)
}

func TestExtractor_DetectCodePatterns(t *testing.T) {
	extractor := NewExtractor(nil, ExtractorConfig{})

	code := `
func doSomething() error {
	result, err := operation()
	if err != nil {
		return fmt.Errorf("operation failed: %w", err)
	}

	another, err := anotherOp()
	if err != nil {
		return err
	}

	return nil
}
`

	patterns := extractor.detectCodePatterns(code)
	assert.GreaterOrEqual(t, len(patterns), 1)

	// Should detect Go error handling pattern
	var found bool
	for _, p := range patterns {
		if p.Name == "Go Error Handling" {
			found = true
			assert.Equal(t, PatternTypeCode, p.PatternType)
		}
	}
	assert.True(t, found, "should detect Go error handling pattern")
}

func TestConsolidator_CalculateDecay(t *testing.T) {
	consolidator := NewConsolidator(nil, ConsolidatorConfig{
		DecayHalfLife: 7 * 24 * time.Hour,
	})

	tests := []struct {
		name        string
		confidence  float64
		lastAccess  time.Time
		accessCount int
		wantLess    float64
	}{
		{
			name:        "recent access",
			confidence:  1.0,
			lastAccess:  time.Now().Add(-1 * time.Hour),
			accessCount: 1,
			wantLess:    1.01, // Almost no decay
		},
		{
			name:        "week old access",
			confidence:  1.0,
			lastAccess:  time.Now().Add(-7 * 24 * time.Hour),
			accessCount: 1,
			wantLess:    0.7, // Significant decay
		},
		{
			name:        "week old with high access count",
			confidence:  1.0,
			lastAccess:  time.Now().Add(-7 * 24 * time.Hour),
			accessCount: 10,
			wantLess:    0.9, // Less decay due to repetition
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := consolidator.calculateDecay(tt.confidence, tt.lastAccess, tt.accessCount)
			assert.Less(t, got, tt.wantLess)
			assert.Greater(t, got, 0.0)
		})
	}
}

func TestApplier_Apply(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Pre-populate with some knowledge
	store.StoreFact(ctx, &LearnedFact{
		Content:    "Go uses gofmt for formatting",
		Domain:     "go",
		Source:     SourceExplicit,
		Confidence: 0.9,
	})
	store.StorePreference(ctx, &UserPreference{
		Key:        "error_style",
		Value:      "wrap with context",
		Scope:      ScopeDomain,
		ScopeValue: "go",
		Source:     SourceExplicit,
		Confidence: 0.95,
	})
	store.StoreConstraint(ctx, &LearnedConstraint{
		ConstraintType: ConstraintAvoid,
		Description:    "Avoid naked returns in functions with named return values",
		Domain:         "go",
		Severity:       0.7,
		Source:         SourceCorrection,
	})

	applier := NewApplier(store, ApplierConfig{
		MinConfidence: 0.5,
	})

	result, err := applier.Apply(ctx, "format my go code", "go", "")
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.IsEmpty())
	assert.Greater(t, len(result.ContextAdditions), 0)
}

func TestEngine_Integration(t *testing.T) {
	graph, err := hypergraph.NewStore(hypergraph.Options{
		Path:              "",
		CreateIfNotExists: true,
	})
	require.NoError(t, err)
	defer graph.Close()

	engine := NewEngine(graph, EngineConfig{})

	ctx := context.Background()

	// Learn from success
	err = engine.LearnSuccess(ctx, "sess-1", "task-1", "Write tests", "func TestFoo(t *testing.T) {...}", "claude", "direct", "go", 0.9)
	require.NoError(t, err)

	// Learn preference
	err = engine.LearnPreference(ctx, "sess-1", "test_style", "table-driven", ScopeDomain, "go", true)
	require.NoError(t, err)

	// Learn from correction
	err = engine.LearnCorrection(ctx, "sess-1", "task-2", "Handle error", "panic(err)", "return err", "code", "Don't panic", "go", 0.8)
	require.NoError(t, err)

	// Apply knowledge
	result, err := engine.Apply(ctx, "write some go tests", "go", "")
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Check stats
	stats, err := engine.Stats(ctx)
	require.NoError(t, err)
	assert.Greater(t, stats.TotalKnowledge, 0)
}

func TestSignal_GetDetails(t *testing.T) {
	t.Run("correction details", func(t *testing.T) {
		signal := NewCorrectionSignal(SignalContext{}, CorrectionDetails{
			OriginalOutput:  "bad",
			CorrectedOutput: "good",
			Severity:        0.5,
		})

		details, ok := signal.GetCorrectionDetails()
		assert.True(t, ok)
		assert.Equal(t, "bad", details.OriginalOutput)
		assert.Equal(t, "good", details.CorrectedOutput)
	})

	t.Run("preference details", func(t *testing.T) {
		signal := NewPreferenceSignal(SignalContext{}, PreferenceDetails{
			Key:      "style",
			Value:    "functional",
			Explicit: true,
		})

		details, ok := signal.GetPreferenceDetails()
		assert.True(t, ok)
		assert.Equal(t, "style", details.Key)
		assert.Equal(t, "functional", details.Value)
		assert.True(t, details.Explicit)
	})

	t.Run("pattern details", func(t *testing.T) {
		signal := NewPatternSignal(SignalContext{}, PatternDetails{
			Name:        "Error Handling",
			PatternType: PatternTypeCode,
			Trigger:     "error check",
		}, 0.9)

		details, ok := signal.GetPatternDetails()
		assert.True(t, ok)
		assert.Equal(t, "Error Handling", details.Name)
		assert.Equal(t, PatternTypeCode, details.PatternType)
	})
}

func TestLearnedFact_SuccessRate(t *testing.T) {
	tests := []struct {
		name     string
		success  int
		failure  int
		wantRate float64
	}{
		{"no data", 0, 0, 0.5},
		{"all success", 10, 0, 1.0},
		{"all failure", 0, 10, 0.0},
		{"mixed", 7, 3, 0.7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fact := &LearnedFact{
				SuccessCount: tt.success,
				FailureCount: tt.failure,
			}
			assert.InDelta(t, tt.wantRate, fact.SuccessRate(), 0.001)
		})
	}
}

func TestApplyResult_Methods(t *testing.T) {
	t.Run("empty result", func(t *testing.T) {
		result := &ApplyResult{}
		assert.True(t, result.IsEmpty())
		assert.Equal(t, 0, result.ItemCount())
	})

	t.Run("non-empty result", func(t *testing.T) {
		result := &ApplyResult{
			RelevantFacts:      []*LearnedFact{{}, {}},
			ApplicablePatterns: []*LearnedPattern{{}},
			Preferences:        []*UserPreference{{}},
		}
		assert.False(t, result.IsEmpty())
		assert.Equal(t, 4, result.ItemCount())
	})
}
