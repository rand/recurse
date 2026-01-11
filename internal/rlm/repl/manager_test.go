package repl

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_StartStop(t *testing.T) {
	m, err := NewManager(Options{
		Sandbox: DefaultSandboxConfig(),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start the REPL
	err = m.Start(ctx)
	require.NoError(t, err)
	assert.True(t, m.Running())

	// Stop the REPL
	err = m.Stop()
	require.NoError(t, err)
	assert.False(t, m.Running())
}

func TestManager_Execute(t *testing.T) {
	m, err := NewManager(Options{
		Sandbox: DefaultSandboxConfig(),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, m.Start(ctx))
	defer m.Stop()

	// Simple expression
	result, err := m.Execute(ctx, "1 + 1")
	require.NoError(t, err)
	assert.Equal(t, "2", result.ReturnVal)
	assert.Empty(t, result.Error)

	// Print statement
	result, err = m.Execute(ctx, "print('hello')")
	require.NoError(t, err)
	assert.Equal(t, "hello\n", result.Output)
	assert.Empty(t, result.Error)

	// Multi-line code
	result, err = m.Execute(ctx, `
x = 10
y = 20
x + y
`)
	require.NoError(t, err)
	assert.Equal(t, "30", result.ReturnVal)

	// Syntax error
	result, err = m.Execute(ctx, "def foo(")
	require.NoError(t, err)
	assert.NotEmpty(t, result.Error)
	assert.Contains(t, result.Error, "SyntaxError")
}

func TestManager_Variables(t *testing.T) {
	m, err := NewManager(Options{
		Sandbox: DefaultSandboxConfig(),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, m.Start(ctx))
	defer m.Stop()

	// Set a variable
	err = m.SetVar(ctx, "my_content", "Hello, World!")
	require.NoError(t, err)

	// Get the variable
	result, err := m.GetVar(ctx, "my_content", 0, 0)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", result.Value)
	assert.Equal(t, 13, result.Length)
	assert.Equal(t, "str", result.Type)

	// Get sliced
	result, err = m.GetVar(ctx, "my_content", 0, 5)
	require.NoError(t, err)
	assert.Equal(t, "Hello", result.Value)

	// Use variable in execution
	execResult, err := m.Execute(ctx, "len(my_content)")
	require.NoError(t, err)
	assert.Equal(t, "13", execResult.ReturnVal)

	// List variables
	listResult, err := m.ListVars(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, listResult.Variables)

	found := false
	for _, v := range listResult.Variables {
		if v.Name == "my_content" {
			found = true
			assert.Equal(t, "str", v.Type)
			assert.Equal(t, 13, v.Length)
		}
	}
	assert.True(t, found, "my_content variable not found in list")
}

func TestManager_Status(t *testing.T) {
	m, err := NewManager(Options{
		Sandbox: DefaultSandboxConfig(),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Status before start
	status, err := m.Status(ctx)
	require.NoError(t, err)
	assert.False(t, status.Running)

	// Start and check status
	require.NoError(t, m.Start(ctx))
	defer m.Stop()

	status, err = m.Status(ctx)
	require.NoError(t, err)
	assert.True(t, status.Running)
	assert.GreaterOrEqual(t, status.Uptime, int64(0))
}

func TestManager_PreloadedModules(t *testing.T) {
	m, err := NewManager(Options{
		Sandbox: DefaultSandboxConfig(),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, m.Start(ctx))
	defer m.Stop()

	// Test that standard modules are available
	modules := []string{"re", "json", "ast", "pathlib", "itertools", "collections"}
	for _, mod := range modules {
		result, err := m.Execute(ctx, mod)
		require.NoError(t, err, "module %s should be available", mod)
		assert.Empty(t, result.Error, "module %s should not error", mod)
		assert.Contains(t, result.ReturnVal, "module", "module %s should return module repr", mod)
	}

	// Test using re module
	result, err := m.Execute(ctx, `re.findall(r'\d+', 'abc123def456')`)
	require.NoError(t, err)
	assert.Equal(t, "['123', '456']", result.ReturnVal)

	// Test using pathlib
	result, err = m.Execute(ctx, `str(Path('.').resolve())`)
	require.NoError(t, err)
	assert.NotEmpty(t, result.ReturnVal)
}
