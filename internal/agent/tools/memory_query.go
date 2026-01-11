package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/memory/tiers"
)

const MemoryQueryToolName = "memory_query"

//go:embed memory_query.md
var memoryQueryDescription []byte

// MemoryQueryParams defines parameters for querying memory.
type MemoryQueryParams struct {
	// Query text to search for
	Query string `json:"query,omitempty" description:"Text to search for in memory content"`

	// Filter by memory type
	Type string `json:"type,omitempty" description:"Filter by type: fact, entity, snippet, decision, experience, or empty for all"`

	// Get related nodes to a specific node
	RelatedTo string `json:"related_to,omitempty" description:"Node ID to find related nodes for"`

	// Depth for related node traversal
	Depth int `json:"depth,omitempty" description:"Traversal depth for related nodes (default: 1)"`

	// Maximum results
	Limit int `json:"limit,omitempty" description:"Maximum number of results (default: 10)"`

	// Get recent context instead of searching
	Recent bool `json:"recent,omitempty" description:"If true, returns most recently accessed nodes instead of searching"`
}

// MemoryQueryResult contains the query results.
type MemoryQueryResult struct {
	Nodes   []MemoryNode `json:"nodes"`
	Count   int          `json:"count"`
	Query   string       `json:"query,omitempty"`
	Filters string       `json:"filters,omitempty"`
}

// MemoryNode is a simplified node for query results.
type MemoryNode struct {
	ID         string  `json:"id"`
	Type       string  `json:"type"`
	Subtype    string  `json:"subtype,omitempty"`
	Content    string  `json:"content"`
	Confidence float64 `json:"confidence"`
	Score      float64 `json:"score,omitempty"`
	Depth      int     `json:"depth,omitempty"`
}

// NewMemoryQueryTool creates a tool for querying memory.
func NewMemoryQueryTool(taskMemory *tiers.TaskMemory) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		MemoryQueryToolName,
		string(memoryQueryDescription),
		func(ctx context.Context, params MemoryQueryParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			limit := params.Limit
			if limit == 0 {
				limit = 10
			}

			var nodes []MemoryNode
			var filterDesc string

			// Determine query mode
			switch {
			case params.RelatedTo != "":
				// Find related nodes
				depth := params.Depth
				if depth == 0 {
					depth = 1
				}
				related, err := taskMemory.GetRelated(ctx, params.RelatedTo, depth)
				if err != nil {
					return fantasy.ToolResponse{}, fmt.Errorf("get related: %w", err)
				}
				for _, r := range related {
					if len(nodes) >= limit {
						break
					}
					nodes = append(nodes, MemoryNode{
						ID:         r.Node.ID,
						Type:       string(r.Node.Type),
						Subtype:    r.Node.Subtype,
						Content:    truncateContent(r.Node.Content, 200),
						Confidence: r.Node.Confidence,
						Depth:      r.Depth,
					})
				}
				filterDesc = fmt.Sprintf("related to %s (depth: %d)", params.RelatedTo[:8], depth)

			case params.Recent:
				// Get recent context
				recent, err := taskMemory.GetContext(ctx, limit)
				if err != nil {
					return fantasy.ToolResponse{}, fmt.Errorf("get context: %w", err)
				}
				for _, n := range recent {
					nodes = append(nodes, MemoryNode{
						ID:         n.ID,
						Type:       string(n.Type),
						Subtype:    n.Subtype,
						Content:    truncateContent(n.Content, 200),
						Confidence: n.Confidence,
					})
				}
				filterDesc = "recent"

			case params.Query != "":
				// Search by content
				results, err := taskMemory.Search(ctx, params.Query, limit)
				if err != nil {
					return fantasy.ToolResponse{}, fmt.Errorf("search: %w", err)
				}

				// Filter by type if specified
				nodeType := parseNodeType(params.Type)
				for _, r := range results {
					if nodeType != "" && r.Node.Type != nodeType {
						continue
					}
					if len(nodes) >= limit {
						break
					}
					nodes = append(nodes, MemoryNode{
						ID:         r.Node.ID,
						Type:       string(r.Node.Type),
						Subtype:    r.Node.Subtype,
						Content:    truncateContent(r.Node.Content, 200),
						Confidence: r.Node.Confidence,
						Score:      r.Score,
					})
				}
				filterDesc = fmt.Sprintf("query=%q", params.Query)
				if params.Type != "" {
					filterDesc += fmt.Sprintf(", type=%s", params.Type)
				}

			default:
				// Get all facts (default behavior)
				facts, err := taskMemory.GetFacts(ctx)
				if err != nil {
					return fantasy.ToolResponse{}, fmt.Errorf("get facts: %w", err)
				}
				for _, f := range facts {
					if len(nodes) >= limit {
						break
					}
					nodes = append(nodes, MemoryNode{
						ID:         f.ID,
						Type:       string(f.Type),
						Subtype:    f.Subtype,
						Content:    truncateContent(f.Content, 200),
						Confidence: f.Confidence,
					})
				}
				filterDesc = "all facts"
			}

			result := MemoryQueryResult{
				Nodes:   nodes,
				Count:   len(nodes),
				Query:   params.Query,
				Filters: filterDesc,
			}

			// Build text output
			var lines []string
			lines = append(lines, fmt.Sprintf("Found %d nodes (%s):", len(nodes), filterDesc))
			lines = append(lines, "")

			for i, n := range nodes {
				typeInfo := n.Type
				if n.Subtype != "" {
					typeInfo += "/" + n.Subtype
				}
				line := fmt.Sprintf("%d. [%s] %s", i+1, typeInfo, n.Content)
				if n.Score > 0 {
					line += fmt.Sprintf(" (score: %.1f)", n.Score)
				}
				if n.Depth > 0 {
					line += fmt.Sprintf(" (depth: %d)", n.Depth)
				}
				lines = append(lines, line)
				lines = append(lines, fmt.Sprintf("   id: %s, confidence: %.2f", n.ID[:8], n.Confidence))
			}

			if len(nodes) == 0 {
				lines = append(lines, "(no results)")
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(strings.Join(lines, "\n")),
				result,
			), nil
		})
}

func parseNodeType(t string) hypergraph.NodeType {
	switch strings.ToLower(t) {
	case "fact":
		return hypergraph.NodeTypeFact
	case "entity":
		return hypergraph.NodeTypeEntity
	case "snippet":
		return hypergraph.NodeTypeSnippet
	case "decision":
		return hypergraph.NodeTypeDecision
	case "experience":
		return hypergraph.NodeTypeExperience
	default:
		return ""
	}
}

func truncateContent(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
