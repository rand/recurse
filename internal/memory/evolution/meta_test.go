package evolution

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockOutcomeStore implements OutcomeStore for testing.
type mockOutcomeStore struct {
	mu       sync.Mutex
	outcomes []RetrievalOutcome
}

func newMockOutcomeStore() *mockOutcomeStore {
	return &mockOutcomeStore{
		outcomes: make([]RetrievalOutcome, 0),
	}
}

func (m *mockOutcomeStore) RecordOutcome(_ context.Context, outcome hypergraph.RetrievalOutcome) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Convert hypergraph.RetrievalOutcome to evolution.RetrievalOutcome
	m.outcomes = append(m.outcomes, RetrievalOutcome{
		Timestamp:      outcome.Timestamp,
		QueryHash:      outcome.QueryHash,
		QueryType:      outcome.QueryType,
		NodeID:         outcome.NodeID,
		NodeType:       outcome.NodeType,
		NodeSubtype:    outcome.NodeSubtype,
		RelevanceScore: outcome.RelevanceScore,
		WasUsed:        outcome.WasUsed,
		ContextTokens:  outcome.ContextTokens,
		LatencyMs:      outcome.LatencyMs,
	})
	return nil
}

func (m *mockOutcomeStore) QueryOutcomes(_ context.Context, since time.Time) ([]RetrievalOutcome, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var results []RetrievalOutcome
	for _, o := range m.outcomes {
		if o.Timestamp.After(since) || o.Timestamp.Equal(since) {
			results = append(results, o)
		}
	}
	return results, nil
}

func (m *mockOutcomeStore) QueryOutcomesByNodeType(_ context.Context, nodeType string, since time.Time) ([]RetrievalOutcome, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var results []RetrievalOutcome
	for _, o := range m.outcomes {
		if o.NodeType == nodeType && (o.Timestamp.After(since) || o.Timestamp.Equal(since)) {
			results = append(results, o)
		}
	}
	return results, nil
}

func (m *mockOutcomeStore) QueryOutcomesByQueryType(_ context.Context, queryType string, since time.Time) ([]RetrievalOutcome, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var results []RetrievalOutcome
	for _, o := range m.outcomes {
		if o.QueryType == queryType && (o.Timestamp.After(since) || o.Timestamp.Equal(since)) {
			results = append(results, o)
		}
	}
	return results, nil
}

func (m *mockOutcomeStore) GetOutcomeStats(_ context.Context, _ time.Time) (*OutcomeStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stats := &OutcomeStats{
		TotalOutcomes: len(m.outcomes),
		ByNodeType:    make(map[string]TypeStats),
		ByQueryType:   make(map[string]TypeStats),
	}
	return stats, nil
}

// mockProposalStore implements ProposalStore for testing.
type mockProposalStore struct {
	mu        sync.Mutex
	proposals map[string]*Proposal
}

func newMockProposalStore() *mockProposalStore {
	return &mockProposalStore{
		proposals: make(map[string]*Proposal),
	}
}

func (m *mockProposalStore) Save(_ context.Context, proposal *Proposal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.proposals[proposal.ID] = proposal
	return nil
}

func (m *mockProposalStore) Get(_ context.Context, id string) (*Proposal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.proposals[id]; ok {
		return p, nil
	}
	return nil, nil
}

func (m *mockProposalStore) List(_ context.Context, filter ProposalFilter) ([]*Proposal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var results []*Proposal
	for _, p := range m.proposals {
		// Apply status filter if provided
		if len(filter.Status) > 0 {
			matches := false
			for _, s := range filter.Status {
				if p.Status == s {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}
		}
		results = append(results, p)
	}
	return results, nil
}

func (m *mockProposalStore) Update(_ context.Context, proposal *Proposal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.proposals[proposal.ID] = proposal
	return nil
}

func (m *mockProposalStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.proposals, id)
	return nil
}

func TestPatternDetector_DetectTypeMismatches(t *testing.T) {
	outcomeStore := newMockOutcomeStore()
	config := DefaultDetectorConfig()
	config.MinSampleSize = 5
	detector := NewPatternDetector(nil, outcomeStore, config)

	ctx := context.Background()
	now := time.Now()

	// Add outcomes that show a type mismatch pattern
	// "fact" nodes being retrieved but low relevance for "computational" queries
	for i := 0; i < 10; i++ {
		outcomeStore.RecordOutcome(ctx, hypergraph.RetrievalOutcome{
			Timestamp:      now,
			QueryType:      "computational",
			NodeID:         "node-" + string(rune('a'+i)),
			NodeType:       "fact",
			RelevanceScore: 0.2, // Low relevance
			WasUsed:        false,
		})
	}

	patterns, err := detector.DetectPatterns(ctx)
	require.NoError(t, err)

	// Should detect the mismatch pattern
	assert.NotEmpty(t, patterns)

	var foundMismatch bool
	for _, p := range patterns {
		if p.Type() == PatternNodeTypeMismatch {
			foundMismatch = true
			mismatch := p.(*NodeTypeMismatchPattern)
			assert.Equal(t, "fact", mismatch.CurrentType)
			assert.Equal(t, "computational", mismatch.QueryType)
			assert.True(t, mismatch.Confidence() > 0.5)
		}
	}
	assert.True(t, foundMismatch, "should detect node type mismatch pattern")
}

func TestPatternDetector_DetectRetrievalMismatch(t *testing.T) {
	outcomeStore := newMockOutcomeStore()
	config := DefaultDetectorConfig()
	config.MinSampleSize = 5
	config.HitRateThreshold = 0.6
	detector := NewPatternDetector(nil, outcomeStore, config)

	ctx := context.Background()
	now := time.Now()

	// Add outcomes showing poor hit rate for analytical queries
	for i := 0; i < 20; i++ {
		outcomeStore.RecordOutcome(ctx, hypergraph.RetrievalOutcome{
			Timestamp:      now,
			QueryType:      "analytical",
			NodeID:         "node-" + string(rune('a'+i%10)),
			NodeType:       "decision",
			RelevanceScore: 0.3,
			WasUsed:        i < 4, // Only 4/20 = 20% hit rate
		})
	}

	patterns, err := detector.DetectPatterns(ctx)
	require.NoError(t, err)

	var foundRetrieval bool
	for _, p := range patterns {
		if p.Type() == PatternRetrievalMismatch {
			foundRetrieval = true
			mismatch := p.(*RetrievalMismatchPattern)
			assert.Equal(t, "analytical", mismatch.QueryType)
			assert.True(t, mismatch.Metrics.HitRate < 0.6)
		}
	}
	assert.True(t, foundRetrieval, "should detect retrieval mismatch pattern")
}

func TestProposalGenerator_GenerateFromMismatch(t *testing.T) {
	generator := NewProposalGenerator()

	pattern := &NodeTypeMismatchPattern{
		CurrentType:   "code_snippet",
		SuggestedType: "api_pattern",
		confidence:    0.85,
		Examples:      []string{"node-1", "node-2", "node-3"},
		QueryType:     "retrieval",
		Occurrences:   25,
		AvgRelevance:  0.25,
		detectedAt:    time.Now(),
	}

	proposal := generator.Generate(pattern)
	require.NotNil(t, proposal)

	assert.Equal(t, ProposalNewSubtype, proposal.Type)
	assert.Contains(t, proposal.Title, "api_pattern")
	assert.Equal(t, ProposalStatusPending, proposal.Status)
	assert.Equal(t, PatternNodeTypeMismatch, proposal.SourcePattern)
	assert.True(t, proposal.Impact.Reversible)
	assert.Len(t, proposal.Changes, 1)
	assert.Equal(t, "add_subtype", proposal.Changes[0].Operation)
}

func TestProposalGenerator_SkipsLowConfidence(t *testing.T) {
	generator := NewProposalGenerator()

	// Pattern below confidence threshold
	pattern := &NodeTypeMismatchPattern{
		CurrentType:   "fact",
		SuggestedType: "computed_value",
		confidence:    0.5, // Below 0.7 threshold
		Examples:      []string{"node-1"},
		QueryType:     "computational",
		Occurrences:   5,
		AvgRelevance:  0.4,
		detectedAt:    time.Now(),
	}

	proposal := generator.Generate(pattern)
	assert.Nil(t, proposal, "should skip low confidence patterns")
}

func TestMetaEvolutionManager_HandleDecision_Approve(t *testing.T) {
	proposalStore := newMockProposalStore()
	outcomeStore := newMockOutcomeStore()
	config := DefaultMetaEvolutionConfig()

	manager := NewMetaEvolutionManager(nil, proposalStore, outcomeStore, nil, config)

	ctx := context.Background()

	// Create a proposal
	proposal := &Proposal{
		ID:          "test-proposal-1",
		Type:        ProposalRetrievalConfig,
		Title:       "Test proposal",
		Status:      ProposalStatusPending,
		CreatedAt:   time.Now(),
		Impact:      ImpactAssessment{Reversible: true},
		Changes:     []SchemaChange{{Operation: "update_config", Target: "retrieval"}},
		Confidence:  0.8,
	}
	proposalStore.Save(ctx, proposal)

	// Approve the proposal
	decision := ProposalDecision{
		ProposalID: proposal.ID,
		Action:     ActionApprove,
		Reason:     "Looks good",
		DecidedBy:  "test-user",
		DecidedAt:  time.Now(),
	}

	err := manager.HandleDecision(ctx, decision)
	require.NoError(t, err)

	// Verify status updated
	updated, _ := proposalStore.Get(ctx, proposal.ID)
	assert.Equal(t, ProposalStatusApplied, updated.Status)
	assert.False(t, updated.AppliedAt.IsZero())
}

func TestMetaEvolutionManager_HandleDecision_Reject(t *testing.T) {
	proposalStore := newMockProposalStore()
	outcomeStore := newMockOutcomeStore()
	config := DefaultMetaEvolutionConfig()

	manager := NewMetaEvolutionManager(nil, proposalStore, outcomeStore, nil, config)

	ctx := context.Background()

	proposal := &Proposal{
		ID:        "test-proposal-2",
		Type:      ProposalNewSubtype,
		Title:     "Test proposal",
		Status:    ProposalStatusPending,
		CreatedAt: time.Now(),
	}
	proposalStore.Save(ctx, proposal)

	decision := ProposalDecision{
		ProposalID: proposal.ID,
		Action:     ActionReject,
		Reason:     "Not needed right now",
		DecidedBy:  "test-user",
		DecidedAt:  time.Now(),
	}

	err := manager.HandleDecision(ctx, decision)
	require.NoError(t, err)

	updated, _ := proposalStore.Get(ctx, proposal.ID)
	assert.Equal(t, ProposalStatusRejected, updated.Status)
	assert.Equal(t, "Not needed right now", updated.StatusNote)
}

func TestMetaEvolutionManager_HandleDecision_Defer(t *testing.T) {
	proposalStore := newMockProposalStore()
	outcomeStore := newMockOutcomeStore()
	config := DefaultMetaEvolutionConfig()

	manager := NewMetaEvolutionManager(nil, proposalStore, outcomeStore, nil, config)

	ctx := context.Background()

	proposal := &Proposal{
		ID:        "test-proposal-3",
		Type:      ProposalDecayAdjust,
		Title:     "Test proposal",
		Status:    ProposalStatusPending,
		CreatedAt: time.Now(),
	}
	proposalStore.Save(ctx, proposal)

	deferUntil := time.Now().Add(24 * time.Hour)
	decision := ProposalDecision{
		ProposalID: proposal.ID,
		Action:     ActionDefer,
		Reason:     "Need to evaluate more",
		DeferUntil: deferUntil,
		DecidedBy:  "test-user",
		DecidedAt:  time.Now(),
	}

	err := manager.HandleDecision(ctx, decision)
	require.NoError(t, err)

	updated, _ := proposalStore.Get(ctx, proposal.ID)
	assert.Equal(t, ProposalStatusDeferred, updated.Status)
	assert.Equal(t, deferUntil.Unix(), updated.DeferUntil.Unix())
}

func TestMetaEvolutionManager_RunAnalysis_Disabled(t *testing.T) {
	proposalStore := newMockProposalStore()
	outcomeStore := newMockOutcomeStore()
	config := DefaultMetaEvolutionConfig()
	config.Enabled = false

	manager := NewMetaEvolutionManager(nil, proposalStore, outcomeStore, nil, config)

	result, err := manager.RunAnalysis(context.Background())
	require.NoError(t, err)

	assert.True(t, result.Skipped)
	assert.Equal(t, "meta-evolution disabled", result.Reason)
}

func TestMetaEvolutionManager_RunAnalysis_MaxPendingReached(t *testing.T) {
	proposalStore := newMockProposalStore()
	outcomeStore := newMockOutcomeStore()
	config := DefaultMetaEvolutionConfig()
	config.MaxPendingProposals = 2

	manager := NewMetaEvolutionManager(nil, proposalStore, outcomeStore, nil, config)

	ctx := context.Background()

	// Add proposals to reach the limit
	for i := 0; i < 2; i++ {
		proposalStore.Save(ctx, &Proposal{
			ID:        "pending-" + string(rune('a'+i)),
			Status:    ProposalStatusPending,
			CreatedAt: time.Now(),
		})
	}

	result, err := manager.RunAnalysis(ctx)
	require.NoError(t, err)

	assert.True(t, result.Skipped)
	assert.Contains(t, result.Reason, "max pending proposals")
}

func TestProposalStats(t *testing.T) {
	proposals := []*Proposal{
		{ID: "1", Type: ProposalNewSubtype, Status: ProposalStatusPending, SourcePattern: PatternMissingSubtype},
		{ID: "2", Type: ProposalNewSubtype, Status: ProposalStatusApplied, SourcePattern: PatternNodeTypeMismatch},
		{ID: "3", Type: ProposalRetrievalConfig, Status: ProposalStatusRejected, SourcePattern: PatternRetrievalMismatch},
		{ID: "4", Type: ProposalDecayAdjust, Status: ProposalStatusDeferred, SourcePattern: PatternHighDecayOnUseful},
	}

	stats := CalculateStats(proposals)

	assert.Equal(t, 4, stats.Total)
	assert.Equal(t, 1, stats.Pending)
	assert.Equal(t, 1, stats.Approved)
	assert.Equal(t, 1, stats.Applied)
	assert.Equal(t, 1, stats.Rejected)
	assert.Equal(t, 1, stats.Deferred)
	assert.Equal(t, 2, stats.ByType[ProposalNewSubtype])
	assert.Equal(t, 1, stats.ByPattern[PatternMissingSubtype])
}

func TestRetrievalMetrics(t *testing.T) {
	outcomes := []RetrievalOutcome{
		{RelevanceScore: 0.8, WasUsed: true, LatencyMs: 100},
		{RelevanceScore: 0.6, WasUsed: true, LatencyMs: 150},
		{RelevanceScore: 0.3, WasUsed: false, LatencyMs: 200},
		{RelevanceScore: 0.2, WasUsed: false, LatencyMs: 120},
	}

	metrics := calculateMetrics(outcomes)

	assert.InDelta(t, 0.475, metrics.AvgRelevance, 0.01) // (0.8+0.6+0.3+0.2)/4
	assert.InDelta(t, 0.5, metrics.HitRate, 0.01)        // 2/4
	assert.InDelta(t, 0.5, metrics.FalsePositives, 0.01) // 1 - 0.5
	assert.Equal(t, 4, metrics.SampleSize)
}

func TestCosineSimilarity(t *testing.T) {
	// Create two identical embeddings
	// Float32 1.0 in little-endian: 0x00 0x00 0x80 0x3F
	embed1 := []byte{0x00, 0x00, 0x80, 0x3F, 0x00, 0x00, 0x80, 0x3F}
	embed2 := []byte{0x00, 0x00, 0x80, 0x3F, 0x00, 0x00, 0x80, 0x3F}

	similarity := cosineSimilarity(embed1, embed2)
	assert.InDelta(t, 1.0, similarity, 0.01, "identical vectors should have similarity ~1.0")

	// Empty embeddings
	empty := []byte{}
	assert.Equal(t, 0.0, cosineSimilarity(empty, empty))

	// Different length embeddings
	short := []byte{0x00, 0x00, 0x80, 0x3F}
	assert.Equal(t, 0.0, cosineSimilarity(embed1, short))
}

func TestPatternDescriptions(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		pattern  Pattern
		contains string
	}{
		{
			name: "NodeTypeMismatch",
			pattern: &NodeTypeMismatchPattern{
				CurrentType: "fact",
				QueryType:   "computational",
				detectedAt:  now,
			},
			contains: "fact",
		},
		{
			name: "MissingSubtype",
			pattern: &MissingSubtypePattern{
				ParentType:  "code_snippet",
				ClusterSize: 25,
				Cohesion:    0.8,
				Separation:  0.6,
				detectedAt:  now,
			},
			contains: "code_snippet",
		},
		{
			name: "RetrievalMismatch",
			pattern: &RetrievalMismatchPattern{
				QueryType:       "analytical",
				CurrentStrategy: "keyword",
				detectedAt:      now,
			},
			contains: "analytical",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc := tt.pattern.Description()
			assert.Contains(t, desc, tt.contains)
			assert.False(t, tt.pattern.DetectedAt().IsZero())
		})
	}
}
