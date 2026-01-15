package budget

import (
	"context"
	"testing"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) (*Store, *hypergraph.Store) {
	t.Helper()
	graph, err := hypergraph.NewStore(hypergraph.Options{
		Path:              "", // In-memory
		CreateIfNotExists: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { graph.Close() })

	return NewStore(graph), graph
}

func TestStore_SessionRecord(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	record := &SessionRecord{
		ID:        "test-session-1",
		ProjectID: "test-project",
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Now(),
		FinalState: State{
			InputTokens:  1000,
			OutputTokens: 500,
			TotalCost:    0.05,
		},
		Limits:    DefaultLimits(),
		TaskCount: 5,
	}

	// Save session
	err := store.SaveSession(ctx, record)
	require.NoError(t, err)

	// Get session
	got, err := store.GetSession(ctx, record.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, record.ID, got.ID)
	assert.Equal(t, record.ProjectID, got.ProjectID)
	assert.Equal(t, record.FinalState.InputTokens, got.FinalState.InputTokens)

	// List sessions
	sessions, err := store.ListSessions(ctx, "", 10)
	require.NoError(t, err)
	assert.Len(t, sessions, 1)

	// Delete session
	err = store.DeleteSession(ctx, record.ID)
	require.NoError(t, err)

	got, err = store.GetSession(ctx, record.ID)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestStore_ProjectBudget(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	pb := &ProjectBudget{
		ProjectID:    "test-project",
		Limits:       DefaultLimits(),
		MonthlyLimit: 100.0,
		ResetDay:     1,
	}

	// Save project budget
	err := store.SaveProjectBudget(ctx, pb)
	require.NoError(t, err)

	// Get project budget
	got, err := store.GetProjectBudget(ctx, "test-project")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, pb.ProjectID, got.ProjectID)
	assert.Equal(t, pb.MonthlyLimit, got.MonthlyLimit)

	// Update spending
	err = store.UpdateProjectSpending(ctx, "test-project", 1.50, 10000)
	require.NoError(t, err)

	got, err = store.GetProjectBudget(ctx, "test-project")
	require.NoError(t, err)
	assert.Equal(t, 1.50, got.SpentThisMonth)
	assert.Equal(t, int64(10000), got.TokensThisMonth)

	// Increment session count
	err = store.IncrementSessionCount(ctx, "test-project")
	require.NoError(t, err)

	got, err = store.GetProjectBudget(ctx, "test-project")
	require.NoError(t, err)
	assert.Equal(t, 1, got.SessionCount)

	// Delete project budget
	err = store.DeleteProjectBudget(ctx, "test-project")
	require.NoError(t, err)

	got, err = store.GetProjectBudget(ctx, "test-project")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestStore_GetOrCreateProjectBudget(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Get or create - should create
	pb, err := store.GetOrCreateProjectBudget(ctx, "new-project", DefaultLimits())
	require.NoError(t, err)
	require.NotNil(t, pb)
	assert.Equal(t, "new-project", pb.ProjectID)

	// Get or create again - should return existing
	pb2, err := store.GetOrCreateProjectBudget(ctx, "new-project", DefaultLimits())
	require.NoError(t, err)
	// Compare truncated times to avoid monotonic clock differences
	assert.True(t, pb.CreatedAt.Truncate(time.Second).Equal(pb2.CreatedAt.Truncate(time.Second)))
}

func TestStore_SpendingSummary(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Create project budget
	pb := &ProjectBudget{
		ProjectID:      "test-project",
		Limits:         DefaultLimits(),
		MonthlyLimit:   100.0,
		SpentThisMonth: 25.0,
		SpentAllTime:   150.0,
		ResetDay:       1,
	}
	err := store.SaveProjectBudget(ctx, pb)
	require.NoError(t, err)

	// Get spending summary
	summary, err := store.GetSpendingSummary(ctx, "test-project")
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, 25.0, summary.SpentThisMonth)
	assert.Equal(t, 150.0, summary.SpentAllTime)
	assert.Equal(t, 75.0, summary.MonthlyRemaining)
	assert.Equal(t, 25.0, summary.MonthlyPercent)
}

func TestManager_Session(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	cfg := ManagerConfig{
		ProjectID:   "test-project",
		Limits:      DefaultLimits(),
		Enforcement: DefaultEnforcementConfig(),
	}
	mgr := NewManager(store, cfg)

	// Start session
	err := mgr.StartSession(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, mgr.SessionID())

	// Add tokens
	err = mgr.AddTokens(100, 50, 0, 0.0003, 0.00075)
	require.NoError(t, err)

	state := mgr.State()
	assert.Equal(t, int64(100), state.InputTokens)
	assert.Equal(t, int64(50), state.OutputTokens)

	// End session
	err = mgr.EndSession(ctx)
	require.NoError(t, err)
	assert.Empty(t, mgr.SessionID())
}

func TestManager_TaskTracking(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	mgr := NewManager(store, ManagerConfig{
		Limits:      DefaultLimits(),
		Enforcement: DefaultEnforcementConfig(),
	})

	err := mgr.StartSession(ctx)
	require.NoError(t, err)

	// Start and end tasks
	mgr.StartTask()
	mgr.AddTokens(50, 25, 0, 0.00015, 0.000375)
	mgr.EndTask()

	mgr.StartTask()
	mgr.AddTokens(75, 30, 0, 0.000225, 0.00045)
	mgr.EndTask()

	// Check state - tokens are tracked in State
	state := mgr.State()
	assert.Equal(t, int64(125), state.InputTokens)
	assert.Equal(t, int64(55), state.OutputTokens)
	// Task count is tracked in the session record, not in State
}

func TestManager_BudgetCheck(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	limits := Limits{
		MaxInputTokens:  1000,
		MaxOutputTokens: 500,
		MaxTotalCost:    1.0,
	}
	mgr := NewManager(store, ManagerConfig{
		Limits:      limits,
		Enforcement: DefaultEnforcementConfig(),
	})

	err := mgr.StartSession(ctx)
	require.NoError(t, err)

	// Should proceed - within limits
	check := mgr.CheckBudget(100, 50, 0.001, 0.001)
	assert.True(t, check.CanProceed)

	// Add tokens near the limit
	mgr.AddTokens(900, 400, 0, 0.9, 0.0)

	// Should not proceed - would exceed limits
	check = mgr.CheckBudget(200, 100, 0.2, 0.0)
	assert.False(t, check.CanProceed)
	assert.Contains(t, check.BlockReason, "exceeded")
}

func TestManager_Events(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	mgr := NewManager(store, ManagerConfig{
		Limits:      DefaultLimits(),
		Enforcement: DefaultEnforcementConfig(),
	})

	var events []Event
	mgr.SetEventCallback(func(e Event) {
		events = append(events, e)
	})

	err := mgr.StartSession(ctx)
	require.NoError(t, err)

	mgr.StartTask()
	mgr.AddTokens(100, 50, 0, 0.0003, 0.00075)
	mgr.EndTask()

	err = mgr.EndSession(ctx)
	require.NoError(t, err)

	// Should have: session_start, task_start, tokens_added, task_end, session_end
	assert.GreaterOrEqual(t, len(events), 5)

	// Check event types
	types := make(map[EventType]bool)
	for _, e := range events {
		types[e.Type] = true
	}
	assert.True(t, types[EventSessionStart])
	assert.True(t, types[EventSessionEnd])
	assert.True(t, types[EventTaskStart])
	assert.True(t, types[EventTaskEnd])
	assert.True(t, types[EventTokensAdded])
}

func TestManager_Report(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	mgr := NewManager(store, ManagerConfig{
		Limits:      DefaultLimits(),
		Enforcement: DefaultEnforcementConfig(),
	})

	err := mgr.StartSession(ctx)
	require.NoError(t, err)

	mgr.AddTokens(500, 250, 100, 0.0015, 0.00375)

	report := mgr.Report()
	assert.NotEmpty(t, report.Summary)
	assert.Greater(t, report.State.InputTokens, int64(0))
}

func TestEnforcementConfig_Defaults(t *testing.T) {
	cfg := DefaultEnforcementConfig()
	assert.Equal(t, 0.80, cfg.WarnAt)
	assert.Equal(t, 1.00, cfg.BlockAt)
	assert.Equal(t, ActionLog, cfg.OnWarning)
	assert.Equal(t, ActionBlock, cfg.OnBlock)
}
