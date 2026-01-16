package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/rand/recurse/internal/rlm"
)

const RLMResumeToolName = "rlm_resume"

//go:embed rlm_resume.md
var rlmResumeDescription []byte

// RLMResumeParams defines parameters for the resume tool.
type RLMResumeParams struct {
	// IncludeRecent includes summaries from recent sessions beyond the latest
	IncludeRecent bool `json:"include_recent,omitempty"`
}

// RLMResumeResult contains the resume query result.
type RLMResumeResult struct {
	HasContext       bool     `json:"has_context"`
	Summary          string   `json:"summary,omitempty"`
	UnfinishedWork   string   `json:"unfinished_work,omitempty"`
	NextSteps        []string `json:"next_steps,omitempty"`
	ActiveFiles      []string `json:"active_files,omitempty"`
	RecentSessionIDs []string `json:"recent_session_ids,omitempty"`
}

// NewRLMResumeTool creates a tool for querying session resumption context.
// [SPEC-09.08]
func NewRLMResumeTool(rlmService *rlm.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		RLMResumeToolName,
		string(rlmResumeDescription),
		func(ctx context.Context, params RLMResumeParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if rlmService == nil {
				return fantasy.NewTextResponse("RLM service is not initialized"), nil
			}

			sessionCtx, err := rlmService.ResumeSession(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("resume session: %w", err)
			}

			if sessionCtx == nil || sessionCtx.PreviousSession == nil {
				return fantasy.WithResponseMetadata(
					fantasy.NewTextResponse("No previous session context found. This appears to be a fresh start."),
					RLMResumeResult{HasContext: false},
				), nil
			}

			// Build human-readable output
			var lines []string
			lines = append(lines, "Session Resumption Context")
			lines = append(lines, strings.Repeat("=", 40))

			prev := sessionCtx.PreviousSession
			if prev.Summary != "" {
				lines = append(lines, "")
				lines = append(lines, "Last Session Summary:")
				lines = append(lines, fmt.Sprintf("  %s", prev.Summary))
			}

			if prev.Duration > 0 {
				lines = append(lines, fmt.Sprintf("  Duration: %s", prev.Duration.Round(1e9)))
			}

			// Tasks completed
			if len(prev.TasksCompleted) > 0 {
				lines = append(lines, "")
				lines = append(lines, "Tasks Completed:")
				for _, t := range prev.TasksCompleted {
					lines = append(lines, fmt.Sprintf("  - %s", t.Description))
					if t.Outcome != "" {
						lines = append(lines, fmt.Sprintf("    Outcome: %s", t.Outcome))
					}
				}
			}

			// Tasks failed
			if len(prev.TasksFailed) > 0 {
				lines = append(lines, "")
				lines = append(lines, "Tasks That Failed:")
				for _, t := range prev.TasksFailed {
					lines = append(lines, fmt.Sprintf("  - %s", t.Description))
					if t.Outcome != "" {
						lines = append(lines, fmt.Sprintf("    Outcome: %s", t.Outcome))
					}
				}
			}

			// Key insights
			if len(prev.KeyInsights) > 0 {
				lines = append(lines, "")
				lines = append(lines, "Key Insights:")
				for _, insight := range prev.KeyInsights {
					lines = append(lines, fmt.Sprintf("  - %s", insight))
				}
			}

			// Blockers
			if len(prev.BlockersHit) > 0 {
				lines = append(lines, "")
				lines = append(lines, "Blockers Encountered:")
				for _, blocker := range prev.BlockersHit {
					lines = append(lines, fmt.Sprintf("  - %s", blocker))
				}
			}

			// Unfinished work
			if sessionCtx.UnfinishedWork != "" {
				lines = append(lines, "")
				lines = append(lines, "Unfinished Work:")
				lines = append(lines, fmt.Sprintf("  %s", sessionCtx.UnfinishedWork))
			}

			// Next steps
			if len(sessionCtx.RecommendedStart) > 0 {
				lines = append(lines, "")
				lines = append(lines, "Recommended Next Steps:")
				for i, step := range sessionCtx.RecommendedStart {
					lines = append(lines, fmt.Sprintf("  %d. %s", i+1, step))
				}
			}

			// Active files
			if len(sessionCtx.ActiveFiles) > 0 {
				lines = append(lines, "")
				lines = append(lines, "Active Files:")
				for _, file := range sessionCtx.ActiveFiles {
					lines = append(lines, fmt.Sprintf("  - %s", file))
				}
			}

			// Recent sessions (if requested)
			if params.IncludeRecent && len(sessionCtx.RecentSessions) > 0 {
				lines = append(lines, "")
				lines = append(lines, "Previous Sessions:")
				for i, sess := range sessionCtx.RecentSessions {
					if sess.Summary != "" {
						lines = append(lines, fmt.Sprintf("  %d. %s", i+2, sess.Summary))
					}
				}
			}

			result := RLMResumeResult{
				HasContext:     true,
				Summary:        prev.Summary,
				UnfinishedWork: sessionCtx.UnfinishedWork,
				NextSteps:      sessionCtx.RecommendedStart,
				ActiveFiles:    sessionCtx.ActiveFiles,
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(strings.Join(lines, "\n")),
				result,
			), nil
		})
}
