// Package evolution implements memory tier transitions, consolidation, and decay.
package evolution

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

// ConsolidationConfig configures the consolidation process.
type ConsolidationConfig struct {
	// MinNodes is the minimum number of nodes to trigger consolidation.
	MinNodes int

	// SimilarityThreshold for deduplication (0-1).
	SimilarityThreshold float64

	// PreserveSourceLinks keeps links from summaries to source nodes.
	PreserveSourceLinks bool

	// MaxSummaryLength limits summary node content length.
	MaxSummaryLength int
}

// DefaultConsolidationConfig returns sensible defaults.
func DefaultConsolidationConfig() ConsolidationConfig {
	return ConsolidationConfig{
		MinNodes:            3,
		SimilarityThreshold: 0.85,
		PreserveSourceLinks: true,
		MaxSummaryLength:    1000,
	}
}

// ConsolidationResult contains the outcome of consolidation.
type ConsolidationResult struct {
	// NodesProcessed is the total nodes examined.
	NodesProcessed int

	// NodesMerged is the count of nodes deduplicated.
	NodesMerged int

	// SummariesCreated is the count of new summary nodes.
	SummariesCreated int

	// EdgesStrengthened is the count of hyperedges with increased weight.
	EdgesStrengthened int

	// Duration of the consolidation process.
	Duration time.Duration
}

// Consolidator performs memory consolidation between tiers.
type Consolidator struct {
	store  *hypergraph.Store
	config ConsolidationConfig
}

// NewConsolidator creates a new consolidator.
func NewConsolidator(store *hypergraph.Store, config ConsolidationConfig) *Consolidator {
	return &Consolidator{
		store:  store,
		config: config,
	}
}

// Consolidate performs consolidation from source tier to target tier.
func (c *Consolidator) Consolidate(ctx context.Context, sourceTier, targetTier hypergraph.Tier) (*ConsolidationResult, error) {
	start := time.Now()
	result := &ConsolidationResult{}

	// Get all nodes in source tier
	nodes, err := c.store.ListNodes(ctx, hypergraph.NodeFilter{
		Tiers: []hypergraph.Tier{sourceTier},
		Limit: 1000,
	})
	if err != nil {
		return nil, fmt.Errorf("list source nodes: %w", err)
	}

	result.NodesProcessed = len(nodes)

	if len(nodes) < c.config.MinNodes {
		result.Duration = time.Since(start)
		return result, nil
	}

	// Group nodes by type for consolidation
	byType := groupByType(nodes)

	for nodeType, typeNodes := range byType {
		// Deduplicate similar nodes
		merged, err := c.deduplicateNodes(ctx, typeNodes)
		if err != nil {
			return nil, fmt.Errorf("deduplicate %s: %w", nodeType, err)
		}
		result.NodesMerged += merged

		// Create summary nodes if we have enough content
		if len(typeNodes) >= c.config.MinNodes {
			summaries, err := c.createSummaries(ctx, typeNodes, targetTier)
			if err != nil {
				return nil, fmt.Errorf("create summaries for %s: %w", nodeType, err)
			}
			result.SummariesCreated += summaries
		}
	}

	// Strengthen frequently-traversed edges
	strengthened, err := c.strengthenEdges(ctx, sourceTier)
	if err != nil {
		return nil, fmt.Errorf("strengthen edges: %w", err)
	}
	result.EdgesStrengthened = strengthened

	// Re-fetch surviving nodes in source tier for promotion
	survivingNodes, err := c.store.ListNodes(ctx, hypergraph.NodeFilter{
		Tiers: []hypergraph.Tier{sourceTier},
		Limit: 1000,
	})
	if err != nil {
		return nil, fmt.Errorf("list surviving nodes: %w", err)
	}

	// Promote remaining nodes to target tier
	if err := c.promoteNodes(ctx, survivingNodes, targetTier); err != nil {
		return nil, fmt.Errorf("promote nodes: %w", err)
	}

	result.Duration = time.Since(start)
	return result, nil
}

// deduplicateNodes merges similar nodes within a group.
func (c *Consolidator) deduplicateNodes(ctx context.Context, nodes []*hypergraph.Node) (int, error) {
	merged := 0

	// Simple content-based deduplication
	seen := make(map[string]*hypergraph.Node)

	for _, node := range nodes {
		key := normalizeContent(node.Content)

		if existing, ok := seen[key]; ok {
			// Merge into existing node
			if err := c.mergeNodes(ctx, existing, node); err != nil {
				return merged, err
			}
			merged++
		} else {
			seen[key] = node
		}
	}

	return merged, nil
}

// mergeNodes combines two similar nodes.
func (c *Consolidator) mergeNodes(ctx context.Context, target, source *hypergraph.Node) error {
	// Increase access count and confidence
	target.AccessCount += source.AccessCount
	if source.Confidence > target.Confidence {
		target.Confidence = source.Confidence
	}

	// Update the target node
	if err := c.store.UpdateNode(ctx, target); err != nil {
		return fmt.Errorf("update target: %w", err)
	}

	// Delete the source node (edges will cascade)
	if err := c.store.DeleteNode(ctx, source.ID); err != nil {
		return fmt.Errorf("delete source: %w", err)
	}

	return nil
}

// createSummaries generates summary nodes for a group of related nodes.
func (c *Consolidator) createSummaries(ctx context.Context, nodes []*hypergraph.Node, targetTier hypergraph.Tier) (int, error) {
	if len(nodes) == 0 {
		return 0, nil
	}

	// Group related nodes (by subtype or relationship)
	groups := c.groupRelatedNodes(nodes)
	created := 0

	for _, group := range groups {
		if len(group) < 2 {
			continue
		}

		// Create summary content
		summary := c.generateSummary(group)
		if len(summary) == 0 {
			continue
		}

		// Create summary node
		summaryNode := hypergraph.NewNode(hypergraph.NodeTypeFact, summary)
		summaryNode.Tier = targetTier
		summaryNode.Subtype = "summary"
		summaryNode.Confidence = c.averageConfidence(group)

		if err := c.store.CreateNode(ctx, summaryNode); err != nil {
			return created, fmt.Errorf("create summary: %w", err)
		}

		// Link summary to source nodes (preserve detail)
		if c.config.PreserveSourceLinks {
			for i, source := range group {
				edge := hypergraph.NewHyperedge(hypergraph.HyperedgeComposition, "summarizes")
				if err := c.store.CreateHyperedge(ctx, edge); err != nil {
					continue
				}
				c.store.AddMember(ctx, hypergraph.Membership{
					HyperedgeID: edge.ID,
					NodeID:      summaryNode.ID,
					Role:        hypergraph.RoleSubject,
					Position:    0,
				})
				c.store.AddMember(ctx, hypergraph.Membership{
					HyperedgeID: edge.ID,
					NodeID:      source.ID,
					Role:        hypergraph.RoleObject,
					Position:    i + 1,
				})
			}
		}

		created++
	}

	return created, nil
}

// groupRelatedNodes clusters nodes by their relationships or subtypes.
func (c *Consolidator) groupRelatedNodes(nodes []*hypergraph.Node) [][]*hypergraph.Node {
	// Simple grouping by subtype
	bySubtype := make(map[string][]*hypergraph.Node)

	for _, node := range nodes {
		key := node.Subtype
		if key == "" {
			key = "default"
		}
		bySubtype[key] = append(bySubtype[key], node)
	}

	var groups [][]*hypergraph.Node
	for _, group := range bySubtype {
		groups = append(groups, group)
	}

	return groups
}

// generateSummary creates a summary from a group of nodes.
func (c *Consolidator) generateSummary(nodes []*hypergraph.Node) string {
	if len(nodes) == 0 {
		return ""
	}

	// Collect unique content points
	var points []string
	seen := make(map[string]bool)

	for _, node := range nodes {
		content := strings.TrimSpace(node.Content)
		if content == "" || seen[content] {
			continue
		}
		seen[content] = true

		// Truncate individual points
		if len(content) > 200 {
			content = content[:197] + "..."
		}
		points = append(points, content)
	}

	if len(points) == 0 {
		return ""
	}

	// Build summary
	summary := fmt.Sprintf("Summary of %d items:\n", len(points))
	for i, point := range points {
		if i >= 10 {
			summary += fmt.Sprintf("... and %d more\n", len(points)-10)
			break
		}
		summary += fmt.Sprintf("- %s\n", point)
	}

	// Enforce max length
	if len(summary) > c.config.MaxSummaryLength {
		summary = summary[:c.config.MaxSummaryLength-3] + "..."
	}

	return summary
}

// averageConfidence calculates average confidence across nodes.
func (c *Consolidator) averageConfidence(nodes []*hypergraph.Node) float64 {
	if len(nodes) == 0 {
		return 0
	}

	var sum float64
	for _, node := range nodes {
		sum += node.Confidence
	}
	return sum / float64(len(nodes))
}

// strengthenEdges increases weight of frequently-used edges.
func (c *Consolidator) strengthenEdges(ctx context.Context, tier hypergraph.Tier) (int, error) {
	// Get edges in this tier
	edges, err := c.store.ListHyperedges(ctx, hypergraph.HyperedgeFilter{
		Limit: 500,
	})
	if err != nil {
		return 0, err
	}

	strengthened := 0
	for _, edge := range edges {
		// Check if edge connects nodes in the source tier
		members, err := c.store.GetMemberNodes(ctx, edge.ID)
		if err != nil {
			continue
		}

		inTier := 0
		for _, m := range members {
			if m.Tier == tier {
				inTier++
			}
		}

		// Strengthen if most members are in this tier
		if inTier > len(members)/2 {
			edge.Weight *= 1.1 // 10% boost
			if edge.Weight > 10 {
				edge.Weight = 10 // Cap at 10
			}
			if err := c.store.UpdateHyperedge(ctx, edge); err != nil {
				continue
			}
			strengthened++
		}
	}

	return strengthened, nil
}

// promoteNodes moves nodes from source tier to target tier.
func (c *Consolidator) promoteNodes(ctx context.Context, nodes []*hypergraph.Node, targetTier hypergraph.Tier) error {
	for _, node := range nodes {
		node.Tier = targetTier
		if err := c.store.UpdateNode(ctx, node); err != nil {
			return fmt.Errorf("promote node %s: %w", node.ID, err)
		}
	}
	return nil
}

// Helper functions

func groupByType(nodes []*hypergraph.Node) map[hypergraph.NodeType][]*hypergraph.Node {
	result := make(map[hypergraph.NodeType][]*hypergraph.Node)
	for _, node := range nodes {
		result[node.Type] = append(result[node.Type], node)
	}
	return result
}

func normalizeContent(content string) string {
	// Normalize whitespace and case for comparison
	content = strings.ToLower(content)
	content = strings.Join(strings.Fields(content), " ")
	return content
}
