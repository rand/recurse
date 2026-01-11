package meta

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLLMClient implements LLMClient for testing.
type mockLLMClient struct {
	response string
	err      error
}

func (m *mockLLMClient) Complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func TestNewController(t *testing.T) {
	client := &mockLLMClient{}
	ctrl := NewController(client, DefaultConfig())

	require.NotNil(t, ctrl)
	assert.Equal(t, 5, ctrl.MaxDepth())
}

func TestController_Decide_Direct(t *testing.T) {
	client := &mockLLMClient{
		response: `{"action": "DIRECT", "params": {}, "reasoning": "Simple task, can answer directly"}`,
	}
	ctrl := NewController(client, DefaultConfig())

	decision, err := ctrl.Decide(context.Background(), State{
		Task:          "What is 2+2?",
		ContextTokens: 100,
		BudgetRemain:  1000,
	})

	require.NoError(t, err)
	assert.Equal(t, ActionDirect, decision.Action)
	assert.Contains(t, decision.Reasoning, "Simple task")
}

func TestController_Decide_Decompose(t *testing.T) {
	client := &mockLLMClient{
		response: `{
			"action": "DECOMPOSE",
			"params": {"strategy": "file", "chunks": ["main.go", "utils.go"]},
			"reasoning": "Multiple files need to be analyzed separately"
		}`,
	}
	ctrl := NewController(client, DefaultConfig())

	decision, err := ctrl.Decide(context.Background(), State{
		Task:          "Refactor the codebase",
		ContextTokens: 50000,
		BudgetRemain:  100000,
	})

	require.NoError(t, err)
	assert.Equal(t, ActionDecompose, decision.Action)
	assert.Equal(t, StrategyFile, decision.Params.Strategy)
	assert.Len(t, decision.Params.Chunks, 2)
}

func TestController_Decide_MemoryQuery(t *testing.T) {
	client := &mockLLMClient{
		response: `{
			"action": "MEMORY_QUERY",
			"params": {"query": "database connection patterns"},
			"reasoning": "Need to recall previous decisions about DB architecture"
		}`,
	}
	ctrl := NewController(client, DefaultConfig())

	decision, err := ctrl.Decide(context.Background(), State{
		Task:          "Add a new database table",
		ContextTokens: 1000,
		BudgetRemain:  5000,
		MemoryHints:   []string{"DB design decisions exist"},
	})

	require.NoError(t, err)
	assert.Equal(t, ActionMemoryQuery, decision.Action)
	assert.Equal(t, "database connection patterns", decision.Params.Query)
}

func TestController_Decide_Synthesize(t *testing.T) {
	client := &mockLLMClient{
		response: `{
			"action": "SYNTHESIZE",
			"params": {},
			"reasoning": "All subtasks complete, need to combine results"
		}`,
	}
	ctrl := NewController(client, DefaultConfig())

	decision, err := ctrl.Decide(context.Background(), State{
		Task:           "Summarize the analysis",
		ContextTokens:  2000,
		BudgetRemain:   3000,
		PartialResults: []string{"result1", "result2", "result3"},
	})

	require.NoError(t, err)
	assert.Equal(t, ActionSynthesize, decision.Action)
}

func TestController_Decide_MaxDepthReached(t *testing.T) {
	client := &mockLLMClient{
		// Should not be called
		response: `{"action": "DECOMPOSE", "params": {}, "reasoning": "Should not see this"}`,
	}
	ctrl := NewController(client, DefaultConfig())

	decision, err := ctrl.Decide(context.Background(), State{
		Task:           "Complex task",
		ContextTokens:  1000,
		BudgetRemain:   10000,
		RecursionDepth: 5,
		MaxDepth:       5,
	})

	require.NoError(t, err)
	assert.Equal(t, ActionDirect, decision.Action)
	assert.Contains(t, decision.Reasoning, "Maximum recursion depth")
}

func TestController_Decide_BudgetExhausted(t *testing.T) {
	client := &mockLLMClient{}
	ctrl := NewController(client, DefaultConfig())

	decision, err := ctrl.Decide(context.Background(), State{
		Task:          "Some task",
		ContextTokens: 1000,
		BudgetRemain:  0,
	})

	require.NoError(t, err)
	assert.Equal(t, ActionDirect, decision.Action)
	assert.Contains(t, decision.Reasoning, "Budget exhausted")
}

func TestParseDecision_Valid(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     Action
	}{
		{
			name:     "direct action",
			response: `{"action": "DIRECT", "params": {}, "reasoning": "test"}`,
			want:     ActionDirect,
		},
		{
			name:     "wrapped in markdown",
			response: "```json\n{\"action\": \"DECOMPOSE\", \"params\": {}, \"reasoning\": \"test\"}\n```",
			want:     ActionDecompose,
		},
		{
			name:     "with extra text",
			response: "Here's my decision:\n{\"action\": \"SUBCALL\", \"params\": {}, \"reasoning\": \"test\"}\nThat's it.",
			want:     ActionSubcall,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := parseDecision(tt.response)
			require.NoError(t, err)
			assert.Equal(t, tt.want, decision.Action)
		})
	}
}

func TestParseDecision_Invalid(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{"no json", "This is just text"},
		{"invalid json", "{action: DIRECT}"},
		{"unknown action", `{"action": "UNKNOWN", "params": {}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseDecision(tt.response)
			assert.Error(t, err)
		})
	}
}

func TestController_BuildPrompt(t *testing.T) {
	client := &mockLLMClient{}
	ctrl := NewController(client, DefaultConfig())

	state := State{
		Task:           "Analyze code",
		ContextTokens:  5000,
		BudgetRemain:   10000,
		RecursionDepth: 2,
		MaxDepth:       5,
		MemoryHints:    []string{"previous analysis exists"},
		PartialResults: []string{"part1", "part2"},
	}

	prompt := ctrl.buildPrompt(state)

	assert.Contains(t, prompt, "Analyze code")
	assert.Contains(t, prompt, "5000 tokens")
	assert.Contains(t, prompt, "10000 tokens")
	assert.Contains(t, prompt, "2/5")
	assert.Contains(t, prompt, "previous analysis exists")
	assert.Contains(t, prompt, "Partial results available: 2")
	assert.Contains(t, prompt, "DIRECT")
	assert.Contains(t, prompt, "DECOMPOSE")
}
