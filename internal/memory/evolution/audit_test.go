package evolution

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

func TestDefaultAuditConfig(t *testing.T) {
	cfg := DefaultAuditConfig()

	assert.Empty(t, cfg.Path)
	assert.Equal(t, 1000, cfg.MaxBufferSize)
	assert.True(t, cfg.Enabled)
}

func TestNewAuditLogger(t *testing.T) {
	cfg := DefaultAuditConfig()

	logger, err := NewAuditLogger(cfg)
	require.NoError(t, err)
	require.NotNil(t, logger)

	assert.True(t, logger.enabled)
	assert.Nil(t, logger.file) // No file when path is empty
}

func TestNewAuditLogger_WithFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")

	cfg := AuditConfig{
		Path:          path,
		MaxBufferSize: 100,
		Enabled:       true,
	}

	logger, err := NewAuditLogger(cfg)
	require.NoError(t, err)
	require.NotNil(t, logger)
	defer logger.Close()

	assert.NotNil(t, logger.file)
	assert.FileExists(t, path)
}

func TestLog_Basic(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	entry := AuditEntry{
		EventType: AuditPromote,
		NodeID:    "node-123",
		Result:    &AuditResult{Success: true, NodesAffected: 1},
	}

	err = logger.Log(entry)
	require.NoError(t, err)

	entries := logger.GetRecentEntries(10)
	require.Len(t, entries, 1)
	assert.Equal(t, AuditPromote, entries[0].EventType)
	assert.Equal(t, "node-123", entries[0].NodeID)
	assert.False(t, entries[0].Timestamp.IsZero())
}

func TestLog_Disabled(t *testing.T) {
	cfg := DefaultAuditConfig()
	cfg.Enabled = false

	logger, err := NewAuditLogger(cfg)
	require.NoError(t, err)

	err = logger.Log(AuditEntry{EventType: AuditPromote})
	require.NoError(t, err)

	entries := logger.GetRecentEntries(10)
	assert.Len(t, entries, 0) // Nothing logged when disabled
}

func TestLog_BufferLimit(t *testing.T) {
	cfg := DefaultAuditConfig()
	cfg.MaxBufferSize = 5

	logger, err := NewAuditLogger(cfg)
	require.NoError(t, err)

	// Log more entries than buffer size
	for i := 0; i < 10; i++ {
		err := logger.Log(AuditEntry{
			EventType: AuditAccess,
			NodeID:    "node-" + string(rune('0'+i)),
		})
		require.NoError(t, err)
	}

	entries := logger.GetRecentEntries(100)
	assert.Len(t, entries, 5) // Only most recent 5 kept
}

func TestLogConsolidation(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	result := &ConsolidationResult{
		NodesProcessed:    10,
		NodesMerged:       2,
		SummariesCreated:  1,
		EdgesStrengthened: 3,
		Duration:          time.Millisecond * 100,
	}

	err = logger.LogConsolidation(hypergraph.TierTask, hypergraph.TierSession, result, nil)
	require.NoError(t, err)

	entries := logger.GetRecentEntries(1)
	require.Len(t, entries, 1)
	assert.Equal(t, AuditConsolidate, entries[0].EventType)
	assert.Equal(t, hypergraph.TierTask, entries[0].SourceTier)
	assert.Equal(t, hypergraph.TierSession, entries[0].TargetTier)
	assert.True(t, entries[0].Result.Success)
}

func TestLogMerge(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	err = logger.LogMerge("target-id", "source-id")
	require.NoError(t, err)

	entries := logger.GetRecentEntries(1)
	require.Len(t, entries, 1)
	assert.Equal(t, AuditMerge, entries[0].EventType)
	assert.Equal(t, "target-id", entries[0].NodeID)
	assert.Equal(t, "source-id", entries[0].Details["merged_from"])
}

func TestLogSummarize(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	sourceIDs := []string{"src-1", "src-2", "src-3"}
	err = logger.LogSummarize("summary-id", sourceIDs, hypergraph.TierSession)
	require.NoError(t, err)

	entries := logger.GetRecentEntries(1)
	require.Len(t, entries, 1)
	assert.Equal(t, AuditSummarize, entries[0].EventType)
	assert.Equal(t, "summary-id", entries[0].NodeID)
	assert.Equal(t, sourceIDs, entries[0].NodeIDs)
	assert.Equal(t, 4, entries[0].Result.NodesAffected)
}

func TestLogPromotion(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	result := &PromotionResult{
		TaskToSession:     5,
		SessionToLongterm: 2,
		Skipped:           3,
		Duration:          time.Millisecond * 50,
	}

	err = logger.LogPromotion(result, nil)
	require.NoError(t, err)

	entries := logger.GetRecentEntries(1)
	require.Len(t, entries, 1)
	assert.Equal(t, AuditPromote, entries[0].EventType)
	assert.Equal(t, 7, entries[0].Result.NodesAffected)
}

func TestLogNodePromotion(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	err = logger.LogNodePromotion("node-1", hypergraph.TierTask, hypergraph.TierSession)
	require.NoError(t, err)

	entries := logger.GetRecentEntries(1)
	require.Len(t, entries, 1)
	assert.Equal(t, "node-1", entries[0].NodeID)
	assert.Equal(t, hypergraph.TierTask, entries[0].SourceTier)
	assert.Equal(t, hypergraph.TierSession, entries[0].TargetTier)
}

func TestLogDemotion(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	err = logger.LogDemotion("node-1", hypergraph.TierLongterm, hypergraph.TierSession, nil)
	require.NoError(t, err)

	entries := logger.GetRecentEntries(1)
	require.Len(t, entries, 1)
	assert.Equal(t, AuditDemote, entries[0].EventType)
	assert.True(t, entries[0].Result.Success)
}

func TestLogDecay(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	result := &DecayResult{
		NodesProcessed: 100,
		NodesDecayed:   25,
		Duration:       time.Millisecond * 200,
	}

	err = logger.LogDecay(result, nil)
	require.NoError(t, err)

	entries := logger.GetRecentEntries(1)
	require.Len(t, entries, 1)
	assert.Equal(t, AuditDecay, entries[0].EventType)
	assert.Equal(t, 25, entries[0].Result.NodesAffected)
}

func TestLogArchive(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	result := &DecayResult{
		NodesProcessed: 50,
		NodesArchived:  10,
		Duration:       time.Millisecond * 100,
	}

	err = logger.LogArchive(result, nil)
	require.NoError(t, err)

	entries := logger.GetRecentEntries(1)
	require.Len(t, entries, 1)
	assert.Equal(t, AuditArchive, entries[0].EventType)
	assert.Equal(t, 10, entries[0].Result.NodesAffected)
}

func TestLogRestore(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	err = logger.LogRestore("node-1", nil)
	require.NoError(t, err)

	entries := logger.GetRecentEntries(1)
	require.Len(t, entries, 1)
	assert.Equal(t, AuditRestore, entries[0].EventType)
	assert.Equal(t, hypergraph.TierArchive, entries[0].SourceTier)
	assert.Equal(t, hypergraph.TierLongterm, entries[0].TargetTier)
}

func TestLogPrune(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	result := &DecayResult{
		NodesProcessed: 20,
		NodesPruned:    5,
		Duration:       time.Millisecond * 50,
	}

	err = logger.LogPrune(result, nil)
	require.NoError(t, err)

	entries := logger.GetRecentEntries(1)
	require.Len(t, entries, 1)
	assert.Equal(t, AuditPrune, entries[0].EventType)
	assert.Equal(t, 5, entries[0].Result.NodesAffected)
}

func TestLogAccess(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	err = logger.LogAccess("node-1")
	require.NoError(t, err)

	entries := logger.GetRecentEntries(1)
	require.Len(t, entries, 1)
	assert.Equal(t, AuditAccess, entries[0].EventType)
	assert.Equal(t, "node-1", entries[0].NodeID)
}

func TestGetRecentEntries(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	// Log multiple entries
	for i := 0; i < 10; i++ {
		logger.LogAccess("node-" + string(rune('0'+i)))
	}

	// Get subset
	entries := logger.GetRecentEntries(3)
	assert.Len(t, entries, 3)

	// Get more than available
	entries = logger.GetRecentEntries(100)
	assert.Len(t, entries, 10)

	// Get with zero/negative limit
	entries = logger.GetRecentEntries(0)
	assert.Len(t, entries, 10)
}

func TestGetEntriesByType(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	// Log mixed entries
	logger.LogAccess("node-1")
	logger.LogAccess("node-2")
	logger.LogPromotion(&PromotionResult{}, nil)
	logger.LogAccess("node-3")
	logger.LogDecay(&DecayResult{}, nil)

	// Filter by type
	accessEntries := logger.GetEntriesByType(AuditAccess, 10)
	assert.Len(t, accessEntries, 3)

	promoteEntries := logger.GetEntriesByType(AuditPromote, 10)
	assert.Len(t, promoteEntries, 1)
}

func TestGetEntriesByNode(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	// Log entries for different nodes
	logger.LogAccess("node-1")
	logger.LogAccess("node-2")
	logger.LogNodePromotion("node-1", hypergraph.TierTask, hypergraph.TierSession)
	logger.LogSummarize("summary", []string{"node-1", "node-3"}, hypergraph.TierSession)

	// Get entries for node-1
	entries := logger.GetEntriesByNode("node-1", 10)
	assert.Len(t, entries, 3) // Access, promotion, and summarize (in NodeIDs)

	// Get entries for node-2
	entries = logger.GetEntriesByNode("node-2", 10)
	assert.Len(t, entries, 1)
}

func TestGetStats(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	// Log various entries
	logger.LogAccess("node-1")
	logger.LogAccess("node-2")
	logger.LogPromotion(&PromotionResult{TaskToSession: 5, Duration: time.Millisecond * 100}, nil)
	logger.LogDecay(&DecayResult{NodesDecayed: 10, Duration: time.Millisecond * 200}, nil)

	stats := logger.GetStats()

	assert.Equal(t, 4, stats.TotalEntries)
	assert.Equal(t, 4, stats.SuccessCount)
	assert.Equal(t, 0, stats.ErrorCount)
	assert.Equal(t, 17, stats.TotalNodesAffected) // 1 + 1 + 5 + 10
	assert.Equal(t, 2, stats.ByType[AuditAccess])
	assert.Equal(t, 1, stats.ByType[AuditPromote])
	assert.Equal(t, 1, stats.ByType[AuditDecay])
	assert.Greater(t, stats.AverageDuration, time.Duration(0))
}

func TestClear(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	logger.LogAccess("node-1")
	logger.LogAccess("node-2")

	assert.Len(t, logger.GetRecentEntries(10), 2)

	logger.Clear()

	assert.Len(t, logger.GetRecentEntries(10), 0)
}

func TestSetEnabled(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	assert.True(t, logger.IsEnabled())

	logger.SetEnabled(false)
	assert.False(t, logger.IsEnabled())

	// Logging while disabled
	logger.LogAccess("node-1")
	assert.Len(t, logger.GetRecentEntries(10), 0)

	logger.SetEnabled(true)
	logger.LogAccess("node-2")
	assert.Len(t, logger.GetRecentEntries(10), 1)
}

func TestLogWithError(t *testing.T) {
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)

	testErr := assert.AnError

	logger.LogConsolidation(hypergraph.TierTask, hypergraph.TierSession, &ConsolidationResult{}, testErr)
	logger.LogPromotion(&PromotionResult{}, testErr)
	logger.LogDemotion("node-1", hypergraph.TierLongterm, hypergraph.TierSession, testErr)

	entries := logger.GetRecentEntries(10)
	assert.Len(t, entries, 3)

	for _, entry := range entries {
		assert.False(t, entry.Result.Success)
		assert.NotEmpty(t, entry.Result.Error)
	}

	stats := logger.GetStats()
	assert.Equal(t, 3, stats.ErrorCount)
	assert.Equal(t, 0, stats.SuccessCount)
}

func TestFileLogging(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	cfg := AuditConfig{
		Path:          path,
		MaxBufferSize: 100,
		Enabled:       true,
	}

	logger, err := NewAuditLogger(cfg)
	require.NoError(t, err)

	// Log some entries
	logger.LogAccess("node-1")
	logger.LogPromotion(&PromotionResult{TaskToSession: 3}, nil)

	// Close to flush
	require.NoError(t, logger.Close())

	// Verify file has content
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "access")
	assert.Contains(t, string(data), "promote")
	assert.Contains(t, string(data), "node-1")
}

func TestAuditEventTypes(t *testing.T) {
	types := []AuditEventType{
		AuditConsolidate,
		AuditMerge,
		AuditSummarize,
		AuditPromote,
		AuditDemote,
		AuditDecay,
		AuditArchive,
		AuditRestore,
		AuditPrune,
		AuditAccess,
	}

	for _, typ := range types {
		assert.NotEmpty(t, string(typ))
	}
}

func TestSetStore_PersistsToDatabase(t *testing.T) {
	// Create in-memory store
	store, err := hypergraph.NewStore(hypergraph.Options{})
	require.NoError(t, err)
	defer store.Close()

	// Create audit logger with store
	logger, err := NewAuditLogger(DefaultAuditConfig())
	require.NoError(t, err)
	logger.SetStore(store)

	// Log a promotion event
	result := &PromotionResult{
		TaskToSession:     3,
		SessionToLongterm: 2,
		Duration:          time.Millisecond * 100,
	}
	err = logger.LogPromotion(result, nil)
	require.NoError(t, err)

	// Log a consolidation event
	consResult := &ConsolidationResult{
		NodesProcessed:    10,
		NodesMerged:       2,
		SummariesCreated:  1,
		EdgesStrengthened: 3,
		Duration:          time.Millisecond * 50,
	}
	err = logger.LogConsolidation(hypergraph.TierTask, hypergraph.TierSession, consResult, nil)
	require.NoError(t, err)

	// Verify entries were persisted to evolution_log
	ctx := context.Background()
	entries, err := store.ListEvolutionLog(ctx, hypergraph.EvolutionFilter{Limit: 10})
	require.NoError(t, err)

	// Should have at least 2 entries (promote and consolidate)
	assert.GreaterOrEqual(t, len(entries), 2)

	// Check for promote entry
	var foundPromote, foundConsolidate bool
	for _, entry := range entries {
		if entry.Operation == hypergraph.EvolutionPromote {
			foundPromote = true
		}
		if entry.Operation == hypergraph.EvolutionConsolidate {
			foundConsolidate = true
		}
	}
	assert.True(t, foundPromote, "should have a promote entry")
	assert.True(t, foundConsolidate, "should have a consolidate entry")
}

func TestMapEventToOperation(t *testing.T) {
	tests := []struct {
		eventType AuditEventType
		expected  hypergraph.EvolutionOperation
	}{
		{AuditConsolidate, hypergraph.EvolutionConsolidate},
		{AuditMerge, hypergraph.EvolutionConsolidate},
		{AuditSummarize, hypergraph.EvolutionConsolidate},
		{AuditPromote, hypergraph.EvolutionPromote},
		{AuditDecay, hypergraph.EvolutionDecay},
		{AuditArchive, hypergraph.EvolutionArchive},
		{AuditPrune, hypergraph.EvolutionPrune},
		{AuditAccess, ""}, // Access doesn't map to evolution operation
		{AuditDemote, ""},
		{AuditRestore, ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			got := mapEventToOperation(tt.eventType)
			assert.Equal(t, tt.expected, got)
		})
	}
}
