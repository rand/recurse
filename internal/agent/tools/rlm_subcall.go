package tools

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"charm.land/fantasy"
)

const RLMSubcallToolName = "rlm_subcall"

//go:embed rlm_subcall.md
var rlmSubcallDescription []byte

// RLMSubcallParams defines parameters for sub-LM invocation.
type RLMSubcallParams struct {
	// Prompt is the task or question to apply to the snippet.
	Prompt string `json:"prompt" description:"The task or question to apply to the snippet"`

	// Snippet is the content to process.
	Snippet string `json:"snippet" description:"The content to process (code, text, or data)"`

	// MaxTokens is the maximum tokens for the response (default: 2000).
	MaxTokens int `json:"max_tokens,omitempty" description:"Maximum tokens for the response (default: 2000)"`
}

// RLMSubcallResult contains the result of a sub-LM call.
type RLMSubcallResult struct {
	Response   string `json:"response"`
	TokensUsed int    `json:"tokens_used"`
	DurationMs int64  `json:"duration_ms"`
}

// SubcallProvider interface for making sub-LM calls.
type SubcallProvider interface {
	// Subcall invokes a sub-LM with the given prompt and snippet.
	Subcall(ctx context.Context, prompt, snippet string, maxTokens int) (response string, tokensUsed int, err error)
}

// NewRLMSubcallTool creates a tool for invoking sub-LM calls.
func NewRLMSubcallTool(provider SubcallProvider) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		RLMSubcallToolName,
		string(rlmSubcallDescription),
		func(ctx context.Context, params RLMSubcallParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Prompt == "" {
				return fantasy.NewTextErrorResponse("prompt is required"), nil
			}

			if params.Snippet == "" {
				return fantasy.NewTextErrorResponse("snippet is required"), nil
			}

			maxTokens := params.MaxTokens
			if maxTokens == 0 {
				maxTokens = 2000
			}

			if provider == nil {
				return fantasy.NewTextErrorResponse("subcall provider not configured"), nil
			}

			start := time.Now()

			response, tokensUsed, err := provider.Subcall(ctx, params.Prompt, params.Snippet, maxTokens)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("subcall: %w", err)
			}

			duration := time.Since(start).Milliseconds()

			result := RLMSubcallResult{
				Response:   response,
				TokensUsed: tokensUsed,
				DurationMs: duration,
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(response),
				result,
			), nil
		})
}

// DefaultSubcallProvider implements SubcallProvider using a Fantasy provider.
type DefaultSubcallProvider struct {
	provider fantasy.Provider
	model    string
}

// NewDefaultSubcallProvider creates a subcall provider using the given Fantasy provider.
func NewDefaultSubcallProvider(provider fantasy.Provider, model string) *DefaultSubcallProvider {
	if model == "" {
		model = "claude-3-5-sonnet-latest" // Default to Sonnet for sub-calls
	}
	return &DefaultSubcallProvider{
		provider: provider,
		model:    model,
	}
}

// Subcall implements SubcallProvider.
func (p *DefaultSubcallProvider) Subcall(ctx context.Context, prompt, snippet string, maxTokens int) (string, int, error) {
	lm, err := p.provider.LanguageModel(ctx, p.model)
	if err != nil {
		return "", 0, fmt.Errorf("get language model: %w", err)
	}

	// Build the full prompt with snippet
	fullPrompt := fmt.Sprintf("%s\n\n```\n%s\n```", prompt, snippet)

	maxTokens64 := int64(maxTokens)
	call := fantasy.Call{
		Prompt:          fantasy.Prompt{fantasy.NewUserMessage(fullPrompt)},
		MaxOutputTokens: &maxTokens64,
	}

	resp, err := lm.Generate(ctx, call)
	if err != nil {
		return "", 0, fmt.Errorf("generate: %w", err)
	}

	text := resp.Content.Text()
	tokensUsed := int(resp.Usage.TotalTokens)

	return text, tokensUsed, nil
}
