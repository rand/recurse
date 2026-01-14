package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rand/recurse/internal/rlm/repl"
)

// Steering handles user interaction and context externalization.
type Steering struct {
	replMgr        *repl.Manager
	contextLoader  *ContextLoader
	contextEnabled bool
}

// SteeringConfig configures steering behavior.
type SteeringConfig struct {
	// ContextEnabled enables context externalization to REPL.
	ContextEnabled bool
}

// NewSteering creates a new steering handler.
func NewSteering(cfg SteeringConfig) *Steering {
	return &Steering{
		contextEnabled: cfg.ContextEnabled,
	}
}

// SetREPLManager sets the REPL manager for context externalization.
func (s *Steering) SetREPLManager(replMgr *repl.Manager) {
	s.replMgr = replMgr
	if replMgr != nil {
		s.contextLoader = NewContextLoader(replMgr)
	}
}

// ContextLoader returns the context loader.
func (s *Steering) ContextLoader() *ContextLoader {
	return s.contextLoader
}

// IsContextEnabled returns whether context externalization is enabled.
func (s *Steering) IsContextEnabled() bool {
	return s.contextEnabled && s.contextLoader != nil
}

// SetContextEnabled enables or disables context externalization.
func (s *Steering) SetContextEnabled(enabled bool) {
	s.contextEnabled = enabled
}

// HasREPL returns whether a REPL manager is available.
func (s *Steering) HasREPL() bool {
	return s.replMgr != nil
}

// ExternalizeContext loads context sources into the REPL for manipulation.
// This is a key RLM feature - context is externalized as Python variables
// that the LLM can explore via code rather than ingesting directly.
func (s *Steering) ExternalizeContext(ctx context.Context, sources []ContextSource) (*LoadedContext, error) {
	if !s.IsContextEnabled() {
		return nil, fmt.Errorf("context externalization not enabled")
	}
	return s.contextLoader.Load(ctx, sources)
}

// ExternalizePrompt stores the user's prompt as a REPL variable.
// This enables the true RLM paradigm where the LLM receives only the query
// and accesses context through code.
func (s *Steering) ExternalizePrompt(ctx context.Context, prompt string) (*LoadedContext, error) {
	return s.ExternalizeContext(ctx, []ContextSource{{
		Name:    "user_query",
		Content: prompt,
		Type:    ContextTypeCustom,
		Metadata: map[string]any{
			"source": "user_input",
		},
	}})
}

// EnhancePrompt adds orchestration insights to the prompt.
func (s *Steering) EnhancePrompt(prompt string, analysis *AnalysisResult) string {
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
	if analysis.UseExternalizedContext && analysis.LoadedContext != nil && s.contextLoader != nil {
		sb.WriteString("<!-- Context externalized to REPL -->\n")
		sb.WriteString(s.contextLoader.GenerateContextPrompt(analysis.LoadedContext))
	}

	sb.WriteString("<!-- End RLM Analysis -->\n\n")

	// Original prompt
	sb.WriteString(prompt)

	return sb.String()
}

// GenerateRLMSystemPrompt generates a system prompt for RLM-style execution.
// This instructs the LLM to use externalized context via Python code.
func (s *Steering) GenerateRLMSystemPrompt(loaded *LoadedContext) string {
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
	if loaded != nil && len(loaded.Variables) > 0 && s.contextLoader != nil {
		sb.WriteString(s.contextLoader.GenerateContextPrompt(loaded))
	}

	return sb.String()
}

// ContextLoader handles loading context into the REPL.
type ContextLoader struct {
	replMgr *repl.Manager
}

// NewContextLoader creates a new context loader.
func NewContextLoader(replMgr *repl.Manager) *ContextLoader {
	return &ContextLoader{replMgr: replMgr}
}

// Load loads context sources into the REPL.
func (cl *ContextLoader) Load(ctx context.Context, sources []ContextSource) (*LoadedContext, error) {
	loaded := &LoadedContext{
		Variables: make(map[string]VariableInfo),
		LoadTime:  time.Now(),
	}

	for _, src := range sources {
		// Create Python assignment code
		code := fmt.Sprintf("%s = %q", src.Name, src.Content)

		// Execute in REPL
		_, err := cl.replMgr.Execute(ctx, code)
		if err != nil {
			return nil, fmt.Errorf("load context %s: %w", src.Name, err)
		}

		// Track variable info
		tokens := len(src.Content) / 4
		loaded.Variables[src.Name] = VariableInfo{
			Name:          src.Name,
			Type:          src.Type,
			Size:          len(src.Content),
			TokenEstimate: tokens,
			Metadata:      src.Metadata,
		}
		loaded.TotalTokens += tokens
	}

	return loaded, nil
}

// GenerateContextPrompt generates a prompt section describing loaded context.
func (cl *ContextLoader) GenerateContextPrompt(loaded *LoadedContext) string {
	if loaded == nil || len(loaded.Variables) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## Loaded Context Variables\n\n")

	for name, info := range loaded.Variables {
		sb.WriteString(fmt.Sprintf("- `%s` (%s): ~%d tokens\n",
			name, info.Type, info.TokenEstimate))
	}

	sb.WriteString(fmt.Sprintf("\nTotal: ~%d tokens externalized\n", loaded.TotalTokens))

	return sb.String()
}

// ClearContext clears all context variables from the REPL.
func (cl *ContextLoader) ClearContext(ctx context.Context, varNames []string) error {
	if len(varNames) == 0 {
		return nil
	}
	deleteCode := "del " + strings.Join(varNames, ", ")
	_, err := cl.replMgr.Execute(ctx, deleteCode)
	return err
}
