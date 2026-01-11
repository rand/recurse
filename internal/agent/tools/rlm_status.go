package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/rand/recurse/internal/rlm/repl"
)

const RLMStatusToolName = "rlm_status"

//go:embed rlm_status.md
var rlmStatusDescription []byte

type RLMStatusParams struct{}

type RLMStatusResult struct {
	Running      bool              `json:"running"`
	MemoryUsedMB float64           `json:"memory_used_mb"`
	Uptime       int64             `json:"uptime_seconds"`
	ExecCount    int               `json:"exec_count"`
	Variables    []repl.VarInfo    `json:"variables,omitempty"`
}

func NewRLMStatusTool(replManager *repl.Manager) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		RLMStatusToolName,
		string(rlmStatusDescription),
		func(ctx context.Context, params RLMStatusParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			result := RLMStatusResult{
				Running: replManager.Running(),
			}

			if !result.Running {
				return fantasy.WithResponseMetadata(
					fantasy.NewTextResponse("REPL is not running"),
					result,
				), nil
			}

			// Get status from REPL
			status, err := replManager.Status(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("get status: %w", err)
			}

			result.MemoryUsedMB = status.MemoryUsedMB
			result.Uptime = status.Uptime
			result.ExecCount = status.ExecCount

			// Get variables
			vars, err := replManager.ListVars(ctx)
			if err == nil && vars != nil {
				result.Variables = vars.Variables
			}

			// Build output
			var lines []string
			lines = append(lines, "REPL Status: Running")
			lines = append(lines, fmt.Sprintf("Memory: %.2f MB", result.MemoryUsedMB))
			lines = append(lines, fmt.Sprintf("Uptime: %d seconds", result.Uptime))
			lines = append(lines, fmt.Sprintf("Executions: %d", result.ExecCount))

			if len(result.Variables) > 0 {
				lines = append(lines, "")
				lines = append(lines, "Variables:")
				for _, v := range result.Variables {
					info := fmt.Sprintf("  %s: %s", v.Name, v.Type)
					if v.Length > 0 {
						info += fmt.Sprintf(" (len=%d)", v.Length)
					}
					lines = append(lines, info)
				}
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(strings.Join(lines, "\n")),
				result,
			), nil
		})
}
