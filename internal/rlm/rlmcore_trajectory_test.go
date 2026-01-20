package rlm

import (
	"testing"

	"github.com/rand/recurse/internal/rlmcore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTrajectoryRecorder records events for testing.
type mockTrajectoryRecorder struct {
	events []TraceEvent
}

func (m *mockTrajectoryRecorder) RecordEvent(event TraceEvent) error {
	m.events = append(m.events, event)
	return nil
}

func TestRLMCoreTrajectoryBridge_Available(t *testing.T) {
	if !rlmcore.Available() {
		t.Skip("rlm-core not available")
	}

	recorder := &mockTrajectoryRecorder{}
	bridge := NewRLMCoreTrajectoryBridge(recorder)

	require.NotNil(t, bridge)
	assert.True(t, bridge.Available())
}

func TestRLMCoreTrajectoryBridge_Nil(t *testing.T) {
	var bridge *RLMCoreTrajectoryBridge
	assert.False(t, bridge.Available())
	assert.NoError(t, bridge.RecordEvent(nil))
	assert.Empty(t, bridge.FinalAnswer())
	assert.False(t, bridge.HasError())
}

func TestRLMCoreTrajectoryBridge_RecordRLMStart(t *testing.T) {
	if !rlmcore.Available() {
		t.Skip("rlm-core not available")
	}

	recorder := &mockTrajectoryRecorder{}
	bridge := NewRLMCoreTrajectoryBridge(recorder)
	require.NotNil(t, bridge)

	err := bridge.RecordRLMStart("test query")
	require.NoError(t, err)

	require.Len(t, recorder.events, 1)
	event := recorder.events[0]
	assert.Equal(t, "decision", event.Type)
	assert.Equal(t, "RLM Start", event.Action)
	assert.Equal(t, "test query", event.Details)
	assert.Equal(t, 0, event.Depth)
	assert.Equal(t, "completed", event.Status)
}

func TestRLMCoreTrajectoryBridge_RecordAnalyze(t *testing.T) {
	if !rlmcore.Available() {
		t.Skip("rlm-core not available")
	}

	recorder := &mockTrajectoryRecorder{}
	bridge := NewRLMCoreTrajectoryBridge(recorder)
	require.NotNil(t, bridge)

	err := bridge.RecordAnalyze(1, "analysis result")
	require.NoError(t, err)

	require.Len(t, recorder.events, 1)
	event := recorder.events[0]
	assert.Equal(t, "DECOMPOSE", event.Type)
	assert.Equal(t, "Analyzing", event.Action)
	assert.Equal(t, "analysis result", event.Details)
	assert.Equal(t, 1, event.Depth)
}

func TestRLMCoreTrajectoryBridge_RecordREPLExec(t *testing.T) {
	if !rlmcore.Available() {
		t.Skip("rlm-core not available")
	}

	recorder := &mockTrajectoryRecorder{}
	bridge := NewRLMCoreTrajectoryBridge(recorder)
	require.NotNil(t, bridge)

	err := bridge.RecordREPLExec(1, "print('hello')")
	require.NoError(t, err)

	require.Len(t, recorder.events, 1)
	event := recorder.events[0]
	assert.Equal(t, "execute", event.Type)
	assert.Equal(t, "REPL Execute", event.Action)
	assert.Equal(t, "print('hello')", event.Details)
}

func TestRLMCoreTrajectoryBridge_RecordREPLResult(t *testing.T) {
	if !rlmcore.Available() {
		t.Skip("rlm-core not available")
	}

	recorder := &mockTrajectoryRecorder{}
	bridge := NewRLMCoreTrajectoryBridge(recorder)
	require.NotNil(t, bridge)

	err := bridge.RecordREPLResult(1, "hello", true)
	require.NoError(t, err)

	require.Len(t, recorder.events, 1)
	event := recorder.events[0]
	assert.Equal(t, "execute", event.Type)
	assert.Equal(t, "REPL Result", event.Action)
}

func TestRLMCoreTrajectoryBridge_RecordRecurseStartEnd(t *testing.T) {
	if !rlmcore.Available() {
		t.Skip("rlm-core not available")
	}

	recorder := &mockTrajectoryRecorder{}
	bridge := NewRLMCoreTrajectoryBridge(recorder)
	require.NotNil(t, bridge)

	// Record start
	err := bridge.RecordRecurseStart(1, "subquery")
	require.NoError(t, err)

	// Record end
	err = bridge.RecordRecurseEnd(1, "subresult")
	require.NoError(t, err)

	require.Len(t, recorder.events, 2)
	assert.Equal(t, "SUBCALL", recorder.events[0].Type)
	assert.Equal(t, "Recurse Start", recorder.events[0].Action)
	assert.Equal(t, "SYNTHESIZE", recorder.events[1].Type)
	assert.Equal(t, "Recurse End", recorder.events[1].Action)
}

func TestRLMCoreTrajectoryBridge_RecordError(t *testing.T) {
	if !rlmcore.Available() {
		t.Skip("rlm-core not available")
	}

	recorder := &mockTrajectoryRecorder{}
	bridge := NewRLMCoreTrajectoryBridge(recorder)
	require.NotNil(t, bridge)

	err := bridge.RecordError(0, "something went wrong")
	require.NoError(t, err)

	require.Len(t, recorder.events, 1)
	event := recorder.events[0]
	assert.Equal(t, "Error", event.Action)
	assert.Equal(t, "failed", event.Status)

	assert.True(t, bridge.HasError())
}

func TestRLMCoreTrajectoryBridge_RecordFinalAnswer(t *testing.T) {
	if !rlmcore.Available() {
		t.Skip("rlm-core not available")
	}

	recorder := &mockTrajectoryRecorder{}
	bridge := NewRLMCoreTrajectoryBridge(recorder)
	require.NotNil(t, bridge)

	err := bridge.RecordFinalAnswer(0, "the answer is 42")
	require.NoError(t, err)

	require.Len(t, recorder.events, 1)
	event := recorder.events[0]
	assert.Equal(t, "SYNTHESIZE", event.Type)
	assert.Equal(t, "Final Answer", event.Action)
	assert.Equal(t, "completed", event.Status)

	assert.Equal(t, "the answer is 42", bridge.FinalAnswer())
}

func TestRLMCoreTrajectoryBridge_ParentIDTracking(t *testing.T) {
	if !rlmcore.Available() {
		t.Skip("rlm-core not available")
	}

	recorder := &mockTrajectoryRecorder{}
	bridge := NewRLMCoreTrajectoryBridge(recorder)
	require.NotNil(t, bridge)

	// Record events at different depths
	_ = bridge.RecordRLMStart("query")           // depth 0, id = core-1
	_ = bridge.RecordAnalyze(1, "analysis")      // depth 1, parent = core-1
	_ = bridge.RecordRecurseStart(2, "subquery") // depth 2, parent = core-2

	require.Len(t, recorder.events, 3)

	// Depth 0 has no parent
	assert.Empty(t, recorder.events[0].ParentID)

	// Depth 1 should have depth 0 as parent
	assert.Equal(t, "core-1", recorder.events[1].ParentID)

	// Depth 2 should have depth 1 as parent
	assert.Equal(t, "core-2", recorder.events[2].ParentID)
}

func TestMapTrajectoryEventType(t *testing.T) {
	tests := []struct {
		input    rlmcore.TrajectoryEventType
		expected string
	}{
		{rlmcore.EventRLMStart, "decision"},
		{rlmcore.EventAnalyze, "DECOMPOSE"},
		{rlmcore.EventREPLExec, "execute"},
		{rlmcore.EventREPLResult, "execute"},
		{rlmcore.EventReason, "decision"},
		{rlmcore.EventRecurseStart, "SUBCALL"},
		{rlmcore.EventRecurseEnd, "SYNTHESIZE"},
		{rlmcore.EventFinal, "SYNTHESIZE"},
		{rlmcore.EventError, "decision"},
		{rlmcore.EventToolUse, "execute"},
		{rlmcore.EventVerifyStart, "DECOMPOSE"},
		{rlmcore.EventClaimExtracted, "DECOMPOSE"},
		{rlmcore.EventEvidenceChecked, "MEMORY_QUERY"},
		{rlmcore.EventVerifyComplete, "SYNTHESIZE"},
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.input)), func(t *testing.T) {
			result := mapTrajectoryEventType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUseRLMCoreTrajectory(t *testing.T) {
	// Should match rlmcore.Available()
	assert.Equal(t, rlmcore.Available(), UseRLMCoreTrajectory())
}
