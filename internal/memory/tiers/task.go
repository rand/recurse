// Package tiers implements memory tier management for the hypergraph store.
package tiers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

// FactVerifier verifies facts before storage.
// [SPEC-08.15] Interface for hallucination detection integration.
type FactVerifier interface {
	// VerifyFact checks if a fact should be stored based on evidence.
	// Returns allowed (bool), adjusted confidence, and any error.
	VerifyFact(ctx context.Context, content string, evidence string, confidence float64) (allowed bool, adjustedConfidence float64, err error)

	// Enabled returns whether verification is active.
	Enabled() bool
}

// TaskMemoryConfig configures task tier behavior.
type TaskMemoryConfig struct {
	// Maximum number of nodes in task tier
	MaxNodes int

	// Automatically consolidate when adding nodes
	AutoConsolidate bool

	// Threshold for considering nodes similar (for deduplication)
	SimilarityThreshold float64

	// RequireEvidence rejects facts without evidence when verification is enabled
	RequireEvidence bool
}

// DefaultTaskConfig returns the default task tier configuration.
func DefaultTaskConfig() TaskMemoryConfig {
	return TaskMemoryConfig{
		MaxNodes:            1000,
		AutoConsolidate:     true,
		SimilarityThreshold: 0.9,
	}
}

// ExperienceMetadata contains the full metadata stored with experience nodes.
// This is the JSON structure stored in node.Metadata.
type ExperienceMetadata struct {
	// Core fields (always present)
	Outcome string    `json:"outcome"`
	Success bool      `json:"success"`
	Time    time.Time `json:"time"`

	// Extended fields (optional, for richer context)
	TaskDescription  string   `json:"task_description,omitempty"`  // What was being done
	Approach         string   `json:"approach,omitempty"`          // How it was approached
	FilesModified    []string `json:"files_modified,omitempty"`    // File provenance
	BlockersHit      []string `json:"blockers_hit,omitempty"`      // Obstacles encountered
	InsightsGained   []string `json:"insights_gained,omitempty"`   // What was learned
	RelatedDecisions []string `json:"related_decisions,omitempty"` // Links to decision node IDs
	Duration         string   `json:"duration,omitempty"`          // How long it took
}

// ExperienceOptions provides optional extended fields when adding experiences.
type ExperienceOptions struct {
	TaskDescription  string
	Approach         string
	FilesModified    []string
	BlockersHit      []string
	InsightsGained   []string
	RelatedDecisions []string
	Duration         time.Duration
}

// TaskMemory manages task-tier working memory.
// It provides high-level operations for capturing and retrieving
// facts, snippets, and relations during active problem-solving.
type TaskMemory struct {
	store  *hypergraph.Store
	config TaskMemoryConfig
	logger *slog.Logger

	// Current task context
	taskID      string
	taskStarted time.Time

	// Hallucination detection [SPEC-08.15-18]
	verifier FactVerifier
}

// NewTaskMemory creates a new task memory manager.
func NewTaskMemory(store *hypergraph.Store, config TaskMemoryConfig) *TaskMemory {
	if config.MaxNodes == 0 {
		config = DefaultTaskConfig()
	}
	return &TaskMemory{
		store:       store,
		config:      config,
		logger:      slog.Default(),
		taskID:      fmt.Sprintf("task-%d", time.Now().UnixNano()),
		taskStarted: time.Now(),
	}
}

// SetFactVerifier sets the fact verifier for hallucination detection.
// [SPEC-08.15]
func (tm *TaskMemory) SetFactVerifier(verifier FactVerifier) {
	tm.verifier = verifier
}

// SetLogger sets the logger for task memory.
func (tm *TaskMemory) SetLogger(logger *slog.Logger) {
	tm.logger = logger
}

// StartTask begins a new task context.
func (tm *TaskMemory) StartTask(ctx context.Context, description string) error {
	tm.taskID = fmt.Sprintf("task-%d", time.Now().UnixNano())
	tm.taskStarted = time.Now()

	// Create a task node to anchor this context
	node := hypergraph.NewNode(hypergraph.NodeTypeEntity, description)
	node.Subtype = "task"
	node.Tier = hypergraph.TierTask
	node.Metadata, _ = json.Marshal(map[string]any{
		"task_id":    tm.taskID,
		"started_at": tm.taskStarted,
	})

	return tm.store.CreateNode(ctx, node)
}

// AddFact stores a fact in working memory.
// This delegates to AddFactWithEvidence with empty evidence.
func (tm *TaskMemory) AddFact(ctx context.Context, content string, confidence float64) (*hypergraph.Node, error) {
	return tm.AddFactWithEvidence(ctx, content, confidence, "")
}

// AddFactWithEvidence stores a fact in working memory after optional verification.
// [SPEC-08.15-18] Verifies facts before storage when a verifier is configured.
func (tm *TaskMemory) AddFactWithEvidence(ctx context.Context, content string, confidence float64, evidence string) (*hypergraph.Node, error) {
	// [SPEC-08.15] Verify facts before storage
	if tm.verifier != nil && tm.verifier.Enabled() {
		// Require evidence if configured
		if evidence == "" && tm.config.RequireEvidence {
			tm.logger.Warn("fact rejected - no evidence provided",
				"content", truncateContent(content, 50),
			)
			return nil, fmt.Errorf("add fact: evidence required for verification")
		}

		// Verify the fact
		allowed, adjustedConfidence, err := tm.verifier.VerifyFact(ctx, content, evidence, confidence)
		if err != nil {
			// Log but continue - graceful degradation handled by verifier
			tm.logger.Warn("fact verification error",
				"content", truncateContent(content, 50),
				"error", err,
			)
		}

		if !allowed {
			tm.logger.Info("fact rejected by verifier",
				"content", truncateContent(content, 50),
				"original_confidence", confidence,
			)
			return nil, fmt.Errorf("add fact: rejected by hallucination verifier")
		}

		// Use adjusted confidence
		confidence = adjustedConfidence
		tm.logger.Debug("fact verified",
			"content", truncateContent(content, 50),
			"original_confidence", confidence,
			"adjusted_confidence", adjustedConfidence,
		)
	}

	if tm.config.AutoConsolidate {
		// Check for similar existing facts
		if existing, _ := tm.findSimilar(ctx, content, hypergraph.NodeTypeFact); existing != nil {
			// Update access count and return existing
			tm.store.IncrementAccess(ctx, existing.ID)
			existing.AccessCount++ // Reflect the increment in the returned struct
			return existing, nil
		}
	}

	node := hypergraph.NewNode(hypergraph.NodeTypeFact, content)
	node.Tier = hypergraph.TierTask
	node.Confidence = confidence

	if err := tm.store.CreateNode(ctx, node); err != nil {
		return nil, fmt.Errorf("add fact: %w", err)
	}

	if err := tm.checkCapacity(ctx); err != nil {
		return node, err // Return node but also the capacity warning
	}

	return node, nil
}

// truncateContent truncates a string for logging.
func truncateContent(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// AddSnippet stores a code snippet in working memory.
func (tm *TaskMemory) AddSnippet(ctx context.Context, content string, file string, line int) (*hypergraph.Node, error) {
	node := hypergraph.NewNode(hypergraph.NodeTypeSnippet, content)
	node.Tier = hypergraph.TierTask
	node.Provenance, _ = json.Marshal(hypergraph.Provenance{
		File:   file,
		Line:   line,
		Source: "task",
	})

	if err := tm.store.CreateNode(ctx, node); err != nil {
		return nil, fmt.Errorf("add snippet: %w", err)
	}

	return node, nil
}

// AddEntity stores an entity (file, function, class, etc.) in working memory.
func (tm *TaskMemory) AddEntity(ctx context.Context, content string, subtype string) (*hypergraph.Node, error) {
	if tm.config.AutoConsolidate {
		// Check for existing entity with same content and subtype
		existing, err := tm.store.ListNodes(ctx, hypergraph.NodeFilter{
			Types:    []hypergraph.NodeType{hypergraph.NodeTypeEntity},
			Subtypes: []string{subtype},
			Tiers:    []hypergraph.Tier{hypergraph.TierTask},
			Limit:    100,
		})
		if err == nil {
			for _, e := range existing {
				if e.Content == content {
					tm.store.IncrementAccess(ctx, e.ID)
					e.AccessCount++ // Reflect the increment in the returned struct
					return e, nil
				}
			}
		}
	}

	node := hypergraph.NewNode(hypergraph.NodeTypeEntity, content)
	node.Subtype = subtype
	node.Tier = hypergraph.TierTask

	if err := tm.store.CreateNode(ctx, node); err != nil {
		return nil, fmt.Errorf("add entity: %w", err)
	}

	return node, nil
}

// AddDecision stores a decision with its rationale.
func (tm *TaskMemory) AddDecision(ctx context.Context, decision string, rationale string, alternatives []string) (*hypergraph.Node, error) {
	node := hypergraph.NewNode(hypergraph.NodeTypeDecision, decision)
	node.Tier = hypergraph.TierTask
	node.Metadata, _ = json.Marshal(map[string]any{
		"rationale":    rationale,
		"alternatives": alternatives,
		"decided_at":   time.Now(),
	})

	if err := tm.store.CreateNode(ctx, node); err != nil {
		return nil, fmt.Errorf("add decision: %w", err)
	}

	return node, nil
}

// AddExperience stores an experience (success/failure) for learning.
// This is the basic version - use AddExperienceWithOptions for richer context.
func (tm *TaskMemory) AddExperience(ctx context.Context, description string, outcome string, success bool) (*hypergraph.Node, error) {
	return tm.AddExperienceWithOptions(ctx, description, outcome, success, nil)
}

// AddExperienceWithOptions stores an experience with optional extended metadata.
// The opts parameter can be nil for basic experiences.
func (tm *TaskMemory) AddExperienceWithOptions(ctx context.Context, description string, outcome string, success bool, opts *ExperienceOptions) (*hypergraph.Node, error) {
	node := hypergraph.NewNode(hypergraph.NodeTypeExperience, description)
	node.Tier = hypergraph.TierTask

	// Build metadata with core and optional fields
	meta := ExperienceMetadata{
		Outcome: outcome,
		Success: success,
		Time:    time.Now(),
	}

	// Add extended fields if provided
	if opts != nil {
		meta.TaskDescription = opts.TaskDescription
		meta.Approach = opts.Approach
		meta.FilesModified = opts.FilesModified
		meta.BlockersHit = opts.BlockersHit
		meta.InsightsGained = opts.InsightsGained
		meta.RelatedDecisions = opts.RelatedDecisions
		if opts.Duration > 0 {
			meta.Duration = opts.Duration.String()
		}
	}

	node.Metadata, _ = json.Marshal(meta)

	// Successful experiences have higher confidence
	if success {
		node.Confidence = 1.0
	} else {
		node.Confidence = 0.5
	}

	if err := tm.store.CreateNode(ctx, node); err != nil {
		return nil, fmt.Errorf("add experience: %w", err)
	}

	return node, nil
}

// Relate creates a relationship between two nodes.
func (tm *TaskMemory) Relate(ctx context.Context, label string, subjectID, objectID string) (*hypergraph.Hyperedge, error) {
	return tm.store.CreateRelation(ctx, label, subjectID, objectID)
}

// GetContext retrieves the current working context.
// Returns nodes ordered by relevance (access count and recency).
func (tm *TaskMemory) GetContext(ctx context.Context, limit int) ([]*hypergraph.Node, error) {
	return tm.store.RecentNodes(ctx, limit, []hypergraph.Tier{hypergraph.TierTask})
}

// GetFacts retrieves all facts in working memory.
func (tm *TaskMemory) GetFacts(ctx context.Context) ([]*hypergraph.Node, error) {
	return tm.store.ListNodes(ctx, hypergraph.NodeFilter{
		Types: []hypergraph.NodeType{hypergraph.NodeTypeFact},
		Tiers: []hypergraph.Tier{hypergraph.TierTask},
	})
}

// GetRelated finds nodes related to the given node.
func (tm *TaskMemory) GetRelated(ctx context.Context, nodeID string, depth int) ([]*hypergraph.ConnectedNode, error) {
	return tm.store.GetConnected(ctx, nodeID, hypergraph.TraversalOptions{
		Direction: hypergraph.TraverseBoth,
		MaxDepth:  depth,
		Tiers:     []hypergraph.Tier{hypergraph.TierTask},
	})
}

// Search searches working memory by content.
func (tm *TaskMemory) Search(ctx context.Context, query string, limit int) ([]*hypergraph.SearchResult, error) {
	return tm.store.SearchByContent(ctx, query, hypergraph.SearchOptions{
		Tiers: []hypergraph.Tier{hypergraph.TierTask},
		Limit: limit,
	})
}

// Clear removes all task-tier nodes, preparing for a new task.
func (tm *TaskMemory) Clear(ctx context.Context) error {
	nodes, err := tm.store.ListNodes(ctx, hypergraph.NodeFilter{
		Tiers: []hypergraph.Tier{hypergraph.TierTask},
	})
	if err != nil {
		return fmt.Errorf("list task nodes: %w", err)
	}

	for _, node := range nodes {
		if err := tm.store.DeleteNode(ctx, node.ID); err != nil {
			return fmt.Errorf("delete node %s: %w", node.ID, err)
		}
	}

	return nil
}

// Stats returns statistics about the task memory.
func (tm *TaskMemory) Stats(ctx context.Context) (*TaskStats, error) {
	count, err := tm.store.CountNodes(ctx, hypergraph.NodeFilter{
		Tiers: []hypergraph.Tier{hypergraph.TierTask},
	})
	if err != nil {
		return nil, err
	}

	factCount, _ := tm.store.CountNodes(ctx, hypergraph.NodeFilter{
		Types: []hypergraph.NodeType{hypergraph.NodeTypeFact},
		Tiers: []hypergraph.Tier{hypergraph.TierTask},
	})

	entityCount, _ := tm.store.CountNodes(ctx, hypergraph.NodeFilter{
		Types: []hypergraph.NodeType{hypergraph.NodeTypeEntity},
		Tiers: []hypergraph.Tier{hypergraph.TierTask},
	})

	snippetCount, _ := tm.store.CountNodes(ctx, hypergraph.NodeFilter{
		Types: []hypergraph.NodeType{hypergraph.NodeTypeSnippet},
		Tiers: []hypergraph.Tier{hypergraph.TierTask},
	})

	decisionCount, _ := tm.store.CountNodes(ctx, hypergraph.NodeFilter{
		Types: []hypergraph.NodeType{hypergraph.NodeTypeDecision},
		Tiers: []hypergraph.Tier{hypergraph.TierTask},
	})

	return &TaskStats{
		TotalNodes:    int(count),
		MaxNodes:      tm.config.MaxNodes,
		Facts:         int(factCount),
		Entities:      int(entityCount),
		Snippets:      int(snippetCount),
		Decisions:     int(decisionCount),
		TaskID:        tm.taskID,
		TaskStarted:   tm.taskStarted,
		TaskDuration:  time.Since(tm.taskStarted),
		CapacityUsed:  float64(count) / float64(tm.config.MaxNodes),
	}, nil
}

// TaskStats holds statistics about task memory.
type TaskStats struct {
	TotalNodes   int           `json:"total_nodes"`
	MaxNodes     int           `json:"max_nodes"`
	Facts        int           `json:"facts"`
	Entities     int           `json:"entities"`
	Snippets     int           `json:"snippets"`
	Decisions    int           `json:"decisions"`
	TaskID       string        `json:"task_id"`
	TaskStarted  time.Time     `json:"task_started"`
	TaskDuration time.Duration `json:"task_duration"`
	CapacityUsed float64       `json:"capacity_used"`
}

// Helper methods

// findSimilar finds an existing node with similar content.
// For now, uses exact match; will be enhanced with embedding similarity.
func (tm *TaskMemory) findSimilar(ctx context.Context, content string, nodeType hypergraph.NodeType) (*hypergraph.Node, error) {
	results, err := tm.store.SearchByContent(ctx, content, hypergraph.SearchOptions{
		Types: []hypergraph.NodeType{nodeType},
		Tiers: []hypergraph.Tier{hypergraph.TierTask},
		Limit: 10,
	})
	if err != nil {
		return nil, err
	}

	// For now, require exact match
	// TODO: Implement embedding-based similarity
	for _, r := range results {
		if r.Node.Content == content {
			return r.Node, nil
		}
	}

	return nil, nil
}

// checkCapacity checks if we're at capacity and may need consolidation.
func (tm *TaskMemory) checkCapacity(ctx context.Context) error {
	count, err := tm.store.CountNodes(ctx, hypergraph.NodeFilter{
		Tiers: []hypergraph.Tier{hypergraph.TierTask},
	})
	if err != nil {
		return nil
	}

	if int(count) > tm.config.MaxNodes {
		// In Phase 2, we just warn; Phase 4 will implement actual consolidation
		return fmt.Errorf("task memory at capacity (%d/%d nodes)", count, tm.config.MaxNodes)
	}

	return nil
}

// Store returns the underlying hypergraph store.
func (tm *TaskMemory) Store() *hypergraph.Store {
	return tm.store
}

// Config returns the current configuration.
func (tm *TaskMemory) Config() TaskMemoryConfig {
	return tm.config
}
