package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/rlm/meta"
	"github.com/rand/recurse/internal/rlm/repl"
)

// =============================================================================
// Types Tests
// =============================================================================

func TestVariableInfo_BackwardsCompatibility(t *testing.T) {
	info := VariableInfo{
		Name:          "test_var",
		Type:          ContextTypeFile,
		Size:          1000,
		TokenEstimate: 250,
	}

	// Test backwards compatibility methods
	assert.Equal(t, 1000, info.Length())
	assert.Equal(t, 250, info.TokenCount())
}

func TestContextType_Values(t *testing.T) {
	types := []ContextType{
		ContextTypeFile,
		ContextTypeSearch,
		ContextTypeMemory,
		ContextTypeCustom,
		ContextTypePrompt,
	}

	for _, ct := range types {
		assert.NotEmpty(t, string(ct))
	}
}

func TestLoadedContext_Fields(t *testing.T) {
	loaded := &LoadedContext{
		Variables: map[string]VariableInfo{
			"file1": {Name: "file1", TokenEstimate: 100},
			"file2": {Name: "file2", TokenEstimate: 200},
		},
		TotalTokens: 300,
		LoadTime:    time.Now(),
	}

	assert.Len(t, loaded.Variables, 2)
	assert.Equal(t, 300, loaded.TotalTokens)
	assert.False(t, loaded.LoadTime.IsZero())
}

func TestSubtask_Fields(t *testing.T) {
	subtask := Subtask{
		ID:               "test-1",
		Description:      "Test subtask",
		Type:             "analysis",
		RecommendedModel: "claude-haiku",
		RecommendedTier:  meta.TierFast,
		Dependencies:     []string{"other-1"},
		Priority:         5,
	}

	assert.Equal(t, "test-1", subtask.ID)
	assert.Equal(t, "analysis", subtask.Type)
	assert.Contains(t, subtask.Dependencies, "other-1")
}

func TestTraceEvent_Fields(t *testing.T) {
	event := TraceEvent{
		ID:        "trace-1",
		Type:      "decision",
		Action:    "DECOMPOSE",
		Details:   "Breaking down task",
		Tokens:    100,
		Duration:  time.Millisecond * 50,
		Timestamp: time.Now(),
		Depth:     1,
		ParentID:  "parent-1",
		Status:    "completed",
	}

	assert.Equal(t, "trace-1", event.ID)
	assert.Equal(t, "decision", event.Type)
	assert.Equal(t, 1, event.Depth)
}

// =============================================================================
// Intelligent Tests
// =============================================================================

func TestNewIntelligent(t *testing.T) {
	intel := NewIntelligent(nil, IntelligentConfig{
		Enabled: true,
	})

	require.NotNil(t, intel)
	assert.True(t, intel.IsEnabled())
	assert.NotEmpty(t, intel.models) // Should have default models
}

func TestIntelligent_EnableDisable(t *testing.T) {
	intel := NewIntelligent(nil, IntelligentConfig{Enabled: true})

	assert.True(t, intel.IsEnabled())

	intel.SetEnabled(false)
	assert.False(t, intel.IsEnabled())

	intel.SetEnabled(true)
	assert.True(t, intel.IsEnabled())
}

func TestIntelligent_Analyze_Disabled(t *testing.T) {
	intel := NewIntelligent(nil, IntelligentConfig{Enabled: false})

	result, err := intel.Analyze(context.Background(), "test prompt", 100)
	require.NoError(t, err)

	assert.Equal(t, "test prompt", result.OriginalPrompt)
	assert.Equal(t, "test prompt", result.EnhancedPrompt)
	assert.Nil(t, result.Decision)
}

func TestIntelligent_Analyze_NoMetaController(t *testing.T) {
	intel := NewIntelligent(nil, IntelligentConfig{Enabled: true})

	result, err := intel.Analyze(context.Background(), "test prompt", 100)
	require.NoError(t, err)

	// Should still return a result, just without decision
	assert.Equal(t, "test prompt", result.OriginalPrompt)
	assert.Nil(t, result.Decision)
}

func TestIntelligent_AnalyzeContextNeeds(t *testing.T) {
	intel := NewIntelligent(nil, IntelligentConfig{Enabled: true})

	tests := []struct {
		name           string
		prompt         string
		expectPatterns bool
		expectQueries  bool
		expectConcepts bool
		minPriority    int
	}{
		{
			name:           "bug investigation",
			prompt:         "Fix the bug in the authentication module",
			expectQueries:  true,
			expectConcepts: true,
			minPriority:    9,
		},
		{
			name:           "file-related request",
			prompt:         "Read the file config.yaml and update the import settings",
			expectPatterns: true,
			minPriority:    8,
		},
		{
			name:           "code search",
			prompt:         "Find the function handleRequest and understand how it works",
			expectQueries:  true,
			minPriority:    7,
		},
		{
			name:           "architecture question",
			prompt:         "Explain how the database layer architecture works",
			expectConcepts: true,
			minPriority:    6,
		},
		{
			name:           "test request",
			prompt:         "Add tests for the new feature",
			expectPatterns: true,
			expectQueries:  true,
			minPriority:    5,
		},
		{
			name:           "simple prompt",
			prompt:         "Hello world",
			minPriority:    5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			needs := intel.analyzeContextNeeds(tt.prompt)

			if tt.expectPatterns {
				assert.NotEmpty(t, needs.FilePatterns, "expected file patterns")
			}
			if tt.expectQueries {
				assert.NotEmpty(t, needs.SearchQueries, "expected search queries")
			}
			if tt.expectConcepts {
				assert.NotEmpty(t, needs.ConceptsToUnderstand, "expected concepts")
			}
			assert.GreaterOrEqual(t, needs.Priority, tt.minPriority, "priority too low")
		})
	}
}

func TestIntelligent_DetermineRouting(t *testing.T) {
	intel := NewIntelligent(nil, IntelligentConfig{
		Enabled: true,
		Models:  meta.DefaultModels(),
	})

	tests := []struct {
		name         string
		prompt       string
		expectedTier meta.ModelTier
	}{
		{
			name:         "reasoning task",
			prompt:       "Prove this mathematical theorem step by step",
			expectedTier: meta.TierReasoning,
		},
		{
			name:         "complex analysis",
			prompt:       "Analyze and refactor this complex codebase",
			expectedTier: meta.TierPowerful,
		},
		{
			name:         "simple task",
			prompt:       "Just fix the typo quickly",
			expectedTier: meta.TierFast,
		},
		{
			name:         "normal task",
			prompt:       "Implement the new feature",
			expectedTier: meta.TierBalanced,
		},
	}

	decision := &meta.Decision{Action: meta.ActionDirect}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routing := intel.determineRouting(tt.prompt, decision)
			assert.Equal(t, tt.expectedTier, routing.PrimaryTier)
			assert.NotEmpty(t, routing.Reasoning)
			assert.NotEmpty(t, routing.SubtaskRoutes)
		})
	}
}

func TestIntelligent_CreateSubtasks(t *testing.T) {
	intel := NewIntelligent(nil, IntelligentConfig{
		Enabled: true,
		Models:  meta.DefaultModels(),
	})

	tests := []struct {
		name         string
		strategy     meta.DecomposeStrategy
		minSubtasks  int
		hasSynthesis bool
	}{
		{
			name:         "file strategy",
			strategy:     meta.StrategyFile,
			minSubtasks:  3,
			hasSynthesis: true,
		},
		{
			name:         "function strategy",
			strategy:     meta.StrategyFunction,
			minSubtasks:  3,
			hasSynthesis: true,
		},
		{
			name:         "concept strategy",
			strategy:     meta.StrategyConcept,
			minSubtasks:  3,
			hasSynthesis: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := &meta.Decision{
				Action: meta.ActionDecompose,
				Params: meta.DecisionParams{
					Strategy: tt.strategy,
				},
			}

			subtasks := intel.createSubtasks("test task", decision)
			assert.GreaterOrEqual(t, len(subtasks), tt.minSubtasks)

			if tt.hasSynthesis {
				lastTask := subtasks[len(subtasks)-1]
				assert.Equal(t, "synthesize", lastTask.ID)
				assert.Equal(t, "synthesis", lastTask.Type)
			}

			// Verify all subtasks have models assigned
			for _, st := range subtasks {
				assert.NotEmpty(t, st.RecommendedModel, "subtask %s should have a model", st.ID)
			}
		})
	}
}

func TestIntelligent_CreateSubtasks_WithChunks(t *testing.T) {
	intel := NewIntelligent(nil, IntelligentConfig{
		Enabled: true,
		Models:  meta.DefaultModels(),
	})

	decision := &meta.Decision{
		Action: meta.ActionDecompose,
		Params: meta.DecisionParams{
			Chunks: []string{"chunk 1", "chunk 2", "chunk 3"},
		},
	}

	subtasks := intel.createSubtasks("test task", decision)
	assert.Len(t, subtasks, 3)

	for i, st := range subtasks {
		assert.Equal(t, decision.Params.Chunks[i], st.Description)
	}
}

func TestIntelligent_FindModelForTier(t *testing.T) {
	models := []meta.ModelSpec{
		{ID: "fast-model", Tier: meta.TierFast},
		{ID: "balanced-model", Tier: meta.TierBalanced},
		{ID: "powerful-model", Tier: meta.TierPowerful},
	}

	intel := NewIntelligent(nil, IntelligentConfig{
		Enabled: true,
		Models:  models,
	})

	assert.Equal(t, "fast-model", intel.findModelForTier(meta.TierFast))
	assert.Equal(t, "balanced-model", intel.findModelForTier(meta.TierBalanced))
	assert.Equal(t, "powerful-model", intel.findModelForTier(meta.TierPowerful))
}

func TestIntelligent_FindModelForTier_Fallback(t *testing.T) {
	intel := NewIntelligent(nil, IntelligentConfig{
		Enabled: true,
		Models:  []meta.ModelSpec{}, // Empty models
	})

	// Should fall back to default
	model := intel.findModelForTier(meta.TierFast)
	assert.Equal(t, "anthropic/claude-haiku-4.5", model)
}

// =============================================================================
// Helper Functions Tests
// =============================================================================

func TestContainsAny(t *testing.T) {
	tests := []struct {
		s        string
		substrs  []string
		expected bool
	}{
		{"hello world", []string{"hello", "foo"}, true},
		{"hello world", []string{"foo", "bar"}, false},
		{"analyze this code", []string{"analyze", "refactor"}, true},
		{"", []string{"test"}, false},
		{"test", []string{}, false},
	}

	for _, tt := range tests {
		result := containsAny(tt.s, tt.substrs)
		assert.Equal(t, tt.expected, result)
	}
}

func TestExtractCodeTerms(t *testing.T) {
	tests := []struct {
		prompt   string
		expected []string
	}{
		{"Find handleRequest function", []string{"handleRequest"}},
		{"Check user_settings and UserProfile", []string{"user_settings", "UserProfile"}},
		{"Just a simple prompt", []string{}},
	}

	for _, tt := range tests {
		terms := extractCodeTerms(tt.prompt)
		for _, exp := range tt.expected {
			assert.Contains(t, terms, exp)
		}
	}
}

func TestHasUpperInMiddle(t *testing.T) {
	tests := []struct {
		s        string
		expected bool
	}{
		{"handleRequest", true},
		{"UserProfile", true},
		{"handle", false},
		{"ALLCAPS", true},
		{"lowercase", false},
		{"a", false},
		{"", false},
	}

	for _, tt := range tests {
		result := hasUpperInMiddle(tt.s)
		assert.Equal(t, tt.expected, result, "hasUpperInMiddle(%q)", tt.s)
	}
}

// =============================================================================
// Recovery Tests
// =============================================================================

func TestDefaultRecoveryConfig(t *testing.T) {
	cfg := DefaultRecoveryConfig()

	assert.Equal(t, 1, cfg.MaxRetries)
	assert.Equal(t, 100*time.Millisecond, cfg.RetryDelay)
	assert.True(t, cfg.EnableDegradation)
	assert.True(t, cfg.LogErrors)
}

func TestNewRecoveryManager(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	mgr := NewRecoveryManager(cfg)

	require.NotNil(t, mgr)
	assert.Equal(t, cfg.MaxRetries, mgr.Config().MaxRetries)
	assert.Equal(t, 0, mgr.RetryCount())
	assert.Empty(t, mgr.ErrorHistory())
}

func TestRecoveryManager_ClassifyError_Nil(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	category := mgr.ClassifyError(nil)
	assert.Equal(t, ErrorCategoryRetryable, category)
}

func TestRecoveryManager_ClassifyError_Timeout(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	tests := []struct {
		name string
		err  error
	}{
		{"context deadline", context.DeadlineExceeded},
		{"timeout string", errors.New("operation timeout")},
		{"deadline exceeded string", errors.New("deadline exceeded for request")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			category := mgr.ClassifyError(tt.err)
			assert.Equal(t, ErrorCategoryTimeout, category)
		})
	}
}

func TestRecoveryManager_ClassifyError_Retryable(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	tests := []struct {
		name string
		err  error
	}{
		{"connection error", errors.New("connection refused")},
		{"temporary error", errors.New("temporary failure")},
		{"retry suggestion", errors.New("please retry")},
		{"unavailable", errors.New("service unavailable")},
		{"python error", errors.New("python: SyntaxError")},
		{"syntax error", errors.New("syntax error at line 5")},
		{"nameerror", errors.New("NameError: name 'x' is not defined")},
		{"typeerror", errors.New("TypeError: unsupported operand")},
		{"repl error", errors.New("repl execution failed")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			category := mgr.ClassifyError(tt.err)
			assert.Equal(t, ErrorCategoryRetryable, category)
		})
	}
}

func TestRecoveryManager_ClassifyError_Degradable(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	tests := []struct {
		name string
		err  error
	}{
		{"decompose error", errors.New("failed to decompose task")},
		{"synthesize error", errors.New("synthesize failed")},
		{"orchestration error", errors.New("orchestration error")},
		{"unknown error", errors.New("something went wrong")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			category := mgr.ClassifyError(tt.err)
			assert.Equal(t, ErrorCategoryDegradable, category)
		})
	}
}

func TestRecoveryManager_ClassifyError_Terminal(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	tests := []struct {
		name string
		err  error
	}{
		{"invalid", errors.New("invalid request")},
		{"not found", errors.New("resource not found")},
		{"permission denied", errors.New("permission denied")},
		{"unauthorized", errors.New("unauthorized access")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			category := mgr.ClassifyError(tt.err)
			assert.Equal(t, ErrorCategoryTerminal, category)
		})
	}
}

func TestRecoveryManager_ClassifyError_Resource(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	resourceErr := repl.NewResourceError(
		&repl.ResourceViolation{Resource: "memory", Hard: true},
		&repl.ResourceStats{PeakMemoryMB: 1100},
	)

	category := mgr.ClassifyError(resourceErr)
	assert.Equal(t, ErrorCategoryResource, category)
}

func TestRecoveryManager_DetermineAction_Retryable(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	mgr := NewRecoveryManager(cfg)

	err := errors.New("syntax error in code")
	action := mgr.DetermineAction(err, meta.ActionDirect, meta.State{})

	assert.True(t, action.ShouldRetry)
	assert.False(t, action.Degraded)
	assert.NotEmpty(t, action.RetryPrompt)
	assert.Contains(t, action.Message, "Retrying")
}

func TestRecoveryManager_DetermineAction_RetryExhausted(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	mgr := NewRecoveryManager(cfg)
	mgr.SetRetryCount(cfg.MaxRetries)

	err := errors.New("syntax error in code")
	action := mgr.DetermineAction(err, meta.ActionDirect, meta.State{})

	assert.False(t, action.ShouldRetry)
	assert.True(t, action.Degraded)
	assert.Contains(t, action.Message, "Max retries reached")
}

func TestRecoveryManager_DetermineAction_Degradable(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	mgr := NewRecoveryManager(cfg)

	err := errors.New("decomposition failed")
	action := mgr.DetermineAction(err, meta.ActionDecompose, meta.State{})

	assert.False(t, action.ShouldRetry)
	assert.True(t, action.Degraded)
	assert.Contains(t, action.Message, "falling back")
}

func TestRecoveryManager_DetermineAction_Terminal(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	mgr := NewRecoveryManager(cfg)

	err := errors.New("permission denied")
	action := mgr.DetermineAction(err, meta.ActionDirect, meta.State{})

	assert.False(t, action.ShouldRetry)
	assert.False(t, action.Degraded)
	assert.Contains(t, action.Message, "Unrecoverable")
}

func TestRecoveryManager_DetermineAction_Resource(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	mgr := NewRecoveryManager(cfg)

	resourceErr := repl.NewResourceError(
		&repl.ResourceViolation{Resource: "memory", Hard: true},
		&repl.ResourceStats{PeakMemoryMB: 1100},
	)

	action := mgr.DetermineAction(resourceErr, meta.ActionDirect, meta.State{})

	assert.False(t, action.ShouldRetry)
	assert.True(t, action.Degraded)
	assert.Contains(t, action.Message, "Resource limit")
}

func TestRecoveryManager_RetryCounter(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	assert.Equal(t, 0, mgr.RetryCount())

	mgr.IncrementRetry()
	assert.Equal(t, 1, mgr.RetryCount())

	mgr.IncrementRetry()
	assert.Equal(t, 2, mgr.RetryCount())

	mgr.ResetRetry()
	assert.Equal(t, 0, mgr.RetryCount())
}

func TestRecoveryManager_RecordError(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	mgr := NewRecoveryManager(cfg)

	record := ErrorRecord{
		Category:  ErrorCategoryRetryable,
		Action:    "REPL",
		Error:     "syntax error",
		Context:   "test context",
		Recovered: true,
	}

	mgr.RecordError(record)

	history := mgr.ErrorHistory()
	require.Len(t, history, 1)
	assert.Equal(t, ErrorCategoryRetryable, history[0].Category)
	assert.True(t, history[0].Recovered)
	assert.NotZero(t, history[0].Timestamp)
}

func TestRecoveryManager_RecordError_Disabled(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	cfg.LogErrors = false
	mgr := NewRecoveryManager(cfg)

	record := ErrorRecord{
		Category: ErrorCategoryRetryable,
		Error:    "syntax error",
	}

	mgr.RecordError(record)

	history := mgr.ErrorHistory()
	assert.Empty(t, history)
}

func TestRecoveryManager_ErrorStats(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	mgr.RecordError(ErrorRecord{Category: ErrorCategoryRetryable, Recovered: true})
	mgr.RecordError(ErrorRecord{Category: ErrorCategoryRetryable, Recovered: false})
	mgr.RecordError(ErrorRecord{Category: ErrorCategoryTimeout, Recovered: true, Degraded: true})
	mgr.RecordError(ErrorRecord{Category: ErrorCategoryTerminal, Recovered: false})

	stats := mgr.ErrorStats()

	assert.Equal(t, 4, stats.TotalErrors)
	assert.Equal(t, 2, stats.RecoveredCount)
	assert.Equal(t, 1, stats.DegradedCount)
	assert.Equal(t, 0.5, stats.RecoveryRate)
	assert.Equal(t, 2, stats.CategoryCounts[ErrorCategoryRetryable])
	assert.Equal(t, 1, stats.CategoryCounts[ErrorCategoryTimeout])
	assert.Equal(t, 1, stats.CategoryCounts[ErrorCategoryTerminal])
}

func TestRecoveryManager_ErrorStats_Empty(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	stats := mgr.ErrorStats()

	assert.Equal(t, 0, stats.TotalErrors)
	assert.Equal(t, float64(0), stats.RecoveryRate)
}

func TestRecoveryManager_HistoryBounding(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	// Add more than 1000 errors
	for i := 0; i < 1050; i++ {
		mgr.RecordError(ErrorRecord{Category: ErrorCategoryRetryable})
	}

	// History should be bounded
	history := mgr.ErrorHistory()
	assert.LessOrEqual(t, len(history), 1000)
}

func TestTruncateError(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		max      int
		expected string
	}{
		{"short", "short error", 20, "short error"},
		{"exact", "12345", 5, "12345"},
		{"long", "this is a very long error message", 10, "this is..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateError(tt.input, tt.max)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRecoverableError(t *testing.T) {
	original := errors.New("original error")
	action := &RecoveryAction{
		ShouldRetry: true,
		RetryPrompt: "try again",
	}

	wrapped := WrapWithRecovery(original, action)

	assert.Equal(t, "original error", wrapped.Error())
	assert.Equal(t, original, wrapped.Unwrap())
	assert.Equal(t, action, wrapped.Action)
}

func TestIsRecoverable(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		recoverable bool
	}{
		{
			"retryable",
			WrapWithRecovery(errors.New("err"), &RecoveryAction{ShouldRetry: true}),
			true,
		},
		{
			"degradable",
			WrapWithRecovery(errors.New("err"), &RecoveryAction{Degraded: true}),
			true,
		},
		{
			"terminal",
			WrapWithRecovery(errors.New("err"), &RecoveryAction{ShouldRetry: false, Degraded: false}),
			false,
		},
		{
			"plain error",
			errors.New("plain error"),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.recoverable, IsRecoverable(tt.err))
		})
	}
}

func TestShouldDegrade(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		degrade bool
	}{
		{
			"degradable",
			WrapWithRecovery(errors.New("err"), &RecoveryAction{Degraded: true}),
			true,
		},
		{
			"not degradable",
			WrapWithRecovery(errors.New("err"), &RecoveryAction{Degraded: false}),
			false,
		},
		{
			"plain error",
			errors.New("plain error"),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.degrade, ShouldDegrade(tt.err))
		})
	}
}

// =============================================================================
// Steering Tests
// =============================================================================

func TestNewSteering(t *testing.T) {
	steering := NewSteering(SteeringConfig{
		ContextEnabled: true,
	})

	require.NotNil(t, steering)
	// No REPL manager yet, so context should not be enabled
	assert.False(t, steering.IsContextEnabled())
}

func TestSteering_SetContextEnabled(t *testing.T) {
	steering := NewSteering(SteeringConfig{ContextEnabled: true})

	// Even if enabled, no REPL means false
	assert.False(t, steering.IsContextEnabled())

	steering.SetContextEnabled(false)
	assert.False(t, steering.IsContextEnabled())
}

func TestSteering_HasREPL(t *testing.T) {
	steering := NewSteering(SteeringConfig{})

	assert.False(t, steering.HasREPL())
}

func TestSteering_EnhancePrompt_NoDecision(t *testing.T) {
	steering := NewSteering(SteeringConfig{})

	analysis := &AnalysisResult{
		Decision: nil,
	}

	enhanced := steering.EnhancePrompt("original prompt", analysis)
	assert.Equal(t, "original prompt", enhanced)
}

func TestSteering_EnhancePrompt_WithDecision(t *testing.T) {
	steering := NewSteering(SteeringConfig{})

	analysis := &AnalysisResult{
		Decision: &meta.Decision{
			Action:    meta.ActionDirect,
			Reasoning: "Simple task",
		},
		Routing: &TaskRouting{
			PrimaryTier: meta.TierBalanced,
			Reasoning:   "Normal complexity",
		},
	}

	enhanced := steering.EnhancePrompt("original prompt", analysis)

	assert.Contains(t, enhanced, "RLM Analysis")
	assert.Contains(t, enhanced, "DIRECT")
	assert.Contains(t, enhanced, "Simple task")
	assert.Contains(t, enhanced, "original prompt")
}

func TestSteering_GenerateRLMSystemPrompt(t *testing.T) {
	steering := NewSteering(SteeringConfig{ContextEnabled: true})

	loaded := &LoadedContext{
		Variables: map[string]VariableInfo{
			"file_content": {
				Name:          "file_content",
				Type:          ContextTypeFile,
				TokenEstimate: 500,
			},
		},
		TotalTokens: 500,
	}

	prompt := steering.GenerateRLMSystemPrompt(loaded)

	// Check for key RLM instructions
	assert.Contains(t, prompt, "RLM")
	assert.Contains(t, prompt, "Context is External")
	assert.Contains(t, prompt, "peek")
	assert.Contains(t, prompt, "grep")
	assert.Contains(t, prompt, "FINAL")
}

func TestSteering_GenerateRLMSystemPrompt_NoContext(t *testing.T) {
	steering := NewSteering(SteeringConfig{})

	prompt := steering.GenerateRLMSystemPrompt(nil)

	// Should still have base instructions
	assert.Contains(t, prompt, "RLM")
	assert.Contains(t, prompt, "Context is External")
}

// =============================================================================
// ContextLoader Tests
// =============================================================================

func TestNewContextLoader(t *testing.T) {
	loader := NewContextLoader(nil)
	require.NotNil(t, loader)
}

func TestContextLoader_GenerateContextPrompt_Empty(t *testing.T) {
	loader := NewContextLoader(nil)

	prompt := loader.GenerateContextPrompt(nil)
	assert.Empty(t, prompt)

	prompt = loader.GenerateContextPrompt(&LoadedContext{Variables: map[string]VariableInfo{}})
	assert.Empty(t, prompt)
}

func TestContextLoader_GenerateContextPrompt_WithVariables(t *testing.T) {
	loader := NewContextLoader(nil)

	loaded := &LoadedContext{
		Variables: map[string]VariableInfo{
			"code": {
				Name:          "code",
				Type:          ContextTypeFile,
				TokenEstimate: 1000,
			},
			"search_results": {
				Name:          "search_results",
				Type:          ContextTypeSearch,
				TokenEstimate: 500,
			},
		},
		TotalTokens: 1500,
	}

	prompt := loader.GenerateContextPrompt(loaded)

	assert.Contains(t, prompt, "code")
	assert.Contains(t, prompt, "search_results")
	assert.Contains(t, prompt, "1500")
}

// =============================================================================
// Integration Tests
// =============================================================================

func TestAnalysisResult_FullFlow(t *testing.T) {
	// Test a complete analysis result with all fields
	result := &AnalysisResult{
		OriginalPrompt: "Fix the authentication bug",
		EnhancedPrompt: "Enhanced: Fix the authentication bug",
		Decision: &meta.Decision{
			Action:    meta.ActionDecompose,
			Reasoning: "Complex task needs breakdown",
		},
		ContextNeeds: &ContextNeeds{
			FilePatterns:  []string{"**/*.go"},
			SearchQueries: []string{"auth", "login"},
			Priority:      9,
		},
		Routing: &TaskRouting{
			PrimaryTier: meta.TierPowerful,
			Reasoning:   "Bug fix requires deep analysis",
		},
		ShouldDecompose: true,
		Subtasks: []Subtask{
			{ID: "find", Description: "Find auth code"},
			{ID: "analyze", Description: "Analyze bug"},
			{ID: "fix", Description: "Apply fix"},
		},
		AnalysisTime: 50 * time.Millisecond,
		ModelUsed:    "meta-controller",
	}

	assert.Equal(t, "Fix the authentication bug", result.OriginalPrompt)
	assert.True(t, result.ShouldDecompose)
	assert.Len(t, result.Subtasks, 3)
	assert.Equal(t, 9, result.ContextNeeds.Priority)
}
