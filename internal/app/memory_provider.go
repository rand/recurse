package app

import (
	"context"

	"github.com/rand/recurse/internal/memory/evolution"
	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/tui/components/dialogs/memory"
)

// MemoryStoreAdapter adapts hypergraph.Store to the memory.MemoryProvider interface.
type MemoryStoreAdapter struct {
	store *hypergraph.Store
}

// NewMemoryStoreAdapter creates a new adapter for the hypergraph store.
func NewMemoryStoreAdapter(store *hypergraph.Store) *MemoryStoreAdapter {
	return &MemoryStoreAdapter{store: store}
}

// GetRecent returns recent nodes from memory.
func (a *MemoryStoreAdapter) GetRecent(limit int) ([]*hypergraph.Node, error) {
	ctx := context.Background()
	return a.store.RecentNodes(ctx, limit, nil)
}

// Search searches nodes by content.
func (a *MemoryStoreAdapter) Search(query string, limit int) ([]*hypergraph.Node, error) {
	ctx := context.Background()
	results, err := a.store.Search(ctx, query, hypergraph.SearchOptions{
		Limit: limit,
	})
	if err != nil {
		return nil, err
	}

	// Extract nodes from search results
	nodes := make([]*hypergraph.Node, len(results))
	for i, r := range results {
		nodes[i] = r.Node
	}
	return nodes, nil
}

// Store returns the underlying hypergraph store.
func (a *MemoryStoreAdapter) Store() *hypergraph.Store {
	return a.store
}

// GetStats returns memory statistics.
func (a *MemoryStoreAdapter) GetStats() (memory.MemoryStats, error) {
	ctx := context.Background()
	stats, err := a.store.Stats(ctx)
	if err != nil {
		return memory.MemoryStats{}, err
	}

	// Convert hypergraph.Stats to memory.MemoryStats
	byType := make(map[hypergraph.NodeType]int)
	for k, v := range stats.NodesByType {
		byType[hypergraph.NodeType(k)] = int(v)
	}

	byTier := make(map[hypergraph.Tier]int)
	for k, v := range stats.NodesByTier {
		byTier[hypergraph.Tier(k)] = int(v)
	}

	return memory.MemoryStats{
		TotalNodes: int(stats.NodeCount),
		ByType:     byType,
		ByTier:     byTier,
	}, nil
}

// ProposalProviderAdapter adapts the MetaEvolutionManager to the memory.ProposalProvider interface.
// This surfaces meta-evolution proposals to the TUI for user review.
type ProposalProviderAdapter struct {
	manager *evolution.MetaEvolutionManager
}

// NewProposalProviderAdapter creates a new adapter for meta-evolution proposals.
func NewProposalProviderAdapter(manager *evolution.MetaEvolutionManager) *ProposalProviderAdapter {
	if manager == nil {
		return nil
	}
	return &ProposalProviderAdapter{manager: manager}
}

// GetPendingProposals returns pending schema evolution proposals.
func (a *ProposalProviderAdapter) GetPendingProposals(ctx context.Context) ([]*evolution.Proposal, error) {
	return a.manager.GetPendingProposals(ctx)
}

// ApproveProposal approves a proposal for application.
func (a *ProposalProviderAdapter) ApproveProposal(ctx context.Context, id string) error {
	decision := evolution.ProposalDecision{
		ProposalID: id,
		Action:     evolution.ActionApprove,
		Reason:     "approved via TUI",
		DecidedBy:  "user",
	}
	return a.manager.HandleDecision(ctx, decision)
}

// RejectProposal rejects a proposal.
func (a *ProposalProviderAdapter) RejectProposal(ctx context.Context, id, reason string) error {
	decision := evolution.ProposalDecision{
		ProposalID: id,
		Action:     evolution.ActionReject,
		Reason:     reason,
		DecidedBy:  "user",
	}
	return a.manager.HandleDecision(ctx, decision)
}

// DeferProposal defers a proposal for later review.
func (a *ProposalProviderAdapter) DeferProposal(ctx context.Context, id string) error {
	decision := evolution.ProposalDecision{
		ProposalID: id,
		Action:     evolution.ActionDefer,
		Reason:     "deferred via TUI",
		DecidedBy:  "user",
	}
	return a.manager.HandleDecision(ctx, decision)
}
