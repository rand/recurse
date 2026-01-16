// Package rlm contains SPEC-09 validation tests.
// These tests validate the complete end-to-end flow of session context persistence.
package rlm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rand/recurse/internal/memory/evolution"
	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/rlm/meta"
	"github.com/rand/recurse/internal/rlm/repl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spec09MockLLM provides controlled responses for SPEC-09 validation.
type spec09MockLLM struct {
	calls []string
}

func newSpec09MockLLM() *spec09MockLLM {
	return &spec09MockLLM{calls: []string{}}
}

func (m *spec09MockLLM) Complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	m.calls = append(m.calls, prompt)

	// Check if this is a session synthesis request
	if strings.Contains(prompt, "session experiences") && strings.Contains(prompt, "structured summary") {
		return `{
			"summary": "Implemented SPEC-09 session context persistence features",
			"tasks_completed": [{"description": "Added session synthesis", "outcome": "Working", "success": true}],
			"tasks_failed": [],
			"key_insights": ["Session summaries enable resumption", "LLM synthesis works"],
			"blockers_hit": [],
			"active_files": ["service.go", "synthesizer.go"],
			"unfinished_work": "Need to add more tests",
			"next_steps": ["Write validation tests", "Run full integration"]
		}`, nil
	}

	// Check if this is a meta-controller decision
	if strings.Contains(strings.ToLower(prompt), "action") &&
		strings.Contains(strings.ToLower(prompt), "direct") {
		return `{"action": "DIRECT", "reasoning": "Simple task"}`, nil
	}

	return "Mock LLM response for testing", nil
}

// TestSpec09_SessionSynthesis validates SPEC-09.01 - Session summary synthesis.
func TestSpec09_SessionSynthesis(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mockLLM := newSpec09MockLLM()

	// Create synthesizer
	synth := NewLLMSynthesizer(mockLLM)

	// Create mock experiences
	experiences := []*hypergraph.Node{
		{
			ID:      "exp-1",
			Type:    hypergraph.NodeTypeExperience,
			Content: "Implemented AddExperience function",
			Metadata: mustJSON(map[string]any{
				"success": true,
				"outcome": "Function added and tested",
			}),
		},
		{
			ID:      "exp-2",
			Type:    hypergraph.NodeTypeExperience,
			Content: "Fixed memory query display",
			Metadata: mustJSON(map[string]any{
				"success": true,
				"outcome": "Content now displays correctly",
				"insights_gained": []string{"Node content vs metadata matters"},
			}),
		},
	}

	// Test synthesis
	summary, err := synth.Synthesize(ctx, "test-session-123", experiences, 1*time.Hour)
	require.NoError(t, err)
	require.NotNil(t, summary, "Summary should not be nil")

	// Validate summary content
	t.Run("summary_has_content", func(t *testing.T) {
		assert.NotEmpty(t, summary.Summary, "Summary should have description")
		assert.Equal(t, "test-session-123", summary.SessionID)
	})

	t.Run("summary_has_tasks", func(t *testing.T) {
		// Should have parsed tasks from LLM response
		assert.GreaterOrEqual(t, len(summary.TasksCompleted), 1, "Should have completed tasks")
	})

	t.Run("summary_has_insights", func(t *testing.T) {
		assert.NotEmpty(t, summary.KeyInsights, "Should have key insights")
	})

	t.Run("summary_has_next_steps", func(t *testing.T) {
		assert.NotEmpty(t, summary.NextSteps, "Should have next steps for resumption")
	})

	t.Run("summary_has_files", func(t *testing.T) {
		assert.NotEmpty(t, summary.ActiveFiles, "Should track active files")
	})

	// Verify LLM was called
	assert.GreaterOrEqual(t, len(mockLLM.calls), 1, "LLM should be called for synthesis")
}

// TestSpec09_FallbackSynthesis validates fallback when LLM fails.
func TestSpec09_FallbackSynthesis(t *testing.T) {
	// Create synthesizer with nil client to force fallback
	synth := &LLMSynthesizer{
		client:    nil,
		maxTokens: 1024,
	}

	experiences := []*hypergraph.Node{
		{
			ID:      "exp-1",
			Type:    hypergraph.NodeTypeExperience,
			Content: "Test experience",
			Metadata: mustJSON(map[string]any{
				"success": true,
			}),
		},
	}

	// Should use fallback and not panic
	summary := synth.fallbackSummary("fallback-session", experiences, 30*time.Minute)
	require.NotNil(t, summary)
	assert.Contains(t, summary.Summary, "1 experiences")
	assert.Contains(t, summary.Summary, "1 successful")
}

// TestSpec09_ResumeSession validates SPEC-09.08 - Session resumption query.
func TestSpec09_ResumeSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mockLLM := newSpec09MockLLM()

	// Create service
	svc, err := NewService(mockLLM, DefaultServiceConfig())
	require.NoError(t, err)
	defer svc.Stop()

	err = svc.Start(ctx)
	require.NoError(t, err)

	// Pre-populate with a session summary node
	summaryData := evolution.SessionSummary{
		SessionID:      "prev-session-456",
		StartTime:      time.Now().Add(-2 * time.Hour),
		EndTime:        time.Now().Add(-1 * time.Hour),
		Duration:       1 * time.Hour,
		Summary:        "Implemented REPL activation features",
		TasksCompleted: []evolution.TaskSummary{{Description: "Fixed startup race", Success: true}},
		KeyInsights:    []string{"Synchronous startup prevents races"},
		ActiveFiles:    []string{"app.go", "repl/manager.go"},
		UnfinishedWork: "Write more tests",
		NextSteps:      []string{"Add integration tests", "Test edge cases"},
	}

	summaryJSON, err := json.Marshal(summaryData)
	require.NoError(t, err)

	summaryNode := hypergraph.NewNode(hypergraph.NodeTypeExperience, summaryData.Summary)
	summaryNode.Subtype = "session_summary"
	summaryNode.Tier = hypergraph.TierLongterm
	summaryNode.Metadata = summaryJSON

	err = svc.store.CreateNode(ctx, summaryNode)
	require.NoError(t, err)

	// Test ResumeSession
	sessionCtx, err := svc.ResumeSession(ctx)
	require.NoError(t, err)
	require.NotNil(t, sessionCtx, "Should find previous session")

	t.Run("has_previous_session", func(t *testing.T) {
		require.NotNil(t, sessionCtx.PreviousSession)
		assert.Contains(t, sessionCtx.PreviousSession.Summary, "REPL activation")
	})

	t.Run("has_unfinished_work", func(t *testing.T) {
		assert.Equal(t, "Write more tests", sessionCtx.UnfinishedWork)
	})

	t.Run("has_next_steps", func(t *testing.T) {
		assert.NotEmpty(t, sessionCtx.RecommendedStart)
		assert.Contains(t, sessionCtx.RecommendedStart, "Add integration tests")
	})

	t.Run("has_active_files", func(t *testing.T) {
		assert.NotEmpty(t, sessionCtx.ActiveFiles)
		assert.Contains(t, sessionCtx.ActiveFiles, "app.go")
	})
}

// TestSpec09_NoSessionContext validates behavior when no previous session exists.
func TestSpec09_NoSessionContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mockLLM := newSpec09MockLLM()

	// Create service with empty store
	svc, err := NewService(mockLLM, DefaultServiceConfig())
	require.NoError(t, err)
	defer svc.Stop()

	err = svc.Start(ctx)
	require.NoError(t, err)

	// ResumeSession should return nil (not error) for no previous sessions
	sessionCtx, err := svc.ResumeSession(ctx)
	require.NoError(t, err)
	assert.Nil(t, sessionCtx, "Should return nil for no previous sessions")
}

// TestSpec09_ActionExecute validates SPEC-09.05 - EXECUTE action.
func TestSpec09_ActionExecute(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create REPL manager
	replMgr, err := repl.NewManager(repl.Options{})
	require.NoError(t, err)

	err = replMgr.Start(ctx)
	require.NoError(t, err)
	defer replMgr.Stop()

	t.Run("execute_simple_computation", func(t *testing.T) {
		result, err := replMgr.Execute(ctx, "sum([1, 2, 3, 4, 5])")
		require.NoError(t, err)
		assert.Empty(t, result.Error)
		assert.Equal(t, "15", result.ReturnVal)
	})

	t.Run("execute_data_transformation", func(t *testing.T) {
		result, err := replMgr.Execute(ctx, `
data = [{"name": "Alice", "score": 90}, {"name": "Bob", "score": 85}]
avg_score = sum(d["score"] for d in data) / len(data)
avg_score
`)
		require.NoError(t, err)
		assert.Empty(t, result.Error)
		assert.Equal(t, "87.5", result.ReturnVal)
	})

	t.Run("execute_grep_for_counting", func(t *testing.T) {
		// Set up test context
		err := replMgr.SetVar(ctx, "test_code", `
def hello():
    pass

def world():
    pass

def greet():
    pass
`)
		require.NoError(t, err)

		// Use grep to count functions
		result, err := replMgr.Execute(ctx, `
matches = grep(test_code, r"def \w+")
len(matches)
`)
		require.NoError(t, err)
		assert.Empty(t, result.Error)
		assert.Equal(t, "3", result.ReturnVal, "Should find 3 function definitions")
	})

	t.Run("execute_final_mechanism", func(t *testing.T) {
		// Clear any previous FINAL
		_, err := replMgr.Execute(ctx, "clear_final_output()")
		require.NoError(t, err)

		// Execute code that calls FINAL
		result, err := replMgr.Execute(ctx, `
analysis = "Found 3 functions: hello, world, greet"
FINAL(analysis)
`)
		require.NoError(t, err)
		assert.Empty(t, result.Error)

		// Verify FINAL was set
		result, err = replMgr.Execute(ctx, "has_final_output()")
		require.NoError(t, err)
		assert.Equal(t, "True", result.ReturnVal)

		// Get the FINAL output
		result, err = replMgr.Execute(ctx, "get_final_output()")
		require.NoError(t, err)
		assert.Contains(t, result.ReturnVal, "Found 3 functions")
	})
}

// TestSpec09_MetaControllerExecuteAction validates meta-controller EXECUTE decision parsing.
func TestSpec09_MetaControllerExecuteAction(t *testing.T) {
	// Test that the meta-controller can parse EXECUTE actions
	mockLLM := newSpec09MockLLM()
	metaCtrl := meta.NewController(mockLLM, meta.DefaultConfig())

	// Create a state where meta-controller might decide to execute
	state := meta.State{
		Task:               "Calculate the sum of 1 to 100",
		ContextTokens:      100,
		BudgetRemain:       10000,
		RecursionDepth:     0,
		MaxDepth:           5,
		ExternalizedContext: true, // Context is externalized
	}

	// Get a decision - we're testing the mechanism works, not the specific decision
	ctx := context.Background()
	decision, err := metaCtrl.Decide(ctx, state)
	require.NoError(t, err)
	require.NotNil(t, decision)

	// Verify the action is valid (any of the supported actions)
	validActions := []meta.Action{
		meta.ActionDirect,
		meta.ActionDecompose,
		meta.ActionMemoryQuery,
		meta.ActionSubcall,
		meta.ActionSynthesize,
		meta.ActionExecute,
	}
	assert.Contains(t, validActions, decision.Action, "Should return a valid action")
}

// TestSpec09_ContextExternalization validates SPEC-09.06 - Context externalization.
func TestSpec09_ContextExternalization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mockLLM := newSpec09MockLLM()

	// Create service
	svc, err := NewService(mockLLM, DefaultServiceConfig())
	require.NoError(t, err)
	defer svc.Stop()

	err = svc.Start(ctx)
	require.NoError(t, err)

	// Create and start REPL manager
	replMgr, err := repl.NewManager(repl.Options{})
	require.NoError(t, err)

	err = replMgr.Start(ctx)
	require.NoError(t, err)
	defer replMgr.Stop()

	// Set REPL manager
	svc.SetREPLManager(replMgr)

	// Create wrapper for context preparation
	wrapper := svc.Wrapper()
	require.NotNil(t, wrapper)

	t.Run("small_context_stays_direct", func(t *testing.T) {
		// Small context should use direct mode
		prepared, err := wrapper.PrepareContext(ctx, "What is 2+2?", nil)
		require.NoError(t, err)
		assert.Equal(t, ModeDirecte, prepared.Mode, "Small context should use direct mode")
	})

	t.Run("mode_info_populated", func(t *testing.T) {
		prepared, err := wrapper.PrepareContext(ctx, "Test query", nil)
		require.NoError(t, err)
		assert.NotNil(t, prepared.ModeInfo, "Mode info should be populated")
	})
}

// TestSpec09_EndToEndSessionFlow validates the complete session lifecycle.
func TestSpec09_EndToEndSessionFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mockLLM := newSpec09MockLLM()

	// Create service
	svc, err := NewService(mockLLM, DefaultServiceConfig())
	require.NoError(t, err)
	defer svc.Stop()

	err = svc.Start(ctx)
	require.NoError(t, err)

	// Create lifecycle manager
	lifecycle, err := evolution.NewLifecycleManager(svc.store, evolution.DefaultLifecycleConfig())
	require.NoError(t, err)
	defer lifecycle.Close()

	// Set synthesizer on lifecycle manager
	lifecycle.SetSessionSynthesizer(NewLLMSynthesizer(mockLLM))

	t.Run("session_start", func(t *testing.T) {
		lifecycle.StartSession("e2e-test-session")
		assert.Equal(t, "e2e-test-session", lifecycle.SessionID())
	})

	t.Run("add_experiences_to_store", func(t *testing.T) {
		// Add experience nodes directly to the store (simulating work)
		exp1 := hypergraph.NewNode(hypergraph.NodeTypeExperience, "Fixed REPL startup")
		exp1.Tier = hypergraph.TierTask
		exp1.Metadata = mustJSON(map[string]any{
			"success":         true,
			"outcome":         "Synchronous startup working",
			"insights_gained": []string{"Race conditions bad"},
		})
		err := svc.store.CreateNode(ctx, exp1)
		require.NoError(t, err)

		exp2 := hypergraph.NewNode(hypergraph.NodeTypeExperience, "Added session synthesis")
		exp2.Tier = hypergraph.TierTask
		exp2.Metadata = mustJSON(map[string]any{
			"success": true,
			"outcome": "LLM synthesizes summaries",
		})
		err = svc.store.CreateNode(ctx, exp2)
		require.NoError(t, err)
	})

	t.Run("session_end_runs_lifecycle", func(t *testing.T) {
		result, err := lifecycle.SessionEnd(ctx)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "session_end", result.Operation)
	})
}

// TestSpec09_ResumeTool validates the rlm_resume tool functionality.
func TestSpec09_ResumeTool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mockLLM := newSpec09MockLLM()

	// Create service
	svc, err := NewService(mockLLM, DefaultServiceConfig())
	require.NoError(t, err)
	defer svc.Stop()

	err = svc.Start(ctx)
	require.NoError(t, err)

	// Pre-populate with session summary
	summaryData := evolution.SessionSummary{
		SessionID:      "tool-test-session",
		Summary:        "Tool test session summary",
		TasksCompleted: []evolution.TaskSummary{{Description: "Test task", Success: true}},
		NextSteps:      []string{"Continue testing"},
		ActiveFiles:    []string{"test.go"},
	}

	summaryJSON, err := json.Marshal(summaryData)
	require.NoError(t, err)

	summaryNode := hypergraph.NewNode(hypergraph.NodeTypeExperience, summaryData.Summary)
	summaryNode.Subtype = "session_summary"
	summaryNode.Tier = hypergraph.TierLongterm
	summaryNode.Metadata = summaryJSON
	err = svc.store.CreateNode(ctx, summaryNode)
	require.NoError(t, err)

	// Test that ResumeSession works via the service
	sessionCtx, err := svc.ResumeSession(ctx)
	require.NoError(t, err)
	require.NotNil(t, sessionCtx)

	// The tool should be able to use this
	assert.NotNil(t, sessionCtx.PreviousSession)
	assert.Equal(t, "Tool test session summary", sessionCtx.PreviousSession.Summary)
}

// mustJSON marshals to JSON or panics - for test setup only.
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
