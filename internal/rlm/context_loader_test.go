package rlm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeVarName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"valid_name", "valid_name"},
		{"CamelCase", "CamelCase"},
		{"with-dashes", "with_dashes"},
		{"with.dots", "with_dots"},
		{"123starts_with_digit", "_123starts_with_digit"},
		{"spaces here", "spaces_here"},
		{"", "context"},
		{"file/path", "file_path"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeVarName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		ext      string
		expected string
	}{
		{".go", "go"},
		{".py", "python"},
		{".ts", "typescript"},
		{".js", "javascript"},
		{".rs", "rust"},
		{".unknown", "text"},
		{"", "text"},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			result := detectLanguage(tt.ext)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildDescription(t *testing.T) {
	tests := []struct {
		name     string
		source   ContextSource
		expected string
	}{
		{
			name: "file with path",
			source: ContextSource{
				Type: ContextTypeFile,
				Metadata: map[string]any{
					"source": "/path/to/file.go",
				},
			},
			expected: "File content from file.go",
		},
		{
			name: "file without path",
			source: ContextSource{
				Type:     ContextTypeFile,
				Metadata: map[string]any{},
			},
			expected: "File content",
		},
		{
			name: "search results",
			source: ContextSource{
				Type: ContextTypeSearch,
				Metadata: map[string]any{
					"query":        "handleRequest",
					"result_count": 5,
				},
			},
			expected: "5 search results for 'handleRequest'",
		},
		{
			name: "memory context",
			source: ContextSource{
				Type:     ContextTypeMemory,
				Metadata: map[string]any{},
			},
			expected: "Memory context from hypergraph",
		},
		{
			name: "custom",
			source: ContextSource{
				Type:     ContextTypeCustom,
				Metadata: map[string]any{},
			},
			expected: "Custom context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildDescription(tt.source)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestVariableInfo(t *testing.T) {
	info := VariableInfo{
		Name:          "test_var",
		Type:          ContextTypeFile,
		Size:          1000,
		TokenEstimate: 250,
		Description:   "Test file",
		Source:        "/path/to/test.go",
	}

	assert.Equal(t, "test_var", info.Name)
	assert.Equal(t, ContextTypeFile, info.Type)
	assert.Equal(t, 1000, info.Size)
	assert.Equal(t, 250, info.TokenEstimate)
	assert.Equal(t, "Test file", info.Description)
	assert.Equal(t, "/path/to/test.go", info.Source)
}

func TestToManifest(t *testing.T) {
	loaded := &LoadedContext{
		Variables: map[string]VariableInfo{
			"file1": {Name: "file1", Type: ContextTypeFile, TokenEstimate: 100},
			"file2": {Name: "file2", Type: ContextTypeFile, TokenEstimate: 200},
		},
		TotalTokens: 300,
	}

	manifest := ToManifest(loaded)
	assert.Len(t, manifest.Variables, 2)
	assert.Equal(t, 300, manifest.TotalTokens)
}

func TestContextManifest_ToJSON(t *testing.T) {
	manifest := &ContextManifest{
		Variables: []VariableInfo{
			{Name: "test", Type: ContextTypeFile, TokenEstimate: 100},
		},
		TotalTokens: 100,
		Summary:     "Test",
	}

	json := manifest.ToJSON()
	assert.Contains(t, json, "\"test\"")
	assert.Contains(t, json, "\"total_tokens\": 100")
}

func TestContextSource_Types(t *testing.T) {
	// Verify all context types are defined
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

func TestNewContextLoader_NilREPL(t *testing.T) {
	loader := NewContextLoader(nil)
	assert.NotNil(t, loader)
}
