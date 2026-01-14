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

// TestSelectMode_ClassificationBased tests mode selection with task classification.
func TestSelectMode_ClassificationBased(t *testing.T) {
	// Create wrapper with classifier enabled and mock REPL manager
	svc := &Service{}
	w := NewWrapper(svc, DefaultWrapperConfig())

	// Mock REPL manager (just needs to be non-nil for mode selection)
	ctx := context.Background()
	replMgr, err := repl.NewManager(repl.Options{})
	require.NoError(t, err)
	require.NoError(t, replMgr.Start(ctx))
	defer replMgr.Stop()
	w.SetREPLManager(replMgr)

	// Context for testing
	contexts := []ContextSource{{Type: ContextTypeFile, Content: "test content"}}

	tests := []struct {
		name         string
		query        string
		tokens       int
		wantMode     ExecutionMode
		wantContains string // Substring expected in reason
	}{
		{
			name:         "computational task selects RLM at low tokens",
			query:        "How many times does 'error' appear in the text?",
			tokens:       1000,
			wantMode:     ModeRLM,
			wantContains: "computational",
		},
		{
			name:         "retrieval task selects Direct even at high tokens",
			query:        "What is the secret access code mentioned in the text?",
			tokens:       10000,
			wantMode:     ModeDirecte,
			wantContains: "retrieval",
		},
		{
			name:         "analytical task at small context uses Direct",
			query:        "Did Alice work with Bob?",
			tokens:       2000,
			wantMode:     ModeDirecte,
			wantContains: "analytical",
		},
		{
			name:         "analytical task at large context uses RLM",
			query:        "Did Alice collaborate with Bob on the project?",
			tokens:       10000,
			wantMode:     ModeRLM,
			wantContains: "analytical",
		},
		{
			name:         "unknown task falls back to size threshold",
			query:        "Tell me about the weather",
			tokens:       5000,
			wantMode:     ModeRLM,
			wantContains: "context size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Classify the query
			classification := w.classifier.Classify(tt.query, contexts)

			// Select mode
			mode, reason, _ := w.selectMode(context.Background(), tt.query, tt.tokens, contexts, &classification)

			assert.Equal(t, tt.wantMode, mode, "mode mismatch for: %s", tt.query)
			assert.Contains(t, reason, tt.wantContains, "reason should contain '%s', got: %s", tt.wantContains, reason)
		})
	}
}

// TestSelectMode_NoClassifier tests fallback when classifier is disabled.
func TestSelectMode_NoClassifier(t *testing.T) {
	svc := &Service{}
	cfg := DefaultWrapperConfig()
	cfg.DisableClassifier = true
	w := NewWrapper(svc, cfg)

	ctx := context.Background()
	replMgr, err := repl.NewManager(repl.Options{})
	require.NoError(t, err)
	require.NoError(t, replMgr.Start(ctx))
	defer replMgr.Stop()
	w.SetREPLManager(replMgr)

	assert.Nil(t, w.classifier, "classifier should be nil when disabled")

	contexts := []ContextSource{{Type: ContextTypeFile, Content: "test"}}

	// Without classifier, should fall back to size-based selection
	mode, reason, _ := w.selectMode(context.Background(), "How many errors?", 5000, contexts, nil)
	assert.Equal(t, ModeRLM, mode)
	assert.Contains(t, reason, "context size")

	mode, reason, _ = w.selectMode(context.Background(), "How many errors?", 1000, contexts, nil)
	assert.Equal(t, ModeDirecte, mode)
	assert.Contains(t, reason, "context size")
}

// TestPrepareContext_IncludesClassification tests that PrepareContext includes classification.
func TestPrepareContext_IncludesClassification(t *testing.T) {
	svc := &Service{}
	w := NewWrapper(svc, DefaultWrapperConfig())

	ctx := context.Background()
	replMgr, err := repl.NewManager(repl.Options{})
	require.NoError(t, err)
	require.NoError(t, replMgr.Start(ctx))
	defer replMgr.Stop()
	w.SetREPLManager(replMgr)

	// Create context large enough for RLM consideration
	largeContent := strings.Repeat("The customer ordered a apple. ", 500)
	contexts := []ContextSource{{Type: ContextTypeFile, Content: largeContent}}

	// Counting query should be classified as computational
	prepared, err := w.PrepareContext(ctx, "How many times does 'apple' appear?", contexts)
	require.NoError(t, err)

	// Should have classification
	require.NotNil(t, prepared.Classification, "should include classification")
	assert.Equal(t, TaskTypeComputational, prepared.Classification.Type)
	assert.NotEmpty(t, prepared.ModeReason)
}

// TestSelectMode_NoREPL tests that Direct is selected when REPL unavailable.
func TestSelectMode_NoREPL(t *testing.T) {
	svc := &Service{}
	w := NewWrapper(svc, DefaultWrapperConfig())
	// Don't set REPL manager

	contexts := []ContextSource{{Type: ContextTypeFile, Content: "test"}}
	classification := Classification{Type: TaskTypeComputational, Confidence: 0.9}

	mode, reason, _ := w.selectMode(context.Background(), "Count words", 10000, contexts, &classification)
	assert.Equal(t, ModeDirecte, mode)
	assert.Contains(t, reason, "REPL not available")
}

// TestGenerateRLMSystemPrompt_TaskTypeGuidance tests task-specific guidance in system prompt.
func TestGenerateRLMSystemPrompt_TaskTypeGuidance(t *testing.T) {
	svc := &Service{}
	w := NewWrapper(svc, DefaultWrapperConfig())

	loaded := &LoadedContext{
		Variables: map[string]VariableInfo{
			"context": {Description: "Test context", TokenEstimate: 1000},
		},
	}

	tests := []struct {
		name           string
		classification *Classification
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "computational task gets counting guidance",
			classification: &Classification{
				Type:       TaskTypeComputational,
				Confidence: 0.9,
			},
			wantContains: []string{
				"Task Type: COMPUTATIONAL",
				"counting/summing/aggregation",
				"Do NOT use llm_call()",
				"Counting (ONE iteration)",
				"re.findall",
			},
			wantNotContain: []string{
				"Task Type: RETRIEVAL",
				"Task Type: ANALYTICAL",
			},
		},
		{
			name: "retrieval task gets search guidance",
			classification: &Classification{
				Type:       TaskTypeRetrieval,
				Confidence: 0.85,
			},
			wantContains: []string{
				"Task Type: RETRIEVAL",
				"find/locate task",
				"grep()",
				"Finding Specific Value (ONE iteration)",
			},
			wantNotContain: []string{
				"Task Type: COMPUTATIONAL",
				"Task Type: ANALYTICAL",
			},
		},
		{
			name: "analytical task gets relationship guidance",
			classification: &Classification{
				Type:       TaskTypeAnalytical,
				Confidence: 0.8,
			},
			wantContains: []string{
				"Task Type: ANALYTICAL",
				"reasoning about relationships",
				"llm_call()",
				"Relationship Analysis",
			},
			wantNotContain: []string{
				"Task Type: COMPUTATIONAL",
				"Task Type: RETRIEVAL",
			},
		},
		{
			name:           "low confidence gets no task-specific guidance",
			classification: &Classification{Type: TaskTypeComputational, Confidence: 0.3},
			wantContains: []string{
				"Efficiency First",
				"Quick Count", // Default examples
			},
			wantNotContain: []string{
				"Task Type: COMPUTATIONAL",
			},
		},
		{
			name:           "nil classification gets default guidance",
			classification: nil,
			wantContains: []string{
				"Efficiency First",
				"Quick Count",
				"Quick Search",
			},
			wantNotContain: []string{
				"Task Type:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := w.generateRLMSystemPrompt(loaded, tt.classification, nil)

			for _, want := range tt.wantContains {
				assert.Contains(t, prompt, want, "prompt should contain %q", want)
			}

			for _, notWant := range tt.wantNotContain {
				assert.NotContains(t, prompt, notWant, "prompt should NOT contain %q", notWant)
			}
		})
	}
}

// TestGenerateRLMSystemPrompt_EfficiencyEmphasis tests that efficiency is emphasized.
func TestGenerateRLMSystemPrompt_EfficiencyEmphasis(t *testing.T) {
	svc := &Service{}
	w := NewWrapper(svc, DefaultWrapperConfig())

	loaded := &LoadedContext{
		Variables: map[string]VariableInfo{
			"data": {Description: "Data", TokenEstimate: 500},
		},
	}

	prompt := w.generateRLMSystemPrompt(loaded, nil, nil)

	// Check for efficiency emphasis
	assert.Contains(t, prompt, "Efficiency First")
	assert.Contains(t, prompt, "ONE iteration")
	assert.Contains(t, prompt, "FINAL() immediately")
	assert.Contains(t, prompt, "Use Python directly")

	// Check that examples are concise (should have FINAL in them)
	assert.Contains(t, prompt, "FINAL(")
}

// TestGenerateRLMSystemPrompt_WithSuggestion tests that REPL suggestions are included.
func TestGenerateRLMSystemPrompt_WithSuggestion(t *testing.T) {
	svc := &Service{}
	w := NewWrapper(svc, DefaultWrapperConfig())

	loaded := &LoadedContext{
		Variables: map[string]VariableInfo{
			"data": {Description: "Data", TokenEstimate: 500},
		},
	}

	suggestion := &REPLSuggestion{
		Pattern:    "count_occurrences",
		Approach:   "Use Counter to count items precisely",
		CodeHint:   "from collections import Counter",
		Confidence: 0.9,
		Variables:  []string{"error"},
	}

	prompt := w.generateRLMSystemPrompt(loaded, nil, suggestion)

	// Check that suggestion is included
	assert.Contains(t, prompt, "Computation Suggestion")
	assert.Contains(t, prompt, "count_occurrences")
	assert.Contains(t, prompt, "Use Counter to count items precisely")
	assert.Contains(t, prompt, "from collections import Counter")
}

// TestGenerateRLMSystemPrompt_LowConfidenceSuggestionOmitted tests low confidence suggestions are omitted.
func TestGenerateRLMSystemPrompt_LowConfidenceSuggestionOmitted(t *testing.T) {
	svc := &Service{}
	w := NewWrapper(svc, DefaultWrapperConfig())

	loaded := &LoadedContext{
		Variables: map[string]VariableInfo{
			"data": {Description: "Data", TokenEstimate: 500},
		},
	}

	suggestion := &REPLSuggestion{
		Pattern:    "weak_pattern",
		Approach:   "Maybe try this approach",
		Confidence: 0.5, // Below 0.7 threshold
	}

	prompt := w.generateRLMSystemPrompt(loaded, nil, suggestion)

	// Check that low-confidence suggestion is NOT included
	assert.NotContains(t, prompt, "Computation Suggestion")
	assert.NotContains(t, prompt, "weak_pattern")
}

// TestPrepareContextWithOptions_ModeOverride tests explicit mode override.
func TestPrepareContextWithOptions_ModeOverride(t *testing.T) {
	ctx := context.Background()

	// Create wrapper with REPL for RLM support
	svc := &Service{}
	w := NewWrapper(svc, DefaultWrapperConfig())

	replMgr, err := repl.NewManager(repl.Options{})
	require.NoError(t, err)
	require.NoError(t, replMgr.Start(ctx))
	defer replMgr.Stop()
	w.SetREPLManager(replMgr)

	// Small context that would normally use Direct mode
	contexts := []ContextSource{{
		Type:    ContextTypeFile,
		Content: "Small content",
	}}

	t.Run("auto mode uses automatic selection", func(t *testing.T) {
		prepared, err := w.PrepareContextWithOptions(ctx, "test", contexts, PrepareOptions{
			ModeOverride: ModeOverrideAuto,
		})
		require.NoError(t, err)
		// Small context should use Direct by default
		assert.Equal(t, ModeDirecte, prepared.Mode)
		assert.NotContains(t, prepared.ModeReason, "override")
	})

	t.Run("force RLM overrides automatic selection", func(t *testing.T) {
		prepared, err := w.PrepareContextWithOptions(ctx, "test", contexts, PrepareOptions{
			ModeOverride: ModeOverrideRLM,
		})
		require.NoError(t, err)
		assert.Equal(t, ModeRLM, prepared.Mode)
		assert.Contains(t, prepared.ModeReason, "forced RLM")
	})

	t.Run("force Direct overrides automatic selection", func(t *testing.T) {
		// Large context that would normally use RLM
		largeContent := strings.Repeat("Large content. ", 1000)
		largeContexts := []ContextSource{{Type: ContextTypeFile, Content: largeContent}}

		prepared, err := w.PrepareContextWithOptions(ctx, "test", largeContexts, PrepareOptions{
			ModeOverride: ModeOverrideDirect,
		})
		require.NoError(t, err)
		assert.Equal(t, ModeDirecte, prepared.Mode)
		assert.Contains(t, prepared.ModeReason, "forced Direct")
	})

	t.Run("empty override uses auto", func(t *testing.T) {
		prepared, err := w.PrepareContextWithOptions(ctx, "test", contexts, PrepareOptions{})
		require.NoError(t, err)
		// Should behave like auto mode
		assert.Equal(t, ModeDirecte, prepared.Mode)
	})
}

// TestPrepareContextWithOptions_RLMNotAvailable tests error when forcing RLM without REPL.
func TestPrepareContextWithOptions_RLMNotAvailable(t *testing.T) {
	ctx := context.Background()

	// Create wrapper WITHOUT REPL
	svc := &Service{}
	w := NewWrapper(svc, DefaultWrapperConfig())
	// Don't set REPL manager

	contexts := []ContextSource{{Type: ContextTypeFile, Content: "test"}}

	t.Run("force RLM without REPL returns error", func(t *testing.T) {
		_, err := w.PrepareContextWithOptions(ctx, "test", contexts, PrepareOptions{
			ModeOverride: ModeOverrideRLM,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context loader not available")
	})

	t.Run("auto mode gracefully falls back to Direct", func(t *testing.T) {
		prepared, err := w.PrepareContextWithOptions(ctx, "test", contexts, PrepareOptions{
			ModeOverride: ModeOverrideAuto,
		})
		require.NoError(t, err)
		assert.Equal(t, ModeDirecte, prepared.Mode)
	})
}

// TestPrepareContextWithOptions_SkipClassification tests skipping classification.
func TestPrepareContextWithOptions_SkipClassification(t *testing.T) {
	ctx := context.Background()

	svc := &Service{}
	w := NewWrapper(svc, DefaultWrapperConfig())

	contexts := []ContextSource{{Type: ContextTypeFile, Content: "test"}}

	t.Run("classification enabled by default", func(t *testing.T) {
		prepared, err := w.PrepareContextWithOptions(ctx, "How many times does 'x' appear?", contexts, PrepareOptions{})
		require.NoError(t, err)
		assert.NotNil(t, prepared.Classification)
		assert.Equal(t, TaskTypeComputational, prepared.Classification.Type)
	})

	t.Run("skip classification when requested", func(t *testing.T) {
		prepared, err := w.PrepareContextWithOptions(ctx, "How many times does 'x' appear?", contexts, PrepareOptions{
			SkipClassification: true,
		})
		require.NoError(t, err)
		assert.Nil(t, prepared.Classification)
	})
}

// TestModeOverrideConstants tests mode override constant values.
func TestModeOverrideConstants(t *testing.T) {
	assert.Equal(t, ModeOverride("auto"), ModeOverrideAuto)
	assert.Equal(t, ModeOverride("rlm"), ModeOverrideRLM)
	assert.Equal(t, ModeOverride("direct"), ModeOverrideDirect)
}
