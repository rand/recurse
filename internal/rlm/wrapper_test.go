package rlm

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rand/recurse/internal/rlm/repl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// TestExtractPythonCode tests the Python code extraction function.
func TestExtractPythonCode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple python block",
			input:    "Here's code:\n```python\nprint('hello')\n```",
			expected: "print('hello')",
		},
		{
			name:     "python block with multiple lines",
			input:    "```python\nx = 1\ny = 2\nprint(x + y)\n```",
			expected: "x = 1\ny = 2\nprint(x + y)",
		},
		{
			name:     "generic code block with python",
			input:    "```\ndef foo():\n    return 42\n```",
			expected: "def foo():\n    return 42",
		},
		{
			name:     "no code block",
			input:    "Just a plain response without any code.",
			expected: "",
		},
		{
			name:     "multiple code blocks takes first",
			input:    "```python\nfirst()\n```\nsome text\n```python\nsecond()\n```",
			expected: "first()",
		},
		{
			name:     "code block with FINAL",
			input:    "```python\nresult = 'done'\nFINAL(result)\n```",
			expected: "result = 'done'\nFINAL(result)",
		},
		{
			name:     "unclosed code block",
			input:    "```python\nprint('hello')",
			expected: "print('hello')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPythonCode(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestLooksLikePython tests Python detection heuristics.
func TestLooksLikePython(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected bool
	}{
		{"def function", "def hello():\n    pass", true},
		{"class definition", "class Foo:\n    pass", true},
		{"import statement", "import os", true},
		{"from import", "from typing import Any", true},
		{"FINAL call", "FINAL('result')", true},
		{"llm_call", "result = llm_call('prompt', context)", true},
		{"peek function", "preview = peek(ctx, 0, 100)", true},
		{"grep function", "matches = grep(ctx, 'pattern')", true},
		{"assignment", "x = 42", true},
		{"comparison", "if x == 42:", true},
		{"plain english", "This is just a regular sentence.", false},
		{"json data", `{"key": "value"}`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := looksLikePython(tt.code)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestLooksLikeFinalAnswer tests final answer detection.
func TestLooksLikeFinalAnswer(t *testing.T) {
	tests := []struct {
		name     string
		response string
		expected bool
	}{
		{
			name:     "contains code block",
			response: "Here's my answer:\n```python\nprint('test')\n```",
			expected: false,
		},
		{
			name:     "conclusion phrase",
			response: "In conclusion, the function returns 42.",
			expected: true,
		},
		{
			name:     "answer is phrase",
			response: "The answer is 42.",
			expected: true,
		},
		{
			name:     "based on analysis",
			response: "Based on my analysis, the code has 3 functions.",
			expected: true,
		},
		{
			name:     "long response without phrases",
			response: strings.Repeat("Some text without special phrases. ", 50),
			expected: false,
		},
		{
			name:     "short plain text",
			response: "Hello world",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := looksLikeFinalAnswer(tt.response)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestDefaultRLMConfig tests default configuration values.
func TestDefaultRLMConfig(t *testing.T) {
	cfg := DefaultRLMConfig()
	assert.Equal(t, 10, cfg.MaxIterations)
	assert.Equal(t, 4096, cfg.MaxTokensPerCall)
	assert.Equal(t, 5*time.Minute, cfg.Timeout)
}

// TestDefaultWrapperConfig tests default wrapper configuration.
func TestDefaultWrapperConfig(t *testing.T) {
	cfg := DefaultWrapperConfig()
	assert.Equal(t, 4000, cfg.MinContextTokensForRLM)
	assert.Equal(t, 32000, cfg.MaxDirectContextTokens)
}

// TestExecuteRLM_NotRLMMode tests error when not in RLM mode.
func TestExecuteRLM_NotRLMMode(t *testing.T) {
	w := &Wrapper{}
	prepared := &PreparedPrompt{Mode: ModeDirecte}

	_, err := w.ExecuteRLM(context.Background(), prepared)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not in RLM mode")
}

// TestExecuteRLM_NoREPL tests error when REPL not configured.
func TestExecuteRLM_NoREPL(t *testing.T) {
	w := &Wrapper{}
	prepared := &PreparedPrompt{Mode: ModeRLM}

	_, err := w.ExecuteRLM(context.Background(), prepared)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "REPL manager not configured")
}

// TestExecuteRLM_NoClient tests error when LLM client not configured.
func TestExecuteRLM_NoClient(t *testing.T) {
	replMgr, err := repl.NewManager(repl.Options{})
	require.NoError(t, err)

	w := &Wrapper{replMgr: replMgr}
	prepared := &PreparedPrompt{Mode: ModeRLM}

	_, err = w.ExecuteRLM(context.Background(), prepared)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM client not configured")
}

// wrapperMockLLMClient is a test double for RLM wrapper tests.
type wrapperMockLLMClient struct {
	responses []string
	callIndex int
	calls     []string
}

func (m *wrapperMockLLMClient) Complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	m.calls = append(m.calls, prompt)
	if m.callIndex < len(m.responses) {
		resp := m.responses[m.callIndex]
		m.callIndex++
		return resp, nil
	}
	return "", nil
}

// TestExecuteRLM_SingleIteration tests successful single-iteration execution.
func TestExecuteRLM_SingleIteration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create REPL manager
	replMgr, err := repl.NewManager(repl.Options{})
	require.NoError(t, err)
	require.NoError(t, replMgr.Start(ctx))
	defer replMgr.Stop()

	// Create mock client that returns code with FINAL
	mockClient := &wrapperMockLLMClient{
		responses: []string{
			"```python\nresult = 2 + 2\nFINAL(str(result))\n```",
		},
	}

	w := &Wrapper{
		replMgr: replMgr,
		client:  mockClient,
	}

	prepared := &PreparedPrompt{
		Mode:         ModeRLM,
		SystemPrompt: "You are an RLM assistant.",
		FinalPrompt:  "Calculate 2 + 2",
	}

	result, err := w.ExecuteRLMWithConfig(ctx, prepared, RLMConfig{
		MaxIterations:    5,
		MaxTokensPerCall: 1024,
		Timeout:          10 * time.Second,
	})

	require.NoError(t, err)
	assert.Equal(t, 1, result.Iterations)
	assert.Equal(t, "4", result.FinalOutput)
	assert.Empty(t, result.Error)
}

// TestExecuteRLM_MultipleIterations tests execution with multiple rounds.
func TestExecuteRLM_MultipleIterations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	replMgr, err := repl.NewManager(repl.Options{})
	require.NoError(t, err)
	require.NoError(t, replMgr.Start(ctx))
	defer replMgr.Stop()

	// First response explores, second provides answer
	mockClient := &wrapperMockLLMClient{
		responses: []string{
			"```python\nx = 10\nprint(f'x is {x}')\n```",
			"```python\nFINAL('x is 10')\n```",
		},
	}

	w := &Wrapper{
		replMgr: replMgr,
		client:  mockClient,
	}

	prepared := &PreparedPrompt{
		Mode:         ModeRLM,
		SystemPrompt: "You are an RLM assistant.",
		FinalPrompt:  "What is x?",
	}

	result, err := w.ExecuteRLMWithConfig(ctx, prepared, RLMConfig{
		MaxIterations:    5,
		MaxTokensPerCall: 1024,
		Timeout:          10 * time.Second,
	})

	require.NoError(t, err)
	assert.Equal(t, 2, result.Iterations)
	assert.Equal(t, "x is 10", result.FinalOutput)
	assert.Empty(t, result.Error)
}

// TestExecuteRLM_MaxIterationsReached tests iteration limit.
func TestExecuteRLM_MaxIterationsReached(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	replMgr, err := repl.NewManager(repl.Options{})
	require.NoError(t, err)
	require.NoError(t, replMgr.Start(ctx))
	defer replMgr.Stop()

	// Never calls FINAL
	mockClient := &wrapperMockLLMClient{
		responses: []string{
			"```python\nprint('iteration 1')\n```",
			"```python\nprint('iteration 2')\n```",
			"```python\nprint('iteration 3')\n```",
		},
	}

	w := &Wrapper{
		replMgr: replMgr,
		client:  mockClient,
	}

	prepared := &PreparedPrompt{
		Mode:         ModeRLM,
		SystemPrompt: "You are an RLM assistant.",
		FinalPrompt:  "Keep printing",
	}

	result, err := w.ExecuteRLMWithConfig(ctx, prepared, RLMConfig{
		MaxIterations:    3,
		MaxTokensPerCall: 1024,
		Timeout:          10 * time.Second,
	})

	require.NoError(t, err)
	assert.Equal(t, 3, result.Iterations)
	assert.Empty(t, result.FinalOutput)
	assert.Contains(t, result.Error, "max iterations")
}

// TestFormatConversation tests conversation formatting.
func TestFormatConversation(t *testing.T) {
	w := &Wrapper{}

	messages := []conversationMessage{
		{Role: "system", Content: "System instructions"},
		{Role: "user", Content: "User question"},
		{Role: "assistant", Content: "Assistant response"},
	}

	result := w.formatConversation(messages)

	assert.Contains(t, result, "<system>")
	assert.Contains(t, result, "System instructions")
	assert.Contains(t, result, "</system>")
	assert.Contains(t, result, "User: User question")
	assert.Contains(t, result, "Assistant: Assistant response")
	assert.True(t, strings.HasSuffix(result, "Assistant: "))
}

// TestBuildExecutionFeedback tests feedback generation.
func TestBuildExecutionFeedback(t *testing.T) {
	w := &Wrapper{}

	t.Run("success with output", func(t *testing.T) {
		result := &repl.ExecuteResult{
			Output:    "Hello, world!\n",
			ReturnVal: "42",
		}
		feedback := w.buildExecutionFeedback(result)

		assert.Contains(t, feedback, "Code executed")
		assert.Contains(t, feedback, "Hello, world!")
		assert.Contains(t, feedback, "42")
		assert.Contains(t, feedback, "FINAL(response)")
	})

	t.Run("error", func(t *testing.T) {
		result := &repl.ExecuteResult{
			Error: "NameError: name 'foo' is not defined",
		}
		feedback := w.buildExecutionFeedback(result)

		assert.Contains(t, feedback, "Error")
		assert.Contains(t, feedback, "NameError")
		assert.Contains(t, feedback, "fix the error")
	})

	t.Run("none return value", func(t *testing.T) {
		result := &repl.ExecuteResult{
			ReturnVal: "None",
		}
		feedback := w.buildExecutionFeedback(result)

		assert.NotContains(t, feedback, "Return value: None")
	})
}

// =============================================================================
// Property-Based Tests
// =============================================================================

// TestProperty_ExtractPythonCodeNeverPanics verifies extraction handles any input.
func TestProperty_ExtractPythonCodeNeverPanics(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		input := rapid.String().Draw(t, "input")
		// Should never panic
		result := extractPythonCode(input)
		// Result is either empty or contains something
		assert.True(t, result == "" || len(result) > 0)
	})
}

// TestProperty_ExtractPythonCodePreservesValidCode verifies valid code blocks are extracted.
func TestProperty_ExtractPythonCodePreservesValidCode(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate valid Python code
		code := rapid.SampledFrom([]string{
			"print('hello')",
			"x = 42",
			"def foo():\n    return 1",
			"FINAL('done')",
			"result = llm_call('prompt', 'context')",
		}).Draw(t, "code")

		// Wrap in code block
		input := "```python\n" + code + "\n```"

		result := extractPythonCode(input)
		assert.Equal(t, code, result, "Should extract the exact code from block")
	})
}

// TestProperty_LooksLikePythonIsDeterministic verifies consistent detection.
func TestProperty_LooksLikePythonIsDeterministic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		code := rapid.String().Draw(t, "code")

		result1 := looksLikePython(code)
		result2 := looksLikePython(code)

		assert.Equal(t, result1, result2, "Should return same result for same input")
	})
}

// TestProperty_LooksLikeFinalAnswerCodeBlocksAlwaysFalse verifies code blocks aren't final answers.
func TestProperty_LooksLikeFinalAnswerCodeBlocksAlwaysFalse(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		content := rapid.String().Draw(t, "content")
		// Any response containing ``` should not be a final answer
		response := "```\n" + content + "\n```"

		result := looksLikeFinalAnswer(response)
		assert.False(t, result, "Code blocks should never be final answers")
	})
}

// TestProperty_ConversationFormattingPreservesContent verifies no content loss.
func TestProperty_ConversationFormattingPreservesContent(t *testing.T) {
	w := &Wrapper{}

	rapid.Check(t, func(t *rapid.T) {
		systemMsg := rapid.String().Filter(func(s string) bool {
			return !strings.Contains(s, "<") && !strings.Contains(s, ">")
		}).Draw(t, "system")
		userMsg := rapid.String().Draw(t, "user")

		messages := []conversationMessage{
			{Role: "system", Content: systemMsg},
			{Role: "user", Content: userMsg},
		}

		result := w.formatConversation(messages)

		if systemMsg != "" {
			assert.Contains(t, result, systemMsg, "System content should be preserved")
		}
		if userMsg != "" {
			assert.Contains(t, result, userMsg, "User content should be preserved")
		}
	})
}

// TestNewWrapper tests wrapper creation.
func TestNewWrapper(t *testing.T) {
	client := &mockLLMClient{}
	svc, err := NewService(client, DefaultServiceConfig())
	require.NoError(t, err)
	defer svc.Stop()

	t.Run("with defaults", func(t *testing.T) {
		w := NewWrapper(svc, WrapperConfig{})
		assert.Equal(t, 4000, w.minContextTokensForRLM)
		assert.Equal(t, 32000, w.maxDirectContextTokens)
	})

	t.Run("with custom config", func(t *testing.T) {
		w := NewWrapper(svc, WrapperConfig{
			MinContextTokensForRLM: 8000,
			MaxDirectContextTokens: 64000,
		})
		assert.Equal(t, 8000, w.minContextTokensForRLM)
		assert.Equal(t, 64000, w.maxDirectContextTokens)
	})
}

// TestWrapper_SetREPLManager tests REPL manager configuration.
func TestWrapper_SetREPLManager(t *testing.T) {
	w := &Wrapper{}
	assert.Nil(t, w.replMgr)
	assert.Nil(t, w.contextLoader)

	replMgr, err := repl.NewManager(repl.Options{})
	require.NoError(t, err)

	w.SetREPLManager(replMgr)
	assert.Equal(t, replMgr, w.replMgr)
	assert.NotNil(t, w.contextLoader)
}

// TestWrapper_SetLLMClient tests LLM client configuration.
func TestWrapper_SetLLMClient(t *testing.T) {
	w := &Wrapper{}
	assert.Nil(t, w.client)

	client := &mockLLMClient{}
	w.SetLLMClient(client)
	assert.Equal(t, client, w.client)
}

// TestPrepareContext_DirectMode tests direct mode selection.
func TestPrepareContext_DirectMode(t *testing.T) {
	client := &mockLLMClient{}
	svc, err := NewService(client, DefaultServiceConfig())
	require.NoError(t, err)
	defer svc.Stop()

	w := NewWrapper(svc, DefaultWrapperConfig())

	ctx := context.Background()
	prepared, err := w.PrepareContext(ctx, "Simple prompt", nil)

	require.NoError(t, err)
	assert.Equal(t, ModeDirecte, prepared.Mode)
	assert.Contains(t, prepared.FinalPrompt, "Simple prompt")
}

// TestPrepareContext_RLMMode tests RLM mode selection with large context.
func TestPrepareContext_RLMMode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := &mockLLMClient{}
	svc, err := NewService(client, DefaultServiceConfig())
	require.NoError(t, err)
	defer svc.Stop()

	replMgr, err := repl.NewManager(repl.Options{})
	require.NoError(t, err)
	require.NoError(t, replMgr.Start(ctx))
	defer replMgr.Stop()

	w := NewWrapper(svc, WrapperConfig{
		MinContextTokensForRLM: 100, // Low threshold for testing
		MaxDirectContextTokens: 32000,
	})
	w.SetREPLManager(replMgr)

	// Create large context to trigger RLM mode
	largeContent := strings.Repeat("This is a large context for testing. ", 100)
	contexts := []ContextSource{
		{Type: ContextTypeFile, Content: largeContent},
	}

	prepared, err := w.PrepareContext(ctx, "Analyze this code", contexts)

	require.NoError(t, err)
	assert.Equal(t, ModeRLM, prepared.Mode)
	assert.NotEmpty(t, prepared.SystemPrompt)
	assert.NotNil(t, prepared.LoadedContext)
}

// TestRLMExecutionResult_Fields tests result struct fields.
func TestRLMExecutionResult_Fields(t *testing.T) {
	result := RLMExecutionResult{
		FinalOutput:   "test output",
		FinalType:     "json",
		FinalMetadata: map[string]string{"key": "value"},
		Iterations:    3,
		TotalTokens:   1000,
		TotalCost:     0.01,
		StartTime:     time.Now(),
		Duration:      5 * time.Second,
		Error:         "",
		Note:          "test note",
	}

	assert.Equal(t, "test output", result.FinalOutput)
	assert.Equal(t, "json", result.FinalType)
	assert.Equal(t, "value", result.FinalMetadata["key"])
	assert.Equal(t, 3, result.Iterations)
	assert.Equal(t, 1000, result.TotalTokens)
	assert.Equal(t, 0.01, result.TotalCost)
	assert.Equal(t, 5*time.Second, result.Duration)
	assert.Empty(t, result.Error)
	assert.Equal(t, "test note", result.Note)
}
