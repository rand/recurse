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
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Content    string    `json:"content"`
	Confidence float64   `json:"confidence"`
	Tier       string    `json:"tier"`
	CreatedAt  time.Time `json:"created_at"`
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
					idPrefix := n.ID
					if len(idPrefix) > 8 {
						idPrefix = idPrefix[:8]
					}
					lines = append(lines, fmt.Sprintf("[%d] %s (%s) - %s", i+1, n.Type, n.Tier, n.CreatedAt.Format("2006-01-02 15:04")))

					// Format content based on type
					summary := formatNodeContent(n.Type, n.Content)
					lines = append(lines, fmt.Sprintf("    %s", summary))
				}
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(strings.Join(lines, "\n")),
				result,
			), nil
		})
}

// formatNodeContent extracts a human-readable summary from node content.
func formatNodeContent(nodeType, content string) string {
	// Try to parse as JSON and extract meaningful fields
	var data map[string]any
	if err := json.Unmarshal([]byte(content), &data); err == nil {
		return formatJSONContent(nodeType, data)
	}

	// Not JSON - just truncate plain text
	if len(content) > 150 {
		return content[:150] + "..."
	}
	return content
}

// formatJSONContent formats JSON data based on node type.
func formatJSONContent(nodeType string, data map[string]any) string {
	switch nodeType {
	case "experience":
		// Session metadata
		var parts []string
		if id, ok := data["id"].(string); ok {
			// Extract just the timestamp part from session ID
			if strings.HasPrefix(id, "session-") {
				parts = append(parts, fmt.Sprintf("Session %s", id[8:]))
			}
		}
		if duration, ok := data["duration"].(string); ok {
			parts = append(parts, fmt.Sprintf("Duration: %s", duration))
		}
		if startTime, ok := data["start_time"].(string); ok {
			if t, err := time.Parse(time.RFC3339, startTime); err == nil {
				parts = append(parts, fmt.Sprintf("Started: %s", t.Format("Jan 2 15:04")))
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " | ")
		}

	case "fact":
		// Try to extract key facts
		if content, ok := data["content"].(string); ok {
			if len(content) > 150 {
				return content[:150] + "..."
			}
			return content
		}

	case "decision":
		// Extract decision info
		var parts []string
		if choice, ok := data["choice"].(string); ok {
			parts = append(parts, fmt.Sprintf("Choice: %s", choice))
		}
		if reason, ok := data["reason"].(string); ok {
			if len(reason) > 100 {
				reason = reason[:100] + "..."
			}
			parts = append(parts, fmt.Sprintf("Reason: %s", reason))
		}
		if len(parts) > 0 {
			return strings.Join(parts, " | ")
		}
	}

	// Fallback: show top-level keys
	var keys []string
	for k := range data {
		keys = append(keys, k)
	}
	if len(keys) > 5 {
		keys = keys[:5]
	}
	return fmt.Sprintf("Fields: %s", strings.Join(keys, ", "))
}
