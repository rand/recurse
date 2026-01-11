package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/rand/recurse/internal/rlm/repl"
)

const RLMExecuteToolName = "rlm_execute"

//go:embed rlm_execute.md
var rlmExecuteDescription []byte

type RLMExecuteParams struct {
	Code string `json:"code" description:"Python code to execute"`
}

type RLMExecuteResultMeta struct {
	Output     string `json:"output"`
	ReturnVal  string `json:"return_value"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

func NewRLMExecuteTool(replManager *repl.Manager) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		RLMExecuteToolName,
		string(rlmExecuteDescription),
		func(ctx context.Context, params RLMExecuteParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Code == "" {
				return fantasy.NewTextErrorResponse("code is required"), nil
			}

			if !replManager.Running() {
				return fantasy.NewTextErrorResponse("REPL is not running"), nil
			}

			result, err := replManager.Execute(ctx, params.Code)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("execute: %w", err)
			}

			// Build output
			var parts []string

			if result.Output != "" {
				parts = append(parts, result.Output)
			}

			if result.ReturnVal != "" {
				if len(parts) > 0 {
					parts = append(parts, "")
				}
				parts = append(parts, "=> "+result.ReturnVal)
			}

			if result.Error != "" {
				if len(parts) > 0 {
					parts = append(parts, "")
				}
				parts = append(parts, "Error:\n"+result.Error)
			}

			output := strings.Join(parts, "\n")
			if output == "" {
				output = "(no output)"
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(output),
				RLMExecuteResultMeta{
					Output:     result.Output,
					ReturnVal:  result.ReturnVal,
					Error:      result.Error,
					DurationMs: result.Duration,
				},
			), nil
		})
}
