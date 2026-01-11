package meta

import (
	"context"
	"fmt"

	"charm.land/fantasy"
)

// HaikuClient implements LLMClient using Claude Haiku via Fantasy.
type HaikuClient struct {
	provider fantasy.Provider
	model    string
}

// HaikuConfig configures the Haiku client.
type HaikuConfig struct {
	// Provider is the Fantasy provider to use.
	Provider fantasy.Provider

	// Model overrides the default model (claude-3-5-haiku-latest).
	Model string
}

// NewHaikuClient creates a new Haiku client.
func NewHaikuClient(cfg HaikuConfig) (*HaikuClient, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("provider is required")
	}

	model := cfg.Model
	if model == "" {
		model = "claude-3-5-haiku-latest"
	}

	return &HaikuClient{
		provider: cfg.Provider,
		model:    model,
	}, nil
}

// Complete implements LLMClient.
func (h *HaikuClient) Complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	if maxTokens == 0 {
		maxTokens = 500
	}

	// Get language model from provider
	lm, err := h.provider.LanguageModel(ctx, h.model)
	if err != nil {
		return "", fmt.Errorf("get language model: %w", err)
	}

	// Build prompt
	maxTokens64 := int64(maxTokens)
	call := fantasy.Call{
		Prompt:          fantasy.Prompt{fantasy.NewUserMessage(prompt)},
		MaxOutputTokens: &maxTokens64,
	}

	// Call the model
	resp, err := lm.Generate(ctx, call)
	if err != nil {
		return "", fmt.Errorf("haiku generate: %w", err)
	}

	// Extract text from response
	text := resp.Content.Text()
	if text == "" {
		return "", fmt.Errorf("empty response from haiku")
	}

	return text, nil
}

// Model returns the configured model name.
func (h *HaikuClient) Model() string {
	return h.model
}
