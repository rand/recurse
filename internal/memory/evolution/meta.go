package evolution

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

// MetaEvolutionConfig configures the meta-evolution manager.
type MetaEvolutionConfig struct {
	// Detector configuration
	Detector DetectorConfig

	// AnalysisInterval is how often to run pattern detection.
	AnalysisInterval time.Duration

	// MaxPendingProposals is the maximum proposals to keep pending.
	MaxPendingProposals int

	// AutoApplyLowRisk auto-applies low-risk proposals with high confidence.
	AutoApplyLowRisk bool

	// AutoApplyConfidenceThreshold is minimum confidence for auto-apply.
	AutoApplyConfidenceThreshold float64

	// ProposalExpiry is how long proposals stay pending before expiring.
	ProposalExpiry time.Duration

	// Enabled controls whether meta-evolution runs.
	Enabled bool
}

// DefaultMetaEvolutionConfig returns sensible defaults.
func DefaultMetaEvolutionConfig() MetaEvolutionConfig {
	return MetaEvolutionConfig{
		Detector:                     DefaultDetectorConfig(),
		AnalysisInterval:             time.Hour,
		MaxPendingProposals:          10,
		AutoApplyLowRisk:             false,
		AutoApplyConfidenceThreshold: 0.9,
		ProposalExpiry:               7 * 24 * time.Hour,
		Enabled:                      true,
	}
}

// MetaEvolutionManager orchestrates memory architecture adaptation.
// It detects patterns, generates proposals, and applies approved changes.
type MetaEvolutionManager struct {
	mu            sync.RWMutex
	store         *hypergraph.Store
	config        MetaEvolutionConfig
	detector      *PatternDetector
	generator     *ProposalGenerator
	proposalStore ProposalStore
	outcomeStore  OutcomeStore
	audit         *AuditLogger

	// Callbacks
	onNewProposal      []func(*Proposal)
	onProposalDecision []func(*Proposal, ProposalDecision)

	// Background analysis
	stopAnalysis chan struct{}
	analysisWg   sync.WaitGroup
}

// NewMetaEvolutionManager creates a new meta-evolution manager.
func NewMetaEvolutionManager(
	store *hypergraph.Store,
	proposalStore ProposalStore,
	outcomeStore OutcomeStore,
	audit *AuditLogger,
	config MetaEvolutionConfig,
) *MetaEvolutionManager {
	detector := NewPatternDetector(store, outcomeStore, config.Detector)
	generator := NewProposalGenerator()

	return &MetaEvolutionManager{
		store:         store,
		config:        config,
		detector:      detector,
		generator:     generator,
		proposalStore: proposalStore,
		outcomeStore:  outcomeStore,
		audit:         audit,
		stopAnalysis:  make(chan struct{}),
	}
}

// OnNewProposal registers a callback for when new proposals are generated.
func (m *MetaEvolutionManager) OnNewProposal(callback func(*Proposal)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onNewProposal = append(m.onNewProposal, callback)
}

// OnProposalDecision registers a callback for proposal decisions.
func (m *MetaEvolutionManager) OnProposalDecision(callback func(*Proposal, ProposalDecision)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onProposalDecision = append(m.onProposalDecision, callback)
}

// RunAnalysis performs pattern detection and generates proposals.
func (m *MetaEvolutionManager) RunAnalysis(ctx context.Context) (*AnalysisResult, error) {
	if !m.config.Enabled {
		return &AnalysisResult{Skipped: true, Reason: "meta-evolution disabled"}, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	start := time.Now()
	result := &AnalysisResult{
		Timestamp: start,
	}

	// Check pending proposal limit
	pending, err := m.proposalStore.List(ctx, ProposalFilter{
		Status: []ProposalStatus{ProposalStatusPending},
	})
	if err != nil {
		return nil, fmt.Errorf("list pending proposals: %w", err)
	}

	if len(pending) >= m.config.MaxPendingProposals {
		result.Skipped = true
		result.Reason = fmt.Sprintf("max pending proposals reached (%d)", len(pending))
		return result, nil
	}

	// Detect patterns
	patterns, err := m.detector.DetectPatterns(ctx)
	if err != nil {
		return nil, fmt.Errorf("detect patterns: %w", err)
	}
	result.PatternsDetected = len(patterns)

	// Generate proposals from patterns
	for _, pattern := range patterns {
		proposal := m.generator.Generate(pattern)
		if proposal == nil {
			continue
		}

		// Check for duplicate proposals
		if m.isDuplicateProposal(ctx, proposal, pending) {
			result.DuplicatesSkipped++
			continue
		}

		// Save proposal
		if err := m.proposalStore.Save(ctx, proposal); err != nil {
			result.Errors = append(result.Errors, err)
			continue
		}

		result.ProposalsGenerated++
		pending = append(pending, proposal) // Track to avoid duplicates in same run

		// Invoke callbacks
		for _, cb := range m.onNewProposal {
			cb(proposal)
		}

		// Auto-apply if configured and meets criteria
		if m.shouldAutoApply(proposal) {
			decision := ProposalDecision{
				ProposalID: proposal.ID,
				Action:     ActionApprove,
				Reason:     "auto-approved: low risk, high confidence",
				DecidedBy:  "meta-evolution-auto",
				DecidedAt:  time.Now(),
			}
			if err := m.HandleDecision(ctx, decision); err != nil {
				result.Errors = append(result.Errors, err)
			} else {
				result.AutoApplied++
			}
		}
	}

	result.Duration = time.Since(start)

	// Log the analysis
	if m.audit != nil {
		m.audit.Log(AuditEntry{
			EventType: "meta_analysis",
			Details: map[string]any{
				"patterns_detected":   result.PatternsDetected,
				"proposals_generated": result.ProposalsGenerated,
				"auto_applied":        result.AutoApplied,
				"duration":            result.Duration.String(),
			},
			Result: &AuditResult{
				Success: len(result.Errors) == 0,
			},
		})
	}

	return result, nil
}

// AnalysisResult contains the outcome of a pattern analysis run.
type AnalysisResult struct {
	Timestamp          time.Time
	PatternsDetected   int
	ProposalsGenerated int
	DuplicatesSkipped  int
	AutoApplied        int
	Duration           time.Duration
	Skipped            bool
	Reason             string
	Errors             []error
}

// HandleDecision processes a user decision on a proposal.
func (m *MetaEvolutionManager) HandleDecision(ctx context.Context, decision ProposalDecision) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	proposal, err := m.proposalStore.Get(ctx, decision.ProposalID)
	if err != nil {
		return fmt.Errorf("get proposal: %w", err)
	}

	if proposal.Status != ProposalStatusPending && proposal.Status != ProposalStatusDeferred {
		return fmt.Errorf("proposal %s is not pending (status: %s)", decision.ProposalID, proposal.Status)
	}

	proposal.UpdatedAt = time.Now()

	switch decision.Action {
	case ActionApprove:
		// Apply the schema changes
		if err := m.applyChanges(ctx, proposal); err != nil {
			proposal.Status = ProposalStatusFailed
			proposal.StatusNote = err.Error()
			m.proposalStore.Update(ctx, proposal)
			return fmt.Errorf("apply changes: %w", err)
		}
		proposal.Status = ProposalStatusApplied
		proposal.AppliedAt = time.Now()
		proposal.StatusNote = decision.Reason

		if m.audit != nil {
			m.audit.Log(AuditEntry{
				EventType: "proposal_applied",
				Details: map[string]any{
					"proposal_id": proposal.ID,
					"type":        proposal.Type,
					"decided_by":  decision.DecidedBy,
				},
				Result: &AuditResult{Success: true},
			})
		}

	case ActionReject:
		proposal.Status = ProposalStatusRejected
		proposal.StatusNote = decision.Reason

		if m.audit != nil {
			m.audit.Log(AuditEntry{
				EventType: "proposal_rejected",
				Details: map[string]any{
					"proposal_id": proposal.ID,
					"type":        proposal.Type,
					"reason":      decision.Reason,
					"decided_by":  decision.DecidedBy,
				},
				Result: &AuditResult{Success: true},
			})
		}

	case ActionDefer:
		proposal.Status = ProposalStatusDeferred
		proposal.DeferUntil = decision.DeferUntil
		proposal.StatusNote = decision.Reason
	}

	if err := m.proposalStore.Update(ctx, proposal); err != nil {
		return fmt.Errorf("update proposal: %w", err)
	}

	// Invoke callbacks
	for _, cb := range m.onProposalDecision {
		cb(proposal, decision)
	}

	return nil
}

// applyChanges applies the schema changes from a proposal.
func (m *MetaEvolutionManager) applyChanges(ctx context.Context, proposal *Proposal) error {
	for _, change := range proposal.Changes {
		if err := m.applyChange(ctx, change); err != nil {
			return fmt.Errorf("apply change %s: %w", change.Operation, err)
		}
	}
	return nil
}

// applyChange applies a single schema change.
func (m *MetaEvolutionManager) applyChange(ctx context.Context, change SchemaChange) error {
	switch change.Operation {
	case "add_subtype":
		return m.applyAddSubtype(ctx, change)
	case "update_config":
		return m.applyUpdateConfig(ctx, change)
	case "adjust_decay":
		return m.applyAdjustDecay(ctx, change)
	case "tune_retrieval":
		return m.applyTuneRetrieval(ctx, change)
	default:
		return fmt.Errorf("unknown operation: %s", change.Operation)
	}
}

func (m *MetaEvolutionManager) applyAddSubtype(ctx context.Context, change SchemaChange) error {
	parentType := change.Target
	name, _ := change.Parameters["name"].(string)
	nodeIDs, _ := change.Parameters["node_ids"].([]string)

	if name == "" {
		return fmt.Errorf("subtype name required")
	}

	// Update nodes with the new subtype
	for _, nodeID := range nodeIDs {
		node, err := m.store.GetNode(ctx, nodeID)
		if err != nil {
			continue // Skip nodes that can't be found
		}

		if string(node.Type) == parentType {
			node.Subtype = name
			if err := m.store.UpdateNode(ctx, node); err != nil {
				return fmt.Errorf("update node %s: %w", nodeID, err)
			}
		}
	}

	return nil
}

func (m *MetaEvolutionManager) applyUpdateConfig(_ context.Context, _ SchemaChange) error {
	// This would update retrieval configuration
	// Implementation depends on how retrieval config is stored
	return nil
}

func (m *MetaEvolutionManager) applyAdjustDecay(_ context.Context, _ SchemaChange) error {
	// This would adjust decay parameters
	// Implementation depends on decay configuration storage
	return nil
}

func (m *MetaEvolutionManager) applyTuneRetrieval(_ context.Context, _ SchemaChange) error {
	// This would tune retrieval parameters
	// Implementation depends on retrieval configuration
	return nil
}

// isDuplicateProposal checks if a similar proposal already exists.
func (m *MetaEvolutionManager) isDuplicateProposal(_ context.Context, new *Proposal, existing []*Proposal) bool {
	for _, p := range existing {
		if p.Type == new.Type && p.SourcePattern == new.SourcePattern {
			// Check if targeting same entity
			if len(new.Changes) > 0 && len(p.Changes) > 0 {
				if new.Changes[0].Target == p.Changes[0].Target {
					return true
				}
			}
		}
	}
	return false
}

// shouldAutoApply determines if a proposal should be auto-applied.
func (m *MetaEvolutionManager) shouldAutoApply(proposal *Proposal) bool {
	if !m.config.AutoApplyLowRisk {
		return false
	}

	return proposal.Impact.RiskLevel == "low" &&
		proposal.Impact.Reversible &&
		proposal.Confidence >= m.config.AutoApplyConfidenceThreshold
}

// GetPendingProposals returns all pending proposals.
func (m *MetaEvolutionManager) GetPendingProposals(ctx context.Context) ([]*Proposal, error) {
	return m.proposalStore.List(ctx, ProposalFilter{
		Status: []ProposalStatus{ProposalStatusPending, ProposalStatusDeferred},
	})
}

// GetProposal retrieves a specific proposal by ID.
func (m *MetaEvolutionManager) GetProposal(ctx context.Context, id string) (*Proposal, error) {
	return m.proposalStore.Get(ctx, id)
}

// GetProposalStats returns aggregate statistics about proposals.
func (m *MetaEvolutionManager) GetProposalStats(ctx context.Context) (*ProposalStats, error) {
	proposals, err := m.proposalStore.List(ctx, ProposalFilter{})
	if err != nil {
		return nil, err
	}
	return CalculateStats(proposals), nil
}

// RecordOutcome records a retrieval outcome for pattern detection.
func (m *MetaEvolutionManager) RecordOutcome(ctx context.Context, outcome RetrievalOutcome) error {
	if m.outcomeStore == nil {
		return nil
	}
	outcome.Timestamp = time.Now()
	return m.outcomeStore.RecordOutcome(ctx, outcome)
}

// StartBackgroundAnalysis starts periodic pattern analysis.
func (m *MetaEvolutionManager) StartBackgroundAnalysis(ctx context.Context) {
	if m.config.AnalysisInterval <= 0 {
		return
	}

	m.analysisWg.Add(1)
	go func() {
		defer m.analysisWg.Done()
		ticker := time.NewTicker(m.config.AnalysisInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-m.stopAnalysis:
				return
			case <-ticker.C:
				m.RunAnalysis(ctx)
			}
		}
	}()
}

// StopBackgroundAnalysis stops periodic pattern analysis.
func (m *MetaEvolutionManager) StopBackgroundAnalysis() {
	close(m.stopAnalysis)
	m.analysisWg.Wait()
	m.stopAnalysis = make(chan struct{}) // Reset for potential restart
}

// ExpirePendingProposals marks old pending proposals as expired.
func (m *MetaEvolutionManager) ExpirePendingProposals(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-m.config.ProposalExpiry)

	proposals, err := m.proposalStore.List(ctx, ProposalFilter{
		Status: []ProposalStatus{ProposalStatusPending},
		Until:  cutoff,
	})
	if err != nil {
		return 0, err
	}

	expired := 0
	for _, p := range proposals {
		if p.CreatedAt.Before(cutoff) {
			p.Status = ProposalStatusRejected
			p.StatusNote = "expired: no decision within expiry period"
			p.UpdatedAt = time.Now()
			if err := m.proposalStore.Update(ctx, p); err == nil {
				expired++
			}
		}
	}

	return expired, nil
}

// ReactivateDeferredProposals reactivates proposals whose defer period has passed.
func (m *MetaEvolutionManager) ReactivateDeferredProposals(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	proposals, err := m.proposalStore.List(ctx, ProposalFilter{
		Status: []ProposalStatus{ProposalStatusDeferred},
	})
	if err != nil {
		return 0, err
	}

	reactivated := 0
	for _, p := range proposals {
		if !p.DeferUntil.IsZero() && p.DeferUntil.Before(now) {
			p.Status = ProposalStatusPending
			p.StatusNote = "reactivated: defer period ended"
			p.DeferUntil = time.Time{}
			p.UpdatedAt = now
			if err := m.proposalStore.Update(ctx, p); err == nil {
				reactivated++
			}
		}
	}

	return reactivated, nil
}
