# SPEC-06: Meta-Evolution

> Enable the memory system to adapt its own architecture based on observed usage patterns.

## Overview

Meta-evolution allows the memory system to propose structural changes when it detects consistent patterns of suboptimal behavior. Based on research from MemEvolve (arxiv 2512.18746), the system treats memory as a "genotype" that can evolve.

## Current State

- Fixed memory architecture
- Manual tuning required
- No adaptation to usage patterns
- Suboptimal retrieval for some content types

## Requirements

### [SPEC-06.01] Pattern Detection

Detect patterns from retrieval outcomes:

```sql
-- Track retrieval outcomes
CREATE TABLE retrieval_outcomes (
    id INTEGER PRIMARY KEY,
    timestamp TIMESTAMP,
    query_hash TEXT,
    query_type TEXT,      -- computational, retrieval, analytical
    node_type TEXT,
    node_subtype TEXT,
    relevance_score FLOAT,
    was_used BOOLEAN,
    context_tokens INT,
    latency_ms INT
);
```

Pattern types to detect:
- Node type mismatch (low relevance for type)
- Missing subtype (semantic clusters within type)
- Retrieval strategy mismatch (underperforming queries)

### [SPEC-06.02] Pattern Types

```go
type NodeTypeMismatch struct {
    CurrentType    string
    SuggestedType  string
    Confidence     float64
    Examples       []string
}

type MissingSubtype struct {
    ParentType     string
    ProposedName   string
    ClusterSize    int
    Cohesion       float64  // Intra-cluster similarity
    Separation     float64  // Inter-cluster distance
}

type RetrievalMismatch struct {
    QueryType       string
    CurrentStrategy string
    Metrics         RetrievalMetrics
    SuggestedChange string
}
```

### [SPEC-06.03] Proposal Types

Generate proposals for schema changes:

| Proposal Type | Trigger | Impact |
|--------------|---------|--------|
| new_subtype | Semantic cluster detected | Re-label nodes |
| rename_type | Consistent mismatch | Update type names |
| merge_types | High overlap | Consolidate types |
| split_type | Low cohesion | Create subtypes |
| retrieval_config | Poor query performance | Update strategy |
| decay_adjust | Useful nodes decaying | Modify decay rate |

### [SPEC-06.04] Proposal Structure

```go
type Proposal struct {
    ID          string
    Type        ProposalType
    Title       string
    Description string
    Rationale   string
    Evidence    []Evidence
    Impact      ImpactAssessment
    Changes     []SchemaChange
    Confidence  float64
    Status      ProposalStatus
}

type ImpactAssessment struct {
    NodesAffected     int
    EdgesAffected     int
    ReindexRequired   bool
    EstimatedDuration time.Duration
    Reversible        bool
}
```

### [SPEC-06.05] User Approval Workflow

Surface proposals via TUI:

```
┌─ Memory Inspector ─────────────────────────────────────┐
│ [Nodes] [Edges] [Tiers] [Evolution] [Proposals (2)]    │
├────────────────────────────────────────────────────────┤
│ Pending Proposals                                       │
│                                                        │
│ ▶ Add subtype 'api_pattern' under 'code_snippet'      │
│   Confidence: 85% | Impact: 47 nodes | Reversible     │
│   [View Details] [Approve] [Reject] [Defer]           │
└────────────────────────────────────────────────────────┘
```

Actions:
- Approve: Apply schema changes
- Reject: Store reason for future tuning
- Defer: Schedule for later review

### [SPEC-06.06] Success Metrics

| Metric | Target |
|--------|--------|
| Retrieval Precision | >80% |
| Retrieval Recall | >90% |
| Query Latency | <100ms p95 |
| User Approval Rate | >60% |
| False Positive Rate | <30% |
| Rollback Rate | <10% |

## Implementation Tasks

- [x] Add retrieval_outcomes table (`internal/memory/hypergraph/schema.sql`)
- [x] Instrument memory queries for outcome logging (`internal/memory/hypergraph/hybrid_search.go`)
- [x] Implement PatternDetector (`internal/memory/evolution/detector.go`)
- [x] Add clustering analysis for subtype detection (`internal/memory/evolution/detector.go`)
- [x] Create ProposalGenerator (`internal/memory/evolution/proposals.go`)
- [x] Build proposal storage and lifecycle (`internal/memory/evolution/proposal_store.go`, `internal/memory/evolution/outcome_store.go`)
- [x] Add TUI Proposals tab (`internal/tui/components/dialogs/memory/memory.go`)
- [x] Implement approval workflow (`internal/memory/evolution/meta.go`)
- [x] Add audit logging (`internal/memory/evolution/audit.go`)
- [x] Wire into LifecycleManager (`internal/rlm/service.go`)
- [x] Write tests (`internal/memory/evolution/*_test.go`)

## Dependencies

- `internal/memory/hypergraph/` - Store integration
- `internal/memory/embeddings/` - Clustering analysis
- `internal/tui/` - Proposals UI

## Acceptance Criteria

1. Detect meaningful patterns from 100+ queries
2. Generate actionable proposals with clear rationale
3. User can approve/reject/defer from TUI
4. Approved changes improve retrieval metrics
5. System learns from rejection patterns
