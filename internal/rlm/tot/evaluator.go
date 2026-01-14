package tot

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Evaluator evaluates thought nodes to determine their quality.
type Evaluator interface {
	// EvaluateThought returns the evaluation of a thought node.
	EvaluateThought(ctx context.Context, node *ThoughtNode) (*EvaluationResult, error)
}

// EvaluationResult contains the evaluation of a thought.
type EvaluationResult struct {
	// Value is the quality score (0-1).
	Value float64

	// Confidence is how confident the evaluation is (0-1).
	Confidence float64

	// IsTerminal indicates if this is a solution state.
	IsTerminal bool

	// Reasoning explains the evaluation.
	Reasoning string

	// Critique identifies issues with the thought.
	Critique string
}

// Generator generates child thoughts from a node.
type Generator interface {
	// GenerateThoughts generates up to n child thoughts.
	GenerateThoughts(ctx context.Context, node *ThoughtNode, n int) ([]string, error)
}

// LLMClient is the interface for LLM operations.
type LLMClient interface {
	// Complete sends a prompt and returns the response.
	Complete(ctx context.Context, prompt string, maxTokens int) (string, error)
}

// LLMEvaluator uses an LLM to evaluate thoughts.
type LLMEvaluator struct {
	client       LLMClient
	systemPrompt string
}

// NewLLMEvaluator creates an LLM-based evaluator.
func NewLLMEvaluator(client LLMClient) *LLMEvaluator {
	return &LLMEvaluator{
		client: client,
		systemPrompt: `You are evaluating a reasoning step in a problem-solving process.
Rate the quality of this thought step on a scale of 0 to 1, where:
- 0.0-0.3: Poor reasoning, incorrect, or unhelpful
- 0.3-0.5: Partial progress but has issues
- 0.5-0.7: Reasonable step, on the right track
- 0.7-0.9: Good reasoning, likely leads to solution
- 0.9-1.0: Excellent, directly solves or nearly solves the problem

Respond in this exact format:
VALUE: [0.0-1.0]
CONFIDENCE: [0.0-1.0]
TERMINAL: [true/false]
REASONING: [brief explanation]
CRITIQUE: [any issues or concerns]`,
	}
}

// EvaluateThought evaluates a thought using the LLM.
func (e *LLMEvaluator) EvaluateThought(ctx context.Context, node *ThoughtNode) (*EvaluationResult, error) {
	// Build context from path
	path := node.PathThoughts()
	var contextBuilder strings.Builder

	contextBuilder.WriteString("Problem: ")
	if len(path) > 0 {
		contextBuilder.WriteString(path[0])
	}
	contextBuilder.WriteString("\n\nReasoning steps so far:\n")

	for i, thought := range path[1:] {
		contextBuilder.WriteString(fmt.Sprintf("%d. %s\n", i+1, thought))
	}

	prompt := fmt.Sprintf("%s\n\n%s\n\nEvaluate the last reasoning step.",
		e.systemPrompt, contextBuilder.String())

	response, err := e.client.Complete(ctx, prompt, 500)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}

	return parseEvaluationResponse(response)
}

// parseEvaluationResponse parses the LLM response into an EvaluationResult.
func parseEvaluationResponse(response string) (*EvaluationResult, error) {
	result := &EvaluationResult{
		Value:      0.5,
		Confidence: 0.5,
	}

	// Parse VALUE
	valueRe := regexp.MustCompile(`VALUE:\s*([\d.]+)`)
	if matches := valueRe.FindStringSubmatch(response); len(matches) > 1 {
		if v, err := strconv.ParseFloat(matches[1], 64); err == nil {
			result.Value = clamp(v, 0, 1)
		}
	}

	// Parse CONFIDENCE
	confRe := regexp.MustCompile(`CONFIDENCE:\s*([\d.]+)`)
	if matches := confRe.FindStringSubmatch(response); len(matches) > 1 {
		if v, err := strconv.ParseFloat(matches[1], 64); err == nil {
			result.Confidence = clamp(v, 0, 1)
		}
	}

	// Parse TERMINAL
	termRe := regexp.MustCompile(`(?i)TERMINAL:\s*(true|false)`)
	if matches := termRe.FindStringSubmatch(response); len(matches) > 1 {
		result.IsTerminal = strings.ToLower(matches[1]) == "true"
	}

	// Parse REASONING
	reasonRe := regexp.MustCompile(`REASONING:\s*(.+?)(?:\n|CRITIQUE:|$)`)
	if matches := reasonRe.FindStringSubmatch(response); len(matches) > 1 {
		result.Reasoning = strings.TrimSpace(matches[1])
	}

	// Parse CRITIQUE
	critiqueRe := regexp.MustCompile(`CRITIQUE:\s*(.+?)$`)
	if matches := critiqueRe.FindStringSubmatch(response); len(matches) > 1 {
		result.Critique = strings.TrimSpace(matches[1])
	}

	return result, nil
}

// LLMGenerator uses an LLM to generate child thoughts.
type LLMGenerator struct {
	client       LLMClient
	systemPrompt string
}

// NewLLMGenerator creates an LLM-based thought generator.
func NewLLMGenerator(client LLMClient) *LLMGenerator {
	return &LLMGenerator{
		client: client,
		systemPrompt: `You are generating possible next reasoning steps for a problem.
Given the problem and current reasoning path, generate diverse next steps.
Each step should explore a different approach or direction.
Be creative but stay relevant to solving the problem.

Format your response as a numbered list:
1. [first possible next step]
2. [second possible next step]
3. [third possible next step]`,
	}
}

// GenerateThoughts generates child thoughts using the LLM.
func (g *LLMGenerator) GenerateThoughts(ctx context.Context, node *ThoughtNode, n int) ([]string, error) {
	// Build context from path
	path := node.PathThoughts()
	var contextBuilder strings.Builder

	contextBuilder.WriteString("Problem: ")
	if len(path) > 0 {
		contextBuilder.WriteString(path[0])
	}
	contextBuilder.WriteString("\n\nReasoning steps so far:\n")

	for i, thought := range path[1:] {
		contextBuilder.WriteString(fmt.Sprintf("%d. %s\n", i+1, thought))
	}

	prompt := fmt.Sprintf("%s\n\n%s\n\nGenerate %d possible next reasoning steps.",
		g.systemPrompt, contextBuilder.String(), n)

	response, err := g.client.Complete(ctx, prompt, 1000)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}

	return parseThoughtList(response, n), nil
}

// parseThoughtList extracts numbered thoughts from response.
func parseThoughtList(response string, maxN int) []string {
	// Match numbered list items
	re := regexp.MustCompile(`\d+\.\s*(.+?)(?:\n|$)`)
	matches := re.FindAllStringSubmatch(response, -1)

	thoughts := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			thought := strings.TrimSpace(match[1])
			if thought != "" {
				thoughts = append(thoughts, thought)
				if len(thoughts) >= maxN {
					break
				}
			}
		}
	}

	return thoughts
}

// MockEvaluator is a simple evaluator for testing.
type MockEvaluator struct {
	// ValueFunc computes value from node properties.
	ValueFunc func(*ThoughtNode) float64

	// TerminalFunc determines if node is terminal.
	TerminalFunc func(*ThoughtNode) bool
}

// EvaluateThought evaluates using the mock functions.
func (e *MockEvaluator) EvaluateThought(_ context.Context, node *ThoughtNode) (*EvaluationResult, error) {
	value := 0.5
	if e.ValueFunc != nil {
		value = e.ValueFunc(node)
	}

	isTerminal := false
	if e.TerminalFunc != nil {
		isTerminal = e.TerminalFunc(node)
	}

	return &EvaluationResult{
		Value:      value,
		Confidence: 0.8,
		IsTerminal: isTerminal,
		Reasoning:  "mock evaluation",
	}, nil
}

// MockGenerator generates thoughts deterministically for testing.
type MockGenerator struct {
	// ThoughtsFunc generates thoughts based on parent.
	ThoughtsFunc func(*ThoughtNode, int) []string
}

// GenerateThoughts generates using the mock function.
func (g *MockGenerator) GenerateThoughts(_ context.Context, node *ThoughtNode, n int) ([]string, error) {
	if g.ThoughtsFunc != nil {
		return g.ThoughtsFunc(node, n), nil
	}

	// Default: generate simple numbered thoughts
	thoughts := make([]string, n)
	for i := 0; i < n; i++ {
		thoughts[i] = fmt.Sprintf("Thought %d from depth %d", i+1, node.Depth)
	}
	return thoughts, nil
}

// HeuristicEvaluator uses simple heuristics for evaluation.
type HeuristicEvaluator struct {
	// Keywords that indicate progress.
	PositiveKeywords []string

	// Keywords that indicate issues.
	NegativeKeywords []string

	// TerminalPatterns match solution states.
	TerminalPatterns []*regexp.Regexp
}

// NewHeuristicEvaluator creates a heuristic evaluator with defaults.
func NewHeuristicEvaluator() *HeuristicEvaluator {
	return &HeuristicEvaluator{
		PositiveKeywords: []string{
			"solution", "answer", "result", "therefore", "conclude",
			"found", "solved", "correct", "verified",
		},
		NegativeKeywords: []string{
			"incorrect", "wrong", "error", "mistake", "invalid",
			"impossible", "cannot", "stuck",
		},
		TerminalPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)the answer is`),
			regexp.MustCompile(`(?i)solution:\s*\S+`),
			regexp.MustCompile(`(?i)therefore,?\s+\S+`),
		},
	}
}

// EvaluateThought evaluates using keyword heuristics.
func (e *HeuristicEvaluator) EvaluateThought(_ context.Context, node *ThoughtNode) (*EvaluationResult, error) {
	thought := strings.ToLower(node.Thought)

	// Count positive/negative indicators
	var positiveCount, negativeCount int

	for _, kw := range e.PositiveKeywords {
		if strings.Contains(thought, kw) {
			positiveCount++
		}
	}

	for _, kw := range e.NegativeKeywords {
		if strings.Contains(thought, kw) {
			negativeCount++
		}
	}

	// Calculate value
	total := positiveCount + negativeCount
	value := 0.5
	if total > 0 {
		value = float64(positiveCount) / float64(total)
	}

	// Adjust by depth (deeper = more progress)
	depthBonus := float64(node.Depth) * 0.05
	value = clamp(value+depthBonus, 0, 1)

	// Check for terminal patterns
	isTerminal := false
	for _, pattern := range e.TerminalPatterns {
		if pattern.MatchString(node.Thought) {
			isTerminal = true
			break
		}
	}

	return &EvaluationResult{
		Value:      value,
		Confidence: 0.6, // Heuristics have moderate confidence
		IsTerminal: isTerminal,
		Reasoning:  fmt.Sprintf("positive: %d, negative: %d", positiveCount, negativeCount),
	}, nil
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
