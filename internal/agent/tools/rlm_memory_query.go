package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/rlm"
)

const RLMMemoryQueryToolName = "rlm_memory_query"

//go:embed rlm_memory_query.md
var rlmMemoryQueryDescription []byte

type RLMMemoryQueryParams struct {
	// Limit is the maximum number of nodes to return (default 20)
	Limit int `json:"limit,omitempty"`
	// Type filters by node type (fact, experience, decision, etc.)
	Type string `json:"type,omitempty"`
	// Tier filters by memory tier (task, session, long_term)
	Tier string `json:"tier,omitempty"`
}

type RLMMemoryNode struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Content    string          `json:"content"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	Confidence float64         `json:"confidence"`
	Tier       string          `json:"tier"`
	CreatedAt  time.Time       `json:"created_at"`
}

type RLMMemoryQueryResult struct {
	Nodes      []RLMMemoryNode `json:"nodes"`
	TotalCount int             `json:"total_count"`
}

func NewRLMMemoryQueryTool(rlmService *rlm.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		RLMMemoryQueryToolName,
		string(rlmMemoryQueryDescription),
		func(ctx context.Context, params RLMMemoryQueryParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if rlmService == nil {
				return fantasy.NewTextResponse("RLM service is not initialized"), nil
			}

			store := rlmService.Store()
			if store == nil {
				return fantasy.NewTextResponse("Hypergraph store is not available"), nil
			}

			// Set defaults
			limit := params.Limit
			if limit <= 0 {
				limit = 20
			}
			if limit > 100 {
				limit = 100
			}

			// Build filter
			filter := hypergraph.NodeFilter{
				Limit: limit,
			}

			// Filter by type if specified
			if params.Type != "" {
				filter.Types = []hypergraph.NodeType{hypergraph.NodeType(params.Type)}
			}

			// Filter by tier if specified
			if params.Tier != "" {
				filter.Tiers = []hypergraph.Tier{hypergraph.Tier(params.Tier)}
			}

			// Query nodes
			nodes, err := store.ListNodes(ctx, filter)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("query nodes: %w", err)
			}

			// Convert to result format
			result := RLMMemoryQueryResult{
				Nodes:      make([]RLMMemoryNode, 0, len(nodes)),
				TotalCount: len(nodes),
			}

			for _, node := range nodes {
				result.Nodes = append(result.Nodes, RLMMemoryNode{
					ID:         node.ID,
					Type:       string(node.Type),
					Content:    node.Content,
					Metadata:   node.Metadata,
					Confidence: node.Confidence,
					Tier:       string(node.Tier),
					CreatedAt:  node.CreatedAt,
				})
			}

			// Build human-readable output
			var lines []string
			lines = append(lines, fmt.Sprintf("Memory Query Results (%d nodes)", len(result.Nodes)))
			lines = append(lines, strings.Repeat("=", 40))

			if len(result.Nodes) == 0 {
				lines = append(lines, "")
				lines = append(lines, "No nodes found matching the query.")
			} else {
				for i, n := range result.Nodes {
					lines = append(lines, "")
					lines = append(lines, fmt.Sprintf("[%d] %s (%s) - %s", i+1, n.Type, n.Tier, n.CreatedAt.Format("2006-01-02 15:04")))

					// Format content and metadata based on type
					summary := formatNodeContent(n.Type, n.Content, n.Metadata)
					lines = append(lines, fmt.Sprintf("    %s", summary))
				}
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(strings.Join(lines, "\n")),
				result,
			), nil
		})
}

// formatNodeContent extracts a human-readable summary from node content and metadata.
func formatNodeContent(nodeType, content string, metadata json.RawMessage) string {
	var parts []string

	// Always show the content (the main description) first
	if content != "" {
		displayContent := content
		if len(displayContent) > 150 {
			displayContent = displayContent[:150] + "..."
		}
		parts = append(parts, displayContent)
	}

	// Parse metadata for additional context
	if len(metadata) > 0 {
		var meta map[string]any
		if err := json.Unmarshal(metadata, &meta); err == nil {
			extraInfo := formatMetadata(nodeType, meta)
			if extraInfo != "" {
				parts = append(parts, extraInfo)
			}
		}
	}

	if len(parts) == 0 {
		return "(no content)"
	}
	return strings.Join(parts, "\n    ")
}

// formatMetadata formats metadata fields based on node type.
func formatMetadata(nodeType string, meta map[string]any) string {
	switch nodeType {
	case "experience":
		var details []string

		// Show outcome if present
		if outcome, ok := meta["outcome"].(string); ok && outcome != "" {
			outcomeDisplay := outcome
			if len(outcomeDisplay) > 100 {
				outcomeDisplay = outcomeDisplay[:100] + "..."
			}
			details = append(details, fmt.Sprintf("Outcome: %s", outcomeDisplay))
		}

		// Show success/failure
		if success, ok := meta["success"].(bool); ok {
			if success {
				details = append(details, "Status: Success")
			} else {
				details = append(details, "Status: Failed")
			}
		}

		// Show insights if present
		if insights, ok := meta["insights_gained"].([]any); ok && len(insights) > 0 {
			insightStrs := make([]string, 0, len(insights))
			for _, i := range insights {
				if s, ok := i.(string); ok {
					insightStrs = append(insightStrs, s)
				}
			}
			if len(insightStrs) > 0 {
				details = append(details, fmt.Sprintf("Insights: %s", strings.Join(insightStrs, "; ")))
			}
		}

		// Show blockers if present
		if blockers, ok := meta["blockers_hit"].([]any); ok && len(blockers) > 0 {
			blockerStrs := make([]string, 0, len(blockers))
			for _, b := range blockers {
				if s, ok := b.(string); ok {
					blockerStrs = append(blockerStrs, s)
				}
			}
			if len(blockerStrs) > 0 {
				details = append(details, fmt.Sprintf("Blockers: %s", strings.Join(blockerStrs, "; ")))
			}
		}

		// Show duration if present
		if duration, ok := meta["duration"].(string); ok && duration != "" {
			details = append(details, fmt.Sprintf("Duration: %s", duration))
		}

		return strings.Join(details, " | ")

	case "decision":
		var details []string

		if rationale, ok := meta["rationale"].(string); ok && rationale != "" {
			rationaleDisplay := rationale
			if len(rationaleDisplay) > 100 {
				rationaleDisplay = rationaleDisplay[:100] + "..."
			}
			details = append(details, fmt.Sprintf("Rationale: %s", rationaleDisplay))
		}

		if alts, ok := meta["alternatives"].([]any); ok && len(alts) > 0 {
			altStrs := make([]string, 0, len(alts))
			for _, a := range alts {
				if s, ok := a.(string); ok {
					altStrs = append(altStrs, s)
				}
			}
			if len(altStrs) > 0 {
				details = append(details, fmt.Sprintf("Alternatives: %s", strings.Join(altStrs, ", ")))
			}
		}

		return strings.Join(details, " | ")

	case "fact":
		var details []string

		if confidence, ok := meta["confidence"].(float64); ok {
			details = append(details, fmt.Sprintf("Confidence: %.0f%%", confidence*100))
		}

		return strings.Join(details, " | ")

	case "snippet":
		var details []string

		if file, ok := meta["file"].(string); ok && file != "" {
			details = append(details, fmt.Sprintf("File: %s", file))
		}
		if line, ok := meta["line"].(float64); ok && line > 0 {
			details = append(details, fmt.Sprintf("Line: %d", int(line)))
		}

		return strings.Join(details, " | ")
	}

	return ""
}
