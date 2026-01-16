// Package rlm provides the synthesizer implementation for session summaries.
package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rand/recurse/internal/memory/evolution"
	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/rlm/meta"
)

// LLMSynthesizer implements SessionSynthesizer using an LLM for synthesis.
// [SPEC-09.01] Uses Claude Haiku for fast, cheap session synthesis.
type LLMSynthesizer struct {
	client    meta.LLMClient
	maxTokens int
}

// NewLLMSynthesizer creates a new LLM-based session synthesizer.
func NewLLMSynthesizer(client meta.LLMClient) *LLMSynthesizer {
	return &LLMSynthesizer{
		client:    client,
		maxTokens: 1024, // Reasonable limit for session summaries
	}
}

// Synthesize creates a session summary from the given experiences using an LLM.
// [SPEC-09.01] Implements evolution.SessionSynthesizer interface.
func (s *LLMSynthesizer) Synthesize(ctx context.Context, sessionID string, experiences []*hypergraph.Node, duration time.Duration) (*evolution.SessionSummary, error) {
	if len(experiences) == 0 {
		return nil, fmt.Errorf("no experiences to synthesize")
	}

	// Build experience context for the LLM
	expContext := s.buildExperienceContext(experiences)

	// Construct the synthesis prompt
	prompt := fmt.Sprintf(`Analyze these session experiences and create a structured summary.

Session ID: %s
Duration: %s
Experiences:
%s

Return a JSON object with this structure:
{
  "summary": "1-2 sentence human-readable summary of what was accomplished",
  "tasks_completed": [{"description": "...", "outcome": "...", "success": true}],
  "tasks_failed": [{"description": "...", "outcome": "...", "success": false}],
  "key_insights": ["insight1", "insight2"],
  "blockers_hit": ["blocker1"],
  "active_files": ["file1.go", "file2.go"],
  "unfinished_work": "Description of work that wasn't completed",
  "next_steps": ["step1", "step2"]
}

Focus on:
1. What was actually accomplished (not just attempted)
2. Key learnings and insights
3. Files that were actively worked on
4. Concrete next steps for resumption

Return ONLY valid JSON, no markdown code blocks.`, sessionID, duration.Round(time.Second), expContext)

	// Call the LLM
	response, err := s.client.Complete(ctx, prompt, s.maxTokens)
	if err != nil {
		// Fallback to basic summary without LLM
		return s.fallbackSummary(sessionID, experiences, duration), nil
	}

	// Parse the response
	summary, err := s.parseResponse(response, sessionID, duration)
	if err != nil {
		// Fallback on parse error
		return s.fallbackSummary(sessionID, experiences, duration), nil
	}

	// Fill in session metadata
	summary.SessionID = sessionID
	summary.Duration = duration
	summary.EndTime = time.Now()
	summary.StartTime = summary.EndTime.Add(-duration)

	return summary, nil
}

// buildExperienceContext formats experiences for the LLM prompt.
func (s *LLMSynthesizer) buildExperienceContext(experiences []*hypergraph.Node) string {
	var lines []string

	for i, exp := range experiences {
		if i >= 20 { // Limit to prevent prompt overflow
			lines = append(lines, fmt.Sprintf("... and %d more experiences", len(experiences)-20))
			break
		}

		line := fmt.Sprintf("- [%s] %s", exp.Type, exp.Content)

		// Include metadata if present
		if len(exp.Metadata) > 0 {
			var meta map[string]any
			if json.Unmarshal(exp.Metadata, &meta) == nil {
				if outcome, ok := meta["outcome"].(string); ok && outcome != "" {
					line += fmt.Sprintf(" (Outcome: %s)", truncate(outcome, 100))
				}
				if success, ok := meta["success"].(bool); ok {
					if success {
						line += " [SUCCESS]"
					} else {
						line += " [FAILED]"
					}
				}
				if insights, ok := meta["insights_gained"].([]any); ok && len(insights) > 0 {
					line += fmt.Sprintf(" (Insights: %v)", insights)
				}
			}
		}

		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// parseResponse parses the LLM response into a SessionSummary.
func (s *LLMSynthesizer) parseResponse(response string, sessionID string, duration time.Duration) (*evolution.SessionSummary, error) {
	// Clean up response - remove markdown code blocks if present
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	// Parse JSON response
	var parsed struct {
		Summary        string `json:"summary"`
		TasksCompleted []struct {
			Description string `json:"description"`
			Outcome     string `json:"outcome"`
			Success     bool   `json:"success"`
		} `json:"tasks_completed"`
		TasksFailed []struct {
			Description string `json:"description"`
			Outcome     string `json:"outcome"`
			Success     bool   `json:"success"`
		} `json:"tasks_failed"`
		KeyInsights    []string `json:"key_insights"`
		BlockersHit    []string `json:"blockers_hit"`
		ActiveFiles    []string `json:"active_files"`
		UnfinishedWork string   `json:"unfinished_work"`
		NextSteps      []string `json:"next_steps"`
	}

	if err := json.Unmarshal([]byte(response), &parsed); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Convert to SessionSummary
	summary := &evolution.SessionSummary{
		SessionID:      sessionID,
		Duration:       duration,
		Summary:        parsed.Summary,
		KeyInsights:    parsed.KeyInsights,
		BlockersHit:    parsed.BlockersHit,
		ActiveFiles:    parsed.ActiveFiles,
		UnfinishedWork: parsed.UnfinishedWork,
		NextSteps:      parsed.NextSteps,
	}

	// Convert tasks
	for _, t := range parsed.TasksCompleted {
		summary.TasksCompleted = append(summary.TasksCompleted, evolution.TaskSummary{
			Description: t.Description,
			Outcome:     t.Outcome,
			Success:     true,
		})
	}
	for _, t := range parsed.TasksFailed {
		summary.TasksFailed = append(summary.TasksFailed, evolution.TaskSummary{
			Description: t.Description,
			Outcome:     t.Outcome,
			Success:     false,
		})
	}

	return summary, nil
}

// fallbackSummary creates a basic summary without LLM when synthesis fails.
func (s *LLMSynthesizer) fallbackSummary(sessionID string, experiences []*hypergraph.Node, duration time.Duration) *evolution.SessionSummary {
	now := time.Now()

	// Count successes and failures
	var successes, failures int
	var insights, blockers []string

	for _, exp := range experiences {
		if len(exp.Metadata) > 0 {
			var meta map[string]any
			if json.Unmarshal(exp.Metadata, &meta) == nil {
				if success, ok := meta["success"].(bool); ok {
					if success {
						successes++
					} else {
						failures++
					}
				}
				if insightList, ok := meta["insights_gained"].([]any); ok {
					for _, i := range insightList {
						if str, ok := i.(string); ok {
							insights = append(insights, str)
						}
					}
				}
				if blockerList, ok := meta["blockers_hit"].([]any); ok {
					for _, b := range blockerList {
						if str, ok := b.(string); ok {
							blockers = append(blockers, str)
						}
					}
				}
			}
		}
	}

	summary := fmt.Sprintf("Session with %d experiences (%d successful, %d failed) over %s",
		len(experiences), successes, failures, duration.Round(time.Second))

	return &evolution.SessionSummary{
		SessionID:   sessionID,
		StartTime:   now.Add(-duration),
		EndTime:     now,
		Duration:    duration,
		Summary:     summary,
		KeyInsights: unique(insights),
		BlockersHit: unique(blockers),
	}
}

// unique removes duplicates from a string slice.
func unique(strs []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range strs {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
