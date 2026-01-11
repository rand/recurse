package meta

import (
	"context"
	"fmt"
	"os"
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openrouter"
)

// ModelTier represents the capability tier of a model.
type ModelTier int

const (
	// TierFast is for quick, simple tasks (cheapest, fastest).
	TierFast ModelTier = iota
	// TierBalanced is for moderate complexity tasks.
	TierBalanced
	// TierPowerful is for complex reasoning tasks.
	TierPowerful
	// TierReasoning is for tasks requiring deep reasoning/thinking.
	TierReasoning
)

// ModelSpec defines a model's characteristics.
type ModelSpec struct {
	ID          string
	Name        string
	Tier        ModelTier
	InputCost   float64 // per million tokens
	OutputCost  float64 // per million tokens
	ContextSize int
	Strengths   []string
}

// DefaultModels returns the default model catalog for OpenRouter routing.
// Updated January 2026 with latest models and pricing from OpenRouter.
func DefaultModels() []ModelSpec {
	return []ModelSpec{
		// ============================================================
		// Fast tier - for simple orchestration decisions, low latency
		// ============================================================
		{
			ID:          "anthropic/claude-haiku-4.5",
			Name:        "Claude Haiku 4.5",
			Tier:        TierFast,
			InputCost:   1.00,
			OutputCost:  5.00,
			ContextSize: 200000,
			Strengths:   []string{"fast", "orchestration", "efficient"},
		},
		{
			ID:          "google/gemini-2.5-flash-lite",
			Name:        "Gemini 2.5 Flash Lite",
			Tier:        TierFast,
			InputCost:   0.10,
			OutputCost:  0.40,
			ContextSize: 1050000,
			Strengths:   []string{"fast", "very-cheap", "large-context"},
		},
		{
			ID:          "google/gemini-2.0-flash-001",
			Name:        "Gemini 2.0 Flash",
			Tier:        TierFast,
			InputCost:   0.10,
			OutputCost:  0.40,
			ContextSize: 1050000,
			Strengths:   []string{"fast", "very-cheap", "large-context"},
		},
		{
			ID:          "qwen/qwen3-8b",
			Name:        "Qwen3 8B",
			Tier:        TierFast,
			InputCost:   0.035,
			OutputCost:  0.138,
			ContextSize: 128000,
			Strengths:   []string{"fast", "very-cheap", "multilingual"},
		},
		{
			ID:          "openai/gpt-5-mini",
			Name:        "GPT-5 Mini",
			Tier:        TierFast,
			InputCost:   0.30,
			OutputCost:  1.20,
			ContextSize: 200000,
			Strengths:   []string{"fast", "reasoning", "tool-use"},
		},

		// ============================================================
		// Balanced tier - for moderate complexity, good cost/performance
		// ============================================================
		{
			ID:          "anthropic/claude-sonnet-4.5",
			Name:        "Claude Sonnet 4.5",
			Tier:        TierBalanced,
			InputCost:   3.00,
			OutputCost:  15.00,
			ContextSize: 1000000,
			Strengths:   []string{"balanced", "coding", "agentic", "large-context"},
		},
		{
			ID:          "google/gemini-2.5-flash",
			Name:        "Gemini 2.5 Flash",
			Tier:        TierBalanced,
			InputCost:   0.30,
			OutputCost:  2.50,
			ContextSize: 1050000,
			Strengths:   []string{"balanced", "reasoning", "large-context", "cheap"},
		},
		{
			ID:          "openai/gpt-5.2",
			Name:        "GPT-5.2",
			Tier:        TierBalanced,
			InputCost:   1.75,
			OutputCost:  14.00,
			ContextSize: 400000,
			Strengths:   []string{"balanced", "agentic", "tool-use", "coding"},
		},
		{
			ID:          "google/gemini-2.5-pro",
			Name:        "Gemini 2.5 Pro",
			Tier:        TierBalanced,
			InputCost:   1.25,
			OutputCost:  10.00,
			ContextSize: 1050000,
			Strengths:   []string{"balanced", "large-context", "multimodal"},
		},
		{
			ID:          "qwen/qwen3-max",
			Name:        "Qwen3 Max",
			Tier:        TierBalanced,
			InputCost:   1.20,
			OutputCost:  6.00,
			ContextSize: 256000,
			Strengths:   []string{"balanced", "multilingual", "reasoning"},
		},

		// ============================================================
		// Powerful tier - for complex tasks requiring deep understanding
		// ============================================================
		{
			ID:          "anthropic/claude-opus-4.5",
			Name:        "Claude Opus 4.5",
			Tier:        TierPowerful,
			InputCost:   5.00,
			OutputCost:  25.00,
			ContextSize: 200000,
			Strengths:   []string{"powerful", "complex-reasoning", "agentic", "coding"},
		},
		{
			ID:          "google/gemini-3-pro-preview",
			Name:        "Gemini 3 Pro Preview",
			Tier:        TierPowerful,
			InputCost:   2.00,
			OutputCost:  12.00,
			ContextSize: 1050000,
			Strengths:   []string{"powerful", "large-context", "multimodal"},
		},
		{
			ID:          "deepseek/deepseek-v3.2-speciale",
			Name:        "DeepSeek V3.2 Speciale",
			Tier:        TierPowerful,
			InputCost:   0.27,
			OutputCost:  0.41,
			ContextSize: 164000,
			Strengths:   []string{"powerful", "very-cheap", "coding"},
		},
		{
			ID:          "qwen/qwen3-coder",
			Name:        "Qwen3 Coder 480B",
			Tier:        TierPowerful,
			InputCost:   1.00,
			OutputCost:  5.00,
			ContextSize: 256000,
			Strengths:   []string{"powerful", "coding", "agentic"},
		},

		// ============================================================
		// Reasoning tier - for deep reasoning, math, logic, proofs
		// ============================================================
		{
			ID:          "deepseek/deepseek-r1-0528",
			Name:        "DeepSeek R1",
			Tier:        TierReasoning,
			InputCost:   0.40,
			OutputCost:  1.75,
			ContextSize: 164000,
			Strengths:   []string{"reasoning", "math", "logic", "cheap"},
		},
		{
			ID:          "qwen/qwq-32b",
			Name:        "QwQ 32B",
			Tier:        TierReasoning,
			InputCost:   0.15,
			OutputCost:  0.40,
			ContextSize: 131000,
			Strengths:   []string{"reasoning", "very-cheap", "math"},
		},
		{
			ID:          "qwen/qwen3-30b-a3b-thinking",
			Name:        "Qwen3 30B Thinking",
			Tier:        TierReasoning,
			InputCost:   0.20,
			OutputCost:  1.20,
			ContextSize: 262000,
			Strengths:   []string{"reasoning", "cheap", "extended-thinking"},
		},
		{
			ID:          "google/gemini-2.5-flash-thinking",
			Name:        "Gemini 2.5 Flash Thinking",
			Tier:        TierReasoning,
			InputCost:   0.30,
			OutputCost:  2.50,
			ContextSize: 1050000,
			Strengths:   []string{"reasoning", "large-context", "configurable-thinking"},
		},
	}
}

// OpenRouterClient implements LLMClient with intelligent model routing.
type OpenRouterClient struct {
	provider fantasy.Provider
	models   []ModelSpec
	selector ModelSelector
	fallback string
}

// ModelSelector chooses the best model for a task.
type ModelSelector interface {
	SelectModel(ctx context.Context, task string, budget int, depth int) *ModelSpec
}

// OpenRouterConfig configures the OpenRouter client.
type OpenRouterConfig struct {
	// APIKey is the OpenRouter API key.
	APIKey string

	// Models overrides the default model catalog.
	Models []ModelSpec

	// Selector overrides the default model selector.
	Selector ModelSelector

	// FallbackModel is used when selection fails.
	FallbackModel string
}

// NewOpenRouterClient creates an OpenRouter client with intelligent routing.
func NewOpenRouterClient(cfg OpenRouterConfig) (*OpenRouterClient, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENROUTER_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("OpenRouter API key not provided (set OPENROUTER_API_KEY)")
	}

	provider, err := openrouter.New(openrouter.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("create OpenRouter provider: %w", err)
	}

	models := cfg.Models
	if len(models) == 0 {
		models = DefaultModels()
	}

	selector := cfg.Selector
	if selector == nil {
		selector = &AdaptiveSelector{models: models}
	}

	fallback := cfg.FallbackModel
	if fallback == "" {
		fallback = "anthropic/claude-haiku-4.5"
	}

	return &OpenRouterClient{
		provider: provider,
		models:   models,
		selector: selector,
		fallback: fallback,
	}, nil
}

// Complete implements LLMClient with intelligent model selection.
func (c *OpenRouterClient) Complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	if maxTokens == 0 {
		maxTokens = 500
	}

	// Extract task context from prompt for model selection
	budget, depth := extractContext(prompt)

	// Select best model for this task
	spec := c.selector.SelectModel(ctx, prompt, budget, depth)
	modelID := c.fallback
	if spec != nil {
		modelID = spec.ID
	}

	// Get language model
	lm, err := c.provider.LanguageModel(ctx, modelID)
	if err != nil {
		// Try fallback
		lm, err = c.provider.LanguageModel(ctx, c.fallback)
		if err != nil {
			return "", fmt.Errorf("get language model: %w", err)
		}
	}

	// Build and execute call
	maxTokens64 := int64(maxTokens)
	call := fantasy.Call{
		Prompt:          fantasy.Prompt{fantasy.NewUserMessage(prompt)},
		MaxOutputTokens: &maxTokens64,
	}

	resp, err := lm.Generate(ctx, call)
	if err != nil {
		return "", fmt.Errorf("openrouter generate: %w", err)
	}

	text := resp.Content.Text()
	if text == "" {
		return "", fmt.Errorf("empty response")
	}

	return text, nil
}

// extractContext parses budget and depth from the prompt.
func extractContext(prompt string) (budget, depth int) {
	// Default values
	budget = 10000
	depth = 0

	// Look for budget info
	if idx := strings.Index(prompt, "Budget remaining:"); idx != -1 {
		fmt.Sscanf(prompt[idx:], "Budget remaining: %d", &budget)
	}

	// Look for depth info
	if idx := strings.Index(prompt, "Recursion depth:"); idx != -1 {
		fmt.Sscanf(prompt[idx:], "Recursion depth: %d", &depth)
	}

	return budget, depth
}

// AdaptiveSelector implements intelligent model selection based on task characteristics.
type AdaptiveSelector struct {
	models []ModelSpec
}

// SelectModel chooses the best model based on task, budget, and depth.
func (s *AdaptiveSelector) SelectModel(ctx context.Context, task string, budget int, depth int) *ModelSpec {
	// Determine required tier based on context
	tier := s.determineTier(task, budget, depth)

	// Find best model for tier
	var candidates []*ModelSpec
	for i := range s.models {
		if s.models[i].Tier == tier {
			candidates = append(candidates, &s.models[i])
		}
	}

	if len(candidates) == 0 {
		// Fall back to fast tier
		for i := range s.models {
			if s.models[i].Tier == TierFast {
				return &s.models[i]
			}
		}
		return nil
	}

	// Select based on task keywords
	return s.rankCandidates(candidates, task)
}

// determineTier chooses model tier based on context.
func (s *AdaptiveSelector) determineTier(task string, budget int, depth int) ModelTier {
	taskLower := strings.ToLower(task)

	// Depth-based selection first (use simpler models at higher depth to save resources)
	// This takes priority over keywords to ensure we don't recurse with expensive models
	if depth >= 3 {
		return TierFast
	}

	// Check for reasoning-specific keywords
	reasoningKeywords := []string{"prove", "theorem", "logic", "math", "calculate", "reason"}
	for _, kw := range reasoningKeywords {
		if strings.Contains(taskLower, kw) {
			return TierReasoning
		}
	}

	// Check for complex task keywords (only if budget allows)
	complexKeywords := []string{"analyze", "refactor", "design", "architect", "complex"}
	for _, kw := range complexKeywords {
		if strings.Contains(taskLower, kw) && budget > 5000 {
			return TierPowerful
		}
	}

	// Budget-based selection
	if budget < 1000 {
		return TierFast
	}

	// Moderate depth prefers balanced
	if depth >= 2 {
		return TierBalanced
	}

	if budget < 5000 {
		return TierBalanced
	}

	// Default to balanced for meta-controller decisions
	return TierBalanced
}

// rankCandidates selects best candidate based on task content.
func (s *AdaptiveSelector) rankCandidates(candidates []*ModelSpec, task string) *ModelSpec {
	taskLower := strings.ToLower(task)

	// Score each candidate
	var bestScore int
	var best *ModelSpec

	for _, c := range candidates {
		score := 0

		// Match strengths to task
		for _, strength := range c.Strengths {
			if strings.Contains(taskLower, strength) {
				score += 10
			}
		}

		// Prefer cheaper models when scores are tied
		if score > bestScore || (score == bestScore && best != nil && c.InputCost < best.InputCost) {
			bestScore = score
			best = c
		}
	}

	if best == nil && len(candidates) > 0 {
		best = candidates[0]
	}

	return best
}

// Provider returns the underlying OpenRouter provider.
func (c *OpenRouterClient) Provider() fantasy.Provider {
	return c.provider
}
