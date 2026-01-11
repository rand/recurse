package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rand/recurse/internal/rlm/repl"
)

// ContextLoader manages externalized context for RLM operations.
// It loads context from various sources (files, search results, etc.)
// into REPL variables that the LLM can manipulate via code.
type ContextLoader struct {
	repl *repl.Manager
}

// NewContextLoader creates a new context loader.
func NewContextLoader(replMgr *repl.Manager) *ContextLoader {
	return &ContextLoader{
		repl: replMgr,
	}
}

// ContextSource represents a source of context to load.
type ContextSource struct {
	// Name is the variable name in the REPL (e.g., "file_content", "search_results")
	Name string

	// Content is the actual context content
	Content string

	// Type describes the context type for the LLM
	Type ContextType

	// Metadata contains additional information about the context
	Metadata map[string]any
}

// ContextType describes the type of context.
type ContextType string

const (
	ContextTypeFile         ContextType = "file"
	ContextTypeSearchResult ContextType = "search_result"
	ContextTypeCodeBlock    ContextType = "code_block"
	ContextTypeConversation ContextType = "conversation"
	ContextTypeMemory       ContextType = "memory"
	ContextTypeCustom       ContextType = "custom"
)

// LoadedContext represents context that has been loaded into the REPL.
type LoadedContext struct {
	// Variables is a map of variable names to their metadata
	Variables map[string]VariableInfo

	// TotalTokens is the estimated total token count across all context
	TotalTokens int

	// Summary describes what context is available
	Summary string
}

// VariableInfo contains metadata about a loaded context variable.
type VariableInfo struct {
	Name        string      `json:"name"`
	Type        ContextType `json:"type"`
	Length      int         `json:"length"`
	TokenCount  int         `json:"token_count"`
	Description string      `json:"description,omitempty"`
	Source      string      `json:"source,omitempty"`
}

// Load loads multiple context sources into the REPL.
func (cl *ContextLoader) Load(ctx context.Context, sources []ContextSource) (*LoadedContext, error) {
	if cl.repl == nil {
		return nil, fmt.Errorf("REPL manager not available")
	}

	loaded := &LoadedContext{
		Variables: make(map[string]VariableInfo),
	}

	var summaryParts []string

	for _, src := range sources {
		// Sanitize variable name
		varName := sanitizeVarName(src.Name)

		// Set the variable in REPL
		if err := cl.repl.SetVar(ctx, varName, src.Content); err != nil {
			return nil, fmt.Errorf("set var %s: %w", varName, err)
		}

		// Calculate token estimate
		tokenCount := estimateTokens(src.Content)

		// Build description
		desc := buildDescription(src)

		info := VariableInfo{
			Name:        varName,
			Type:        src.Type,
			Length:      len(src.Content),
			TokenCount:  tokenCount,
			Description: desc,
		}

		if source, ok := src.Metadata["source"].(string); ok {
			info.Source = source
		}

		loaded.Variables[varName] = info
		loaded.TotalTokens += tokenCount

		summaryParts = append(summaryParts, fmt.Sprintf("%s (%s, ~%d tokens)", varName, src.Type, tokenCount))
	}

	loaded.Summary = fmt.Sprintf("Loaded %d context variables: %s", len(sources), strings.Join(summaryParts, ", "))

	return loaded, nil
}

// LoadFile loads a file's content as a context variable.
func (cl *ContextLoader) LoadFile(ctx context.Context, varName, path, content string) (*LoadedContext, error) {
	ext := filepath.Ext(path)
	return cl.Load(ctx, []ContextSource{{
		Name:    varName,
		Content: content,
		Type:    ContextTypeFile,
		Metadata: map[string]any{
			"source":    path,
			"extension": ext,
			"language":  detectLanguage(ext),
		},
	}})
}

// LoadSearchResults loads search results as a context variable.
func (cl *ContextLoader) LoadSearchResults(ctx context.Context, varName, query string, results []SearchResult) (*LoadedContext, error) {
	// Format results as structured text
	var sb strings.Builder
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("=== Result %d: %s ===\n", i+1, r.Path))
		if r.LineNumber > 0 {
			sb.WriteString(fmt.Sprintf("Line %d:\n", r.LineNumber))
		}
		sb.WriteString(r.Content)
		sb.WriteString("\n\n")
	}

	return cl.Load(ctx, []ContextSource{{
		Name:    varName,
		Content: sb.String(),
		Type:    ContextTypeSearchResult,
		Metadata: map[string]any{
			"query":        query,
			"result_count": len(results),
		},
	}})
}

// SearchResult represents a single search result.
type SearchResult struct {
	Path       string
	Content    string
	LineNumber int
}

// LoadConversation loads conversation history as context.
func (cl *ContextLoader) LoadConversation(ctx context.Context, varName string, messages []ConversationMessage) (*LoadedContext, error) {
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n\n", msg.Role, msg.Content))
	}

	return cl.Load(ctx, []ContextSource{{
		Name:    varName,
		Content: sb.String(),
		Type:    ContextTypeConversation,
		Metadata: map[string]any{
			"message_count": len(messages),
		},
	}})
}

// ConversationMessage represents a message in conversation history.
type ConversationMessage struct {
	Role    string
	Content string
}

// GenerateContextPrompt generates a prompt describing available context.
// This is used to inform the LLM about what context variables are available.
func (cl *ContextLoader) GenerateContextPrompt(loaded *LoadedContext) string {
	if loaded == nil || len(loaded.Variables) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Available Context Variables\n\n")
	sb.WriteString("The following context has been externalized as Python variables. ")
	sb.WriteString("Use the RLM helper functions (peek, grep, partition, etc.) to explore and process them.\n\n")

	for _, info := range loaded.Variables {
		sb.WriteString(fmt.Sprintf("- `%s` (%s): %s\n", info.Name, info.Type, info.Description))
		sb.WriteString(fmt.Sprintf("  - Length: %d chars, ~%d tokens\n", info.Length, info.TokenCount))
		if info.Source != "" {
			sb.WriteString(fmt.Sprintf("  - Source: %s\n", info.Source))
		}
	}

	sb.WriteString(fmt.Sprintf("\nTotal context: ~%d tokens\n\n", loaded.TotalTokens))

	sb.WriteString("### Example Usage\n")
	sb.WriteString("```python\n")
	sb.WriteString("# Peek at first 1000 chars of a variable\n")
	sb.WriteString("peek(file_content, 0, 1000)\n\n")
	sb.WriteString("# Search for patterns\n")
	sb.WriteString("matches = grep(file_content, r'def \\w+')\n\n")
	sb.WriteString("# Partition for parallel processing\n")
	sb.WriteString("chunks = partition(large_context, n=4)\n")
	sb.WriteString("```\n")

	return sb.String()
}

// ClearContext clears all context variables from the REPL.
func (cl *ContextLoader) ClearContext(ctx context.Context, varNames []string) error {
	// Execute Python code to delete variables
	deleteCode := "del " + strings.Join(varNames, ", ")
	_, err := cl.repl.Execute(ctx, deleteCode)
	return err
}

// GetContextInfo returns information about currently loaded context.
func (cl *ContextLoader) GetContextInfo(ctx context.Context) ([]VariableInfo, error) {
	result, err := cl.repl.ListVars(ctx)
	if err != nil {
		return nil, err
	}

	var infos []VariableInfo
	for _, v := range result.Variables {
		infos = append(infos, VariableInfo{
			Name:       v.Name,
			Type:       ContextTypeCustom,
			Length:     v.Length,
			TokenCount: v.Length / 4,
		})
	}

	return infos, nil
}

// Helper functions

func sanitizeVarName(name string) string {
	// Replace invalid characters with underscore
	result := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, name)

	// Ensure it doesn't start with a digit
	if len(result) > 0 && result[0] >= '0' && result[0] <= '9' {
		result = "_" + result
	}

	if result == "" {
		result = "context"
	}

	return result
}

func buildDescription(src ContextSource) string {
	switch src.Type {
	case ContextTypeFile:
		if path, ok := src.Metadata["source"].(string); ok {
			return fmt.Sprintf("File content from %s", filepath.Base(path))
		}
		return "File content"

	case ContextTypeSearchResult:
		if query, ok := src.Metadata["query"].(string); ok {
			count := 0
			if c, ok := src.Metadata["result_count"].(int); ok {
				count = c
			}
			return fmt.Sprintf("%d search results for '%s'", count, query)
		}
		return "Search results"

	case ContextTypeCodeBlock:
		if lang, ok := src.Metadata["language"].(string); ok {
			return fmt.Sprintf("%s code block", lang)
		}
		return "Code block"

	case ContextTypeConversation:
		if count, ok := src.Metadata["message_count"].(int); ok {
			return fmt.Sprintf("Conversation history (%d messages)", count)
		}
		return "Conversation history"

	case ContextTypeMemory:
		return "Memory context from hypergraph"

	default:
		return "Custom context"
	}
}

func detectLanguage(ext string) string {
	languages := map[string]string{
		".go":   "go",
		".py":   "python",
		".js":   "javascript",
		".ts":   "typescript",
		".rs":   "rust",
		".java": "java",
		".c":    "c",
		".cpp":  "cpp",
		".h":    "c",
		".hpp":  "cpp",
		".rb":   "ruby",
		".php":  "php",
		".swift": "swift",
		".kt":   "kotlin",
		".zig":  "zig",
		".md":   "markdown",
		".json": "json",
		".yaml": "yaml",
		".yml":  "yaml",
		".toml": "toml",
		".sql":  "sql",
	}

	if lang, ok := languages[ext]; ok {
		return lang
	}
	return "text"
}

// ContextManifest is a JSON-serializable summary of loaded context.
type ContextManifest struct {
	Variables   []VariableInfo `json:"variables"`
	TotalTokens int            `json:"total_tokens"`
	Summary     string         `json:"summary"`
}

// ToManifest converts LoadedContext to a JSON-serializable manifest.
func (lc *LoadedContext) ToManifest() *ContextManifest {
	vars := make([]VariableInfo, 0, len(lc.Variables))
	for _, v := range lc.Variables {
		vars = append(vars, v)
	}
	return &ContextManifest{
		Variables:   vars,
		TotalTokens: lc.TotalTokens,
		Summary:     lc.Summary,
	}
}

// ToJSON converts the manifest to JSON.
func (cm *ContextManifest) ToJSON() string {
	data, _ := json.MarshalIndent(cm, "", "  ")
	return string(data)
}
