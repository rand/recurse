package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/rand/recurse/internal/rlm"
)

const RLMServiceStatusToolName = "rlm_service_status"

//go:embed rlm_service_status.md
var rlmServiceStatusDescription []byte

type RLMServiceStatusParams struct{}

type RLMServiceStatusResult struct {
	Running          bool    `json:"running"`
	Healthy          bool    `json:"healthy"`
	TotalExecutions  int     `json:"total_executions"`
	TotalTokens      int     `json:"total_tokens"`
	TotalDurationSec float64 `json:"total_duration_sec"`
	TasksCompleted   int     `json:"tasks_completed"`
	SessionsEnded    int     `json:"sessions_ended"`
	Errors           int     `json:"errors"`
	MemoryNodes      int64   `json:"memory_nodes"`
	MemoryEdges      int64   `json:"memory_edges"`
}

func NewRLMServiceStatusTool(rlmService *rlm.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		RLMServiceStatusToolName,
		string(rlmServiceStatusDescription),
		func(ctx context.Context, params RLMServiceStatusParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if rlmService == nil {
				return fantasy.NewTextResponse("RLM service is not initialized"), nil
			}

			// Get service stats
			stats := rlmService.Stats()

			// Get health status
			healthStatus, err := rlmService.HealthCheck(ctx)
			if err != nil {
				healthStatus = &rlm.HealthStatus{Running: rlmService.IsRunning()}
			}

			result := RLMServiceStatusResult{
				Running:          healthStatus.Running,
				Healthy:          healthStatus.Healthy,
				TotalExecutions:  stats.TotalExecutions,
				TotalTokens:      stats.TotalTokens,
				TotalDurationSec: stats.TotalDuration.Seconds(),
				TasksCompleted:   stats.TasksCompleted,
				SessionsEnded:    stats.SessionsEnded,
				Errors:           stats.Errors,
			}

			// Get memory stats if store is available
			if store := rlmService.Store(); store != nil {
				memStats, err := store.Stats(ctx)
				if err == nil {
					result.MemoryNodes = memStats.NodeCount
					result.MemoryEdges = memStats.HyperedgeCount
				}
			}

			// Build human-readable output
			var lines []string
			lines = append(lines, "RLM Service Status")
			lines = append(lines, "==================")
			lines = append(lines, "")

			if result.Running {
				lines = append(lines, "Status: Running")
			} else {
				lines = append(lines, "Status: Not Running")
			}

			if result.Healthy {
				lines = append(lines, "Health: Healthy")
			} else {
				lines = append(lines, "Health: Unhealthy")
			}

			lines = append(lines, "")
			lines = append(lines, "Execution Statistics:")
			lines = append(lines, fmt.Sprintf("  Total Executions: %d", result.TotalExecutions))
			lines = append(lines, fmt.Sprintf("  Total Tokens: %d", result.TotalTokens))
			lines = append(lines, fmt.Sprintf("  Total Duration: %.2fs", result.TotalDurationSec))
			lines = append(lines, fmt.Sprintf("  Tasks Completed: %d", result.TasksCompleted))
			lines = append(lines, fmt.Sprintf("  Sessions Ended: %d", result.SessionsEnded))
			lines = append(lines, fmt.Sprintf("  Errors: %d", result.Errors))

			lines = append(lines, "")
			lines = append(lines, "Memory Statistics:")
			lines = append(lines, fmt.Sprintf("  Nodes: %d", result.MemoryNodes))
			lines = append(lines, fmt.Sprintf("  Hyperedges: %d", result.MemoryEdges))

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(strings.Join(lines, "\n")),
				result,
			), nil
		})
}
