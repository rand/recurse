package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

const (
	subtypeProfile = "routing_profile"
	subtypeHistory = "routing_history"
)

// Store persists routing data using the hypergraph.
type Store struct {
	graph *hypergraph.Store
}

// NewStore creates a new routing store.
func NewStore(graph *hypergraph.Store) *Store {
	return &Store{graph: graph}
}

// SaveProfile persists a model profile.
func (s *Store) SaveProfile(ctx context.Context, p *ModelProfile) error {
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}

	node := &hypergraph.Node{
		ID:        fmt.Sprintf("profile:%s", p.ID),
		Type:      hypergraph.NodeTypeFact,
		Subtype:   subtypeProfile,
		Content:   string(data),
		CreatedAt: time.Now(),
		UpdatedAt: p.UpdatedAt,
		Tier:      hypergraph.TierLongterm,
	}

	// Try update first, then create
	existing, err := s.graph.GetNode(ctx, node.ID)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return fmt.Errorf("check existing: %w", err)
	}
	if existing != nil {
		existing.Content = string(data)
		existing.UpdatedAt = p.UpdatedAt
		return s.graph.UpdateNode(ctx, existing)
	}

	return s.graph.CreateNode(ctx, node)
}

// GetProfile retrieves a model profile by ID.
func (s *Store) GetProfile(ctx context.Context, modelID string) (*ModelProfile, error) {
	node, err := s.graph.GetNode(ctx, fmt.Sprintf("profile:%s", modelID))
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, err
	}
	if node == nil {
		return nil, nil
	}

	var p ModelProfile
	if err := json.Unmarshal([]byte(node.Content), &p); err != nil {
		return nil, fmt.Errorf("unmarshal profile: %w", err)
	}
	return &p, nil
}

// ListProfiles returns all stored model profiles.
func (s *Store) ListProfiles(ctx context.Context) ([]*ModelProfile, error) {
	nodes, err := s.graph.ListNodes(ctx, hypergraph.NodeFilter{
		Types:    []hypergraph.NodeType{hypergraph.NodeTypeFact},
		Subtypes: []string{subtypeProfile},
	})
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}

	profiles := make([]*ModelProfile, 0, len(nodes))
	for _, node := range nodes {
		var p ModelProfile
		if err := json.Unmarshal([]byte(node.Content), &p); err != nil {
			continue // Skip malformed entries
		}
		profiles = append(profiles, &p)
	}
	return profiles, nil
}

// DeleteProfile removes a model profile.
func (s *Store) DeleteProfile(ctx context.Context, modelID string) error {
	return s.graph.DeleteNode(ctx, fmt.Sprintf("profile:%s", modelID))
}

// RecordHistory saves a routing history entry.
func (s *Store) RecordHistory(ctx context.Context, entry *RoutingHistoryEntry) error {
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("hist:%d", time.Now().UnixNano())
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal history: %w", err)
	}

	node := &hypergraph.Node{
		ID:        entry.ID,
		Type:      hypergraph.NodeTypeExperience,
		Subtype:   subtypeHistory,
		Content:   string(data),
		CreatedAt: entry.Timestamp,
		UpdatedAt: entry.Timestamp,
		Tier:      hypergraph.TierSession,
	}

	return s.graph.CreateNode(ctx, node)
}

// GetHistory retrieves routing history with optional filters.
func (s *Store) GetHistory(ctx context.Context, modelID string, limit int) ([]*RoutingHistoryEntry, error) {
	actualLimit := limit
	if actualLimit <= 0 {
		actualLimit = 1000 // Default limit
	}

	nodes, err := s.graph.ListNodes(ctx, hypergraph.NodeFilter{
		Types:    []hypergraph.NodeType{hypergraph.NodeTypeExperience},
		Subtypes: []string{subtypeHistory},
		Limit:    actualLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}

	entries := make([]*RoutingHistoryEntry, 0, len(nodes))
	for _, node := range nodes {
		var entry RoutingHistoryEntry
		if err := json.Unmarshal([]byte(node.Content), &entry); err != nil {
			continue
		}
		// Filter by model if specified
		if modelID != "" && entry.ModelUsed != modelID {
			continue
		}
		entries = append(entries, &entry)
	}

	return entries, nil
}

// GetRecentHistory returns the most recent history entries.
func (s *Store) GetRecentHistory(ctx context.Context, limit int) ([]*RoutingHistoryEntry, error) {
	return s.GetHistory(ctx, "", limit)
}

// GetHistoryByModel returns history for a specific model.
func (s *Store) GetHistoryByModel(ctx context.Context, modelID string, limit int) ([]*RoutingHistoryEntry, error) {
	return s.GetHistory(ctx, modelID, limit)
}

// GetHistoryByCategory returns history entries for a specific task category.
func (s *Store) GetHistoryByCategory(ctx context.Context, category TaskCategory, limit int) ([]*RoutingHistoryEntry, error) {
	actualLimit := limit
	if actualLimit <= 0 {
		actualLimit = 1000
	}

	nodes, err := s.graph.ListNodes(ctx, hypergraph.NodeFilter{
		Types:    []hypergraph.NodeType{hypergraph.NodeTypeExperience},
		Subtypes: []string{subtypeHistory},
		Limit:    actualLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("get history by category: %w", err)
	}

	entries := make([]*RoutingHistoryEntry, 0)
	for _, node := range nodes {
		var entry RoutingHistoryEntry
		if err := json.Unmarshal([]byte(node.Content), &entry); err != nil {
			continue
		}
		if entry.Features != nil && entry.Features.Category == category {
			entries = append(entries, &entry)
			if limit > 0 && len(entries) >= limit {
				break
			}
		}
	}
	return entries, nil
}

// Stats returns storage statistics.
func (s *Store) Stats(ctx context.Context) (*StoreStats, error) {
	profiles, err := s.ListProfiles(ctx)
	if err != nil {
		return nil, err
	}

	history, err := s.GetRecentHistory(ctx, 0)
	if err != nil {
		return nil, err
	}

	// Calculate stats
	stats := &StoreStats{
		ProfileCount: len(profiles),
		HistoryCount: len(history),
	}

	// Count outcomes
	for _, h := range history {
		switch h.Outcome {
		case OutcomeSuccess:
			stats.SuccessCount++
		case OutcomeCorrected:
			stats.CorrectedCount++
		case OutcomeFailed:
			stats.FailedCount++
		}
	}

	// Count by model
	stats.HistoryByModel = make(map[string]int)
	for _, h := range history {
		stats.HistoryByModel[h.ModelUsed]++
	}

	return stats, nil
}

// StoreStats contains storage statistics.
type StoreStats struct {
	ProfileCount   int            `json:"profile_count"`
	HistoryCount   int            `json:"history_count"`
	SuccessCount   int            `json:"success_count"`
	CorrectedCount int            `json:"corrected_count"`
	FailedCount    int            `json:"failed_count"`
	HistoryByModel map[string]int `json:"history_by_model"`
}

// PruneHistory removes old history entries.
func (s *Store) PruneHistory(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)

	nodes, err := s.graph.ListNodes(ctx, hypergraph.NodeFilter{
		Types:    []hypergraph.NodeType{hypergraph.NodeTypeExperience},
		Subtypes: []string{subtypeHistory},
	})
	if err != nil {
		return 0, fmt.Errorf("list history for prune: %w", err)
	}

	pruned := 0
	for _, node := range nodes {
		var entry RoutingHistoryEntry
		if err := json.Unmarshal([]byte(node.Content), &entry); err != nil {
			continue
		}
		if entry.Timestamp.Before(cutoff) {
			if err := s.graph.DeleteNode(ctx, node.ID); err == nil {
				pruned++
			}
		}
	}

	return pruned, nil
}
