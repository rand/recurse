package rlmtrace

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTraceProvider implements TraceProvider for testing.
type mockTraceProvider struct {
	events []TraceEvent
	stats  TraceStats
}

func (m *mockTraceProvider) GetEvents(limit int) ([]TraceEvent, error) {
	if limit > len(m.events) {
		return m.events, nil
	}
	return m.events[:limit], nil
}

func (m *mockTraceProvider) GetEvent(id string) (*TraceEvent, error) {
	for _, e := range m.events {
		if e.ID == id {
			return &e, nil
		}
	}
	return nil, nil
}

func (m *mockTraceProvider) ClearEvents() error {
	m.events = nil
	m.stats = TraceStats{}
	return nil
}

func (m *mockTraceProvider) Stats() TraceStats {
	return m.stats
}

func TestNewTraceDialog(t *testing.T) {
	provider := &mockTraceProvider{
		events: []TraceEvent{
			{ID: "1", Type: EventDecision, Action: "Decide strategy"},
			{ID: "2", Type: EventSubcall, Action: "Process chunk"},
		},
		stats: TraceStats{
			TotalEvents: 2,
			TotalTokens: 500,
		},
	}

	dialog := NewTraceDialog(provider)
	require.NotNil(t, dialog)
	assert.Equal(t, RLMTraceDialogID, dialog.ID())
}

func TestTraceDialog_NilProvider(t *testing.T) {
	dialog := NewTraceDialog(nil)
	require.NotNil(t, dialog)

	// Init should not panic
	cmd := dialog.Init()
	assert.NotNil(t, cmd)
}

func TestTraceEventTypes(t *testing.T) {
	types := []TraceEventType{
		EventDecision,
		EventDecompose,
		EventSubcall,
		EventSynthesize,
		EventMemoryQuery,
		EventExecute,
	}

	for _, typ := range types {
		assert.NotEmpty(t, string(typ))
	}
}

func TestGetEventIcon(t *testing.T) {
	dialog := &traceDialogCmp{}

	tests := []struct {
		eventType TraceEventType
		expected  string
	}{
		{EventDecision, "[D]"},
		{EventDecompose, "[/]"},
		{EventSubcall, "[>]"},
		{EventSynthesize, "[+]"},
		{EventMemoryQuery, "[?]"},
		{EventExecute, "[X]"},
		{"unknown", "[*]"},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			got := dialog.getEventIcon(tt.eventType)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestGetStatusIcon(t *testing.T) {
	dialog := &traceDialogCmp{}

	tests := []struct {
		status   string
		expected string
	}{
		{"completed", "v"},
		{"running", "~"},
		{"failed", "x"},
		{"pending", "."},
		{"unknown", "."},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := dialog.getStatusIcon(tt.status)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestTraceEvent_Fields(t *testing.T) {
	now := time.Now()
	event := TraceEvent{
		ID:        "test-123",
		Type:      EventSubcall,
		Action:    "Process file.go",
		Details:   "Processing main function",
		Tokens:    150,
		Duration:  time.Second * 2,
		Timestamp: now,
		Depth:     1,
		ParentID:  "parent-1",
		Children:  []string{"child-1", "child-2"},
		Status:    "completed",
	}

	assert.Equal(t, "test-123", event.ID)
	assert.Equal(t, EventSubcall, event.Type)
	assert.Equal(t, "Process file.go", event.Action)
	assert.Equal(t, 150, event.Tokens)
	assert.Equal(t, time.Second*2, event.Duration)
	assert.Equal(t, 1, event.Depth)
	assert.Equal(t, "parent-1", event.ParentID)
	assert.Len(t, event.Children, 2)
	assert.Equal(t, "completed", event.Status)
}

func TestTraceStats_Fields(t *testing.T) {
	stats := TraceStats{
		TotalEvents:   10,
		TotalTokens:   5000,
		TotalDuration: time.Minute,
		MaxDepth:      3,
		EventsByType: map[TraceEventType]int{
			EventDecision: 2,
			EventSubcall:  5,
			EventSynthesize: 3,
		},
	}

	assert.Equal(t, 10, stats.TotalEvents)
	assert.Equal(t, 5000, stats.TotalTokens)
	assert.Equal(t, time.Minute, stats.TotalDuration)
	assert.Equal(t, 3, stats.MaxDepth)
	assert.Len(t, stats.EventsByType, 3)
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"this is a very long string", 10, "this is..."},
		{"with\nnewlines", 20, "with newlines"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.max)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatEventLine(t *testing.T) {
	dialog := &traceDialogCmp{
		width: 100,
	}

	event := TraceEvent{
		Type:   EventSubcall,
		Action: "Process main.go",
		Status: "completed",
		Tokens: 100,
		Depth:  1,
	}

	line := dialog.formatEventLine(event)

	assert.Contains(t, line, "[>]")      // subcall icon
	assert.Contains(t, line, "v")        // completed status
	assert.Contains(t, line, "Process")  // action
	assert.Contains(t, line, "[100t]")   // tokens
	assert.Contains(t, line, "  ")       // indent for depth 1
}
