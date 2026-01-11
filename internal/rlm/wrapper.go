package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/rand/recurse/internal/rlm/meta"
	"github.com/rand/recurse/internal/rlm/repl"
)

// Wrapper provides RLM-style completion that externalizes context to REPL.
// This enables the true RLM paradigm where the LLM reasons about context
// via code execution rather than direct context ingestion.
type Wrapper struct {
	service       *Service
	replMgr       *repl.Manager
	contextLoader *ContextLoader
	client        meta.LLMClient

	// Thresholds for when to use RLM mode
	minContextTokensForRLM int
	maxDirectContextTokens int
}

// WrapperConfig configures the RLM wrapper.
type WrapperConfig struct {
	// MinContextTokensForRLM is the minimum context size to trigger RLM mode.
	MinContextTokensForRLM int

	// MaxDirectContextTokens is the max context to include directly (non-RLM).
	MaxDirectContextTokens int
}

// DefaultWrapperConfig returns sensible defaults.
func DefaultWrapperConfig() WrapperConfig {
	return WrapperConfig{
		MinContextTokensForRLM: 4000,  // ~16KB
		MaxDirectContextTokens: 32000, // ~128KB
	}
}

// NewWrapper creates a new RLM wrapper.
func NewWrapper(svc *Service, cfg WrapperConfig) *Wrapper {
	if cfg.MinContextTokensForRLM == 0 {
		cfg.MinContextTokensForRLM = 4000
	}
	if cfg.MaxDirectContextTokens == 0 {
		cfg.MaxDirectContextTokens = 32000
	}

	return &Wrapper{
		service:                svc,
		minContextTokensForRLM: cfg.MinContextTokensForRLM,
		maxDirectContextTokens: cfg.MaxDirectContextTokens,
	}
}

// SetREPLManager sets the REPL manager for context externalization.
func (w *Wrapper) SetREPLManager(replMgr *repl.Manager) {
	w.replMgr = replMgr
	if replMgr != nil {
		w.contextLoader = NewContextLoader(replMgr)
	}
}

// SetLLMClient sets the LLM client for sub-calls.
func (w *Wrapper) SetLLMClient(client meta.LLMClient) {
	w.client = client
}

// PrepareContext prepares context for a prompt, potentially externalizing it.
// Returns the modified prompt and any loaded context info.
func (w *Wrapper) PrepareContext(ctx context.Context, prompt string, contexts []ContextSource) (*PreparedPrompt, error) {
	// Calculate total context size
	totalTokens := estimateTokens(prompt)
	for _, c := range contexts {
		totalTokens += estimateTokens(c.Content)
	}

	// Decide mode based on context size and availability
	if w.shouldUseRLMMode(totalTokens, contexts) && w.contextLoader != nil {
		return w.prepareRLMMode(ctx, prompt, contexts, totalTokens)
	}

	// Direct mode: include context in prompt
	return w.prepareDirectMode(prompt, contexts), nil
}

// PreparedPrompt contains the result of context preparation.
type PreparedPrompt struct {
	// OriginalPrompt is the user's original prompt.
	OriginalPrompt string

	// FinalPrompt is the prompt to send to the LLM.
	FinalPrompt string

	// SystemPrompt is any additional system prompt for RLM mode.
	SystemPrompt string

	// Mode indicates which mode was selected.
	Mode ExecutionMode

	// LoadedContext contains info about externalized context (RLM mode only).
	LoadedContext *LoadedContext

	// TotalTokens is the estimated total tokens.
	TotalTokens int
}

// ExecutionMode indicates how the prompt should be executed.
type ExecutionMode string

const (
	ModeDirecte ExecutionMode = "direct" // Include context in prompt
	ModeRLM     ExecutionMode = "rlm"    // Externalize context to REPL
)

// shouldUseRLMMode determines if RLM mode should be used.
func (w *Wrapper) shouldUseRLMMode(totalTokens int, contexts []ContextSource) bool {
	// Use RLM mode if:
	// 1. Context is large enough to benefit
	// 2. REPL is available
	// 3. There are actual contexts to externalize
	if len(contexts) == 0 {
		return false
	}
	if w.replMgr == nil {
		return false
	}
	if totalTokens < w.minContextTokensForRLM {
		return false
	}
	return true
}

// prepareRLMMode prepares for RLM-style execution.
func (w *Wrapper) prepareRLMMode(ctx context.Context, prompt string, contexts []ContextSource, totalTokens int) (*PreparedPrompt, error) {
	result := &PreparedPrompt{
		OriginalPrompt: prompt,
		Mode:           ModeRLM,
		TotalTokens:    totalTokens,
	}

	// Load contexts into REPL
	loaded, err := w.contextLoader.Load(ctx, contexts)
	if err != nil {
		slog.Warn("Failed to externalize context, falling back to direct mode", "error", err)
		return w.prepareDirectMode(prompt, contexts), nil
	}
	result.LoadedContext = loaded

	// Store the original prompt as a REPL variable too
	if err := w.replMgr.SetVar(ctx, "user_query", prompt); err != nil {
		slog.Warn("Failed to store user query in REPL", "error", err)
	}

	// Generate RLM system prompt
	result.SystemPrompt = w.generateRLMSystemPrompt(loaded)

	// Generate minimal prompt that references externalized context
	result.FinalPrompt = w.generateRLMPrompt(prompt, loaded)

	slog.Info("RLM mode activated",
		"total_tokens", totalTokens,
		"externalized_vars", len(loaded.Variables),
		"mode", "rlm")

	return result, nil
}

// prepareDirectMode prepares for direct execution (context in prompt).
func (w *Wrapper) prepareDirectMode(prompt string, contexts []ContextSource) *PreparedPrompt {
	result := &PreparedPrompt{
		OriginalPrompt: prompt,
		Mode:           ModeDirecte,
	}

	// Build prompt with inline context
	var sb strings.Builder
	sb.WriteString(prompt)

	for _, ctx := range contexts {
		sb.WriteString("\n\n## ")
		sb.WriteString(string(ctx.Type))
		if name, ok := ctx.Metadata["source"].(string); ok {
			sb.WriteString(": ")
			sb.WriteString(name)
		}
		sb.WriteString("\n")
		sb.WriteString(ctx.Content)
	}

	result.FinalPrompt = sb.String()
	result.TotalTokens = estimateTokens(result.FinalPrompt)

	return result
}

// generateRLMSystemPrompt generates the system prompt for RLM mode.
func (w *Wrapper) generateRLMSystemPrompt(loaded *LoadedContext) string {
	var sb strings.Builder

	sb.WriteString(`You are operating in RLM (Recursive Language Model) mode.

Context has been externalized to Python variables. Use code execution to explore and process it.

## Key Rules
1. Context is NOT in this prompt - access it via Python code
2. Use peek(), grep(), partition() to explore context
3. Use llm_call() for focused sub-tasks on context slices
4. Call FINAL(response) when you have your answer

## Available Variables
`)

	if loaded != nil {
		for name, info := range loaded.Variables {
			sb.WriteString(fmt.Sprintf("- %s: %s (~%d tokens)\n", name, info.Description, info.TokenCount))
		}
	}

	sb.WriteString(`
## Available Functions

### Core Operations
- peek(ctx, start, end, by_lines=False) - View slice of context
- grep(ctx, pattern, context_lines=0) - Search for patterns with optional context
- partition(ctx, n=4, overlap=0) - Split into n chunks
- partition_by_lines(ctx, n=4) - Split by lines (respects line boundaries)
- extract_functions(ctx, language) - Extract function definitions from code

### LLM Operations
- llm_call(prompt, context, model) - Single sub-LLM call
- llm_batch(prompts, contexts, model) - Batch sub-LLM calls
- summarize(ctx, max_length=500, focus=None) - Summarize context with LLM
- map_reduce(ctx, map_prompt, reduce_prompt, n_chunks=4) - Map-reduce pattern
- find_relevant(ctx, query, top_k=5) - Find relevant sections for a query

### Utilities
- count_tokens_approx(text) - Estimate token count

### Output Functions
- FINAL(response) - Return text as final answer
- FINAL_VAR(variable_name) - Return variable value as final answer
- FINAL_JSON(obj) - Return JSON-formatted output
- FINAL_CODE(code, language) - Return code with language annotation
- has_final_output() - Check if FINAL was called

## Example Workflows

### Simple Analysis
` + "```python" + `
# Explore the context first
preview = peek(file_content, 0, 2000)
matches = grep(file_content, "function|class")
result = llm_call("Analyze this code", file_content, "balanced")
FINAL(result)
` + "```" + `

### Large Context with Map-Reduce
` + "```python" + `
# For large contexts, use map-reduce pattern
if count_tokens_approx(file_content) > 10000:
    result = map_reduce(
        file_content,
        map_prompt="List all functions and their purpose",
        reduce_prompt="Combine into a comprehensive API overview",
        n_chunks=4
    )
else:
    result = llm_call("Analyze this code", file_content, "balanced")
FINAL(result)
` + "```" + `

### Focused Search
` + "```python" + `
# Find relevant sections for a specific query
relevant = find_relevant(codebase, "error handling")
context = "\n\n".join([r['section'] for r in relevant])
analysis = llm_call("Analyze the error handling patterns", context, "balanced")
FINAL(analysis)
` + "```" + `
`)

	return sb.String()
}

// generateRLMPrompt generates the user prompt for RLM mode.
func (w *Wrapper) generateRLMPrompt(originalPrompt string, loaded *LoadedContext) string {
	var sb strings.Builder

	sb.WriteString("## User Request\n")
	sb.WriteString(originalPrompt)
	sb.WriteString("\n\n")

	sb.WriteString("## Context Available\n")
	sb.WriteString("The following context has been loaded into Python variables:\n")

	if loaded != nil {
		for name, info := range loaded.Variables {
			sb.WriteString(fmt.Sprintf("- `%s`: %s\n", name, info.Description))
		}
	}

	sb.WriteString("\nWrite Python code to explore and process this context, then call FINAL() with your response.\n")

	return sb.String()
}

// ExecuteRLM executes a prompt in RLM mode with code execution loop.
func (w *Wrapper) ExecuteRLM(ctx context.Context, prepared *PreparedPrompt) (*RLMExecutionResult, error) {
	if prepared.Mode != ModeRLM {
		return nil, fmt.Errorf("not in RLM mode")
	}

	result := &RLMExecutionResult{
		StartTime: time.Now(),
	}

	// Clear any previous FINAL output
	if _, err := w.replMgr.Execute(ctx, "clear_final_output()"); err != nil {
		slog.Warn("Failed to clear FINAL output", "error", err)
	}

	// The actual execution loop would involve:
	// 1. Send prompt to LLM
	// 2. LLM returns Python code
	// 3. Execute code in REPL
	// 4. If FINAL() was called, return the result
	// 5. Otherwise, send execution results back to LLM and repeat

	// For now, we return a placeholder - the actual loop requires
	// integration with the agent's conversation handling
	result.Duration = time.Since(result.StartTime)
	result.Note = "RLM execution loop requires agent integration"

	return result, nil
}

// RLMExecutionResult contains the result of RLM execution.
type RLMExecutionResult struct {
	// FinalOutput is the result from FINAL() if called.
	FinalOutput string

	// Iterations is how many code execution rounds occurred.
	Iterations int

	// TotalTokens is the total tokens used across all calls.
	TotalTokens int

	// TotalCost is the estimated total cost.
	TotalCost float64

	// StartTime is when execution started.
	StartTime time.Time

	// Duration is total execution time.
	Duration time.Duration

	// Error is set if execution failed.
	Error string

	// Note contains any additional information.
	Note string
}

// FinalOutputResult contains the result from FINAL() including metadata.
type FinalOutputResult struct {
	Content  string            `json:"content"`
	Type     string            `json:"type"` // "text", "json", "code", "markdown"
	Metadata map[string]string `json:"metadata,omitempty"`
}

// GetFinalOutput retrieves the FINAL() output from the REPL.
func (w *Wrapper) GetFinalOutput(ctx context.Context) (string, error) {
	if w.replMgr == nil {
		return "", fmt.Errorf("REPL not available")
	}

	result, err := w.replMgr.Execute(ctx, "get_final_output()")
	if err != nil {
		return "", err
	}

	// The return value will be the string or None
	output := result.ReturnVal
	if output == "None" || output == "" {
		return "", nil
	}

	// Strip quotes from string representation
	output = strings.Trim(output, "'\"")
	return output, nil
}

// GetFinalOutputWithMetadata retrieves FINAL() output with type and metadata.
func (w *Wrapper) GetFinalOutputWithMetadata(ctx context.Context) (*FinalOutputResult, error) {
	if w.replMgr == nil {
		return nil, fmt.Errorf("REPL not available")
	}

	// Get the full metadata dict
	result, err := w.replMgr.Execute(ctx, "get_final_metadata()")
	if err != nil {
		return nil, err
	}

	if result.ReturnVal == "None" || result.ReturnVal == "" {
		return nil, nil
	}

	// Parse the JSON dict representation
	// Python returns: {'content': '...', 'type': '...', 'metadata': {...}}
	// We need to convert Python dict syntax to JSON
	jsonStr := result.ReturnVal
	jsonStr = strings.ReplaceAll(jsonStr, "'", "\"")
	jsonStr = strings.ReplaceAll(jsonStr, "None", "null")
	jsonStr = strings.ReplaceAll(jsonStr, "True", "true")
	jsonStr = strings.ReplaceAll(jsonStr, "False", "false")

	var output FinalOutputResult
	if err := json.Unmarshal([]byte(jsonStr), &output); err != nil {
		// Fallback to simple string extraction
		content, _ := w.GetFinalOutput(ctx)
		return &FinalOutputResult{Content: content, Type: "text"}, nil
	}

	return &output, nil
}

// HasFinalOutput checks if FINAL() has been called.
func (w *Wrapper) HasFinalOutput(ctx context.Context) (bool, error) {
	if w.replMgr == nil {
		return false, fmt.Errorf("REPL not available")
	}

	result, err := w.replMgr.Execute(ctx, "has_final_output()")
	if err != nil {
		return false, err
	}

	return result.ReturnVal == "True", nil
}

// ClearContext clears all externalized context from the REPL.
func (w *Wrapper) ClearContext(ctx context.Context) error {
	if w.replMgr == nil {
		return fmt.Errorf("REPL not available")
	}

	// Get list of variables and clear them
	vars, err := w.replMgr.ListVars(ctx)
	if err != nil {
		return err
	}

	if len(vars.Variables) == 0 {
		return nil
	}

	// Build list of variable names to delete
	var names []string
	for _, v := range vars.Variables {
		// Don't delete built-in RLM functions
		if !isBuiltinRLMVar(v.Name) {
			names = append(names, v.Name)
		}
	}

	if len(names) > 0 {
		return w.contextLoader.ClearContext(ctx, names)
	}
	return nil
}

func isBuiltinRLMVar(name string) bool {
	builtins := map[string]bool{
		"peek": true, "grep": true, "partition": true,
		"partition_by_lines": true, "extract_functions": true,
		"count_tokens_approx": true, "llm_call": true,
		"llm_batch": true, "FINAL": true, "RLMContext": true,
		"get_final_output": true, "clear_final_output": true,
	}
	return builtins[name]
}
