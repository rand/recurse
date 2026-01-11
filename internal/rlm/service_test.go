package rlm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/memory/evolution"
	"github.com/rand/recurse/internal/memory/hypergraph"
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
