package evolution

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// ProposalType identifies the category of schema change proposal.
type ProposalType string

const (
	ProposalNewSubtype      ProposalType = "new_subtype"
	ProposalRenameType      ProposalType = "rename_type"
	ProposalMergeTypes      ProposalType = "merge_types"
	ProposalSplitType       ProposalType = "split_type"
	ProposalRetrievalConfig ProposalType = "retrieval_config"
	ProposalDecayAdjust     ProposalType = "decay_adjust"
)

// ProposalStatus tracks the lifecycle of a proposal.
type ProposalStatus string

const (
	ProposalStatusPending  ProposalStatus = "pending"
	ProposalStatusApproved ProposalStatus = "approved"
	ProposalStatusRejected ProposalStatus = "rejected"
	ProposalStatusApplied  ProposalStatus = "applied"
	ProposalStatusDeferred ProposalStatus = "deferred"
	ProposalStatusFailed   ProposalStatus = "failed"
)

// Proposal represents a suggested architectural change to the memory system.
type Proposal struct {
	ID          string
	Type        ProposalType
	Title       string
	Description string
	Rationale   string     // Why this change is suggested
	Evidence    []Evidence // Supporting data
	Impact      ImpactAssessment

	// The actual change
	Changes []SchemaChange

	// Metadata
	Confidence  float64
	Priority    int // 1-5, higher is more important
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Status      ProposalStatus
	StatusNote  string    // Reason for rejection, etc.
	DeferUntil  time.Time // For deferred proposals
	AppliedAt   time.Time // When changes were applied

	// Source pattern
	SourcePattern PatternType
}

// Evidence provides supporting data for a proposal.
type Evidence struct {
	Type       string  // "retrieval_stats", "cluster_analysis", "user_feedback"
	Summary    string
	DataPoints int
	Confidence float64
	Details    map[string]any
}

// ImpactAssessment describes the scope of proposed changes.
type ImpactAssessment struct {
	NodesAffected     int
	EdgesAffected     int
	ReindexRequired   bool
	EstimatedDuration time.Duration
	Reversible        bool
	RiskLevel         string // "low", "medium", "high"
}

// SchemaChange describes a single atomic change to apply.
type SchemaChange struct {
	Operation  string         // "add_subtype", "modify_type", "add_index", etc.
	Target     string         // Type or table name
	Parameters map[string]any // Operation-specific parameters
}

// ProposalAction represents a user decision on a proposal.
type ProposalAction string

const (
	ActionApprove ProposalAction = "approve"
	ActionReject  ProposalAction = "reject"
	ActionDefer   ProposalAction = "defer"
)

// ProposalDecision captures a user's decision about a proposal.
type ProposalDecision struct {
	ProposalID string
	Action     ProposalAction
	Reason     string    // Required for reject, optional otherwise
	DeferUntil time.Time // For defer action
	DecidedBy  string    // User identifier
	DecidedAt  time.Time
}

// ProposalStore abstracts proposal persistence.
type ProposalStore interface {
	// Save stores a proposal.
	Save(ctx context.Context, proposal *Proposal) error

	// Get retrieves a proposal by ID.
	Get(ctx context.Context, id string) (*Proposal, error)

	// List returns proposals matching the filter.
	List(ctx context.Context, filter ProposalFilter) ([]*Proposal, error)

	// Update updates an existing proposal.
	Update(ctx context.Context, proposal *Proposal) error

	// Delete removes a proposal.
	Delete(ctx context.Context, id string) error
}

// ProposalFilter specifies criteria for listing proposals.
type ProposalFilter struct {
	Status    []ProposalStatus
	Type      []ProposalType
	Since     time.Time
	Until     time.Time
	Limit     int
	SortBy    string // "created_at", "priority", "confidence"
	SortOrder string // "asc", "desc"
}

// ProposalGenerator creates proposals from detected patterns.
type ProposalGenerator struct {
	// Minimum confidence to generate proposal
	minConfidence map[PatternType]float64
}

// NewProposalGenerator creates a new proposal generator.
func NewProposalGenerator() *ProposalGenerator {
	return &ProposalGenerator{
		minConfidence: map[PatternType]float64{
			PatternNodeTypeMismatch:    0.7,
			PatternMissingSubtype:      0.8,
			PatternRetrievalMismatch:   0.6,
			PatternHighDecayOnUseful:   0.75,
			PatternLowRetrievalHitRate: 0.7,
		},
	}
}

// Generate creates a proposal from a detected pattern.
// Returns nil if the pattern doesn't meet confidence threshold.
func (g *ProposalGenerator) Generate(pattern Pattern) *Proposal {
	minConf, ok := g.minConfidence[pattern.Type()]
	if ok && pattern.Confidence() < minConf {
		return nil
	}

	switch p := pattern.(type) {
	case *NodeTypeMismatchPattern:
		return g.generateTypeMismatchProposal(p)
	case *MissingSubtypePattern:
		return g.generateNewSubtypeProposal(p)
	case *RetrievalMismatchPattern:
		return g.generateRetrievalProposal(p)
	case *HighDecayOnUsefulPattern:
		return g.generateDecayAdjustProposal(p)
	case *LowRetrievalHitRatePattern:
		return g.generateHitRateProposal(p)
	default:
		return nil
	}
}

func (g *ProposalGenerator) generateTypeMismatchProposal(p *NodeTypeMismatchPattern) *Proposal {
	return &Proposal{
		ID:   generateProposalID(),
		Type: ProposalNewSubtype,
		Title: fmt.Sprintf("Add subtype '%s' for '%s' nodes",
			p.SuggestedType, p.CurrentType),
		Description: fmt.Sprintf(
			"Nodes of type '%s' are frequently retrieved but marked irrelevant "+
				"for '%s' queries. Creating a dedicated subtype '%s' would improve "+
				"retrieval precision by allowing more targeted filtering.",
			p.CurrentType, p.QueryType, p.SuggestedType,
		),
		Rationale: fmt.Sprintf(
			"Observed %d retrievals with average relevance of %.1f%% (below 40%% threshold). "+
				"This indicates a systematic mismatch between node type and query expectations.",
			p.Occurrences, p.AvgRelevance*100,
		),
		Evidence: []Evidence{{
			Type:       "retrieval_stats",
			Summary:    fmt.Sprintf("%d retrievals with %.1f%% avg relevance", p.Occurrences, p.AvgRelevance*100),
			DataPoints: p.Occurrences,
			Confidence: p.confidence,
			Details: map[string]any{
				"current_type":  p.CurrentType,
				"query_type":    p.QueryType,
				"avg_relevance": p.AvgRelevance,
				"sample_nodes":  p.Examples,
			},
		}},
		Impact: ImpactAssessment{
			NodesAffected:     len(p.Examples) * 10, // Estimate
			EdgesAffected:     len(p.Examples) * 2,
			ReindexRequired:   false,
			EstimatedDuration: time.Second * 5,
			Reversible:        true,
			RiskLevel:         "low",
		},
		Changes: []SchemaChange{{
			Operation: "add_subtype",
			Target:    p.CurrentType,
			Parameters: map[string]any{
				"name":        p.SuggestedType,
				"sample_ids":  p.Examples,
				"auto_assign": true,
			},
		}},
		Confidence:    p.confidence,
		Priority:      3,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		Status:        ProposalStatusPending,
		SourcePattern: PatternNodeTypeMismatch,
	}
}

func (g *ProposalGenerator) generateNewSubtypeProposal(p *MissingSubtypePattern) *Proposal {
	return &Proposal{
		ID:   generateProposalID(),
		Type: ProposalNewSubtype,
		Title: fmt.Sprintf("Add subtype '%s' under '%s'",
			p.ProposedName, p.ParentType),
		Description: fmt.Sprintf(
			"Analysis detected %d nodes that cluster together within type '%s'. "+
				"These nodes share common characteristics (%s) that distinguish them "+
				"from other nodes of the same type. Creating a dedicated subtype would "+
				"improve retrieval precision.",
			p.ClusterSize, p.ParentType, formatTerms(p.CommonTerms),
		),
		Rationale: fmt.Sprintf(
			"Cluster cohesion: %.1f%%, separation: %.1f%%. "+
				"High cohesion indicates strong internal similarity; "+
				"high separation indicates distinctness from other nodes.",
			p.Cohesion*100, p.Separation*100,
		),
		Evidence: []Evidence{{
			Type:       "cluster_analysis",
			Summary:    fmt.Sprintf("%d nodes form distinct semantic cluster", p.ClusterSize),
			DataPoints: p.ClusterSize,
			Confidence: (p.Cohesion + p.Separation) / 2,
			Details: map[string]any{
				"cohesion":     p.Cohesion,
				"separation":   p.Separation,
				"common_terms": p.CommonTerms,
				"sample_ids":   p.SampleNodeIDs,
			},
		}},
		Impact: ImpactAssessment{
			NodesAffected:     p.ClusterSize,
			EdgesAffected:     p.ClusterSize * 2, // Estimate
			ReindexRequired:   false,
			EstimatedDuration: time.Second * time.Duration(p.ClusterSize/100+1),
			Reversible:        true,
			RiskLevel:         "low",
		},
		Changes: []SchemaChange{{
			Operation: "add_subtype",
			Target:    p.ParentType,
			Parameters: map[string]any{
				"name":       p.ProposedName,
				"node_ids":   p.SampleNodeIDs,
				"auto_label": true,
			},
		}},
		Confidence:    (p.Cohesion + p.Separation) / 2,
		Priority:      2,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		Status:        ProposalStatusPending,
		SourcePattern: PatternMissingSubtype,
	}
}

func (g *ProposalGenerator) generateRetrievalProposal(p *RetrievalMismatchPattern) *Proposal {
	return &Proposal{
		ID:    generateProposalID(),
		Type:  ProposalRetrievalConfig,
		Title: fmt.Sprintf("Adjust retrieval strategy for '%s' queries", p.QueryType),
		Description: fmt.Sprintf(
			"The current '%s' retrieval strategy is underperforming for '%s' queries. "+
				"Hit rate is %.1f%% with %.1f%% false positives. "+
				"Switching to '%s' strategy may improve results.",
			p.CurrentStrategy, p.QueryType,
			p.Metrics.HitRate*100, p.Metrics.FalsePositives*100,
			p.SuggestedChange,
		),
		Rationale: fmt.Sprintf(
			"Based on %d queries, the current strategy produces too many irrelevant results. "+
				"The suggested '%s' strategy is better suited for %s-type queries.",
			p.Metrics.SampleSize, p.SuggestedChange, p.QueryType,
		),
		Evidence: []Evidence{{
			Type:       "retrieval_stats",
			Summary:    fmt.Sprintf("%.1f%% hit rate, %.1f%% false positives", p.Metrics.HitRate*100, p.Metrics.FalsePositives*100),
			DataPoints: p.Metrics.SampleSize,
			Confidence: 1.0 - p.Metrics.HitRate,
			Details: map[string]any{
				"hit_rate":        p.Metrics.HitRate,
				"false_positives": p.Metrics.FalsePositives,
				"avg_latency":     p.Metrics.AvgLatency.String(),
				"sample_size":     p.Metrics.SampleSize,
			},
		}},
		Impact: ImpactAssessment{
			NodesAffected:     0, // Config change, not node change
			EdgesAffected:     0,
			ReindexRequired:   p.SuggestedChange == "keyword", // Keyword may need different index
			EstimatedDuration: time.Second * 1,
			Reversible:        true,
			RiskLevel:         "low",
		},
		Changes: []SchemaChange{{
			Operation: "update_config",
			Target:    "retrieval",
			Parameters: map[string]any{
				"query_type": p.QueryType,
				"strategy":   p.SuggestedChange,
			},
		}},
		Confidence:    p.Confidence(),
		Priority:      3,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		Status:        ProposalStatusPending,
		SourcePattern: PatternRetrievalMismatch,
	}
}

func (g *ProposalGenerator) generateDecayAdjustProposal(p *HighDecayOnUsefulPattern) *Proposal {
	return &Proposal{
		ID:    generateProposalID(),
		Type:  ProposalDecayAdjust,
		Title: fmt.Sprintf("Reduce decay rate for frequently accessed '%s' nodes", p.NodeType),
		Description: fmt.Sprintf(
			"Nodes of type '%s' are being accessed frequently (avg %.1f accesses) "+
				"but still experiencing significant decay (avg %.1f%% rate). "+
				"Reducing decay for these nodes would preserve valuable knowledge.",
			p.NodeType, p.AvgAccessCount, p.AvgDecayRate*100,
		),
		Rationale: fmt.Sprintf(
			"Observed %d nodes with high access counts experiencing decay. "+
				"Access patterns indicate these nodes contain valuable, frequently-used knowledge.",
			p.NodesAffected,
		),
		Evidence: []Evidence{{
			Type:       "access_analysis",
			Summary:    fmt.Sprintf("%d nodes with avg %.1f accesses still decaying", p.NodesAffected, p.AvgAccessCount),
			DataPoints: p.NodesAffected,
			Confidence: p.Confidence(),
			Details: map[string]any{
				"avg_access_count": p.AvgAccessCount,
				"avg_decay_rate":   p.AvgDecayRate,
				"sample_ids":       p.SampleNodeIDs,
			},
		}},
		Impact: ImpactAssessment{
			NodesAffected:     p.NodesAffected,
			EdgesAffected:     0,
			ReindexRequired:   false,
			EstimatedDuration: time.Second * 1,
			Reversible:        true,
			RiskLevel:         "low",
		},
		Changes: []SchemaChange{{
			Operation: "adjust_decay",
			Target:    p.NodeType,
			Parameters: map[string]any{
				"access_threshold":      p.AvgAccessCount / 2,
				"decay_reduction_factor": 0.5,
			},
		}},
		Confidence:    p.Confidence(),
		Priority:      2,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		Status:        ProposalStatusPending,
		SourcePattern: PatternHighDecayOnUseful,
	}
}

func (g *ProposalGenerator) generateHitRateProposal(p *LowRetrievalHitRatePattern) *Proposal {
	return &Proposal{
		ID:    generateProposalID(),
		Type:  ProposalRetrievalConfig,
		Title: "Improve overall retrieval hit rate",
		Description: fmt.Sprintf(
			"Overall retrieval hit rate (%.1f%%) is below target (%.1f%%). "+
				"Most affected query types: %s. "+
				"Consider adjusting retrieval parameters or indexing strategy.",
			p.OverallHitRate*100, p.TargetHitRate*100,
			formatTerms(p.QueryTypes),
		),
		Rationale: fmt.Sprintf(
			"Low hit rate indicates systematic retrieval issues over the past %s. "+
				"This affects user experience and may indicate stale or poorly-organized knowledge.",
			p.TimePeriod.String(),
		),
		Evidence: []Evidence{{
			Type:       "hit_rate_analysis",
			Summary:    fmt.Sprintf("%.1f%% hit rate vs %.1f%% target", p.OverallHitRate*100, p.TargetHitRate*100),
			DataPoints: 1,
			Confidence: p.Confidence(),
			Details: map[string]any{
				"overall_hit_rate": p.OverallHitRate,
				"target_hit_rate":  p.TargetHitRate,
				"affected_types":   p.QueryTypes,
				"time_period":      p.TimePeriod.String(),
			},
		}},
		Impact: ImpactAssessment{
			NodesAffected:     0,
			EdgesAffected:     0,
			ReindexRequired:   true, // May need reindexing
			EstimatedDuration: time.Minute * 5,
			Reversible:        true,
			RiskLevel:         "medium",
		},
		Changes: []SchemaChange{{
			Operation: "tune_retrieval",
			Target:    "global",
			Parameters: map[string]any{
				"affected_query_types": p.QueryTypes,
				"target_hit_rate":      p.TargetHitRate,
			},
		}},
		Confidence:    p.Confidence(),
		Priority:      4,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		Status:        ProposalStatusPending,
		SourcePattern: PatternLowRetrievalHitRate,
	}
}

// Helper functions

func generateProposalID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "prop-" + hex.EncodeToString(b)
}

func formatTerms(terms []string) string {
	if len(terms) == 0 {
		return "none"
	}
	if len(terms) == 1 {
		return terms[0]
	}
	if len(terms) == 2 {
		return terms[0] + " and " + terms[1]
	}
	return terms[0] + ", " + terms[1] + ", and " + fmt.Sprintf("%d others", len(terms)-2)
}

// ProposalStats contains aggregate proposal statistics.
type ProposalStats struct {
	Total     int
	Pending   int
	Approved  int
	Rejected  int
	Applied   int
	Deferred  int
	ByType    map[ProposalType]int
	ByPattern map[PatternType]int
}

// CalculateStats calculates statistics from a list of proposals.
func CalculateStats(proposals []*Proposal) *ProposalStats {
	stats := &ProposalStats{
		Total:     len(proposals),
		ByType:    make(map[ProposalType]int),
		ByPattern: make(map[PatternType]int),
	}

	for _, p := range proposals {
		switch p.Status {
		case ProposalStatusPending:
			stats.Pending++
		case ProposalStatusApproved, ProposalStatusApplied:
			stats.Approved++
			if p.Status == ProposalStatusApplied {
				stats.Applied++
			}
		case ProposalStatusRejected:
			stats.Rejected++
		case ProposalStatusDeferred:
			stats.Deferred++
		}
		stats.ByType[p.Type]++
		stats.ByPattern[p.SourcePattern]++
	}

	return stats
}
