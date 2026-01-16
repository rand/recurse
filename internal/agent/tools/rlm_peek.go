package tools

import (
	"context"
	_ "embed"
	"fmt"

	"charm.land/fantasy"
	"github.com/rand/recurse/internal/rlm/repl"
)

const RLMPeekToolName = "rlm_peek"

//go:embed rlm_peek.md
var rlmPeekDescription []byte

type RLMPeekParams struct {
	Name  string `json:"name" description:"Variable name to peek at"`
	Start int    `json:"start,omitempty" description:"Starting character index (0-based)"`
	End   int    `json:"end,omitempty" description:"Ending character index (exclusive)"`
}

type RLMPeekResult struct {
	Value       string `json:"value"`
	Length      int    `json:"length"`
	TotalLength int    `json:"total_length"`
	Type        string `json:"type"`
}

func NewRLMPeekTool(replManager *repl.Manager) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		RLMPeekToolName,
		string(rlmPeekDescription),
		func(ctx context.Context, params RLMPeekParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Name == "" {
				return fantasy.NewTextErrorResponse("name is required"), nil
			}

			// Ensure REPL is running, attempt to start if not
			if err := ensureREPLRunning(ctx, replManager); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			// Default to first 1000 chars if no range specified
			start := params.Start
			end := params.End
			if start == 0 && end == 0 {
				end = 1000
			}

			result, err := replManager.GetVar(ctx, params.Name, start, end)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Variable '%s' not found or error: %v", params.Name, err)), nil
			}

			// Build output
			output := result.Value
			if result.Length > len(result.Value) {
				output += fmt.Sprintf("\n\n... (showing %d of %d total characters)", len(result.Value), result.Length)
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(output),
				RLMPeekResult{
					Value:       result.Value,
					Length:      len(result.Value),
					TotalLength: result.Length,
					Type:        result.Type,
				},
			), nil
		})
}
