package rlm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/tui/components/dialogs/rlmtrace"
)

func TestNewTraceProvider(t *testing.T) {
	p := NewTraceProvider(100)

	require.NotNil(t, p)
	assert.Equal(t, 100, p.maxLen)
	assert.Empty(t, p.events)
}

func TestNewTraceProvider_DefaultMaxEvents(t *testing.T) {
	p := NewTraceProvider(0)

	assert.Equal(t, 1000, p.maxLen)
}

func TestRecordEvent(t *testing.T) {
	p := NewTraceProvider(100)

	event := TraceEvent{
		ID:        "test-1",
		Type:      "decision",
		Action:    "Test action",
		Tokens:    50,
		Duration:  time.Millisecond * 100,
		Timestamp: time.Now(),
		Depth:     1,
		Status:    "completed",
	}

	err := p.RecordEvent(event)
	require.NoError(t, err)

	events, err := p.GetEvents(10)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, "test-1", events[0].ID)
	assert.Equal(t, rlmtrace.EventDecision, events[0].Type)
}

func TestRecordEvent_UpdatesStats(t *testing.T) {
	p := NewTraceProvider(100)

	p.RecordEvent(TraceEvent{ID: "1", Type: "decision", Tokens: 10, Depth: 1})
	p.RecordEvent(TraceEvent{ID: "2", Type: "DECOMPOSE", Tokens: 20, Depth: 2})
	p.RecordEvent(TraceEvent{ID: "3", Type: "SUBCALL", Tokens: 30, Depth: 3})

	stats := p.Stats()

	assert.Equal(t, 3, stats.TotalEvents)
	assert.Equal(t, 60, stats.TotalTokens)
	assert.Equal(t, 3, stats.MaxDepth)
	assert.Equal(t, 1, stats.EventsByType[rlmtrace.EventDecision])
	assert.Equal(t, 1, stats.EventsByType[rlmtrace.EventDecompose])
	assert.Equal(t, 1, stats.EventsByType[rlmtrace.EventSubcall])
}

func TestRecordEvent_EnforcesMaxLen(t *testing.T) {
	p := NewTraceProvider(5)

	for i := 0; i < 10; i++ {
		p.RecordEvent(TraceEvent{ID: string(rune('0' + i)), Type: "decision"})
	}

	events, err := p.GetEvents(100)
	require.NoError(t, err)
	assert.Len(t, events, 5)

	// Should have the most recent 5 events
	assert.Equal(t, "5", events[0].ID)
	assert.Equal(t, "9", events[4].ID)
}

func TestGetEvents(t *testing.T) {
	p := NewTraceProvider(100)

	for i := 0; i < 10; i++ {
		p.RecordEvent(TraceEvent{ID: string(rune('0' + i)), Type: "decision"})
	}

	// Get subset
	events, err := p.GetEvents(3)
	require.NoError(t, err)
	assert.Len(t, events, 3)

	// Get more than available
	events, err = p.GetEvents(100)
	require.NoError(t, err)
	assert.Len(t, events, 10)

	// Get with zero limit returns all
	events, err = p.GetEvents(0)
	require.NoError(t, err)
	assert.Len(t, events, 10)
}

func TestGetEvent(t *testing.T) {
	p := NewTraceProvider(100)

	p.RecordEvent(TraceEvent{ID: "target", Type: "decision", Action: "Found me"})
	p.RecordEvent(TraceEvent{ID: "other", Type: "decision", Action: "Not me"})

	event, err := p.GetEvent("target")
	require.NoError(t, err)
	require.NotNil(t, event)
	assert.Equal(t, "Found me", event.Action)
}

func TestGetEvent_NotFound(t *testing.T) {
	p := NewTraceProvider(100)

	p.RecordEvent(TraceEvent{ID: "exists", Type: "decision"})

	event, err := p.GetEvent("missing")
	require.NoError(t, err)
	assert.Nil(t, event)
}

func TestClearEvents(t *testing.T) {
	p := NewTraceProvider(100)

	p.RecordEvent(TraceEvent{ID: "1", Type: "decision", Tokens: 100})
	p.RecordEvent(TraceEvent{ID: "2", Type: "decision", Tokens: 200})

	err := p.ClearEvents()
	require.NoError(t, err)

	events, err := p.GetEvents(10)
	require.NoError(t, err)
	assert.Empty(t, events)

	stats := p.Stats()
	assert.Equal(t, 0, stats.TotalEvents)
	assert.Equal(t, 0, stats.TotalTokens)
}

func TestStats(t *testing.T) {
	p := NewTraceProvider(100)

	p.RecordEvent(TraceEvent{
		ID:       "1",
		Type:     "decision",
		Tokens:   50,
		Duration: time.Second,
		Depth:    2,
	})

	stats := p.Stats()

	assert.Equal(t, 1, stats.TotalEvents)
	assert.Equal(t, 50, stats.TotalTokens)
	assert.Equal(t, time.Second, stats.TotalDuration)
	assert.Equal(t, 2, stats.MaxDepth)
}

func TestGetEventsByType(t *testing.T) {
	p := NewTraceProvider(100)

	p.RecordEvent(TraceEvent{ID: "1", Type: "decision"})
	p.RecordEvent(TraceEvent{ID: "2", Type: "DECOMPOSE"})
	p.RecordEvent(TraceEvent{ID: "3", Type: "decision"})
	p.RecordEvent(TraceEvent{ID: "4", Type: "SUBCALL"})

	decisions := p.GetEventsByType(rlmtrace.EventDecision, 10)
	assert.Len(t, decisions, 2)

	decompose := p.GetEventsByType(rlmtrace.EventDecompose, 10)
	assert.Len(t, decompose, 1)
}

func TestGetEventsByParent(t *testing.T) {
	p := NewTraceProvider(100)

	p.RecordEvent(TraceEvent{ID: "parent", Type: "decision"})
	p.RecordEvent(TraceEvent{ID: "child1", Type: "SUBCALL", ParentID: "parent"})
	p.RecordEvent(TraceEvent{ID: "child2", Type: "SUBCALL", ParentID: "parent"})
	p.RecordEvent(TraceEvent{ID: "orphan", Type: "decision"})

	children := p.GetEventsByParent("parent")

	assert.Len(t, children, 2)
	assert.Equal(t, "child1", children[0].ID)
	assert.Equal(t, "child2", children[1].ID)
}

func TestGetRecentDuration(t *testing.T) {
	p := NewTraceProvider(100)

	now := time.Now()
	past := now.Add(-time.Hour)

	p.RecordEvent(TraceEvent{ID: "1", Type: "decision", Duration: time.Second, Timestamp: past})
	p.RecordEvent(TraceEvent{ID: "2", Type: "decision", Duration: time.Second * 2, Timestamp: now})

	// Get duration since 30 minutes ago (should only include recent event)
	since := now.Add(-time.Minute * 30)
	duration := p.GetRecentDuration(since)

	assert.Equal(t, time.Second*2, duration)
}

func TestMapEventType(t *testing.T) {
	tests := []struct {
		input    string
		expected rlmtrace.TraceEventType
	}{
		{"decision", rlmtrace.EventDecision},
		{"DIRECT", rlmtrace.EventDecision},
		{"DECOMPOSE", rlmtrace.EventDecompose},
		{"SUBCALL", rlmtrace.EventSubcall},
		{"SYNTHESIZE", rlmtrace.EventSynthesize},
		{"MEMORY_QUERY", rlmtrace.EventMemoryQuery},
		{"execute", rlmtrace.EventExecute},
		{"unknown", rlmtrace.EventDecision}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapEventType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTraceProvider_ImplementsInterface(t *testing.T) {
	// Compile-time check that TraceProvider implements rlmtrace.TraceProvider
	var _ rlmtrace.TraceProvider = (*TraceProvider)(nil)
}

func TestTraceProvider_Concurrency(t *testing.T) {
	p := NewTraceProvider(100)

	// Concurrent writes
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				p.RecordEvent(TraceEvent{
					ID:   string(rune('0' + id)),
					Type: "decision",
				})
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic and stats should be consistent
	stats := p.Stats()
	assert.Equal(t, 1000, stats.TotalEvents)
}
