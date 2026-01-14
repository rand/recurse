package repl

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// PluginManager Tests
// =============================================================================

func TestNewPluginManager(t *testing.T) {
	pm := NewPluginManager()
	require.NotNil(t, pm)
	assert.Empty(t, pm.ListPlugins())
	assert.Empty(t, pm.ListFunctions())
}

func TestPluginManager_Register(t *testing.T) {
	pm := NewPluginManager()
	ctx := context.Background()

	plugin := &mockPlugin{
		name: "test",
		functions: map[string]REPLFunction{
			"hello": {
				Name:        "hello",
				Description: "Says hello",
				Handler: func(ctx context.Context, args ...any) (any, error) {
					return "Hello!", nil
				},
			},
		},
	}

	err := pm.Register(ctx, plugin)
	require.NoError(t, err)

	assert.Contains(t, pm.ListPlugins(), "test")
	assert.Contains(t, pm.ListFunctions(), "test_hello")
}

func TestPluginManager_Register_Duplicate(t *testing.T) {
	pm := NewPluginManager()
	ctx := context.Background()

	plugin := &mockPlugin{name: "test"}

	err := pm.Register(ctx, plugin)
	require.NoError(t, err)

	// Try to register again
	err = pm.Register(ctx, plugin)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestPluginManager_Register_OnLoadError(t *testing.T) {
	pm := NewPluginManager()
	ctx := context.Background()

	plugin := &mockPlugin{
		name:        "failing",
		onLoadError: assert.AnError,
	}

	err := pm.Register(ctx, plugin)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "OnLoad")
}

func TestPluginManager_Unregister(t *testing.T) {
	pm := NewPluginManager()
	ctx := context.Background()

	plugin := &mockPlugin{
		name: "test",
		functions: map[string]REPLFunction{
			"hello": {Name: "hello", Handler: func(ctx context.Context, args ...any) (any, error) { return nil, nil }},
		},
	}

	err := pm.Register(ctx, plugin)
	require.NoError(t, err)

	err = pm.Unregister("test")
	require.NoError(t, err)

	assert.NotContains(t, pm.ListPlugins(), "test")
	assert.NotContains(t, pm.ListFunctions(), "test_hello")
	assert.True(t, plugin.unloaded)
}

func TestPluginManager_Unregister_NotFound(t *testing.T) {
	pm := NewPluginManager()

	err := pm.Unregister("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestPluginManager_Call(t *testing.T) {
	pm := NewPluginManager()
	ctx := context.Background()

	plugin := &mockPlugin{
		name: "math",
		functions: map[string]REPLFunction{
			"add": {
				Name: "add",
				Handler: func(ctx context.Context, args ...any) (any, error) {
					a, _ := args[0].(float64)
					b, _ := args[1].(float64)
					return a + b, nil
				},
			},
		},
	}

	err := pm.Register(ctx, plugin)
	require.NoError(t, err)

	result, err := pm.Call(ctx, "math_add", 2.0, 3.0)
	require.NoError(t, err)
	assert.Equal(t, 5.0, result)
}

func TestPluginManager_Call_NotFound(t *testing.T) {
	pm := NewPluginManager()
	ctx := context.Background()

	_, err := pm.Call(ctx, "nonexistent", "arg")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestPluginManager_HasFunction(t *testing.T) {
	pm := NewPluginManager()
	ctx := context.Background()

	plugin := &mockPlugin{
		name: "test",
		functions: map[string]REPLFunction{
			"func1": {Name: "func1", Handler: func(ctx context.Context, args ...any) (any, error) { return nil, nil }},
		},
	}

	pm.Register(ctx, plugin)

	assert.True(t, pm.HasFunction("test_func1"))
	assert.False(t, pm.HasFunction("test_func2"))
	assert.False(t, pm.HasFunction("other_func1"))
}

func TestPluginManager_GetPlugin(t *testing.T) {
	pm := NewPluginManager()
	ctx := context.Background()

	plugin := &mockPlugin{name: "test"}
	pm.Register(ctx, plugin)

	got, exists := pm.GetPlugin("test")
	assert.True(t, exists)
	assert.Equal(t, plugin, got)

	_, exists = pm.GetPlugin("nonexistent")
	assert.False(t, exists)
}

func TestPluginManager_GetFunctionInfo(t *testing.T) {
	pm := NewPluginManager()
	ctx := context.Background()

	plugin := &mockPlugin{
		name: "test",
		functions: map[string]REPLFunction{
			"hello": {
				Name:        "hello",
				Description: "Says hello",
				Parameters: []FunctionParameter{
					{Name: "name", Type: "string", Description: "Name to greet", Required: true},
				},
			},
		},
	}

	pm.Register(ctx, plugin)

	info, exists := pm.GetFunctionInfo("test_hello")
	require.True(t, exists)
	assert.Equal(t, "test_hello", info.Name)
	assert.Equal(t, "test", info.Plugin)
	assert.Equal(t, "Says hello", info.Description)
	assert.Len(t, info.Parameters, 1)
}

func TestPluginManager_GenerateManifest(t *testing.T) {
	pm := NewPluginManager()
	ctx := context.Background()

	plugin := &mockPlugin{
		name:        "test",
		description: "Test plugin",
		functions: map[string]REPLFunction{
			"func1": {Name: "func1", Description: "Function 1"},
		},
	}

	pm.Register(ctx, plugin)

	manifest := pm.GenerateManifest()
	assert.Contains(t, manifest, `"name": "test"`)
	assert.Contains(t, manifest, `"description": "Test plugin"`)
	assert.Contains(t, manifest, `"test_func1"`)

	// Verify it's valid JSON
	var parsed map[string]interface{}
	err := json.Unmarshal([]byte(manifest), &parsed)
	assert.NoError(t, err)
}

// =============================================================================
// CodeAnalysisPlugin Tests
// =============================================================================

func TestCodeAnalysisPlugin_Name(t *testing.T) {
	p := NewCodeAnalysisPlugin()
	assert.Equal(t, "code_analysis", p.Name())
}

func TestCodeAnalysisPlugin_Description(t *testing.T) {
	p := NewCodeAnalysisPlugin()
	assert.NotEmpty(t, p.Description())
}

func TestCodeAnalysisPlugin_Functions(t *testing.T) {
	p := NewCodeAnalysisPlugin()
	funcs := p.Functions()

	assert.Contains(t, funcs, "count_lines")
	assert.Contains(t, funcs, "extract_imports")
	assert.Contains(t, funcs, "find_functions")
}

func TestCodeAnalysisPlugin_CountLines(t *testing.T) {
	p := NewCodeAnalysisPlugin()
	ctx := context.Background()

	code := `package main

// This is a comment
func main() {
    // Another comment
    fmt.Println("Hello")
}`

	fn := p.Functions()["count_lines"]
	result, err := fn.Handler(ctx, code)
	require.NoError(t, err)

	counts := result.(LineCount)
	assert.Equal(t, 7, counts.Total)
	assert.Equal(t, 4, counts.Code)
	assert.Equal(t, 2, counts.Comment)
	assert.Equal(t, 1, counts.Blank)
}

func TestCodeAnalysisPlugin_CountLines_NoArgs(t *testing.T) {
	p := NewCodeAnalysisPlugin()
	ctx := context.Background()

	fn := p.Functions()["count_lines"]
	_, err := fn.Handler(ctx)
	assert.Error(t, err)
}

func TestCodeAnalysisPlugin_ExtractImports_Go(t *testing.T) {
	p := NewCodeAnalysisPlugin()
	ctx := context.Background()

	code := `package main

import "fmt"
import (
    "os"
    "path/filepath"
)

func main() {}
`

	fn := p.Functions()["extract_imports"]
	result, err := fn.Handler(ctx, code, "go")
	require.NoError(t, err)

	imports := result.([]string)
	assert.Contains(t, imports, `import "fmt"`)
}

func TestCodeAnalysisPlugin_ExtractImports_Python(t *testing.T) {
	p := NewCodeAnalysisPlugin()
	ctx := context.Background()

	code := `import os
from pathlib import Path
import sys

def main():
    pass
`

	fn := p.Functions()["extract_imports"]
	result, err := fn.Handler(ctx, code, "python")
	require.NoError(t, err)

	imports := result.([]string)
	assert.Len(t, imports, 3)
	assert.Contains(t, imports, "import os")
	assert.Contains(t, imports, "from pathlib import Path")
}

func TestCodeAnalysisPlugin_FindFunctions_Go(t *testing.T) {
	p := NewCodeAnalysisPlugin()
	ctx := context.Background()

	code := `package main

func main() {
    helper()
}

func helper() string {
    return "help"
}

func (s *Server) Handle() error {
    return nil
}
`

	fn := p.Functions()["find_functions"]
	result, err := fn.Handler(ctx, code, "go")
	require.NoError(t, err)

	funcs := result.([]FunctionDef)
	assert.Len(t, funcs, 3)

	names := make([]string, len(funcs))
	for i, f := range funcs {
		names[i] = f.Name
	}
	assert.Contains(t, names, "main")
	assert.Contains(t, names, "helper")
	assert.Contains(t, names, "Handle")
}

func TestCodeAnalysisPlugin_FindFunctions_Python(t *testing.T) {
	p := NewCodeAnalysisPlugin()
	ctx := context.Background()

	code := `def main():
    pass

async def fetch_data():
    pass

def helper():
    return True
`

	fn := p.Functions()["find_functions"]
	result, err := fn.Handler(ctx, code, "python")
	require.NoError(t, err)

	funcs := result.([]FunctionDef)
	assert.Len(t, funcs, 3)

	names := make([]string, len(funcs))
	for i, f := range funcs {
		names[i] = f.Name
	}
	assert.Contains(t, names, "main")
	assert.Contains(t, names, "fetch_data")
	assert.Contains(t, names, "helper")
}

func TestCodeAnalysisPlugin_FindFunctions_JavaScript(t *testing.T) {
	p := NewCodeAnalysisPlugin()
	ctx := context.Background()

	code := `function main() {
    console.log("Hello");
}

const helper = () => {
    return true;
};
`

	fn := p.Functions()["find_functions"]
	result, err := fn.Handler(ctx, code, "javascript")
	require.NoError(t, err)

	funcs := result.([]FunctionDef)
	assert.GreaterOrEqual(t, len(funcs), 1)
}

func TestCodeAnalysisPlugin_Registration(t *testing.T) {
	pm := NewPluginManager()
	ctx := context.Background()

	p := NewCodeAnalysisPlugin()
	err := pm.Register(ctx, p)
	require.NoError(t, err)

	assert.Contains(t, pm.ListPlugins(), "code_analysis")
	assert.True(t, pm.HasFunction("code_analysis_count_lines"))
	assert.True(t, pm.HasFunction("code_analysis_extract_imports"))
	assert.True(t, pm.HasFunction("code_analysis_find_functions"))
}

// =============================================================================
// Helper Functions Tests
// =============================================================================

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input         string
		expectedCount int
	}{
		{"", 0},
		{"single", 1},
		{"line1\nline2", 2},
		{"line1\nline2\n", 2},
		{"a\nb\nc", 3},
	}

	for _, tt := range tests {
		result := splitLines(tt.input)
		assert.Len(t, result, tt.expectedCount, "input: %q", tt.input)
	}
}

func TestTrimSpace(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"hello", "hello"},
		{"  hello", "hello"},
		{"hello  ", "hello"},
		{"  hello  ", "hello"},
		{"\thello\t", "hello"},
		{"  \t hello \t  ", "hello"},
	}

	for _, tt := range tests {
		result := trimSpace(tt.input)
		assert.Equal(t, tt.expected, result)
	}
}

func TestHasPrefix(t *testing.T) {
	assert.True(t, hasPrefix("hello world", "hello"))
	assert.True(t, hasPrefix("hello", "hello"))
	assert.False(t, hasPrefix("hello", "world"))
	assert.False(t, hasPrefix("hi", "hello"))
}

func TestHasSuffix(t *testing.T) {
	assert.True(t, hasSuffix("hello world", "world"))
	assert.True(t, hasSuffix("hello", "hello"))
	assert.False(t, hasSuffix("hello", "world"))
	assert.False(t, hasSuffix("hi", "hello"))
}

func TestContains(t *testing.T) {
	assert.True(t, contains("hello world", "world"))
	assert.True(t, contains("hello world", "hello"))
	assert.True(t, contains("hello world", "lo wo"))
	assert.False(t, contains("hello", "world"))
}

func TestIsComment(t *testing.T) {
	assert.True(t, isComment("// comment"))
	assert.True(t, isComment("# python comment"))
	assert.True(t, isComment("/* block comment"))
	assert.True(t, isComment("* continuation"))
	assert.False(t, isComment("code()"))
	assert.False(t, isComment(""))
}

func TestExtractGoFuncName(t *testing.T) {
	tests := []struct {
		line     string
		expected string
	}{
		{"func main() {", "main"},
		{"func helper(x int) string {", "helper"},
		{"func (s *Server) Handle() {", "Handle"},
		{"func (r receiver) Method() error {", "Method"},
	}

	for _, tt := range tests {
		// The function expects full line starting with "func "
		result := extractGoFuncName(tt.line)
		assert.Equal(t, tt.expected, result, "input: %s", tt.line)
	}
}

func TestExtractPythonFuncName(t *testing.T) {
	tests := []struct {
		line     string
		expected string
	}{
		{"def main():", "main"},
		{"def helper(x, y):", "helper"},
		{"async def fetch():", "fetch"},
	}

	for _, tt := range tests {
		var result string
		if hasPrefix(tt.line, "async def ") {
			result = extractPythonFuncName(tt.line)
		} else {
			result = extractPythonFuncName(tt.line)
		}
		assert.Equal(t, tt.expected, result, "input: %s", tt.line)
	}
}

// =============================================================================
// Mock Plugin for Testing
// =============================================================================

type mockPlugin struct {
	name        string
	description string
	functions   map[string]REPLFunction
	onLoadError error
	unloaded    bool
}

func (p *mockPlugin) Name() string {
	return p.name
}

func (p *mockPlugin) Description() string {
	if p.description != "" {
		return p.description
	}
	return "Mock plugin for testing"
}

func (p *mockPlugin) Functions() map[string]REPLFunction {
	if p.functions == nil {
		return map[string]REPLFunction{}
	}
	return p.functions
}

func (p *mockPlugin) OnLoad(ctx context.Context) error {
	return p.onLoadError
}

func (p *mockPlugin) OnUnload() error {
	p.unloaded = true
	return nil
}
