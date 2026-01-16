package tools

import (
	"context"
	_ "embed"
	"fmt"

	"charm.land/fantasy"
	"github.com/rand/recurse/internal/rlm/repl"
)

const RLMExternalizeToolName = "rlm_externalize"

//go:embed rlm_externalize.md
var rlmExternalizeDescription []byte

type RLMExternalizeParams struct {
	Name    string `json:"name" description:"Variable name (must be a valid Python identifier)"`
	Content string `json:"content" description:"String content to store"`
}

type RLMExternalizeResult struct {
	Name   string `json:"name"`
	Length int    `json:"length"`
}

func NewRLMExternalizeTool(replManager *repl.Manager) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		RLMExternalizeToolName,
		string(rlmExternalizeDescription),
		func(ctx context.Context, params RLMExternalizeParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Name == "" {
				return fantasy.NewTextErrorResponse("name is required"), nil
			}
			if params.Content == "" {
				return fantasy.NewTextErrorResponse("content is required"), nil
			}

			// Ensure REPL is running, attempt to start if not
			if err := ensureREPLRunning(ctx, replManager); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			if err := replManager.SetVar(ctx, params.Name, params.Content); err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("set variable: %w", err)
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(fmt.Sprintf("Stored %d characters as '%s'", len(params.Content), params.Name)),
				RLMExternalizeResult{
					Name:   params.Name,
					Length: len(params.Content),
				},
			), nil
		})
}
