package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/rand/recurse/internal/rlm/repl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupREPL(t *testing.T) (*repl.Manager, func()) {
	t.Helper()

	m, err := repl.NewManager(repl.Options{
		Sandbox: repl.DefaultSandboxConfig(),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	require.NoError(t, m.Start(ctx))

	return m, func() {
		cancel()
		m.Stop()
	}
}

func makeToolCall(t *testing.T, input any) fantasy.ToolCall {
	t.Helper()
	inputJSON, err := json.Marshal(input)
	require.NoError(t, err)
	return fantasy.ToolCall{
		ID:    "test-call",
		Input: string(inputJSON),
	}
}

func TestRLMExternalizeTool(t *testing.T) {
	mgr, cleanup := setupREPL(t)
	defer cleanup()

	tool := NewRLMExternalizeTool(mgr)
	ctx := context.Background()

	// Test basic externalization
	resp, err := tool.Run(ctx, makeToolCall(t, map[string]any{
		"name":    "test_content",
		"content": "Hello, World!",
	}))
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "13 characters")
	assert.Contains(t, resp.Content, "test_content")

	// Verify variable is accessible
	result, err := mgr.GetVar(ctx, "test_content", 0, 0)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", result.Value)
}

func TestRLMPeekTool(t *testing.T) {
	mgr, cleanup := setupREPL(t)
	defer cleanup()

	ctx := context.Background()

	// Store some content first
	err := mgr.SetVar(ctx, "long_content", "0123456789abcdefghij")
	require.NoError(t, err)

	tool := NewRLMPeekTool(mgr)

	// Test peeking with range
	resp, err := tool.Run(ctx, makeToolCall(t, map[string]any{
		"name":  "long_content",
		"start": 0,
		"end":   5,
	}))
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "01234")

	// Test peeking from middle
	resp, err = tool.Run(ctx, makeToolCall(t, map[string]any{
		"name":  "long_content",
		"start": 10,
		"end":   15,
	}))
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "abcde")
}

func TestRLMExecuteTool(t *testing.T) {
	mgr, cleanup := setupREPL(t)
	defer cleanup()

	ctx := context.Background()

	tool := NewRLMExecuteTool(mgr)

	// Test simple expression
	resp, err := tool.Run(ctx, makeToolCall(t, map[string]any{
		"code": "1 + 1",
	}))
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "2")

	// Test with print
	resp, err = tool.Run(ctx, makeToolCall(t, map[string]any{
		"code": "print('hello')",
	}))
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "hello")

	// Test variable persistence
	_, err = tool.Run(ctx, makeToolCall(t, map[string]any{
		"code": "x = 42",
	}))
	require.NoError(t, err)

	resp, err = tool.Run(ctx, makeToolCall(t, map[string]any{
		"code": "x * 2",
	}))
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "84")

	// Test with externalized content
	err = mgr.SetVar(ctx, "data", "line1\nline2\nline3")
	require.NoError(t, err)

	resp, err = tool.Run(ctx, makeToolCall(t, map[string]any{
		"code": "len(data.split('\\n'))",
	}))
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "3")
}

func TestRLMStatusTool(t *testing.T) {
	mgr, cleanup := setupREPL(t)
	defer cleanup()

	ctx := context.Background()

	tool := NewRLMStatusTool(mgr)

	// Test status
	resp, err := tool.Run(ctx, makeToolCall(t, map[string]any{}))
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "Running")
	assert.Contains(t, resp.Content, "Memory")

	// Add a variable and check it appears
	err = mgr.SetVar(ctx, "status_test", "test value")
	require.NoError(t, err)

	resp, err = tool.Run(ctx, makeToolCall(t, map[string]any{}))
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "status_test")
}

func TestRLMToolsIntegration(t *testing.T) {
	mgr, cleanup := setupREPL(t)
	defer cleanup()

	ctx := context.Background()

	externalize := NewRLMExternalizeTool(mgr)
	peek := NewRLMPeekTool(mgr)
	execute := NewRLMExecuteTool(mgr)

	// Workflow: externalize -> peek -> execute
	sampleCode := `def greet(name):
    return f"Hello, {name}!"

def farewell(name):
    return f"Goodbye, {name}!"
`

	// Externalize
	_, err := externalize.Run(ctx, makeToolCall(t, map[string]any{
		"name":    "code",
		"content": sampleCode,
	}))
	require.NoError(t, err)

	// Peek
	resp, err := peek.Run(ctx, makeToolCall(t, map[string]any{
		"name":  "code",
		"start": 0,
		"end":   50,
	}))
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "def greet")

	// Execute to find functions
	resp, err = execute.Run(ctx, makeToolCall(t, map[string]any{
		"code": "re.findall(r'def (\\w+)', code)",
	}))
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "greet")
	assert.Contains(t, resp.Content, "farewell")
}

// mockSubcallProvider implements SubcallProvider for testing.
type mockSubcallProvider struct {
	response   string
	tokensUsed int
	err        error
}

func (m *mockSubcallProvider) Subcall(ctx context.Context, prompt, snippet string, maxTokens int) (string, int, error) {
	if m.err != nil {
		return "", 0, m.err
	}
	return m.response, m.tokensUsed, nil
}

func TestRLMSubcallTool_Basic(t *testing.T) {
	provider := &mockSubcallProvider{
		response:   "This function calculates the sum of prices.",
		tokensUsed: 150,
	}
	tool := NewRLMSubcallTool(provider)
	ctx := context.Background()

	resp, err := tool.Run(ctx, makeToolCall(t, map[string]any{
		"prompt":  "Explain this function",
		"snippet": "func sum(a, b int) int { return a + b }",
	}))
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "calculates the sum")
}

func TestRLMSubcallTool_MissingPrompt(t *testing.T) {
	provider := &mockSubcallProvider{}
	tool := NewRLMSubcallTool(provider)
	ctx := context.Background()

	resp, err := tool.Run(ctx, makeToolCall(t, map[string]any{
		"snippet": "some code",
	}))
	require.NoError(t, err)
	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "prompt is required")
}

func TestRLMSubcallTool_MissingSnippet(t *testing.T) {
	provider := &mockSubcallProvider{}
	tool := NewRLMSubcallTool(provider)
	ctx := context.Background()

	resp, err := tool.Run(ctx, makeToolCall(t, map[string]any{
		"prompt": "Explain this",
	}))
	require.NoError(t, err)
	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "snippet is required")
}

func TestRLMSubcallTool_NilProvider(t *testing.T) {
	tool := NewRLMSubcallTool(nil)
	ctx := context.Background()

	resp, err := tool.Run(ctx, makeToolCall(t, map[string]any{
		"prompt":  "Explain this",
		"snippet": "code",
	}))
	require.NoError(t, err)
	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "not configured")
}

func TestRLMSubcallTool_WithMaxTokens(t *testing.T) {
	provider := &mockSubcallProvider{
		response:   "Brief response",
		tokensUsed: 50,
	}
	tool := NewRLMSubcallTool(provider)
	ctx := context.Background()

	resp, err := tool.Run(ctx, makeToolCall(t, map[string]any{
		"prompt":     "Explain briefly",
		"snippet":    "func foo() {}",
		"max_tokens": 100,
	}))
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "Brief response")
}
