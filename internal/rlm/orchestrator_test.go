package rlm

import (
	"context"
	"testing"

	"github.com/rand/recurse/internal/rlm/meta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrchestrator_Analyze_Disabled(t *testing.T) {
	orchestrator := NewOrchestrator(nil, OrchestratorConfig{
		Enabled: false,
	})

	result, err := orchestrator.Analyze(context.Background(), "test prompt", 100)
	require.NoError(t, err)
	assert.Equal(t, "test prompt", result.OriginalPrompt)
	assert.Equal(t, "test prompt", result.EnhancedPrompt)
	assert.Nil(t, result.Decision)
}

func TestOrchestrator_AnalyzeContextNeeds(t *testing.T) {
	orchestrator := NewOrchestrator(nil, OrchestratorConfig{
		Enabled: true,
		Models:  meta.DefaultModels(),
	})

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			needs := orchestrator.analyzeContextNeeds(tt.prompt)

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

func TestOrchestrator_DetermineRouting(t *testing.T) {
	orchestrator := NewOrchestrator(nil, OrchestratorConfig{
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
			routing := orchestrator.determineRouting(tt.prompt, decision)
			assert.Equal(t, tt.expectedTier, routing.PrimaryTier)
			assert.NotEmpty(t, routing.Reasoning)
			assert.NotEmpty(t, routing.SubtaskRoutes)
		})
	}
}

func TestOrchestrator_CreateSubtasks(t *testing.T) {
	orchestrator := NewOrchestrator(nil, OrchestratorConfig{
		Enabled: true,
		Models:  meta.DefaultModels(),
	})

	tests := []struct {
		name           string
		strategy       meta.DecomposeStrategy
		minSubtasks    int
		hasSynthesis   bool
	}{
		{
			name:         "file strategy",
			strategy:     meta.StrategyFile,
			minSubtasks:  3, // gather, analyze, synthesize
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

			subtasks := orchestrator.createSubtasks("test task", decision)
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

func TestOrchestrator_EnableDisable(t *testing.T) {
	orchestrator := NewOrchestrator(nil, OrchestratorConfig{
		Enabled: true,
	})

	assert.True(t, orchestrator.IsEnabled())

	orchestrator.SetEnabled(false)
	assert.False(t, orchestrator.IsEnabled())

	orchestrator.SetEnabled(true)
	assert.True(t, orchestrator.IsEnabled())
}

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
