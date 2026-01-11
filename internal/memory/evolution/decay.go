package evolution

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

// DecayConfig configures the decay and pruning process.
type DecayConfig struct {
	// HalfLife is the time for confidence to decay to 50% (Ebbinghaus model).
	HalfLife time.Duration

	// AccessBoost is the confidence boost per access (amplification).
	AccessBoost float64

	// ArchiveThreshold is the confidence below which nodes are archived.
	ArchiveThreshold float64

	// PruneThreshold is the confidence below which archived nodes are deleted.
	PruneThreshold float64

	// MinRetention is the minimum time before a node can be archived.
	MinRetention time.Duration

	// ExcludeTiers are tiers that should not decay.
	ExcludeTiers []hypergraph.Tier
}

// DefaultDecayConfig returns sensible defaults for decay.
func DefaultDecayConfig() DecayConfig {
	return DecayConfig{
		HalfLife:         time.Hour * 24 * 7, // 1 week half-life
		AccessBoost:      0.1,                // 10% boost per access
		ArchiveThreshold: 0.3,                // Archive below 30%
		PruneThreshold:   0.1,                // Delete below 10%
		MinRetention:     time.Hour * 24,     // Keep at least 1 day
		ExcludeTiers:     []hypergraph.Tier{hypergraph.TierTask}, // Don't decay task tier
	}
}

// DecayResult contains the outcome of decay processing.
type DecayResult struct {
	// NodesProcessed is the total nodes examined.
	NodesProcessed int

	// NodesDecayed is the count of nodes with reduced confidence.
	NodesDecayed int

	// NodesArchived is the count of nodes moved to archive tier.
	NodesArchived int

	// NodesPruned is the count of nodes deleted.
	NodesPruned int

	// Duration of the decay process.
	Duration time.Duration
}

// Decayer handles memory decay and pruning.
type Decayer struct {
	store  *hypergraph.Store
	config DecayConfig
}

// NewDecayer creates a new decayer.
func NewDecayer(store *hypergraph.Store, config DecayConfig) *Decayer {
	return &Decayer{
		store:  store,
		config: config,
	}
}

// ApplyDecay applies Ebbinghaus decay to all eligible nodes.
func (d *Decayer) ApplyDecay(ctx context.Context) (*DecayResult, error) {
	start := time.Now()
	result := &DecayResult{}

	// Get all non-excluded nodes
	nodes, err := d.getDecayableNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("get decayable nodes: %w", err)
	}

	result.NodesProcessed = len(nodes)
	now := time.Now()

	for _, node := range nodes {
		// Calculate decay based on time since last access or creation
		lastActive := node.CreatedAt
		if node.LastAccessed != nil {
			lastActive = *node.LastAccessed
		}

		elapsed := now.Sub(lastActive)
		decayFactor := d.calculateDecay(elapsed)

		// Apply access amplification
		amplification := d.calculateAmplification(node.AccessCount)

		// Calculate new confidence
		newConfidence := node.Confidence * decayFactor * amplification
		newConfidence = math.Min(1.0, math.Max(0.0, newConfidence))

		if newConfidence < node.Confidence {
			node.Confidence = newConfidence
			if err := d.store.UpdateNode(ctx, node); err != nil {
				return nil, fmt.Errorf("update node %s: %w", node.ID, err)
			}
			result.NodesDecayed++
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// ArchiveLowConfidence moves low-confidence nodes to archive tier.
func (d *Decayer) ArchiveLowConfidence(ctx context.Context) (*DecayResult, error) {
	start := time.Now()
	result := &DecayResult{}

	// Get nodes below archive threshold (excluding already archived)
	nodes, err := d.getDecayableNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("get decayable nodes: %w", err)
	}

	result.NodesProcessed = len(nodes)
	now := time.Now()

	for _, node := range nodes {
		// Skip if already archived
		if node.Tier == hypergraph.TierArchive {
			continue
		}

		// Check minimum retention
		if now.Sub(node.CreatedAt) < d.config.MinRetention {
			continue
		}

		// Archive if below threshold
		if node.Confidence < d.config.ArchiveThreshold {
			node.Tier = hypergraph.TierArchive
			if err := d.store.UpdateNode(ctx, node); err != nil {
				return nil, fmt.Errorf("archive node %s: %w", node.ID, err)
			}
			result.NodesArchived++
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// PruneArchived deletes archived nodes below prune threshold.
func (d *Decayer) PruneArchived(ctx context.Context) (*DecayResult, error) {
	start := time.Now()
	result := &DecayResult{}

	// Get archived nodes
	nodes, err := d.store.ListNodes(ctx, hypergraph.NodeFilter{
		Tiers: []hypergraph.Tier{hypergraph.TierArchive},
		Limit: 1000,
	})
	if err != nil {
		return nil, fmt.Errorf("list archived nodes: %w", err)
	}

	result.NodesProcessed = len(nodes)

	for _, node := range nodes {
		if node.Confidence < d.config.PruneThreshold {
			if err := d.store.DeleteNode(ctx, node.ID); err != nil {
				return nil, fmt.Errorf("delete node %s: %w", node.ID, err)
			}
			result.NodesPruned++
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// RunFullCycle runs decay, archive, and prune in sequence.
func (d *Decayer) RunFullCycle(ctx context.Context) (*DecayResult, error) {
	start := time.Now()
	result := &DecayResult{}

	// Apply decay
	decayResult, err := d.ApplyDecay(ctx)
	if err != nil {
		return nil, fmt.Errorf("apply decay: %w", err)
	}
	result.NodesDecayed = decayResult.NodesDecayed

	// Archive low confidence
	archiveResult, err := d.ArchiveLowConfidence(ctx)
	if err != nil {
		return nil, fmt.Errorf("archive: %w", err)
	}
	result.NodesArchived = archiveResult.NodesArchived

	// Prune archived
	pruneResult, err := d.PruneArchived(ctx)
	if err != nil {
		return nil, fmt.Errorf("prune: %w", err)
	}
	result.NodesPruned = pruneResult.NodesPruned

	result.NodesProcessed = decayResult.NodesProcessed
	result.Duration = time.Since(start)
	return result, nil
}

// RecordAccess records an access to a node, boosting its confidence.
func (d *Decayer) RecordAccess(ctx context.Context, nodeID string) error {
	node, err := d.store.GetNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("get node: %w", err)
	}

	// Increment access count
	if err := d.store.IncrementAccess(ctx, nodeID); err != nil {
		return fmt.Errorf("increment access: %w", err)
	}

	// Boost confidence (capped at 1.0)
	node.Confidence = math.Min(1.0, node.Confidence+d.config.AccessBoost)
	return d.store.UpdateNode(ctx, node)
}

// RestoreFromArchive restores an archived node to long-term tier.
func (d *Decayer) RestoreFromArchive(ctx context.Context, nodeID string) error {
	node, err := d.store.GetNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("get node: %w", err)
	}

	if node.Tier != hypergraph.TierArchive {
		return fmt.Errorf("node %s is not archived", nodeID)
	}

	// Restore to long-term with minimum confidence
	node.Tier = hypergraph.TierLongterm
	node.Confidence = d.config.ArchiveThreshold // Reset to threshold
	return d.store.UpdateNode(ctx, node)
}

// GetDecayStats returns statistics about decay state.
func (d *Decayer) GetDecayStats(ctx context.Context) (*DecayStats, error) {
	stats := &DecayStats{
		ConfidenceBuckets: make(map[string]int),
	}

	// Get all nodes
	nodes, err := d.store.ListNodes(ctx, hypergraph.NodeFilter{Limit: 10000})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	stats.TotalNodes = len(nodes)

	for _, node := range nodes {
		// Count by tier
		switch node.Tier {
		case hypergraph.TierArchive:
			stats.ArchivedNodes++
		}

		// Count at-risk nodes (below archive threshold but not archived)
		if node.Confidence < d.config.ArchiveThreshold && node.Tier != hypergraph.TierArchive {
			stats.AtRiskNodes++
		}

		// Bucket by confidence
		bucket := confidenceBucket(node.Confidence)
		stats.ConfidenceBuckets[bucket]++

		// Track average
		stats.AverageConfidence += node.Confidence
	}

	if stats.TotalNodes > 0 {
		stats.AverageConfidence /= float64(stats.TotalNodes)
	}

	return stats, nil
}

// DecayStats contains decay statistics.
type DecayStats struct {
	TotalNodes        int
	ArchivedNodes     int
	AtRiskNodes       int
	AverageConfidence float64
	ConfidenceBuckets map[string]int
}

// calculateDecay computes decay factor using Ebbinghaus forgetting curve.
// R = e^(-t/S) where S is the stability (half-life adjusted)
func (d *Decayer) calculateDecay(elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 1.0
	}

	// Convert to half-life based exponential decay
	// After one half-life, factor should be 0.5
	halfLives := float64(elapsed) / float64(d.config.HalfLife)
	return math.Pow(0.5, halfLives)
}

// calculateAmplification computes access-based amplification.
// Higher access counts slow decay.
func (d *Decayer) calculateAmplification(accessCount int) float64 {
	if accessCount <= 0 {
		return 1.0
	}

	// Logarithmic boost: each doubling of access gives diminishing returns
	// Base amplification of 1.0 + log2(accessCount) * boost
	return 1.0 + math.Log2(float64(accessCount+1))*d.config.AccessBoost
}

// getDecayableNodes returns nodes that should have decay applied.
func (d *Decayer) getDecayableNodes(ctx context.Context) ([]*hypergraph.Node, error) {
	// Build list of tiers to include (all except excluded)
	allTiers := []hypergraph.Tier{
		hypergraph.TierTask,
		hypergraph.TierSession,
		hypergraph.TierLongterm,
		hypergraph.TierArchive,
	}

	var includeTiers []hypergraph.Tier
	for _, tier := range allTiers {
		excluded := false
		for _, excl := range d.config.ExcludeTiers {
			if tier == excl {
				excluded = true
				break
			}
		}
		if !excluded {
			includeTiers = append(includeTiers, tier)
		}
	}

	return d.store.ListNodes(ctx, hypergraph.NodeFilter{
		Tiers: includeTiers,
		Limit: 10000,
	})
}

// confidenceBucket returns a human-readable bucket for a confidence value.
func confidenceBucket(confidence float64) string {
	switch {
	case confidence >= 0.9:
		return "high (90-100%)"
	case confidence >= 0.7:
		return "good (70-90%)"
	case confidence >= 0.5:
		return "medium (50-70%)"
	case confidence >= 0.3:
		return "low (30-50%)"
	default:
		return "critical (<30%)"
	}
}
