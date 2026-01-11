package evolution

import (
	"context"
	"fmt"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

// PromotionConfig configures the tier promotion process.
type PromotionConfig struct {
	// MinAccessCount is the minimum access count to consider for promotion.
	MinAccessCount int

	// MinConfidence is the minimum confidence threshold for promotion.
	MinConfidence float64

	// MinAge is the minimum age of a node before it can be promoted.
	MinAge time.Duration

	// TaskToSessionThreshold is the access count threshold for task → session.
	TaskToSessionThreshold int

	// SessionToLongtermThreshold is the access count threshold for session → long-term.
	SessionToLongtermThreshold int

	// ConsolidateOnPromotion runs consolidation before promotion.
	ConsolidateOnPromotion bool
}

// DefaultPromotionConfig returns sensible defaults for promotion.
func DefaultPromotionConfig() PromotionConfig {
	return PromotionConfig{
		MinAccessCount:             1,
		MinConfidence:              0.5,
		MinAge:                     time.Minute * 5,
		TaskToSessionThreshold:     2,
		SessionToLongtermThreshold: 5,
		ConsolidateOnPromotion:     true,
	}
}

// PromotionResult contains the outcome of promotion.
type PromotionResult struct {
	// TaskToSession is the count of nodes promoted from task to session.
	TaskToSession int

	// SessionToLongterm is the count of nodes promoted from session to long-term.
	SessionToLongterm int

	// Skipped is the count of nodes that didn't meet promotion criteria.
	Skipped int

	// Duration of the promotion process.
	Duration time.Duration
}

// Promoter handles tier promotion logic.
type Promoter struct {
	store        *hypergraph.Store
	config       PromotionConfig
	consolidator *Consolidator
}

// NewPromoter creates a new promoter.
func NewPromoter(store *hypergraph.Store, config PromotionConfig) *Promoter {
	var consolidator *Consolidator
	if config.ConsolidateOnPromotion {
		consolidator = NewConsolidator(store, DefaultConsolidationConfig())
	}

	return &Promoter{
		store:        store,
		config:       config,
		consolidator: consolidator,
	}
}

// PromoteTaskToSession promotes qualifying nodes from task to session tier.
// Called when a task is completed.
func (p *Promoter) PromoteTaskToSession(ctx context.Context) (*PromotionResult, error) {
	start := time.Now()
	result := &PromotionResult{}

	// Optionally consolidate first
	if p.consolidator != nil {
		if _, err := p.consolidator.Consolidate(ctx, hypergraph.TierTask, hypergraph.TierSession); err != nil {
			return nil, fmt.Errorf("consolidate task tier: %w", err)
		}
	}

	// Get all task-tier nodes
	nodes, err := p.store.ListNodes(ctx, hypergraph.NodeFilter{
		Tiers: []hypergraph.Tier{hypergraph.TierTask},
		Limit: 1000,
	})
	if err != nil {
		return nil, fmt.Errorf("list task nodes: %w", err)
	}

	for _, node := range nodes {
		if p.shouldPromoteToSession(node) {
			node.Tier = hypergraph.TierSession
			if err := p.store.UpdateNode(ctx, node); err != nil {
				return nil, fmt.Errorf("promote node %s: %w", node.ID, err)
			}
			result.TaskToSession++
		} else {
			result.Skipped++
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// PromoteSessionToLongterm promotes qualifying nodes from session to long-term tier.
// Called at session end with reflection pass.
func (p *Promoter) PromoteSessionToLongterm(ctx context.Context) (*PromotionResult, error) {
	start := time.Now()
	result := &PromotionResult{}

	// Optionally consolidate first
	if p.consolidator != nil {
		if _, err := p.consolidator.Consolidate(ctx, hypergraph.TierSession, hypergraph.TierLongterm); err != nil {
			return nil, fmt.Errorf("consolidate session tier: %w", err)
		}
	}

	// Get all session-tier nodes
	nodes, err := p.store.ListNodes(ctx, hypergraph.NodeFilter{
		Tiers: []hypergraph.Tier{hypergraph.TierSession},
		Limit: 1000,
	})
	if err != nil {
		return nil, fmt.Errorf("list session nodes: %w", err)
	}

	for _, node := range nodes {
		if p.shouldPromoteToLongterm(node) {
			node.Tier = hypergraph.TierLongterm
			if err := p.store.UpdateNode(ctx, node); err != nil {
				return nil, fmt.Errorf("promote node %s: %w", node.ID, err)
			}
			result.SessionToLongterm++
		} else {
			result.Skipped++
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// PromoteAll runs full promotion pipeline: task → session → long-term.
func (p *Promoter) PromoteAll(ctx context.Context) (*PromotionResult, error) {
	start := time.Now()
	result := &PromotionResult{}

	// First promote task to session
	taskResult, err := p.PromoteTaskToSession(ctx)
	if err != nil {
		return nil, fmt.Errorf("task to session: %w", err)
	}
	result.TaskToSession = taskResult.TaskToSession

	// Then promote session to long-term
	sessionResult, err := p.PromoteSessionToLongterm(ctx)
	if err != nil {
		return nil, fmt.Errorf("session to longterm: %w", err)
	}
	result.SessionToLongterm = sessionResult.SessionToLongterm
	result.Skipped = taskResult.Skipped + sessionResult.Skipped

	result.Duration = time.Since(start)
	return result, nil
}

// shouldPromoteToSession checks if a node should be promoted from task to session.
func (p *Promoter) shouldPromoteToSession(node *hypergraph.Node) bool {
	// Check minimum confidence
	if node.Confidence < p.config.MinConfidence {
		return false
	}

	// Check minimum access count for task → session
	if node.AccessCount < p.config.TaskToSessionThreshold {
		return false
	}

	// Check minimum age
	if time.Since(node.CreatedAt) < p.config.MinAge {
		return false
	}

	return true
}

// shouldPromoteToLongterm checks if a node should be promoted from session to long-term.
func (p *Promoter) shouldPromoteToLongterm(node *hypergraph.Node) bool {
	// Check minimum confidence (higher threshold for long-term)
	if node.Confidence < p.config.MinConfidence {
		return false
	}

	// Check minimum access count for session → long-term
	if node.AccessCount < p.config.SessionToLongtermThreshold {
		return false
	}

	// Check minimum age (use longer age for long-term promotion)
	if time.Since(node.CreatedAt) < p.config.MinAge*2 {
		return false
	}

	return true
}

// ForcePromote promotes a specific node to the next tier regardless of criteria.
// Useful for explicit user actions or important discoveries.
func (p *Promoter) ForcePromote(ctx context.Context, nodeID string) error {
	node, err := p.store.GetNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("get node: %w", err)
	}

	switch node.Tier {
	case hypergraph.TierTask:
		node.Tier = hypergraph.TierSession
	case hypergraph.TierSession:
		node.Tier = hypergraph.TierLongterm
	case hypergraph.TierLongterm:
		// Already at highest tier (excluding archive)
		return nil
	case hypergraph.TierArchive:
		// Restore from archive to long-term
		node.Tier = hypergraph.TierLongterm
	}

	return p.store.UpdateNode(ctx, node)
}

// Demote moves a node to a lower tier. Useful for corrections or cleanup.
func (p *Promoter) Demote(ctx context.Context, nodeID string, targetTier hypergraph.Tier) error {
	node, err := p.store.GetNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("get node: %w", err)
	}

	// Validate target tier is lower
	tierOrder := map[hypergraph.Tier]int{
		hypergraph.TierTask:     0,
		hypergraph.TierSession:  1,
		hypergraph.TierLongterm: 2,
		hypergraph.TierArchive:  3,
	}

	if tierOrder[targetTier] >= tierOrder[node.Tier] && targetTier != hypergraph.TierArchive {
		return fmt.Errorf("target tier %s is not lower than current tier %s", targetTier, node.Tier)
	}

	node.Tier = targetTier
	return p.store.UpdateNode(ctx, node)
}

// GetPromotionCandidates returns nodes that are candidates for promotion.
func (p *Promoter) GetPromotionCandidates(ctx context.Context, sourceTier hypergraph.Tier) ([]*hypergraph.Node, error) {
	nodes, err := p.store.ListNodes(ctx, hypergraph.NodeFilter{
		Tiers: []hypergraph.Tier{sourceTier},
		Limit: 1000,
	})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	var candidates []*hypergraph.Node
	for _, node := range nodes {
		var shouldPromote bool
		switch sourceTier {
		case hypergraph.TierTask:
			shouldPromote = p.shouldPromoteToSession(node)
		case hypergraph.TierSession:
			shouldPromote = p.shouldPromoteToLongterm(node)
		}

		if shouldPromote {
			candidates = append(candidates, node)
		}
	}

	return candidates, nil
}

// Stats returns promotion statistics for all tiers.
func (p *Promoter) Stats(ctx context.Context) (*PromotionStats, error) {
	stats := &PromotionStats{}

	// Count nodes in each tier
	for _, tier := range []hypergraph.Tier{
		hypergraph.TierTask,
		hypergraph.TierSession,
		hypergraph.TierLongterm,
		hypergraph.TierArchive,
	} {
		count, err := p.store.CountNodes(ctx, hypergraph.NodeFilter{
			Tiers: []hypergraph.Tier{tier},
		})
		if err != nil {
			return nil, fmt.Errorf("count %s nodes: %w", tier, err)
		}

		switch tier {
		case hypergraph.TierTask:
			stats.TaskCount = int(count)
		case hypergraph.TierSession:
			stats.SessionCount = int(count)
		case hypergraph.TierLongterm:
			stats.LongtermCount = int(count)
		case hypergraph.TierArchive:
			stats.ArchiveCount = int(count)
		}
	}

	// Count promotion candidates
	taskCandidates, err := p.GetPromotionCandidates(ctx, hypergraph.TierTask)
	if err != nil {
		return nil, err
	}
	stats.TaskCandidates = len(taskCandidates)

	sessionCandidates, err := p.GetPromotionCandidates(ctx, hypergraph.TierSession)
	if err != nil {
		return nil, err
	}
	stats.SessionCandidates = len(sessionCandidates)

	return stats, nil
}

// PromotionStats contains tier statistics.
type PromotionStats struct {
	TaskCount         int
	SessionCount      int
	LongtermCount     int
	ArchiveCount      int
	TaskCandidates    int
	SessionCandidates int
}
