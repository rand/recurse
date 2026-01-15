package tools

import (
	"context"
	_ "embed"
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
				for i, node := range result.Nodes {
					lines = append(lines, "")
					lines = append(lines, fmt.Sprintf("[%d] %s (%s, %s)", i+1, node.Type, node.Tier, node.ID[:8]))
					lines = append(lines, fmt.Sprintf("    Confidence: %.2f", node.Confidence))
					lines = append(lines, fmt.Sprintf("    Created: %s", node.CreatedAt.Format(time.RFC3339)))
					// Truncate content if too long
					content := node.Content
					if len(content) > 200 {
						content = content[:200] + "..."
					}
					lines = append(lines, fmt.Sprintf("    Content: %s", content))
				}
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(strings.Join(lines, "\n")),
				result,
			), nil
		})
}
