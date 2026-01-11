package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/google/uuid"
	"github.com/rand/recurse/internal/rlm/repl"
	"github.com/rand/recurse/internal/tui/components/dialogs/rlmtrace"
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

// RLMTraceRecorder records trace events for RLM tool usage.
type RLMTraceRecorder interface {
	RecordTraceEvent(event rlmtrace.TraceEvent) error
}

func NewRLMExecuteTool(replManager *repl.Manager, tracer RLMTraceRecorder) fantasy.AgentTool {
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

			startTime := time.Now()
			result, err := replManager.Execute(ctx, params.Code)
			duration := time.Since(startTime)

			// Record trace event
			if tracer != nil {
				status := "completed"
				details := ""
				if result != nil && result.Error != "" {
					status = "failed"
					details = result.Error
				}
				if err != nil {
					status = "failed"
					details = err.Error()
				}

				// Truncate code for display
				codePreview := params.Code
				if len(codePreview) > 100 {
					codePreview = codePreview[:100] + "..."
				}

				_ = tracer.RecordTraceEvent(rlmtrace.TraceEvent{
					ID:        uuid.New().String(),
					Type:      rlmtrace.EventExecute,
					Action:    "Python: " + codePreview,
					Details:   details,
					Duration:  duration,
					Timestamp: startTime,
					Status:    status,
				})
			}

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
