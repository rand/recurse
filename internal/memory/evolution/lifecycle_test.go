package evolution

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

func TestDefaultLifecycleConfig(t *testing.T) {
	cfg := DefaultLifecycleConfig()

	assert.Equal(t, time.Minute*30, cfg.IdleInterval)
	assert.True(t, cfg.RunDecayOnSessionEnd)
	assert.True(t, cfg.RunArchiveOnIdle)
	assert.True(t, cfg.RunPruneOnIdle)
}

func TestNewLifecycleManager(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	require.NotNil(t, mgr)
	defer mgr.Close()

	assert.NotNil(t, mgr.consolidator)
	assert.NotNil(t, mgr.promoter)
	assert.NotNil(t, mgr.decayer)
	assert.NotNil(t, mgr.audit)
}

func TestTaskComplete_Basic(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	defer mgr.Close()

	ctx := context.Background()

	// Add some task tier nodes
	for i := 0; i < 5; i++ {
		node := hypergraph.NewNode(hypergraph.NodeTypeFact, "task fact "+string(rune('A'+i)))
		node.Tier = hypergraph.TierTask
		node.AccessCount = 3 // Above promotion threshold
		require.NoError(t, store.CreateNode(ctx, node))
	}

	result, err := mgr.TaskComplete(ctx)
	require.NoError(t, err)

	assert.Equal(t, "task_complete", result.Operation)
	assert.NotNil(t, result.Consolidation)
	assert.NotNil(t, result.Promotion)
	assert.Greater(t, result.Duration, time.Duration(0))
}

func TestTaskComplete_Callback(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	defer mgr.Close()

	var callbackInvoked bool
	var receivedResult *LifecycleResult

	mgr.OnTaskComplete(func(result *LifecycleResult) {
		callbackInvoked = true
		receivedResult = result
	})

	ctx := context.Background()
	_, err = mgr.TaskComplete(ctx)
	require.NoError(t, err)

	assert.True(t, callbackInvoked)
	assert.NotNil(t, receivedResult)
	assert.Equal(t, "task_complete", receivedResult.Operation)
}

func TestSessionEnd_Basic(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	defer mgr.Close()

	ctx := context.Background()

	// Add some session tier nodes
	for i := 0; i < 5; i++ {
		node := hypergraph.NewNode(hypergraph.NodeTypeFact, "session fact "+string(rune('A'+i)))
		node.Tier = hypergraph.TierSession
		node.AccessCount = 10 // Above longterm threshold
		require.NoError(t, store.CreateNode(ctx, node))
	}

	result, err := mgr.SessionEnd(ctx)
	require.NoError(t, err)

	assert.Equal(t, "session_end", result.Operation)
	assert.NotNil(t, result.Consolidation)
	assert.NotNil(t, result.Promotion)
	assert.NotNil(t, result.Decay) // Decay runs by default
	assert.Greater(t, result.Duration, time.Duration(0))
}

func TestSessionEnd_Callback(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	defer mgr.Close()

	var callbackInvoked bool

	mgr.OnSessionEnd(func(result *LifecycleResult) {
		callbackInvoked = true
	})

	ctx := context.Background()
	_, err = mgr.SessionEnd(ctx)
	require.NoError(t, err)

	assert.True(t, callbackInvoked)
}

func TestSessionEnd_NoDecay(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()
	cfg.RunDecayOnSessionEnd = false

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	defer mgr.Close()

	ctx := context.Background()
	result, err := mgr.SessionEnd(ctx)
	require.NoError(t, err)

	assert.Nil(t, result.Decay)
}

func TestIdleMaintenance_Basic(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	defer mgr.Close()

	ctx := context.Background()

	// Add some long-term nodes for decay
	for i := 0; i < 3; i++ {
		node := hypergraph.NewNode(hypergraph.NodeTypeFact, "longterm fact "+string(rune('A'+i)))
		node.Tier = hypergraph.TierLongterm
		node.Confidence = 0.8
		node.CreatedAt = time.Now().Add(-time.Hour * 24 * 30) // 30 days old
		require.NoError(t, store.CreateNode(ctx, node))
	}

	result, err := mgr.IdleMaintenance(ctx)
	require.NoError(t, err)

	assert.Equal(t, "idle", result.Operation)
	assert.NotNil(t, result.Decay)
	assert.Greater(t, result.Duration, time.Duration(0))
}

func TestIdleMaintenance_Callback(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	defer mgr.Close()

	var callbackInvoked bool

	mgr.OnIdleMaintenance(func(result *LifecycleResult) {
		callbackInvoked = true
	})

	ctx := context.Background()
	_, err = mgr.IdleMaintenance(ctx)
	require.NoError(t, err)

	assert.True(t, callbackInvoked)
}

func TestIdleMaintenance_NoArchive(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()
	cfg.RunArchiveOnIdle = false
	cfg.RunPruneOnIdle = false

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	defer mgr.Close()

	ctx := context.Background()
	result, err := mgr.IdleMaintenance(ctx)
	require.NoError(t, err)

	// Decay still runs
	assert.NotNil(t, result.Decay)
}

func TestStartStopIdleLoop(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()
	cfg.IdleInterval = time.Millisecond * 50 // Fast for testing

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	defer mgr.Close()

	var idleCount int32

	mgr.OnIdleMaintenance(func(result *LifecycleResult) {
		atomic.AddInt32(&idleCount, 1)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr.StartIdleLoop(ctx)

	// Wait for at least one idle run
	time.Sleep(time.Millisecond * 150)

	mgr.StopIdleLoop()

	assert.GreaterOrEqual(t, atomic.LoadInt32(&idleCount), int32(1))
}

func TestIdleLoop_DisabledWhenIntervalZero(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()
	cfg.IdleInterval = 0

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	defer mgr.Close()

	var idleCount int32

	mgr.OnIdleMaintenance(func(result *LifecycleResult) {
		atomic.AddInt32(&idleCount, 1)
	})

	ctx := context.Background()
	mgr.StartIdleLoop(ctx)

	time.Sleep(time.Millisecond * 50)
	mgr.StopIdleLoop()

	// Should not have run
	assert.Equal(t, int32(0), atomic.LoadInt32(&idleCount))
}

func TestLifecycleManager_Stats(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	defer mgr.Close()

	ctx := context.Background()

	// Run some operations
	mgr.TaskComplete(ctx)
	mgr.SessionEnd(ctx)

	stats := mgr.Stats()
	assert.Greater(t, stats.TotalOperations, 0)
	assert.Greater(t, stats.SuccessCount, 0)
}

func TestAuditLogger(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	defer mgr.Close()

	audit := mgr.AuditLogger()
	assert.NotNil(t, audit)
}

func TestForceConsolidate(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	defer mgr.Close()

	ctx := context.Background()

	// Add nodes
	for i := 0; i < 5; i++ {
		node := hypergraph.NewNode(hypergraph.NodeTypeFact, "fact "+string(rune('A'+i)))
		node.Tier = hypergraph.TierTask
		require.NoError(t, store.CreateNode(ctx, node))
	}

	result, err := mgr.ForceConsolidate(ctx, hypergraph.TierTask, hypergraph.TierTask)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.GreaterOrEqual(t, result.NodesProcessed, 0)
}

func TestForcePromote_FromTask(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	defer mgr.Close()

	ctx := context.Background()

	// Add task nodes ready for promotion
	for i := 0; i < 3; i++ {
		node := hypergraph.NewNode(hypergraph.NodeTypeFact, "task fact "+string(rune('A'+i)))
		node.Tier = hypergraph.TierTask
		node.AccessCount = 5
		require.NoError(t, store.CreateNode(ctx, node))
	}

	result, err := mgr.ForcePromote(ctx, true)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestForcePromote_FromSession(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	defer mgr.Close()

	ctx := context.Background()

	// Add session nodes ready for promotion
	for i := 0; i < 3; i++ {
		node := hypergraph.NewNode(hypergraph.NodeTypeFact, "session fact "+string(rune('A'+i)))
		node.Tier = hypergraph.TierSession
		node.AccessCount = 10
		require.NoError(t, store.CreateNode(ctx, node))
	}

	result, err := mgr.ForcePromote(ctx, false)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestForceDecay(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	defer mgr.Close()

	ctx := context.Background()

	// Add longterm nodes
	for i := 0; i < 3; i++ {
		node := hypergraph.NewNode(hypergraph.NodeTypeFact, "longterm fact "+string(rune('A'+i)))
		node.Tier = hypergraph.TierLongterm
		node.Confidence = 0.9
		require.NoError(t, store.CreateNode(ctx, node))
	}

	result, err := mgr.ForceDecay(ctx)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestMultipleCallbacks(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)
	defer mgr.Close()

	var count1, count2 int

	mgr.OnTaskComplete(func(result *LifecycleResult) {
		count1++
	})

	mgr.OnTaskComplete(func(result *LifecycleResult) {
		count2++
	})

	ctx := context.Background()
	mgr.TaskComplete(ctx)

	assert.Equal(t, 1, count1)
	assert.Equal(t, 1, count2)
}

func TestLifecycleResult_Fields(t *testing.T) {
	result := LifecycleResult{
		Operation:     "test",
		Consolidation: &ConsolidationResult{NodesProcessed: 10},
		Promotion:     &PromotionResult{TaskToSession: 5},
		Decay:         &DecayResult{NodesDecayed: 3},
		Duration:      time.Second,
		Errors:        []error{},
	}

	assert.Equal(t, "test", result.Operation)
	assert.Equal(t, 10, result.Consolidation.NodesProcessed)
	assert.Equal(t, 5, result.Promotion.TaskToSession)
	assert.Equal(t, 3, result.Decay.NodesDecayed)
	assert.Equal(t, time.Second, result.Duration)
}

func TestLifecycleStats_Fields(t *testing.T) {
	stats := LifecycleStats{
		TotalOperations:    100,
		SuccessCount:       95,
		ErrorCount:         5,
		TotalNodesAffected: 500,
		AverageDuration:    time.Millisecond * 100,
	}

	assert.Equal(t, 100, stats.TotalOperations)
	assert.Equal(t, 95, stats.SuccessCount)
	assert.Equal(t, 5, stats.ErrorCount)
	assert.Equal(t, 500, stats.TotalNodesAffected)
	assert.Equal(t, time.Millisecond*100, stats.AverageDuration)
}

func TestClose(t *testing.T) {
	store := createTestStoreLifecycle(t)
	cfg := DefaultLifecycleConfig()

	mgr, err := NewLifecycleManager(store, cfg)
	require.NoError(t, err)

	ctx := context.Background()
	mgr.StartIdleLoop(ctx)

	err = mgr.Close()
	require.NoError(t, err)
}

// Helper function to create a test store
func createTestStoreLifecycle(t *testing.T) *hypergraph.Store {
	t.Helper()
	store, err := hypergraph.NewStore(hypergraph.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}
