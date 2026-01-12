# Meta-Evolution of Memory Architecture

> Design document for `recurse-b0y.4`: Surface meta-evolution proposals to user
> Based on research from [MemEvolve (arxiv 2512.18746)](https://arxiv.org/abs/2512.18746)

## Overview

Meta-evolution enables the memory system to adapt its own architecture based on observed usage patterns. Rather than relying on fixed memory designs, the system can propose structural changes when it detects consistent patterns of suboptimal behavior.

**Key insight from MemEvolve**: While memory facilitates agent-level evolution, the underlying memory architecture itself should be meta-adaptable to diverse task contexts.

## Research Foundation

### MemEvolve Framework

MemEvolve introduces a bi-level optimization approach:

1. **Inner Loop**: Execute tasks using candidate memory architectures, collect performance metrics
2. **Outer Loop**: Generate improved architectures based on feedback patterns

The framework treats memory as a "genotype" with four modular components:

| Component | Purpose | Recurse Equivalent |
|-----------|---------|-------------------|
| **Encode** | Structure raw experience | Node type selection, content extraction |
| **Store** | Integrate into memory | Tier placement, hyperedge creation |
| **Retrieve** | Recall information | Query strategy, similarity search |
| **Manage** | Offline maintenance | Consolidation, decay, promotion |

### Diagnose-and-Design Process

MemEvolve's key contribution is the "diagnose-and-design" approach:

1. **Diagnose**: Analyze failures and memory access patterns to create a "defect profile"
2. **Design**: Generate targeted modifications based on identified bottlenecks
3. **Evaluate**: Test candidates using Pareto-ranking across multiple objectives
4. **Select**: Winner becomes the base for the next evolution round

## Pattern Detection Algorithm

### Data Sources

Detect patterns from the `evolution_log` table and runtime metrics:

```sql
-- Memory evolution audit log (existing)
CREATE TABLE evolution_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    operation TEXT NOT NULL,
    node_ids TEXT,
    from_tier TEXT,
    to_tier TEXT,
    reasoning TEXT,
    metadata TEXT
);

-- NEW: Retrieval outcome tracking
CREATE TABLE retrieval_outcomes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    query_hash TEXT NOT NULL,
    query_type TEXT,           -- computational, retrieval, analytical, transformational
    node_type TEXT,            -- fact, code_snippet, decision, etc.
    node_subtype TEXT,         -- user-defined or proposed
    relevance_score FLOAT,     -- 0.0-1.0, from user feedback or implicit signals
    was_used BOOLEAN,          -- did the retrieved content get used?
    context_tokens INT,
    latency_ms INT
);
```

### Pattern Types to Detect

#### 1. Node Type Mismatch Pattern

Detected when nodes are consistently retrieved but marked irrelevant:

```go
type NodeTypeMismatch struct {
    CurrentType    string    // e.g., "fact"
    SuggestedType  string    // e.g., "code_pattern"
    Confidence     float64   // Based on frequency and consistency
    Examples       []string  // Sample node IDs
    DetectedAt     time.Time
}

// Detection criteria:
// - Same node_type retrieved >= N times (default: 10)
// - Average relevance_score < threshold (default: 0.4)
// - Consistent query_type pattern (>70% same type)
```

#### 2. Missing Subtype Pattern

Detected when nodes cluster semantically but lack distinguishing subtypes:

```go
type MissingSubtype struct {
    ParentType     string
    ProposedName   string    // Generated from content analysis
    ClusterSize    int
    Cohesion       float64   // Intra-cluster similarity
    Separation     float64   // Inter-cluster distance
    SampleNodeIDs  []string
}

// Detection criteria:
// - Semantic clustering reveals distinct groups within a type
// - Groups have high cohesion (>0.7) and separation (>0.5)
// - Group size >= minimum (default: 5 nodes)
```

#### 3. Retrieval Strategy Mismatch

Detected when certain query types consistently underperform:

```go
type RetrievalMismatch struct {
    QueryType       string
    CurrentStrategy string    // "semantic", "keyword", "hybrid"
    Metrics         RetrievalMetrics
    SuggestedChange string
}

type RetrievalMetrics struct {
    AvgRelevance    float64
    AvgLatency      time.Duration
    HitRate         float64   // % queries with useful results
    FalsePositives  float64   // % retrieved but unused
}
```

### Detection Algorithm

```go
// PatternDetector analyzes evolution_log and retrieval_outcomes for patterns.
type PatternDetector struct {
    store           *hypergraph.Store
    minSampleSize   int           // Minimum observations before detection
    analysisWindow  time.Duration // How far back to analyze

    // Thresholds
    mismatchThreshold    float64 // Relevance below this triggers mismatch
    clusterCohesion      float64 // Minimum cohesion for subtype proposal
    significanceLevel    float64 // Statistical significance required
}

func (d *PatternDetector) DetectPatterns(ctx context.Context) ([]Pattern, error) {
    var patterns []Pattern

    // 1. Query retrieval outcomes within analysis window
    outcomes, err := d.queryRecentOutcomes(ctx)
    if err != nil {
        return nil, err
    }

    // 2. Group by node_type and analyze
    byType := groupByNodeType(outcomes)
    for nodeType, typeOutcomes := range byType {
        // Check for type mismatch
        if mismatch := d.detectTypeMismatch(nodeType, typeOutcomes); mismatch != nil {
            patterns = append(patterns, mismatch)
        }

        // Check for missing subtypes via clustering
        if subtypes := d.detectMissingSubtypes(ctx, nodeType, typeOutcomes); len(subtypes) > 0 {
            patterns = append(patterns, subtypes...)
        }
    }

    // 3. Analyze by query_type for retrieval strategy issues
    byQueryType := groupByQueryType(outcomes)
    for queryType, queryOutcomes := range byQueryType {
        if mismatch := d.detectRetrievalMismatch(queryType, queryOutcomes); mismatch != nil {
            patterns = append(patterns, mismatch)
        }
    }

    return patterns, nil
}
```

## Proposal Generation

### Proposal Types

```go
type ProposalType string

const (
    ProposalNewSubtype      ProposalType = "new_subtype"
    ProposalRenameType      ProposalType = "rename_type"
    ProposalMergeTypes      ProposalType = "merge_types"
    ProposalSplitType       ProposalType = "split_type"
    ProposalRetrievalConfig ProposalType = "retrieval_config"
    ProposalDecayAdjust     ProposalType = "decay_adjust"
)

type Proposal struct {
    ID          string
    Type        ProposalType
    Title       string
    Description string
    Rationale   string       // Why this change is suggested
    Evidence    []Evidence   // Supporting data
    Impact      ImpactAssessment

    // The actual change
    Changes     []SchemaChange

    // Metadata
    Confidence  float64
    CreatedAt   time.Time
    Status      ProposalStatus // pending, approved, rejected, applied
}

type Evidence struct {
    Type        string // "retrieval_stats", "cluster_analysis", "user_feedback"
    Summary     string
    DataPoints  int
    Confidence  float64
}

type ImpactAssessment struct {
    NodesAffected    int
    EdgesAffected    int
    ReindexRequired  bool
    EstimatedDuration time.Duration
    Reversible       bool
}

type SchemaChange struct {
    Operation   string // "add_subtype", "modify_type", "add_index", etc.
    Target      string // Type or table name
    Parameters  map[string]any
}
```

### Proposal Generation Criteria

| Pattern Type | Proposal Generated | Minimum Confidence |
|--------------|-------------------|-------------------|
| Node type mismatch | `new_subtype` or `rename_type` | 0.7 |
| Semantic cluster | `new_subtype` | 0.8 |
| Retrieval underperformance | `retrieval_config` | 0.6 |
| High decay rate on useful nodes | `decay_adjust` | 0.75 |

### Generation Algorithm

```go
func (g *ProposalGenerator) GenerateProposal(pattern Pattern) (*Proposal, error) {
    switch p := pattern.(type) {
    case *NodeTypeMismatch:
        return g.generateSubtypeProposal(p)
    case *MissingSubtype:
        return g.generateNewSubtypeProposal(p)
    case *RetrievalMismatch:
        return g.generateRetrievalProposal(p)
    default:
        return nil, fmt.Errorf("unknown pattern type: %T", pattern)
    }
}

func (g *ProposalGenerator) generateNewSubtypeProposal(p *MissingSubtype) (*Proposal, error) {
    // Analyze cluster content to suggest name
    suggestedName := g.suggestSubtypeName(p.SampleNodeIDs)

    // Assess impact
    impact := ImpactAssessment{
        NodesAffected:    p.ClusterSize,
        EdgesAffected:    g.countAffectedEdges(p.SampleNodeIDs),
        ReindexRequired:  false, // Subtype addition doesn't require reindex
        EstimatedDuration: time.Second * time.Duration(p.ClusterSize/100),
        Reversible:       true,
    }

    return &Proposal{
        ID:          generateID(),
        Type:        ProposalNewSubtype,
        Title:       fmt.Sprintf("Add subtype '%s' under '%s'", suggestedName, p.ParentType),
        Description: fmt.Sprintf(
            "Analysis detected %d nodes that cluster together within type '%s'. "+
            "Creating a dedicated subtype would improve retrieval precision.",
            p.ClusterSize, p.ParentType,
        ),
        Rationale:   fmt.Sprintf(
            "Cluster cohesion: %.2f, separation: %.2f. "+
            "Current retrieval relevance for these nodes: below threshold.",
            p.Cohesion, p.Separation,
        ),
        Evidence: []Evidence{{
            Type:       "cluster_analysis",
            Summary:    fmt.Sprintf("%d nodes form distinct semantic cluster", p.ClusterSize),
            DataPoints: p.ClusterSize,
            Confidence: p.Cohesion,
        }},
        Impact:     impact,
        Changes: []SchemaChange{{
            Operation:  "add_subtype",
            Target:     p.ParentType,
            Parameters: map[string]any{
                "name":     suggestedName,
                "node_ids": p.SampleNodeIDs,
            },
        }},
        Confidence: (p.Cohesion + p.Separation) / 2,
        CreatedAt:  time.Now(),
        Status:     ProposalStatusPending,
    }, nil
}
```

## User Approval Workflow

### TUI Integration

Proposals surface via the Memory Inspector panel with a dedicated "Proposals" tab:

```
┌─ Memory Inspector ────────────────────────────────────────────┐
│ [Nodes] [Edges] [Tiers] [Evolution] [Proposals (2)]          │
├───────────────────────────────────────────────────────────────┤
│ Pending Proposals                                             │
│                                                               │
│ ▶ Add subtype 'api_pattern' under 'code_snippet'             │
│   Confidence: 85% | Impact: 47 nodes | Reversible: Yes       │
│   [View Details] [Approve] [Reject] [Defer]                  │
│                                                               │
│ ▶ Adjust decay rate for 'decision' nodes                     │
│   Confidence: 72% | Impact: 156 nodes | Reversible: Yes      │
│   [View Details] [Approve] [Reject] [Defer]                  │
│                                                               │
├───────────────────────────────────────────────────────────────┤
│ History: 3 approved, 1 rejected, 0 deferred                  │
└───────────────────────────────────────────────────────────────┘
```

### Detail View

```
┌─ Proposal Detail ─────────────────────────────────────────────┐
│ Add subtype 'api_pattern' under 'code_snippet'               │
│                                                               │
│ RATIONALE                                                     │
│ Analysis detected 47 code snippets that cluster together     │
│ with high semantic similarity (cohesion: 0.87). These nodes  │
│ are frequently retrieved together for API-related queries    │
│ but mixed results with other code snippets reduce precision. │
│                                                               │
│ EVIDENCE                                                      │
│ • Cluster analysis: 47 nodes, cohesion 0.87, separation 0.62 │
│ • Retrieval stats: 23% false positive rate for this cluster  │
│ • Common patterns: REST endpoints, request/response handling │
│                                                               │
│ SAMPLE NODES                                                  │
│ • node_abc123: "func handleGetUser(w http.ResponseWriter..."  │
│ • node_def456: "async function fetchOrders(): Promise<..."   │
│ • node_ghi789: "class APIClient: def __init__(self, base..."  │
│                                                               │
│ IMPACT                                                        │
│ • 47 nodes will be re-labeled                                │
│ • ~12 edges will be updated                                  │
│ • No reindexing required                                     │
│ • Estimated time: <1 second                                  │
│ • This change is reversible                                  │
│                                                               │
│ [Approve] [Reject with Reason] [Defer] [Cancel]              │
└───────────────────────────────────────────────────────────────┘
```

### Approval Actions

```go
type ProposalAction string

const (
    ActionApprove ProposalAction = "approve"
    ActionReject  ProposalAction = "reject"
    ActionDefer   ProposalAction = "defer"
)

type ProposalDecision struct {
    ProposalID string
    Action     ProposalAction
    Reason     string    // Required for reject
    DeferUntil time.Time // For defer action
    DecidedBy  string    // User identifier
    DecidedAt  time.Time
}

func (m *MetaEvolutionManager) HandleDecision(ctx context.Context, decision ProposalDecision) error {
    proposal, err := m.getProposal(decision.ProposalID)
    if err != nil {
        return err
    }

    switch decision.Action {
    case ActionApprove:
        // Apply the schema changes
        if err := m.applyChanges(ctx, proposal.Changes); err != nil {
            return fmt.Errorf("apply changes: %w", err)
        }
        proposal.Status = ProposalStatusApplied

        // Log for learning
        m.audit.LogProposalApproved(proposal, decision)

    case ActionReject:
        proposal.Status = ProposalStatusRejected
        // Store rejection reason for future pattern detection tuning
        m.audit.LogProposalRejected(proposal, decision)

    case ActionDefer:
        proposal.Status = ProposalStatusDeferred
        m.scheduleReview(proposal.ID, decision.DeferUntil)
    }

    return m.saveProposal(proposal)
}
```

## Success Metrics

Based on MemEvolve's evaluation framework:

### Primary Metrics

| Metric | Description | Target |
|--------|-------------|--------|
| **Retrieval Precision** | % of retrieved nodes actually used | >80% |
| **Retrieval Recall** | % of relevant nodes successfully found | >90% |
| **Query Latency** | Time to execute memory queries | <100ms p95 |
| **User Approval Rate** | % of proposals approved | >60% |

### Secondary Metrics

| Metric | Description | Target |
|--------|-------------|--------|
| **False Positive Rate** | % proposals rejected as unhelpful | <30% |
| **Evolution Frequency** | Schema changes per week | 1-3 |
| **Rollback Rate** | % of approved changes reverted | <10% |

### Tracking

```go
type MetaEvolutionMetrics struct {
    // Proposal metrics
    ProposalsGenerated   int
    ProposalsApproved    int
    ProposalsRejected    int
    ProposalsDeferred    int

    // Impact metrics
    RetrievalPrecisionBefore float64
    RetrievalPrecisionAfter  float64
    AvgQueryLatencyBefore    time.Duration
    AvgQueryLatencyAfter     time.Duration

    // Learning metrics
    PatternDetectionAccuracy float64 // Based on approval rate
    ProposalQualityTrend     []float64 // Over time
}
```

## Implementation Plan

### Phase 1: Instrumentation (recurse-b0y.4a)
- Add `retrieval_outcomes` table
- Instrument memory queries to log outcomes
- Add implicit feedback signals (was node content used in response?)

### Phase 2: Pattern Detection (recurse-b0y.4b)
- Implement `PatternDetector` with basic patterns
- Add clustering analysis for subtype detection
- Wire into `LifecycleManager` for periodic analysis

### Phase 3: Proposal System (recurse-b0y.4c)
- Implement `ProposalGenerator`
- Add proposal storage and lifecycle management
- Create audit logging for proposals

### Phase 4: TUI Integration (recurse-b0y.4d)
- Add "Proposals" tab to Memory Inspector
- Implement detail view and action handlers
- Add notification for new proposals

### Phase 5: Learning Loop (future)
- Use approval/rejection patterns to tune detection thresholds
- Implement A/B testing for proposal quality
- Add cross-project learning (opt-in)

## File Structure

```
internal/memory/evolution/
├── meta.go              # MetaEvolutionManager (main orchestrator)
├── meta_test.go
├── patterns.go          # Pattern type definitions
├── patterns_test.go
├── detector.go          # PatternDetector implementation
├── detector_test.go
├── proposals.go         # Proposal types and generator
├── proposals_test.go
└── schema.sql           # New tables (retrieval_outcomes, proposals)

internal/tui/components/dialogs/memory/
├── proposals.go         # Proposals tab component
├── proposals_test.go
├── proposal_detail.go   # Detail view
└── proposal_detail_test.go
```

## References

- [MemEvolve Paper (arxiv 2512.18746)](https://arxiv.org/abs/2512.18746)
- [MemEvolve GitHub](https://github.com/bingreeky/MemEvolve)
- [EvolveLab Framework](https://www.alphaxiv.org/resources/2512.18746v1)
- [HuggingFace Paper Page](https://huggingface.co/papers/2512.18746)
- [Existing task-classification.md](./task-classification.md) - Similar design doc format
