// Package meta implements the RLM meta-controller using Claude Haiku for orchestration decisions.
package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Action represents the orchestration action to take.
type Action string

const (
	// ActionDirect answers directly using current context.
	ActionDirect Action = "DIRECT"

	// ActionDecompose breaks the task into subtasks.
	ActionDecompose Action = "DECOMPOSE"

	// ActionMemoryQuery retrieves from hypergraph memory.
	ActionMemoryQuery Action = "MEMORY_QUERY"

	// ActionSubcall invokes sub-LM on a specific snippet.
	ActionSubcall Action = "SUBCALL"

	// ActionSynthesize combines existing partial results.
	ActionSynthesize Action = "SYNTHESIZE"
)

// DecomposeStrategy specifies how to break down a task.
type DecomposeStrategy string

const (
	StrategyFile     DecomposeStrategy = "file"
	StrategyFunction DecomposeStrategy = "function"
	StrategyConcept  DecomposeStrategy = "concept"
	StrategyCustom   DecomposeStrategy = "custom"
)

// Decision represents an orchestration decision from the meta-controller.
type Decision struct {
	Action    Action          `json:"action"`
	Params    DecisionParams  `json:"params"`
	Reasoning string          `json:"reasoning"`
}

// DecisionParams contains action-specific parameters.
type DecisionParams struct {
	// For DECOMPOSE
	Strategy DecomposeStrategy `json:"strategy,omitempty"`
	Chunks   []string          `json:"chunks,omitempty"`

	// For MEMORY_QUERY
	Query string `json:"query,omitempty"`

	// For SUBCALL
	Prompt  string `json:"prompt,omitempty"`
	Snippet string `json:"snippet,omitempty"`

	// For budget allocation
	TokenBudget int `json:"token_budget,omitempty"`
}

// State represents the current orchestration state.
type State struct {
	Task           string   `json:"task"`
	ContextTokens  int      `json:"context_tokens"`
	BudgetRemain   int      `json:"budget_remaining"`
	RecursionDepth int      `json:"recursion_depth"`
	MaxDepth       int      `json:"max_depth"`
	MemoryHints    []string `json:"memory_hints,omitempty"`
	PartialResults []string `json:"partial_results,omitempty"`
}

// LLMClient interface for making LLM calls.
type LLMClient interface {
	// Complete sends a prompt and returns the completion.
	Complete(ctx context.Context, prompt string, maxTokens int) (string, error)
}

// Controller is the meta-controller that decides orchestration strategy.
type Controller struct {
	client       LLMClient
	maxDepth     int
	systemPrompt string
}

// Config configures the meta-controller.
type Config struct {
	// MaxDepth is the maximum recursion depth (default 5).
	MaxDepth int

	// SystemPrompt overrides the default system prompt.
	SystemPrompt string
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		MaxDepth: 5,
	}
}

// NewController creates a new meta-controller.
func NewController(client LLMClient, cfg Config) *Controller {
	if cfg.MaxDepth == 0 {
		cfg.MaxDepth = 5
	}

	systemPrompt := cfg.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}

	return &Controller{
		client:       client,
		maxDepth:     cfg.MaxDepth,
		systemPrompt: systemPrompt,
	}
}

// Decide makes an orchestration decision given the current state.
func (c *Controller) Decide(ctx context.Context, state State) (*Decision, error) {
	if state.MaxDepth == 0 {
		state.MaxDepth = c.maxDepth
	}

	// Check termination conditions
	if state.RecursionDepth >= state.MaxDepth {
		return &Decision{
			Action:    ActionDirect,
			Reasoning: "Maximum recursion depth reached, must answer directly",
		}, nil
	}

	if state.BudgetRemain <= 0 {
		return &Decision{
			Action:    ActionDirect,
			Reasoning: "Budget exhausted, must answer directly",
		}, nil
	}

	// Build prompt for meta-controller
	prompt := c.buildPrompt(state)

	// Call LLM
	response, err := c.client.Complete(ctx, prompt, 500)
	if err != nil {
		return nil, fmt.Errorf("meta-controller call: %w", err)
	}

	// Parse decision
	decision, err := parseDecision(response)
	if err != nil {
		// Fallback to direct if parsing fails
		return &Decision{
			Action:    ActionDirect,
			Reasoning: fmt.Sprintf("Failed to parse meta-controller response: %v", err),
		}, nil
	}

	return decision, nil
}

// buildPrompt constructs the prompt for the meta-controller.
func (c *Controller) buildPrompt(state State) string {
	var sb strings.Builder

	sb.WriteString(c.systemPrompt)
	sb.WriteString("\n\n")

	sb.WriteString("Current state:\n")
	sb.WriteString(fmt.Sprintf("- Task: %s\n", state.Task))
	sb.WriteString(fmt.Sprintf("- Context size: %d tokens\n", state.ContextTokens))
	sb.WriteString(fmt.Sprintf("- Budget remaining: %d tokens\n", state.BudgetRemain))
	sb.WriteString(fmt.Sprintf("- Recursion depth: %d/%d\n", state.RecursionDepth, state.MaxDepth))

	if len(state.MemoryHints) > 0 {
		sb.WriteString(fmt.Sprintf("- Memory hints: %s\n", strings.Join(state.MemoryHints, ", ")))
	}

	if len(state.PartialResults) > 0 {
		sb.WriteString(fmt.Sprintf("- Partial results available: %d\n", len(state.PartialResults)))
	}

	sb.WriteString("\n")
	sb.WriteString("Options:\n")
	sb.WriteString("1. DIRECT - Answer directly using current context\n")
	sb.WriteString("2. DECOMPOSE - Break into subtasks with strategy: file|function|concept|custom\n")
	sb.WriteString("3. MEMORY_QUERY - Retrieve from hypergraph memory\n")
	sb.WriteString("4. SUBCALL - Invoke sub-LM on specific snippet\n")
	sb.WriteString("5. SYNTHESIZE - Combine existing partial results\n")
	sb.WriteString("\n")
	sb.WriteString(`Output JSON: {"action": "...", "params": {...}, "reasoning": "..."}`)

	return sb.String()
}

// parseDecision extracts a Decision from the LLM response.
func parseDecision(response string) (*Decision, error) {
	// Find JSON in response (may be wrapped in markdown code blocks)
	response = strings.TrimSpace(response)

	// Try to find JSON object
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")

	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON found in response")
	}

	jsonStr := response[start : end+1]

	var decision Decision
	if err := json.Unmarshal([]byte(jsonStr), &decision); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	// Validate action
	switch decision.Action {
	case ActionDirect, ActionDecompose, ActionMemoryQuery, ActionSubcall, ActionSynthesize:
		// Valid
	default:
		return nil, fmt.Errorf("unknown action: %s", decision.Action)
	}

	return &decision, nil
}

// MaxDepth returns the configured maximum recursion depth.
func (c *Controller) MaxDepth() int {
	return c.maxDepth
}

const defaultSystemPrompt = `You are an orchestration controller for a recursive language model system.
Your job is to decide the best strategy for handling a task given the current context and constraints.

Guidelines:
- Use DIRECT when the task is simple enough to answer with current context
- Use DECOMPOSE when the task involves multiple files, functions, or concepts that should be processed separately
- Use MEMORY_QUERY when you need to recall previously learned facts or context
- Use SUBCALL when you need to process a specific snippet with a focused prompt
- Use SYNTHESIZE when you have partial results that need to be combined

Consider:
- Budget constraints: don't decompose if budget is low
- Recursion depth: prefer simpler strategies as depth increases
- Context size: decompose large contexts to avoid overwhelming the model
- Task complexity: match strategy to task requirements`
