package lats

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Expander generates candidate actions for a node.
type Expander interface {
	// Expand generates child nodes with candidate actions.
	Expand(ctx context.Context, node *Node) ([]*Node, error)
}

// Simulator executes actions and evaluates outcomes.
type Simulator interface {
	// Simulate executes the node's action and returns (value, tokens, error).
	Simulate(ctx context.Context, node *Node) (float64, int, error)
}

// Valuator estimates the value of a state.
type Valuator interface {
	// Value returns the estimated value of a node's state (0-1).
	Value(ctx context.Context, node *Node) (float64, error)
}

// LLMClient interface for LLM operations.
type LLMClient interface {
	// Complete sends a prompt and returns (response, tokens, error).
	Complete(ctx context.Context, prompt string) (string, int, error)
}

// LLMExpander uses an LLM to generate candidate actions.
type LLMExpander struct {
	client LLMClient
	tools  *ToolRegistry
}

// NewLLMExpander creates an LLM-based expander.
func NewLLMExpander(client LLMClient, tools *ToolRegistry) *LLMExpander {
	return &LLMExpander{
		client: client,
		tools:  tools,
	}
}

// Expand generates candidate actions using the LLM.
func (e *LLMExpander) Expand(ctx context.Context, node *Node) ([]*Node, error) {
	prompt := e.buildExpansionPrompt(node)

	response, _, err := e.client.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}

	actions := e.parseActions(response)
	children := make([]*Node, 0, len(actions))

	for _, action := range actions {
		// Validate action uses available tool
		if e.tools != nil && !e.tools.Has(action.Tool) {
			continue
		}

		child := &Node{
			Action: action,
			State:  node.State.Clone(),
		}
		children = append(children, child)
	}

	return children, nil
}

func (e *LLMExpander) buildExpansionPrompt(node *Node) string {
	var history strings.Builder
	if node.State != nil {
		for _, obs := range node.State.Observations {
			status := "SUCCESS"
			if !obs.Success {
				status = "FAILED"
			}
			history.WriteString(fmt.Sprintf("Action: %s(%s) [%s]\nResult: %s\n\n",
				obs.Action.Tool, truncate(obs.Action.Input, 100), status, truncate(obs.Result, 500)))
		}
	}

	var toolDesc string
	if e.tools != nil {
		toolDesc = e.tools.Describe()
	} else {
		toolDesc = "- repl: Execute Python code\n- search: Search memory\n- file: Read/write files"
	}

	query := ""
	if node.State != nil {
		query = node.State.Query
	}

	return fmt.Sprintf(`You are solving this task: %s

Previous actions and results:
%s

Available tools:
%s

Generate 3-5 different next actions to try. For each action, explain your reasoning.

Format your response as:
[Action 1]
Tool: <tool_name>
Input: <tool_input>
Reasoning: <why this action>

[Action 2]
Tool: <tool_name>
Input: <tool_input>
Reasoning: <why this action>
`, query, history.String(), toolDesc)
}

func (e *LLMExpander) parseActions(response string) []*Action {
	var actions []*Action

	// Parse action blocks
	actionRe := regexp.MustCompile(`(?s)\[Action \d+\].*?Tool:\s*(\S+).*?Input:\s*(.+?)(?:\n|$).*?Reasoning:\s*(.+?)(?:\n\n|\[Action|\z)`)
	matches := actionRe.FindAllStringSubmatch(response, -1)

	for _, match := range matches {
		if len(match) >= 4 {
			action := &Action{
				Tool:      strings.TrimSpace(match[1]),
				Input:     strings.TrimSpace(match[2]),
				Reasoning: strings.TrimSpace(match[3]),
			}
			actions = append(actions, action)
		}
	}

	// Fallback: try simpler parsing
	if len(actions) == 0 {
		toolRe := regexp.MustCompile(`Tool:\s*(\S+)`)
		inputRe := regexp.MustCompile(`Input:\s*(.+?)(?:\n|$)`)

		toolMatches := toolRe.FindAllStringSubmatch(response, -1)
		inputMatches := inputRe.FindAllStringSubmatch(response, -1)

		for i := 0; i < len(toolMatches) && i < len(inputMatches); i++ {
			action := &Action{
				Tool:  strings.TrimSpace(toolMatches[i][1]),
				Input: strings.TrimSpace(inputMatches[i][1]),
			}
			actions = append(actions, action)
		}
	}

	return actions
}

// RealSimulator executes actions using the tool registry.
type RealSimulator struct {
	tools    *ToolRegistry
	valuator Valuator
}

// NewRealSimulator creates a simulator that executes real tools.
func NewRealSimulator(tools *ToolRegistry, valuator Valuator) *RealSimulator {
	return &RealSimulator{
		tools:    tools,
		valuator: valuator,
	}
}

// Simulate executes the node's action and evaluates the result.
func (s *RealSimulator) Simulate(ctx context.Context, node *Node) (float64, int, error) {
	if node.Action == nil {
		return 0.5, 0, nil
	}

	start := time.Now()

	// Execute the tool action
	var result *ToolResult
	var execErr error

	if s.tools != nil {
		result, execErr = s.tools.Execute(ctx, node.Action.Tool, node.Action.Input)
	} else {
		// Mock execution for testing
		result = &ToolResult{
			Output:  fmt.Sprintf("Mock result for %s", node.Action.Tool),
			Success: true,
		}
	}

	observation := Observation{
		Action:   node.Action,
		Duration: time.Since(start),
	}

	if execErr != nil {
		observation.Result = fmt.Sprintf("Error: %v", execErr)
		observation.Success = false
	} else if result != nil {
		observation.Result = result.Output
		observation.Success = result.Success
		observation.Tokens = result.Tokens
	}

	// Update node state
	if node.State == nil {
		node.State = &AgentState{}
	}
	node.State.Observations = append(node.State.Observations, observation)
	node.State.TokensUsed += observation.Tokens

	// Check for terminal state
	node.IsTerminal = s.isTerminal(node)

	// Evaluate state value
	var value float64
	if s.valuator != nil {
		var err error
		value, err = s.valuator.Value(ctx, node)
		if err != nil {
			// Use heuristic on error
			if observation.Success {
				value = 0.6
			} else {
				value = 0.2
			}
		}
	} else {
		// Default heuristic
		if observation.Success {
			value = 0.6
		} else {
			value = 0.2
		}
	}

	return value, observation.Tokens, nil
}

func (s *RealSimulator) isTerminal(node *Node) bool {
	// Terminal if max depth reached
	if node.Depth >= 10 {
		return true
	}

	// Check if last observation indicates completion
	if node.State != nil && len(node.State.Observations) > 0 {
		last := node.State.Observations[len(node.State.Observations)-1]
		result := strings.ToLower(last.Result)
		return strings.Contains(result, "final_answer") ||
			strings.Contains(result, "task_complete") ||
			strings.Contains(result, "solution:")
	}

	return false
}

// MockSimulator simulates without executing real tools.
type MockSimulator struct {
	// ValueFunc computes value for a node.
	ValueFunc func(*Node) float64

	// TerminalFunc determines if node is terminal.
	TerminalFunc func(*Node) bool
}

// Simulate returns mock values.
func (s *MockSimulator) Simulate(ctx context.Context, node *Node) (float64, int, error) {
	value := 0.5
	if s.ValueFunc != nil {
		value = s.ValueFunc(node)
	}

	if s.TerminalFunc != nil {
		node.IsTerminal = s.TerminalFunc(node)
	}

	// Add mock observation
	if node.Action != nil {
		if node.State == nil {
			node.State = &AgentState{}
		}
		node.State.Observations = append(node.State.Observations, Observation{
			Action:  node.Action,
			Result:  fmt.Sprintf("Mock result for %s", node.Action.Tool),
			Success: true,
			Tokens:  10,
		})
	}

	return value, 10, nil
}

// LLMValuator uses an LLM to evaluate states.
type LLMValuator struct {
	client LLMClient
}

// NewLLMValuator creates an LLM-based valuator.
func NewLLMValuator(client LLMClient) *LLMValuator {
	return &LLMValuator{client: client}
}

// Value evaluates the node's state using the LLM.
func (v *LLMValuator) Value(ctx context.Context, node *Node) (float64, error) {
	prompt := v.buildValuePrompt(node)

	response, _, err := v.client.Complete(ctx, prompt)
	if err != nil {
		return 0, fmt.Errorf("llm complete: %w", err)
	}

	return v.parseValue(response), nil
}

func (v *LLMValuator) buildValuePrompt(node *Node) string {
	var history strings.Builder
	if node.State != nil {
		for _, obs := range node.State.Observations {
			status := "✓"
			if !obs.Success {
				status = "✗"
			}
			history.WriteString(fmt.Sprintf("- %s %s: %s\n",
				status, obs.Action.Tool, truncate(obs.Result, 100)))
		}
	}

	query := ""
	if node.State != nil {
		query = node.State.Query
	}

	return fmt.Sprintf(`Task: %s

Actions taken:
%s

Rate the current progress toward solving the task on a scale of 0.0 to 1.0:
- 0.0: No progress, wrong direction
- 0.5: Some progress, unclear if right path
- 1.0: Task appears solved

Provide a single number between 0.0 and 1.0:`, query, history.String())
}

func (v *LLMValuator) parseValue(response string) float64 {
	// Extract first number from response
	numRe := regexp.MustCompile(`([0-9]*\.?[0-9]+)`)
	match := numRe.FindStringSubmatch(response)

	if len(match) > 1 {
		if val, err := strconv.ParseFloat(match[1], 64); err == nil {
			return clamp(val, 0, 1)
		}
	}

	return 0.5 // Default neutral value
}

// HeuristicValuator uses rules instead of LLM.
type HeuristicValuator struct {
	SuccessBonus   float64
	FailurePenalty float64
	DepthPenalty   float64
}

// NewHeuristicValuator creates a heuristic valuator with defaults.
func NewHeuristicValuator() *HeuristicValuator {
	return &HeuristicValuator{
		SuccessBonus:   0.15,
		FailurePenalty: 0.2,
		DepthPenalty:   0.02,
	}
}

// Value computes value using heuristics.
func (v *HeuristicValuator) Value(ctx context.Context, node *Node) (float64, error) {
	value := 0.5 // Neutral baseline

	if node.State != nil {
		// Reward successful actions
		for _, obs := range node.State.Observations {
			if obs.Success {
				value += v.SuccessBonus
			} else {
				value -= v.FailurePenalty
			}
		}
	}

	// Penalize deep searches
	value -= float64(node.Depth) * v.DepthPenalty

	return clamp(value, 0, 1), nil
}

// MockExpander generates deterministic expansions for testing.
type MockExpander struct {
	// ActionsPerNode is how many actions to generate.
	ActionsPerNode int

	// Tools is the list of available tools.
	Tools []string
}

// Expand generates mock child nodes.
func (e *MockExpander) Expand(ctx context.Context, node *Node) ([]*Node, error) {
	count := e.ActionsPerNode
	if count <= 0 {
		count = 3
	}

	tools := e.Tools
	if len(tools) == 0 {
		tools = []string{"repl", "search", "file"}
	}

	children := make([]*Node, count)
	for i := 0; i < count; i++ {
		tool := tools[i%len(tools)]
		children[i] = &Node{
			Action: &Action{
				Tool:      tool,
				Input:     fmt.Sprintf("input-%d-depth-%d", i, node.Depth+1),
				Reasoning: fmt.Sprintf("Try %s at depth %d", tool, node.Depth+1),
			},
			State: node.State.Clone(),
		}
	}

	return children, nil
}

// Helper functions

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// Ensure math import is used
var _ = math.Inf
