package rlm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/memory/evolution"
	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/rlm/checkpoint"
)

func TestDefaultServiceConfig(t *testing.T) {
	cfg := DefaultServiceConfig()

	assert.Equal(t, 1000, cfg.MaxTraceEvents)
	assert.Empty(t, cfg.StorePath)
	assert.Equal(t, 100000, cfg.Controller.MaxTokenBudget)
}

func TestNewService(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	require.NotNil(t, svc)
	defer svc.Stop()

	assert.NotNil(t, svc.store)
	assert.NotNil(t, svc.controller)
	assert.NotNil(t, svc.lifecycle)
	assert.NotNil(t, svc.tracer)
}

func TestService_StartStop(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()
	cfg.Lifecycle.IdleInterval = 0 // Disable for test

	svc, err := NewService(client, cfg)
	require.NoError(t, err)

	ctx := context.Background()

	// Not running initially
	assert.False(t, svc.IsRunning())

	// Start
	err = svc.Start(ctx)
	require.NoError(t, err)
	assert.True(t, svc.IsRunning())

	// Can't start twice
	err = svc.Start(ctx)
	assert.Error(t, err)

	// Stop
	err = svc.Stop()
	require.NoError(t, err)
	assert.False(t, svc.IsRunning())

	// Stop is idempotent
	err = svc.Stop()
	require.NoError(t, err)
}

func TestService_Execute(t *testing.T) {
	client := &mockLLMClient{
		responses: []string{`{"action": "DIRECT", "reasoning": "Test task"}`},
	}
	cfg := DefaultServiceConfig()
	cfg.Controller.StoreDecisions = false
	cfg.Lifecycle.IdleInterval = 0

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	ctx := context.Background()
	require.NoError(t, svc.Start(ctx))

	result, err := svc.Execute(ctx, "Test task")
	require.NoError(t, err)
	assert.NotEmpty(t, result.Response)

	stats := svc.Stats()
	assert.Equal(t, 1, stats.TotalExecutions)
	assert.Greater(t, stats.TotalTokens, 0)
}

func TestService_ExecuteNotRunning(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	ctx := context.Background()
	// Don't start the service

	_, err = svc.Execute(ctx, "Test task")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestService_TaskComplete(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()
	cfg.Lifecycle.IdleInterval = 0

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	ctx := context.Background()
	require.NoError(t, svc.Start(ctx))

	result, err := svc.TaskComplete(ctx)
	require.NoError(t, err)
	assert.Equal(t, "task_complete", result.Operation)

	stats := svc.Stats()
	assert.Equal(t, 1, stats.TasksCompleted)
}

func TestService_SessionEnd(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()
	cfg.Lifecycle.IdleInterval = 0

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	ctx := context.Background()
	require.NoError(t, svc.Start(ctx))

	result, err := svc.SessionEnd(ctx)
	require.NoError(t, err)
	assert.Equal(t, "session_end", result.Operation)

	stats := svc.Stats()
	assert.Equal(t, 1, stats.SessionsEnded)
}

func TestService_TraceProvider(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	tracer := svc.TraceProvider()
	assert.NotNil(t, tracer)

	// Should implement the interface
	events, err := tracer.GetEvents(10)
	require.NoError(t, err)
	assert.Empty(t, events) // No events yet
}

func TestService_Store(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	store := svc.Store()
	assert.NotNil(t, store)

	ctx := context.Background()
	nodes, err := store.ListNodes(ctx, hypergraph.NodeFilter{Limit: 10})
	require.NoError(t, err)
	// ListNodes returns nil for empty results, which is fine
	assert.Empty(t, nodes)
}

func TestService_LifecycleManager(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	lifecycle := svc.LifecycleManager()
	assert.NotNil(t, lifecycle)
}

func TestService_Controller(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	controller := svc.Controller()
	assert.NotNil(t, controller)
}

func TestService_Uptime(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()
	cfg.Lifecycle.IdleInterval = 0

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	// Not running
	assert.Equal(t, time.Duration(0), svc.Uptime())

	ctx := context.Background()
	require.NoError(t, svc.Start(ctx))

	time.Sleep(time.Millisecond * 10)

	uptime := svc.Uptime()
	assert.Greater(t, uptime, time.Duration(0))
}

func TestService_HealthCheck(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()
	cfg.Lifecycle.IdleInterval = 0

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	ctx := context.Background()
	require.NoError(t, svc.Start(ctx))

	status, err := svc.HealthCheck(ctx)
	require.NoError(t, err)

	assert.True(t, status.Running)
	assert.True(t, status.Healthy)
	assert.True(t, status.Checks["store"])
	assert.True(t, status.Checks["tracer"])
	assert.True(t, status.Checks["lifecycle"])
}

func TestService_HealthCheckNotRunning(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	ctx := context.Background()
	// Don't start

	status, err := svc.HealthCheck(ctx)
	require.NoError(t, err)

	assert.False(t, status.Running)
	assert.False(t, status.Healthy)
}

func TestService_Callbacks(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()
	cfg.Lifecycle.IdleInterval = 0

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	var taskCalled, sessionCalled bool

	svc.OnTaskComplete(func(result *evolution.LifecycleResult) {
		taskCalled = true
	})

	svc.OnSessionEnd(func(result *evolution.LifecycleResult) {
		sessionCalled = true
	})

	ctx := context.Background()
	require.NoError(t, svc.Start(ctx))

	svc.TaskComplete(ctx)
	svc.SessionEnd(ctx)

	assert.True(t, taskCalled)
	assert.True(t, sessionCalled)
}

func TestService_QueryMemory(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	ctx := context.Background()

	// Record some facts first
	require.NoError(t, svc.RecordFact(ctx, "The sky is blue", 0.9))
	require.NoError(t, svc.RecordFact(ctx, "Water is wet", 0.8))

	nodes, err := svc.QueryMemory(ctx, "sky", 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(nodes), 2)
}

func TestService_RecordFact(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	ctx := context.Background()

	err = svc.RecordFact(ctx, "Test fact content", 0.85)
	require.NoError(t, err)

	nodes, err := svc.Store().ListNodes(ctx, hypergraph.NodeFilter{
		Types: []hypergraph.NodeType{hypergraph.NodeTypeFact},
		Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, "Test fact content", nodes[0].Content)
	assert.Equal(t, 0.85, nodes[0].Confidence)
}

func TestService_RecordExperience(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	ctx := context.Background()

	err = svc.RecordExperience(ctx, "User asked about X, found solution Y")
	require.NoError(t, err)

	nodes, err := svc.Store().ListNodes(ctx, hypergraph.NodeFilter{
		Types: []hypergraph.NodeType{hypergraph.NodeTypeExperience},
		Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, nodes, 1)
}

func TestService_GetTraceEvents(t *testing.T) {
	client := &mockLLMClient{
		responses: []string{`{"action": "DIRECT", "reasoning": "Test"}`},
	}
	cfg := DefaultServiceConfig()
	cfg.Controller.StoreDecisions = false
	cfg.Lifecycle.IdleInterval = 0

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	ctx := context.Background()
	require.NoError(t, svc.Start(ctx))

	// Execute to generate trace events
	_, err = svc.Execute(ctx, "Test task")
	require.NoError(t, err)

	events, err := svc.GetTraceEvents(10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(events), 1)
}

func TestService_GetTraceStats(t *testing.T) {
	client := &mockLLMClient{
		responses: []string{`{"action": "DIRECT", "reasoning": "Test"}`},
	}
	cfg := DefaultServiceConfig()
	cfg.Controller.StoreDecisions = false
	cfg.Lifecycle.IdleInterval = 0

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	ctx := context.Background()
	require.NoError(t, svc.Start(ctx))

	// Execute to generate trace events
	_, err = svc.Execute(ctx, "Test task")
	require.NoError(t, err)

	stats := svc.GetTraceStats()
	assert.Greater(t, stats.TotalEvents, 0)
}

func TestService_ClearTrace(t *testing.T) {
	client := &mockLLMClient{
		responses: []string{`{"action": "DIRECT", "reasoning": "Test"}`},
	}
	cfg := DefaultServiceConfig()
	cfg.Controller.StoreDecisions = false
	cfg.Lifecycle.IdleInterval = 0

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	ctx := context.Background()
	require.NoError(t, svc.Start(ctx))

	// Execute to generate trace events
	_, err = svc.Execute(ctx, "Test task")
	require.NoError(t, err)

	events, _ := svc.GetTraceEvents(10)
	assert.NotEmpty(t, events)

	// Clear
	err = svc.ClearTrace()
	require.NoError(t, err)

	events, _ = svc.GetTraceEvents(10)
	assert.Empty(t, events)
}

func TestServiceStats_Fields(t *testing.T) {
	stats := ServiceStats{
		TotalExecutions: 10,
		TotalTokens:     5000,
		TotalDuration:   time.Second * 30,
		TasksCompleted:  5,
		SessionsEnded:   2,
		Errors:          1,
	}

	assert.Equal(t, 10, stats.TotalExecutions)
	assert.Equal(t, 5000, stats.TotalTokens)
	assert.Equal(t, time.Second*30, stats.TotalDuration)
	assert.Equal(t, 5, stats.TasksCompleted)
	assert.Equal(t, 2, stats.SessionsEnded)
	assert.Equal(t, 1, stats.Errors)
}

func TestHealthStatus_Fields(t *testing.T) {
	status := HealthStatus{
		Running: true,
		Healthy: true,
		Checks: map[string]bool{
			"store":     true,
			"tracer":    true,
			"lifecycle": true,
		},
	}

	assert.True(t, status.Running)
	assert.True(t, status.Healthy)
	assert.True(t, status.Checks["store"])
}

func TestService_CheckpointManager(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()
	cfg.Checkpoint.Path = t.TempDir()

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	// Checkpoint manager should be set
	assert.NotNil(t, svc.CheckpointManager())
}

func TestService_SetSessionID(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()
	cfg.Checkpoint.Path = t.TempDir()

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	svc.SetSessionID("test-session-123")

	// Save and load to verify
	require.NoError(t, svc.checkpoint.Save())

	cp, err := svc.LoadCheckpoint()
	require.NoError(t, err)
	require.NotNil(t, cp)
	assert.Equal(t, "test-session-123", cp.SessionID)
}

func TestService_UpdateCheckpointTask(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()
	cfg.Checkpoint.Path = t.TempDir()

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	taskTime := time.Now()
	svc.UpdateCheckpointTask("task-42", taskTime, 100, 25, 10)

	// Save and load to verify
	require.NoError(t, svc.checkpoint.Save())

	cp, err := svc.LoadCheckpoint()
	require.NoError(t, err)
	require.NotNil(t, cp)
	require.NotNil(t, cp.TaskState)

	assert.Equal(t, "task-42", cp.TaskState.TaskID)
	assert.Equal(t, 100, cp.TaskState.NodeCount)
	assert.Equal(t, 25, cp.TaskState.FactCount)
	assert.Equal(t, 10, cp.TaskState.EntityCount)
}

func TestService_UpdateCheckpointRLM(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()
	cfg.Checkpoint.Path = t.TempDir()

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	svc.UpdateCheckpointRLM(3, 10, "count the words", true, "rlm")

	// Save and load to verify
	require.NoError(t, svc.checkpoint.Save())

	cp, err := svc.LoadCheckpoint()
	require.NoError(t, err)
	require.NotNil(t, cp)
	require.NotNil(t, cp.RLMState)

	assert.Equal(t, 3, cp.RLMState.CurrentIteration)
	assert.Equal(t, 10, cp.RLMState.MaxIterations)
	assert.Equal(t, "count the words", cp.RLMState.LastTask)
	assert.True(t, cp.RLMState.REPLActive)
	assert.Equal(t, "rlm", cp.RLMState.Mode)
}

func TestService_ClearCheckpoint(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()
	cfg.Checkpoint.Path = t.TempDir()

	svc, err := NewService(client, cfg)
	require.NoError(t, err)
	defer svc.Stop()

	// Create a checkpoint
	svc.SetSessionID("test-session")
	require.NoError(t, svc.checkpoint.Save())

	// Verify it exists
	cp, err := svc.LoadCheckpoint()
	require.NoError(t, err)
	require.NotNil(t, cp)

	// Clear it
	require.NoError(t, svc.ClearCheckpoint())

	// Verify it's gone
	cp, err = svc.LoadCheckpoint()
	require.NoError(t, err)
	assert.Nil(t, cp)
}

func TestService_CheckpointStatsPersistOnStop(t *testing.T) {
	client := &mockLLMClient{}
	cfg := DefaultServiceConfig()
	cfg.Checkpoint.Path = t.TempDir()
	cfg.Lifecycle.IdleInterval = 0

	svc, err := NewService(client, cfg)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, svc.Start(ctx))

	// Create checkpoint with task state
	svc.SetSessionID("test-session")
	svc.UpdateCheckpointTask("task-1", time.Now(), 50, 10, 5)
	require.NoError(t, svc.checkpoint.Save())

	// Stop persists stats but clears session-specific state
	require.NoError(t, svc.Stop())

	// Create new manager to check stats persist but task state is cleared
	mgr := checkpoint.NewManager(cfg.Checkpoint)
	cp, err := mgr.Load()
	require.NoError(t, err)
	require.NotNil(t, cp, "checkpoint should exist with persisted stats")
	assert.Nil(t, cp.TaskState, "task state should be cleared on normal exit")
	assert.Nil(t, cp.RLMState, "RLM state should be cleared on normal exit")
	assert.NotNil(t, cp.ServiceStats, "service stats should persist across sessions")
}
