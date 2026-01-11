// Package rlm provides the orchestrator for intelligent prompt pre-processing.
package rlm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/rand/recurse/internal/rlm/meta"
	"github.com/rand/recurse/internal/rlm/repl"
)

// AnalysisResult contains the RLM analysis of a user prompt.
type AnalysisResult struct {
	// OriginalPrompt is the user's original prompt.
	OriginalPrompt string

	// EnhancedPrompt is the prompt with additional context/instructions.
	EnhancedPrompt string

	// Decision is the meta-controller's orchestration decision.
	Decision *meta.Decision

	// ContextNeeds identifies what additional context would help.
	ContextNeeds *ContextNeeds

	// Routing suggests how to handle different parts of the task.
	Routing *TaskRouting

	// LoadedContext contains info about context externalized to REPL.
	LoadedContext *LoadedContext

	// AnalysisTime is how long analysis took.
	AnalysisTime time.Duration

	// ModelUsed is which model performed the analysis.
	ModelUsed string

	// ShouldDecompose indicates if the task should be broken into subtasks.
	ShouldDecompose bool

	// Subtasks are the decomposed subtasks if ShouldDecompose is true.
	Subtasks []Subtask

	// UseExternalizedContext indicates if REPL-based context is available.
	UseExternalizedContext bool
}

// ContextNeeds identifies what context would help with the task.
type ContextNeeds struct {
	// FilePatterns are glob patterns for files that might be relevant.
	FilePatterns []string

	// SearchQueries are code search queries to run.
	SearchQueries []string

	// ConceptsToUnderstand are high-level concepts that need exploration.
	ConceptsToUnderstand []string

	// MemoryQueries are queries to run against hypergraph memory.
	MemoryQueries []string

	// Priority indicates how important gathering this context is (0-10).
	Priority int
}

// TaskRouting suggests how to route different parts of the task.
type TaskRouting struct {
	// PrimaryModel is the recommended model for the main task.
	PrimaryModel string

	// PrimaryTier is the recommended tier for the main task.
	PrimaryTier meta.ModelTier

	// Reasoning explains the routing decision.
	Reasoning string

	// SubtaskRoutes maps subtask types to recommended models.
	SubtaskRoutes map[string]string
}

// Subtask represents a decomposed part of the original task.
type Subtask struct {
	// ID is a unique identifier for the subtask.
	ID string

	// Description describes what this subtask should accomplish.
	Description string

	// Type categorizes the subtask (e.g., "code_search", "analysis", "generation").
	Type string

	// RecommendedModel is the model best suited for this subtask.
	RecommendedModel string

	// RecommendedTier is the tier best suited for this subtask.
	RecommendedTier meta.ModelTier

	// Dependencies are IDs of subtasks that must complete first.
	Dependencies []string

	// Priority determines execution order (higher = sooner).
	Priority int
}

// Orchestrator handles intelligent prompt pre-processing and task routing.
type Orchestrator struct {
	metaController  *meta.Controller
	contextLoader   *ContextLoader
	replMgr         *repl.Manager
	models          []meta.ModelSpec
	enabled         bool
	contextEnabled  bool // Whether to externalize context to REPL
}

// OrchestratorConfig configures the orchestrator.
type OrchestratorConfig struct {
	// Enabled controls whether orchestration is active.
	Enabled bool

	// Models is the model catalog for routing decisions.
	Models []meta.ModelSpec

	// ContextEnabled enables context externalization to REPL.
	ContextEnabled bool
}

// NewOrchestrator creates a new RLM orchestrator.
func NewOrchestrator(metaCtrl *meta.Controller, cfg OrchestratorConfig) *Orchestrator {
	models := cfg.Models
	if len(models) == 0 {
		models = meta.DefaultModels()
	}

	return &Orchestrator{
		metaController: metaCtrl,
		models:         models,
		enabled:        cfg.Enabled,
		contextEnabled: cfg.ContextEnabled,
	}
}

// SetREPLManager sets the REPL manager for context externalization.
func (o *Orchestrator) SetREPLManager(replMgr *repl.Manager) {
	o.replMgr = replMgr
	if replMgr != nil {
		o.contextLoader = NewContextLoader(replMgr)
	}
}

// ContextLoader returns the context loader.
func (o *Orchestrator) ContextLoader() *ContextLoader {
	return o.contextLoader
}

// IsContextEnabled returns whether context externalization is enabled.
func (o *Orchestrator) IsContextEnabled() bool {
	return o.contextEnabled && o.contextLoader != nil
}

// SetContextEnabled enables or disables context externalization.
func (o *Orchestrator) SetContextEnabled(enabled bool) {
	o.contextEnabled = enabled
}

// Analyze performs intelligent analysis of a user prompt.
// This should be called before sending the prompt to the main agent.
func (o *Orchestrator) Analyze(ctx context.Context, prompt string, contextTokens int) (*AnalysisResult, error) {
	start := time.Now()

	result := &AnalysisResult{
		OriginalPrompt: prompt,
		EnhancedPrompt: prompt,
	}

	if !o.enabled || o.metaController == nil {
		// Orchestration disabled, return minimal result
		result.AnalysisTime = time.Since(start)
		return result, nil
	}

	// Get meta-controller decision
	state := meta.State{
		Task:           prompt,
		ContextTokens:  contextTokens,
		BudgetRemain:   10000, // Default budget
		RecursionDepth: 0,
		MaxDepth:       5,
	}

	decision, err := o.metaController.Decide(ctx, state)
	if err != nil {
		slog.Warn("RLM meta-controller failed, continuing without orchestration", "error", err)
		result.AnalysisTime = time.Since(start)
		return result, nil
	}

	result.Decision = decision

	// Analyze context needs based on the prompt
	result.ContextNeeds = o.analyzeContextNeeds(prompt)

	// Determine routing
	result.Routing = o.determineRouting(prompt, decision)

	// Check if decomposition is recommended
	if decision.Action == meta.ActionDecompose {
		result.ShouldDecompose = true
		result.Subtasks = o.createSubtasks(prompt, decision)
	}

	// Enhance the prompt with orchestration insights
	result.EnhancedPrompt = o.enhancePrompt(prompt, result)

	result.AnalysisTime = time.Since(start)
	result.ModelUsed = "meta-controller"

	slog.Info("RLM analysis complete",
		"action", decision.Action,
		"reasoning", decision.Reasoning,
		"context_needs", len(result.ContextNeeds.FilePatterns)+len(result.ContextNeeds.SearchQueries),
		"should_decompose", result.ShouldDecompose,
		"duration", result.AnalysisTime)

	return result, nil
}

// analyzeContextNeeds identifies what context would help with the task.
func (o *Orchestrator) analyzeContextNeeds(prompt string) *ContextNeeds {
	needs := &ContextNeeds{
		FilePatterns:         []string{},
		SearchQueries:        []string{},
		ConceptsToUnderstand: []string{},
		MemoryQueries:        []string{},
		Priority:             5,
	}

	promptLower := strings.ToLower(prompt)

	// Detect code-related requests
	if containsAny(promptLower, []string{"function", "method", "class", "struct", "interface", "type"}) {
		needs.SearchQueries = append(needs.SearchQueries, extractCodeTerms(prompt)...)
		needs.Priority = 7
	}

	// Detect file-related requests
	if containsAny(promptLower, []string{"file", "module", "package", "import"}) {
		needs.FilePatterns = append(needs.FilePatterns, "**/*.go", "**/*.ts", "**/*.py")
		needs.Priority = 8
	}

	// Detect bug/error investigation
	if containsAny(promptLower, []string{"bug", "error", "fix", "broken", "failing", "crash"}) {
		needs.SearchQueries = append(needs.SearchQueries, "error", "panic", "return err")
		needs.ConceptsToUnderstand = append(needs.ConceptsToUnderstand, "error handling patterns")
		needs.Priority = 9
	}

	// Detect architecture/design questions
	if containsAny(promptLower, []string{"architecture", "design", "structure", "how does", "explain"}) {
		needs.ConceptsToUnderstand = append(needs.ConceptsToUnderstand, "system architecture")
		needs.MemoryQueries = append(needs.MemoryQueries, "design decisions", "architecture")
		needs.Priority = 6
	}

	// Detect refactoring requests
	if containsAny(promptLower, []string{"refactor", "improve", "optimize", "clean up"}) {
		needs.Priority = 7
		needs.ConceptsToUnderstand = append(needs.ConceptsToUnderstand, "existing patterns", "code conventions")
	}

	// Detect test-related requests
	if containsAny(promptLower, []string{"test", "spec", "coverage", "mock"}) {
		needs.FilePatterns = append(needs.FilePatterns, "**/*_test.go", "**/*.test.ts", "**/test_*.py")
		needs.SearchQueries = append(needs.SearchQueries, "func Test")
	}

	return needs
}

// determineRouting decides how to route the task to models.
func (o *Orchestrator) determineRouting(prompt string, decision *meta.Decision) *TaskRouting {
	routing := &TaskRouting{
		PrimaryTier:   meta.TierBalanced,
		SubtaskRoutes: make(map[string]string),
	}

	promptLower := strings.ToLower(prompt)

	// Determine primary tier based on task characteristics
	switch {
	case containsAny(promptLower, []string{"prove", "theorem", "mathematical", "logic", "formal"}):
		routing.PrimaryTier = meta.TierReasoning
		routing.Reasoning = "Task requires deep reasoning/mathematical analysis"

	case containsAny(promptLower, []string{"analyze", "refactor", "architect", "design complex"}):
		routing.PrimaryTier = meta.TierPowerful
		routing.Reasoning = "Task requires complex analysis and deep understanding"

	case containsAny(promptLower, []string{"quick", "simple", "just", "only"}):
		routing.PrimaryTier = meta.TierFast
		routing.Reasoning = "Task is simple and can be handled quickly"

	default:
		routing.PrimaryTier = meta.TierBalanced
		routing.Reasoning = "Task requires moderate complexity handling"
	}

	// Find best model for the tier
	for _, m := range o.models {
		if m.Tier == routing.PrimaryTier {
			routing.PrimaryModel = m.ID
			break
		}
	}

	// Set subtask routing defaults
	routing.SubtaskRoutes["code_search"] = o.findModelForTier(meta.TierFast)
	routing.SubtaskRoutes["analysis"] = o.findModelForTier(meta.TierBalanced)
	routing.SubtaskRoutes["generation"] = o.findModelForTier(meta.TierBalanced)
	routing.SubtaskRoutes["reasoning"] = o.findModelForTier(meta.TierReasoning)
	routing.SubtaskRoutes["synthesis"] = o.findModelForTier(meta.TierPowerful)

	return routing
}

// createSubtasks decomposes a task into subtasks based on the decision.
func (o *Orchestrator) createSubtasks(prompt string, decision *meta.Decision) []Subtask {
	var subtasks []Subtask

	// Use chunks from decision if available
	if len(decision.Params.Chunks) > 0 {
		for i, chunk := range decision.Params.Chunks {
			subtask := Subtask{
				ID:              fmt.Sprintf("subtask-%d", i+1),
				Description:     chunk,
				Type:            string(decision.Params.Strategy),
				RecommendedTier: meta.TierBalanced,
				Priority:        len(decision.Params.Chunks) - i,
			}
			subtask.RecommendedModel = o.findModelForTier(subtask.RecommendedTier)
			subtasks = append(subtasks, subtask)
		}
	} else {
		// Create default decomposition based on strategy
		switch decision.Params.Strategy {
		case meta.StrategyFile:
			subtasks = append(subtasks, Subtask{
				ID:              "gather-files",
				Description:     "Identify and read relevant files",
				Type:            "code_search",
				RecommendedTier: meta.TierFast,
				Priority:        10,
			})
			subtasks = append(subtasks, Subtask{
				ID:              "analyze-files",
				Description:     "Analyze file contents",
				Type:            "analysis",
				RecommendedTier: meta.TierBalanced,
				Dependencies:    []string{"gather-files"},
				Priority:        5,
			})

		case meta.StrategyFunction:
			subtasks = append(subtasks, Subtask{
				ID:              "find-functions",
				Description:     "Locate relevant functions",
				Type:            "code_search",
				RecommendedTier: meta.TierFast,
				Priority:        10,
			})
			subtasks = append(subtasks, Subtask{
				ID:              "analyze-functions",
				Description:     "Analyze function implementations",
				Type:            "analysis",
				RecommendedTier: meta.TierBalanced,
				Dependencies:    []string{"find-functions"},
				Priority:        5,
			})

		case meta.StrategyConcept:
			subtasks = append(subtasks, Subtask{
				ID:              "understand-concepts",
				Description:     "Build understanding of relevant concepts",
				Type:            "reasoning",
				RecommendedTier: meta.TierReasoning,
				Priority:        10,
			})
			subtasks = append(subtasks, Subtask{
				ID:              "apply-concepts",
				Description:     "Apply understanding to the task",
				Type:            "generation",
				RecommendedTier: meta.TierBalanced,
				Dependencies:    []string{"understand-concepts"},
				Priority:        5,
			})
		}

		// Always add synthesis if we have subtasks
		if len(subtasks) > 0 {
			deps := make([]string, len(subtasks))
			for i, st := range subtasks {
				deps[i] = st.ID
			}
			subtasks = append(subtasks, Subtask{
				ID:              "synthesize",
				Description:     "Combine results into final answer",
				Type:            "synthesis",
				RecommendedTier: meta.TierPowerful,
				Dependencies:    deps,
				Priority:        1,
			})
		}
	}

	// Assign models to subtasks
	for i := range subtasks {
		if subtasks[i].RecommendedModel == "" {
			subtasks[i].RecommendedModel = o.findModelForTier(subtasks[i].RecommendedTier)
		}
	}

	return subtasks
}

// ExternalizeContext loads context sources into the REPL for manipulation.
// This is a key RLM feature - context is externalized as Python variables
// that the LLM can explore via code rather than ingesting directly.
func (o *Orchestrator) ExternalizeContext(ctx context.Context, sources []ContextSource) (*LoadedContext, error) {
	if !o.IsContextEnabled() {
		return nil, fmt.Errorf("context externalization not enabled")
	}
	return o.contextLoader.Load(ctx, sources)
}

// ExternalizePrompt stores the user's prompt as a REPL variable.
// This enables the true RLM paradigm where the LLM receives only the query
// and accesses context through code.
func (o *Orchestrator) ExternalizePrompt(ctx context.Context, prompt string) (*LoadedContext, error) {
	return o.ExternalizeContext(ctx, []ContextSource{{
		Name:    "user_query",
		Content: prompt,
		Type:    ContextTypeCustom,
		Metadata: map[string]any{
			"source": "user_input",
		},
	}})
}

// enhancePrompt adds orchestration insights to the prompt.
func (o *Orchestrator) enhancePrompt(prompt string, analysis *AnalysisResult) string {
	if analysis.Decision == nil {
		return prompt
	}

	var sb strings.Builder

	// Add RLM analysis header
	sb.WriteString("<!-- RLM Analysis -->\n")
	sb.WriteString(fmt.Sprintf("<!-- Strategy: %s -->\n", analysis.Decision.Action))
	sb.WriteString(fmt.Sprintf("<!-- Reasoning: %s -->\n", analysis.Decision.Reasoning))

	if analysis.Routing != nil {
		sb.WriteString(fmt.Sprintf("<!-- Recommended tier: %d (%s) -->\n",
			analysis.Routing.PrimaryTier, analysis.Routing.Reasoning))
	}

	if analysis.ContextNeeds != nil && analysis.ContextNeeds.Priority > 5 {
		sb.WriteString("<!-- High priority context needs identified -->\n")
		if len(analysis.ContextNeeds.SearchQueries) > 0 {
			sb.WriteString(fmt.Sprintf("<!-- Suggested searches: %s -->\n",
				strings.Join(analysis.ContextNeeds.SearchQueries, ", ")))
		}
	}

	// Add context externalization info if available
	if analysis.UseExternalizedContext && analysis.LoadedContext != nil {
		sb.WriteString("<!-- Context externalized to REPL -->\n")
		sb.WriteString(o.contextLoader.GenerateContextPrompt(analysis.LoadedContext))
	}

	sb.WriteString("<!-- End RLM Analysis -->\n\n")

	// Original prompt
	sb.WriteString(prompt)

	return sb.String()
}

// GenerateRLMSystemPrompt generates a system prompt for RLM-style execution.
// This instructs the LLM to use externalized context via Python code.
func (o *Orchestrator) GenerateRLMSystemPrompt(loaded *LoadedContext) string {
	var sb strings.Builder

	sb.WriteString(`You are operating in RLM (Recursive Language Model) mode.

In this mode, context is NOT provided directly in your prompt. Instead, context has been
externalized as Python variables that you can explore and manipulate via code execution.

## Key Principles

1. **Context is External**: Do NOT expect context in the prompt. Use Python code to access it.
2. **Explore First**: Use peek(), grep(), and other helpers to understand the context before acting.
3. **Process Incrementally**: For large context, partition it and process chunks.
4. **Sub-LLM Calls**: Use llm_call() for focused subtasks on portions of context.
5. **Final Output**: Call FINAL(response) when you have your answer.

## Available Functions

- peek(ctx, start, end, by_lines=False) - View a slice of context
- grep(ctx, pattern, context_lines=0) - Search for patterns
- partition(ctx, n=4) - Split into n chunks
- partition_by_lines(ctx, n=4) - Split by lines
- extract_functions(ctx, language) - Find function definitions
- count_tokens_approx(text) - Estimate token count
- llm_call(prompt, context, model) - Make sub-LLM call
- llm_batch(prompts, contexts, model) - Batch LLM calls
- FINAL(response) - Mark final output

## Workflow Example

` + "```python" + `
# 1. First, peek at the context to understand it
preview = peek(context, 0, 2000)

# 2. Search for relevant sections
matches = grep(context, r"error|exception", context_lines=2)

# 3. For large context, partition and process
if count_tokens_approx(context) > 10000:
    chunks = partition(context, n=4)
    summaries = llm_batch(
        ["Summarize the key points"] * 4,
        contexts=chunks,
        model="fast"
    )
    combined = "\n".join(summaries)
else:
    combined = context

# 4. Generate final answer
answer = llm_call("Based on this, answer the user's question", combined)
FINAL(answer)
` + "```" + `

`)

	// Add info about loaded context
	if loaded != nil && len(loaded.Variables) > 0 {
		sb.WriteString(o.contextLoader.GenerateContextPrompt(loaded))
	}

	return sb.String()
}

// findModelForTier finds a model ID for the given tier.
func (o *Orchestrator) findModelForTier(tier meta.ModelTier) string {
	for _, m := range o.models {
		if m.Tier == tier {
			return m.ID
		}
	}
	// Fallback
	if len(o.models) > 0 {
		return o.models[0].ID
	}
	return "anthropic/claude-haiku-4.5"
}

// IsEnabled returns whether orchestration is enabled.
func (o *Orchestrator) IsEnabled() bool {
	return o.enabled
}

// SetEnabled enables or disables orchestration.
func (o *Orchestrator) SetEnabled(enabled bool) {
	o.enabled = enabled
}

// Helper functions

func containsAny(s string, substrs []string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func extractCodeTerms(prompt string) []string {
	// Simple extraction of potential code terms
	terms := []string{}
	words := strings.Fields(prompt)
	for _, word := range words {
		// Look for CamelCase or snake_case terms
		if len(word) > 3 && (strings.Contains(word, "_") || hasUpperInMiddle(word)) {
			terms = append(terms, word)
		}
	}
	return terms
}

func hasUpperInMiddle(s string) bool {
	if len(s) < 2 {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			return true
		}
	}
	return false
}
