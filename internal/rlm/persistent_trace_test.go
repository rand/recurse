package rlm

import (
	"testing"
	"time"

	"github.com/rand/recurse/internal/tui/components/dialogs/rlmtrace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPersistentTraceProvider(t *testing.T) {
	provider, err := NewPersistentTraceProvider(PersistentTraceConfig{})
	require.NoError(t, err)
	defer provider.Close()

	assert.NotNil(t, provider)
}

func TestPersistentTraceProvider_RecordEvent(t *testing.T) {
	provider, err := NewPersistentTraceProvider(PersistentTraceConfig{})
	require.NoError(t, err)
	defer provider.Close()

	event := TraceEvent{
		ID:        "test-1",
		Type:      "DIRECT",
		Action:    "Test action",
		Details:   "Test details",
		Tokens:    100,
		Duration:  time.Second,
		Timestamp: time.Now(),
		Depth:     0,
		Status:    "completed",
	}

	err = provider.RecordEvent(event)
	require.NoError(t, err)

	// Verify event was stored
	events, err := provider.GetEvents(10)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, "test-1", events[0].ID)
	assert.Equal(t, rlmtrace.EventDecision, events[0].Type)
	assert.Equal(t, "Test action", events[0].Action)
	assert.Equal(t, 100, events[0].Tokens)
}

func TestPersistentTraceProvider_GetEvent(t *testing.T) {
	provider, err := NewPersistentTraceProvider(PersistentTraceConfig{})
	require.NoError(t, err)
	defer provider.Close()

	// Record event
	event := TraceEvent{
		ID:        "test-2",
		Type:      "DECOMPOSE",
		Action:    "Decompose task",
		Timestamp: time.Now(),
		Status:    "completed",
	}
	err = provider.RecordEvent(event)
	require.NoError(t, err)

	// Get by ID
	result, err := provider.GetEvent("test-2")
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "test-2", result.ID)
	assert.Equal(t, rlmtrace.EventDecompose, result.Type)

	// Get non-existent
	result, err = provider.GetEvent("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestPersistentTraceProvider_Stats(t *testing.T) {
	provider, err := NewPersistentTraceProvider(PersistentTraceConfig{})
	require.NoError(t, err)
	defer provider.Close()

	// Record some events
	events := []TraceEvent{
		{ID: "1", Type: "DIRECT", Tokens: 100, Duration: time.Second, Timestamp: time.Now(), Depth: 0, Status: "completed"},
		{ID: "2", Type: "DECOMPOSE", Tokens: 200, Duration: 2 * time.Second, Timestamp: time.Now(), Depth: 1, Status: "completed"},
		{ID: "3", Type: "SUBCALL", Tokens: 150, Duration: time.Second, Timestamp: time.Now(), Depth: 2, Status: "completed"},
	}

	for _, e := range events {
		err := provider.RecordEvent(e)
		require.NoError(t, err)
	}

	stats := provider.Stats()
	assert.Equal(t, 3, stats.TotalEvents)
	assert.Equal(t, 450, stats.TotalTokens)
	assert.Equal(t, 2, stats.MaxDepth)
	assert.NotEmpty(t, stats.EventsByType)
}

func TestPersistentTraceProvider_ClearEvents(t *testing.T) {
	provider, err := NewPersistentTraceProvider(PersistentTraceConfig{})
	require.NoError(t, err)
	defer provider.Close()

	// Record event
	event := TraceEvent{
		ID:        "test-clear",
		Type:      "DIRECT",
		Timestamp: time.Now(),
		Status:    "completed",
	}
	err = provider.RecordEvent(event)
	require.NoError(t, err)

	// Verify it exists
	events, err := provider.GetEvents(10)
	require.NoError(t, err)
	assert.Len(t, events, 1)

	// Clear
	err = provider.ClearEvents()
	require.NoError(t, err)

	// Verify cleared
	events, err = provider.GetEvents(10)
	require.NoError(t, err)
	assert.Len(t, events, 0)

	stats := provider.Stats()
	assert.Equal(t, 0, stats.TotalEvents)
	assert.Equal(t, 0, stats.TotalTokens)
}

func TestPersistentTraceProvider_SessionID(t *testing.T) {
	provider, err := NewPersistentTraceProvider(PersistentTraceConfig{
		SessionID: "session-1",
	})
	require.NoError(t, err)
	defer provider.Close()

	// Record event
	event := TraceEvent{
		ID:        "test-session",
		Type:      "DIRECT",
		Timestamp: time.Now(),
		Status:    "completed",
	}
	err = provider.RecordEvent(event)
	require.NoError(t, err)

	// Get by session
	events, err := provider.GetEventsBySession("session-1", 10)
	require.NoError(t, err)
	assert.Len(t, events, 1)

	// Change session
	provider.SetSessionID("session-2")
	event2 := TraceEvent{
		ID:        "test-session-2",
		Type:      "DECOMPOSE",
		Timestamp: time.Now(),
		Status:    "completed",
	}
	err = provider.RecordEvent(event2)
	require.NoError(t, err)

	// Verify session isolation
	events1, err := provider.GetEventsBySession("session-1", 10)
	require.NoError(t, err)
	assert.Len(t, events1, 1)

	events2, err := provider.GetEventsBySession("session-2", 10)
	require.NoError(t, err)
	assert.Len(t, events2, 1)
}

func TestPersistentTraceProvider_ParentChild(t *testing.T) {
	provider, err := NewPersistentTraceProvider(PersistentTraceConfig{})
	require.NoError(t, err)
	defer provider.Close()

	// Record parent
	parent := TraceEvent{
		ID:        "parent-1",
		Type:      "DECOMPOSE",
		Timestamp: time.Now(),
		Status:    "completed",
	}
	err = provider.RecordEvent(parent)
	require.NoError(t, err)

	// Record children
	child1 := TraceEvent{
		ID:        "child-1",
		Type:      "SUBCALL",
		ParentID:  "parent-1",
		Timestamp: time.Now(),
		Status:    "completed",
	}
	child2 := TraceEvent{
		ID:        "child-2",
		Type:      "SUBCALL",
		ParentID:  "parent-1",
		Timestamp: time.Now(),
		Status:    "completed",
	}
	err = provider.RecordEvent(child1)
	require.NoError(t, err)
	err = provider.RecordEvent(child2)
	require.NoError(t, err)

	// Get children by parent
	children, err := provider.GetEventsByParent("parent-1")
	require.NoError(t, err)
	assert.Len(t, children, 2)
}

func TestPersistentTraceProvider_EventTypes(t *testing.T) {
	provider, err := NewPersistentTraceProvider(PersistentTraceConfig{})
	require.NoError(t, err)
	defer provider.Close()

	// Test all event type mappings
	types := []struct {
		input    string
		expected rlmtrace.TraceEventType
	}{
		{"DIRECT", rlmtrace.EventDecision},
		{"decision", rlmtrace.EventDecision},
		{"DECOMPOSE", rlmtrace.EventDecompose},
		{"SUBCALL", rlmtrace.EventSubcall},
		{"SYNTHESIZE", rlmtrace.EventSynthesize},
		{"MEMORY_QUERY", rlmtrace.EventMemoryQuery},
		{"execute", rlmtrace.EventExecute},
	}

	for _, tt := range types {
		event := TraceEvent{
			ID:        "type-" + tt.input,
			Type:      tt.input,
			Timestamp: time.Now(),
			Status:    "completed",
		}
		err := provider.RecordEvent(event)
		require.NoError(t, err)

		// Get the specific event by ID and verify type mapping
		result, err := provider.GetEvent("type-" + tt.input)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, tt.expected, result.Type, "Type mismatch for %s", tt.input)
	}
}
