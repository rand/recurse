package rlm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewProgressEmitter_NilCallback(t *testing.T) {
	emitter := NewProgressEmitter(nil, 10)
	assert.Nil(t, emitter)
}

func TestNewProgressEmitter_WithCallback(t *testing.T) {
	events := []ProgressEvent{}
	callback := func(e ProgressEvent) {
		events = append(events, e)
	}

	emitter := NewProgressEmitter(callback, 10)
	assert.NotNil(t, emitter)
	assert.Equal(t, 10, emitter.maxIterations)
}

func TestProgressEmitter_Emit(t *testing.T) {
	events := []ProgressEvent{}
	callback := func(e ProgressEvent) {
		events = append(events, e)
	}

	emitter := NewProgressEmitter(callback, 5)

	emitter.Emit(ProgressIterationStart, 1, "Test message", ProgressData{})

	assert.Len(t, events, 1)
	assert.Equal(t, ProgressIterationStart, events[0].Type)
	assert.Equal(t, 1, events[0].Iteration)
	assert.Equal(t, 5, events[0].MaxIterations)
	assert.Equal(t, "Test message", events[0].Message)
}

func TestProgressEmitter_NilEmitterSafe(t *testing.T) {
	var emitter *ProgressEmitter

	// These should not panic
	emitter.Emit(ProgressIterationStart, 1, "test", ProgressData{})
	emitter.EmitIterationStart(1)
	emitter.EmitIterationEnd(1, time.Second, true)
	emitter.EmitLLMStart(1)
	emitter.EmitLLMEnd(1, time.Second, 100, true)
	emitter.EmitREPLStart(1, "code")
	emitter.EmitREPLEnd(1, time.Second, "output", "")
	emitter.EmitFinal(1, "answer")
	emitter.EmitError(1, "error")
	emitter.EmitComplete(1, time.Second, "output", false, "")
}

func TestProgressEmitter_AllEventTypes(t *testing.T) {
	events := []ProgressEvent{}
	callback := func(e ProgressEvent) {
		events = append(events, e)
	}

	emitter := NewProgressEmitter(callback, 3)

	emitter.EmitIterationStart(1)
	emitter.EmitLLMStart(1)
	emitter.EmitLLMEnd(1, 500*time.Millisecond, 1000, true)
	emitter.EmitREPLStart(1, "print('hello')")
	emitter.EmitREPLEnd(1, 100*time.Millisecond, "hello", "")
	emitter.EmitFinal(1, "42")
	emitter.EmitIterationEnd(1, 600*time.Millisecond, true)
	emitter.EmitComplete(1, time.Second, "42", true, "FINAL() called")

	assert.Len(t, events, 8)

	// Check event types in order
	expectedTypes := []ProgressEventType{
		ProgressIterationStart,
		ProgressLLMStart,
		ProgressLLMEnd,
		ProgressREPLStart,
		ProgressREPLEnd,
		ProgressFinal,
		ProgressIterationEnd,
		ProgressComplete,
	}

	for i, expectedType := range expectedTypes {
		assert.Equal(t, expectedType, events[i].Type, "event %d type", i)
	}
}

func TestProgressEmitter_DataFields(t *testing.T) {
	var capturedEvent ProgressEvent
	callback := func(e ProgressEvent) {
		capturedEvent = e
	}

	emitter := NewProgressEmitter(callback, 5)

	// Test LLM end data
	emitter.EmitLLMEnd(2, 500*time.Millisecond, 1500, true)
	assert.Equal(t, 500*time.Millisecond, capturedEvent.Data.Duration)
	assert.Equal(t, 1500, capturedEvent.Data.TokensUsed)
	assert.True(t, capturedEvent.Data.HasCode)

	// Test REPL end data
	emitter.EmitREPLEnd(2, 100*time.Millisecond, "output text", "error text")
	assert.Equal(t, 100*time.Millisecond, capturedEvent.Data.Duration)
	assert.Equal(t, "output text", capturedEvent.Data.Output)
	assert.Equal(t, "error text", capturedEvent.Data.Error)

	// Test completion data
	emitter.EmitComplete(3, 2*time.Second, "final answer", true, "early termination")
	assert.Equal(t, 2*time.Second, capturedEvent.Data.Duration)
	assert.Equal(t, "final answer", capturedEvent.Data.FinalOutput)
	assert.True(t, capturedEvent.Data.EarlyTerminated)
	assert.Equal(t, "early termination", capturedEvent.Data.TerminationReason)
}

func TestProgressEmitter_CodeTruncation(t *testing.T) {
	var capturedEvent ProgressEvent
	callback := func(e ProgressEvent) {
		capturedEvent = e
	}

	emitter := NewProgressEmitter(callback, 5)

	// Long code should be truncated
	longCode := string(make([]byte, 200))
	emitter.EmitREPLStart(1, longCode)
	assert.True(t, len(capturedEvent.Data.Code) <= 103) // 100 + "..."
}

func TestFormatProgressEvent(t *testing.T) {
	tests := []struct {
		name     string
		event    ProgressEvent
		contains string
	}{
		{
			name: "iteration start",
			event: ProgressEvent{
				Type:          ProgressIterationStart,
				Iteration:     1,
				MaxIterations: 5,
			},
			contains: "Starting",
		},
		{
			name: "LLM start",
			event: ProgressEvent{
				Type:          ProgressLLMStart,
				Iteration:     1,
				MaxIterations: 5,
			},
			contains: "Thinking",
		},
		{
			name: "LLM end with code",
			event: ProgressEvent{
				Type:          ProgressLLMEnd,
				Iteration:     1,
				MaxIterations: 5,
				Data:          ProgressData{HasCode: true},
			},
			contains: "Generated code",
		},
		{
			name: "REPL error",
			event: ProgressEvent{
				Type:          ProgressREPLEnd,
				Iteration:     1,
				MaxIterations: 5,
				Data:          ProgressData{Error: "NameError"},
			},
			contains: "Error",
		},
		{
			name: "final answer",
			event: ProgressEvent{
				Type:          ProgressFinal,
				Iteration:     1,
				MaxIterations: 5,
				Data:          ProgressData{FinalOutput: "42"},
			},
			contains: "42",
		},
		{
			name: "complete",
			event: ProgressEvent{
				Type:          ProgressComplete,
				Iteration:     2,
				MaxIterations: 5,
				Data:          ProgressData{Duration: time.Second},
			},
			contains: "Complete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatted := FormatProgressEvent(tt.event)
			assert.Contains(t, formatted, tt.contains)
		})
	}
}

func TestTruncateOutput(t *testing.T) {
	short := "short"
	assert.Equal(t, "short", truncateOutput(short, 10))

	long := "this is a very long string that should be truncated"
	result := truncateOutput(long, 20)
	assert.Equal(t, "this is a very long ...", result)
}

func TestFirstLine(t *testing.T) {
	assert.Equal(t, "first", firstLine("first\nsecond"))
	assert.Equal(t, "only", firstLine("only"))
	assert.Equal(t, "short", firstLine("short"))

	// Long single line should be truncated
	long := string(make([]byte, 100))
	result := firstLine(long)
	assert.True(t, len(result) <= 83) // 80 + "..."
}
