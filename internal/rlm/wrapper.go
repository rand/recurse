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
	classifier    *TaskClassifier

	// Thresholds for when to use RLM mode
	minContextTokensForRLM            int
	minContextTokensForComputational  int // Lower threshold for computational tasks
	maxDirectContextTokens            int

	// Classification settings
	classificationConfidenceThreshold float64
}

// WrapperConfig configures the RLM wrapper.
type WrapperConfig struct {
	// MinContextTokensForRLM is the minimum context size to trigger RLM mode.
	MinContextTokensForRLM int

	// MinContextTokensForComputational is the lower threshold for computational tasks.
	// Computational tasks benefit from RLM even at smaller context sizes.
	MinContextTokensForComputational int

	// MaxDirectContextTokens is the max context to include directly (non-RLM).
	MaxDirectContextTokens int

	// ClassificationConfidenceThreshold is the minimum confidence for task-based mode selection.
	// Below this threshold, falls back to size-based selection.
	ClassificationConfidenceThreshold float64

	// DisableClassifier disables task classification (use size-based selection only).
	DisableClassifier bool
}

// DefaultWrapperConfig returns sensible defaults.
func DefaultWrapperConfig() WrapperConfig {
	return WrapperConfig{
		MinContextTokensForRLM:            4000,  // ~16KB - default threshold
		MinContextTokensForComputational:  500,   // ~2KB - lower for computational tasks
		MaxDirectContextTokens:            32000, // ~128KB
		ClassificationConfidenceThreshold: 0.7,   // Require 70% confidence for task-based selection
		DisableClassifier:                 false,
	}
}

// NewWrapper creates a new RLM wrapper.
func NewWrapper(svc *Service, cfg WrapperConfig) *Wrapper {
	if cfg.MinContextTokensForRLM == 0 {
		cfg.MinContextTokensForRLM = 4000
	}
	if cfg.MinContextTokensForComputational == 0 {
		cfg.MinContextTokensForComputational = 500
	}
	if cfg.MaxDirectContextTokens == 0 {
		cfg.MaxDirectContextTokens = 32000
	}
	if cfg.ClassificationConfidenceThreshold == 0 {
		cfg.ClassificationConfidenceThreshold = 0.7
	}

	w := &Wrapper{
		service:                           svc,
		minContextTokensForRLM:            cfg.MinContextTokensForRLM,
		minContextTokensForComputational:  cfg.MinContextTokensForComputational,
		maxDirectContextTokens:            cfg.MaxDirectContextTokens,
		classificationConfidenceThreshold: cfg.ClassificationConfidenceThreshold,
	}

	// Initialize classifier unless disabled
	if !cfg.DisableClassifier {
		w.classifier = NewTaskClassifier()
	}

	return w
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

	// Classify the task if classifier is available
	var classification *Classification
	if w.classifier != nil {
		c := w.classifier.Classify(prompt, contexts)
		classification = &c
	}

	// Decide mode based on classification and context size
	mode, reason := w.selectMode(prompt, totalTokens, contexts, classification)

	if mode == ModeRLM && w.contextLoader != nil {
		prepared, err := w.prepareRLMMode(ctx, prompt, contexts, totalTokens)
		if err != nil {
			return nil, err
		}
		prepared.Classification = classification
		prepared.ModeReason = reason
		return prepared, nil
	}

	// Direct mode: include context in prompt
	prepared := w.prepareDirectMode(prompt, contexts)
	prepared.Classification = classification
	prepared.ModeReason = reason
	return prepared, nil
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

	// ModeReason explains why this mode was selected.
	ModeReason string

	// Classification contains the task classification result (if classifier enabled).
	Classification *Classification

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

// selectMode determines which execution mode to use based on task classification and context size.
// Returns the selected mode and a human-readable reason for the selection.
func (w *Wrapper) selectMode(query string, totalTokens int, contexts []ContextSource, classification *Classification) (ExecutionMode, string) {
	// Basic prerequisites for RLM
	if len(contexts) == 0 {
		return ModeDirecte, "no context to externalize"
	}
	if w.replMgr == nil {
		return ModeDirecte, "REPL not available"
	}

	// Use classification if available and confident
	if classification != nil && classification.Confidence >= w.classificationConfidenceThreshold {
		switch classification.Type {
		case TaskTypeComputational:
			// Computational tasks benefit from RLM even at lower thresholds
			if totalTokens >= w.minContextTokensForComputational {
				return ModeRLM, fmt.Sprintf("computational task (%.0f%% confidence), tokens=%d >= %d",
					classification.Confidence*100, totalTokens, w.minContextTokensForComputational)
			}
			return ModeDirecte, fmt.Sprintf("computational task but context too small (%d < %d tokens)",
				totalTokens, w.minContextTokensForComputational)

		case TaskTypeRetrieval:
			// Retrieval tasks prefer Direct even for larger contexts
			return ModeDirecte, fmt.Sprintf("retrieval task (%.0f%% confidence), Direct is faster",
				classification.Confidence*100)

		case TaskTypeAnalytical:
			// Analytical tasks use RLM only for larger contexts
			if totalTokens >= 8000 {
				return ModeRLM, fmt.Sprintf("analytical task (%.0f%% confidence), large context (%d tokens)",
					classification.Confidence*100, totalTokens)
			}
			return ModeDirecte, fmt.Sprintf("analytical task, context small enough for Direct (%d tokens)",
				totalTokens)
		}
	}

	// Fall back to size-based selection
	if totalTokens >= w.minContextTokensForRLM {
		return ModeRLM, fmt.Sprintf("context size (%d tokens) >= threshold (%d)",
			totalTokens, w.minContextTokensForRLM)
	}

	return ModeDirecte, fmt.Sprintf("context size (%d tokens) < threshold (%d)",
		totalTokens, w.minContextTokensForRLM)
}

// shouldUseRLMMode is a simplified check for backward compatibility.
// Deprecated: Use selectMode for full classification support.
func (w *Wrapper) shouldUseRLMMode(totalTokens int, contexts []ContextSource) bool {
	mode, _ := w.selectMode("", totalTokens, contexts, nil)
	return mode == ModeRLM
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

### Memory Functions (Persistent Knowledge)
- memory_query(query, limit=10) - Search memory for relevant facts
- memory_add_fact(content, confidence=0.8) - Store a fact
- memory_add_experience(content, outcome, success) - Record what worked/didn't
- memory_get_context(limit=10) - Get recent relevant context
- memory_relate(label, subject_id, object_id) - Link two memory nodes

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

// RLMConfig contains configuration for RLM execution.
type RLMConfig struct {
	// MaxIterations is the maximum number of code execution rounds.
	MaxIterations int

	// MaxTokensPerCall is the maximum tokens per LLM call.
	MaxTokensPerCall int

	// Timeout is the maximum total execution time.
	Timeout time.Duration

	// EnableProfiling enables detailed performance profiling.
	EnableProfiling bool
}

// DefaultRLMConfig returns sensible defaults for RLM execution.
func DefaultRLMConfig() RLMConfig {
	return RLMConfig{
		MaxIterations:    10,
		MaxTokensPerCall: 4096,
		Timeout:          5 * time.Minute,
	}
}

// ExecuteRLM executes a prompt in RLM mode with code execution loop.
// The LLM generates Python code which is executed in the REPL. The loop
// continues until FINAL() is called or max iterations is reached.
func (w *Wrapper) ExecuteRLM(ctx context.Context, prepared *PreparedPrompt) (*RLMExecutionResult, error) {
	return w.ExecuteRLMWithConfig(ctx, prepared, DefaultRLMConfig())
}

// ExecuteRLMWithConfig executes RLM with custom configuration.
func (w *Wrapper) ExecuteRLMWithConfig(ctx context.Context, prepared *PreparedPrompt, cfg RLMConfig) (*RLMExecutionResult, error) {
	if prepared.Mode != ModeRLM {
		return nil, fmt.Errorf("not in RLM mode")
	}

	if w.replMgr == nil {
		return nil, fmt.Errorf("REPL manager not configured")
	}

	if w.client == nil {
		return nil, fmt.Errorf("LLM client not configured")
	}

	// Apply timeout
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	result := &RLMExecutionResult{
		StartTime: time.Now(),
	}

	// Initialize profiling if enabled
	var profile *RLMProfile
	if cfg.EnableProfiling {
		profile = NewRLMProfile()
		result.Profile = profile
	}

	// Clear any previous FINAL output
	if _, err := w.replMgr.Execute(ctx, "clear_final_output()"); err != nil {
		slog.Warn("Failed to clear FINAL output", "error", err)
	}

	// Build initial conversation
	conversation := []conversationMessage{
		{Role: "system", Content: prepared.SystemPrompt},
		{Role: "user", Content: prepared.FinalPrompt},
	}

	// Main execution loop
	for iteration := 0; iteration < cfg.MaxIterations; iteration++ {
		result.Iterations = iteration + 1

		// Start iteration profiling
		var iterProfile *IterationProfile
		if profile != nil {
			iterProfile = profile.StartIteration(iteration + 1)
		}

		// Check context cancellation
		if err := ctx.Err(); err != nil {
			result.Error = fmt.Sprintf("context cancelled: %v", err)
			if iterProfile != nil {
				profile.EndIteration(iterProfile)
			}
			break
		}

		// Send conversation to LLM (timed)
		prompt := w.formatConversation(conversation)
		llmStart := time.Now()
		response, err := w.client.Complete(ctx, prompt, cfg.MaxTokensPerCall)
		if iterProfile != nil {
			iterProfile.LLMCallDur = time.Since(llmStart)
		}
		if err != nil {
			result.Error = fmt.Sprintf("LLM call failed: %v", err)
			if iterProfile != nil {
				profile.EndIteration(iterProfile)
			}
			break
		}

		// Estimate tokens used
		promptTokens := estimateTokens(prompt)
		completionTokens := estimateTokens(response)
		result.TotalTokens += promptTokens + completionTokens
		if iterProfile != nil {
			iterProfile.PromptTokens = promptTokens
			iterProfile.CompletionTokens = completionTokens
		}

		// Extract Python code from response (timed as parsing)
		parseStart := time.Now()
		code := extractPythonCode(response)
		if iterProfile != nil {
			iterProfile.ParseDur = time.Since(parseStart)
		}

		if code == "" {
			// No code found - LLM might have provided a direct answer
			// Check if this looks like a final answer
			if looksLikeFinalAnswer(response) {
				result.FinalOutput = response
				if iterProfile != nil {
					iterProfile.HasFinal = true
					profile.EndIteration(iterProfile)
				}
				break
			}

			// Ask LLM to provide code
			conversation = append(conversation,
				conversationMessage{Role: "assistant", Content: response},
				conversationMessage{Role: "user", Content: "Please write Python code to solve this task. Use the available helper functions (peek, grep, partition, llm_call) to explore the context, and call FINAL() with your answer when done."},
			)
			if iterProfile != nil {
				profile.EndIteration(iterProfile)
			}
			continue
		}

		// Record code metrics
		if iterProfile != nil {
			iterProfile.HasCode = true
			iterProfile.CodeLength = len(code)
		}

		// Execute the code in REPL (timed)
		replStart := time.Now()
		execResult, err := w.replMgr.Execute(ctx, code)
		if iterProfile != nil {
			iterProfile.REPLExecDur = time.Since(replStart)
			if execResult != nil {
				iterProfile.REPLOutputLen = len(execResult.Output)
				if execResult.Error != "" {
					iterProfile.REPLError = execResult.Error
				}
			}
		}
		if err != nil {
			result.Error = fmt.Sprintf("REPL execution failed: %v", err)
			if iterProfile != nil {
				profile.EndIteration(iterProfile)
			}
			break
		}

		// Check if FINAL() was called
		hasFinal, err := w.HasFinalOutput(ctx)
		if err != nil {
			slog.Warn("Failed to check FINAL output", "error", err)
		}

		if hasFinal {
			// Get the final output
			finalOutput, err := w.GetFinalOutputWithMetadata(ctx)
			if err != nil {
				result.Error = fmt.Sprintf("failed to get FINAL output: %v", err)
				if iterProfile != nil {
					profile.EndIteration(iterProfile)
				}
				break
			}
			if finalOutput != nil {
				result.FinalOutput = finalOutput.Content
				result.FinalType = finalOutput.Type
				result.FinalMetadata = finalOutput.Metadata
			}
			if iterProfile != nil {
				iterProfile.HasFinal = true
				profile.EndIteration(iterProfile)
			}
			break
		}

		// Build execution feedback for next iteration
		feedback := w.buildExecutionFeedback(execResult)

		conversation = append(conversation,
			conversationMessage{Role: "assistant", Content: "```python\n" + code + "\n```"},
			conversationMessage{Role: "user", Content: feedback},
		)

		// End iteration profiling
		if iterProfile != nil {
			profile.EndIteration(iterProfile)
		}

		slog.Debug("RLM iteration",
			"iteration", iteration+1,
			"has_output", execResult.Output != "",
			"has_error", execResult.Error != "",
			"return_val", truncate(execResult.ReturnVal, 100))
	}

	result.Duration = time.Since(result.StartTime)

	// Finalize profiling
	if profile != nil {
		profile.TotalDuration = result.Duration
		profile.Finalize()
	}

	// If we exhausted iterations without FINAL, note it
	if result.FinalOutput == "" && result.Error == "" && result.Iterations >= cfg.MaxIterations {
		result.Error = fmt.Sprintf("max iterations (%d) reached without FINAL() call", cfg.MaxIterations)
	}

	return result, nil
}

// conversationMessage represents a message in the RLM conversation.
type conversationMessage struct {
	Role    string
	Content string
}

// formatConversation formats the conversation for the LLM.
func (w *Wrapper) formatConversation(messages []conversationMessage) string {
	var sb strings.Builder

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			sb.WriteString("<system>\n")
			sb.WriteString(msg.Content)
			sb.WriteString("\n</system>\n\n")
		case "user":
			sb.WriteString("User: ")
			sb.WriteString(msg.Content)
			sb.WriteString("\n\n")
		case "assistant":
			sb.WriteString("Assistant: ")
			sb.WriteString(msg.Content)
			sb.WriteString("\n\n")
		}
	}

	sb.WriteString("Assistant: ")
	return sb.String()
}

// buildExecutionFeedback creates feedback message from REPL execution.
func (w *Wrapper) buildExecutionFeedback(result *repl.ExecuteResult) string {
	var sb strings.Builder

	sb.WriteString("Code executed. ")

	if result.Error != "" {
		sb.WriteString("Error:\n```\n")
		sb.WriteString(result.Error)
		sb.WriteString("\n```\n")
		sb.WriteString("Please fix the error and try again.")
		return sb.String()
	}

	if result.Output != "" {
		sb.WriteString("Output:\n```\n")
		sb.WriteString(truncate(result.Output, 2000))
		sb.WriteString("\n```\n")
	}

	if result.ReturnVal != "" && result.ReturnVal != "None" {
		sb.WriteString("Return value: ")
		sb.WriteString(truncate(result.ReturnVal, 500))
		sb.WriteString("\n")
	}

	sb.WriteString("\nContinue exploring the context or call FINAL(response) when you have your answer.")

	return sb.String()
}

// extractPythonCode extracts Python code from an LLM response.
// Looks for ```python blocks or bare code that looks like Python.
func extractPythonCode(response string) string {
	// Try to find ```python blocks first
	if idx := strings.Index(response, "```python"); idx != -1 {
		start := idx + len("```python")
		// Skip whitespace after ```python
		for start < len(response) && (response[start] == '\n' || response[start] == '\r') {
			start++
		}

		end := strings.Index(response[start:], "```")
		if end != -1 {
			return strings.TrimSpace(response[start : start+end])
		}
		// No closing ```, take the rest
		return strings.TrimSpace(response[start:])
	}

	// Try generic ``` blocks
	if idx := strings.Index(response, "```"); idx != -1 {
		start := idx + 3
		// Skip language identifier and newline
		if nl := strings.Index(response[start:], "\n"); nl != -1 && nl < 20 {
			start += nl + 1
		}

		end := strings.Index(response[start:], "```")
		if end != -1 {
			code := strings.TrimSpace(response[start : start+end])
			// Verify it looks like Python
			if looksLikePython(code) {
				return code
			}
		}
	}

	return ""
}

// looksLikePython checks if code appears to be Python.
func looksLikePython(code string) bool {
	pythonIndicators := []string{
		"def ", "class ", "import ", "from ", "if ", "for ", "while ",
		"print(", "return ", "FINAL(", "llm_call(", "peek(", "grep(",
		"partition(", "memory_", "= ", "==", "!=",
	}

	for _, indicator := range pythonIndicators {
		if strings.Contains(code, indicator) {
			return true
		}
	}
	return false
}

// looksLikeFinalAnswer checks if a response appears to be a final answer
// rather than code that should be executed.
func looksLikeFinalAnswer(response string) bool {
	// If it contains code blocks, it's not a final answer
	if strings.Contains(response, "```") {
		return false
	}

	// If it's very short and doesn't have Python syntax, might be final
	if len(response) < 500 && !looksLikePython(response) {
		// Check for answer-like phrases
		answerPhrases := []string{
			"the answer is", "in conclusion", "to summarize",
			"based on my analysis", "the result is", "i found that",
		}
		responseLower := strings.ToLower(response)
		for _, phrase := range answerPhrases {
			if strings.Contains(responseLower, phrase) {
				return true
			}
		}
	}

	return false
}

// RLMExecutionResult contains the result of RLM execution.
type RLMExecutionResult struct {
	// FinalOutput is the result from FINAL() if called.
	FinalOutput string

	// FinalType is the type of the final output (text, json, code, markdown).
	FinalType string

	// FinalMetadata contains additional metadata from the final output.
	FinalMetadata map[string]string

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

	// Profile contains detailed performance profiling data (if enabled).
	Profile *RLMProfile
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
