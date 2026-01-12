package rlm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRLMProfile_Basic(t *testing.T) {
	profile := NewRLMProfile()
	assert.NotNil(t, profile)
	assert.Empty(t, profile.Iterations)
}

func TestRLMProfile_Iteration(t *testing.T) {
	profile := NewRLMProfile()

	// Start iteration
	iter := profile.StartIteration(1)
	assert.NotNil(t, iter)
	assert.Equal(t, 1, iter.Number)

	// Simulate some work
	iter.LLMCallDur = 100 * time.Millisecond
	iter.REPLExecDur = 50 * time.Millisecond
	iter.ParseDur = 5 * time.Millisecond
	iter.PromptTokens = 1000
	iter.CompletionTokens = 200
	iter.HasCode = true
	iter.CodeLength = 150

	// End iteration
	time.Sleep(10 * time.Millisecond) // Simulate some time passing
	profile.EndIteration(iter)

	// Verify iteration was recorded
	assert.Len(t, profile.Iterations, 1)
	assert.Equal(t, 1, profile.Iterations[0].Number)
	assert.Equal(t, 100*time.Millisecond, profile.Iterations[0].LLMCallDur)
	assert.Equal(t, 50*time.Millisecond, profile.Iterations[0].REPLExecDur)
	assert.True(t, profile.Iterations[0].HasCode)

	// Verify aggregates
	assert.Equal(t, 100*time.Millisecond, profile.TotalLLMTime)
	assert.Equal(t, 50*time.Millisecond, profile.TotalREPLTime)
	assert.Equal(t, 1000, profile.TotalPromptTokens)
	assert.Equal(t, 200, profile.TotalCompletionTokens)
}

func TestRLMProfile_MultipleIterations(t *testing.T) {
	profile := NewRLMProfile()

	// Iteration 1
	iter1 := profile.StartIteration(1)
	iter1.LLMCallDur = 100 * time.Millisecond
	iter1.REPLExecDur = 50 * time.Millisecond
	iter1.PromptTokens = 1000
	iter1.CompletionTokens = 200
	profile.EndIteration(iter1)

	// Iteration 2
	iter2 := profile.StartIteration(2)
	iter2.LLMCallDur = 120 * time.Millisecond
	iter2.REPLExecDur = 60 * time.Millisecond
	iter2.PromptTokens = 1500
	iter2.CompletionTokens = 300
	iter2.HasFinal = true
	profile.EndIteration(iter2)

	// Verify aggregates
	assert.Len(t, profile.Iterations, 2)
	assert.Equal(t, 220*time.Millisecond, profile.TotalLLMTime)
	assert.Equal(t, 110*time.Millisecond, profile.TotalREPLTime)
	assert.Equal(t, 2500, profile.TotalPromptTokens)
	assert.Equal(t, 500, profile.TotalCompletionTokens)
}

func TestRLMProfile_Summary(t *testing.T) {
	profile := NewRLMProfile()
	profile.TotalDuration = 500 * time.Millisecond

	iter := profile.StartIteration(1)
	iter.LLMCallDur = 300 * time.Millisecond
	iter.REPLExecDur = 100 * time.Millisecond
	iter.ParseDur = 10 * time.Millisecond
	iter.PromptTokens = 1000
	iter.CompletionTokens = 200
	iter.HasCode = true
	iter.HasFinal = true
	profile.EndIteration(iter)

	summary := profile.Summary()
	assert.Contains(t, summary, "RLM Execution Profile")
	assert.Contains(t, summary, "Total Duration: 500ms")
	assert.Contains(t, summary, "Iterations: 1")
	assert.Contains(t, summary, "LLM Calls:")
	assert.Contains(t, summary, "REPL Exec:")
	assert.Contains(t, summary, "[FINAL]")
}

func TestRLMProfile_JSON(t *testing.T) {
	profile := NewRLMProfile()
	profile.TotalDuration = 500 * time.Millisecond

	iter := profile.StartIteration(1)
	iter.LLMCallDur = 300 * time.Millisecond
	iter.REPLExecDur = 100 * time.Millisecond
	iter.PromptTokens = 1000
	iter.CompletionTokens = 200
	iter.HasCode = true
	profile.EndIteration(iter)

	jsonData := profile.JSON()

	assert.Equal(t, int64(500), jsonData["total_duration_ms"])
	assert.Equal(t, 1, jsonData["iterations"])
	assert.Equal(t, int64(300), jsonData["total_llm_time_ms"])
	assert.Equal(t, int64(100), jsonData["total_repl_time_ms"])
	assert.Equal(t, 1000, jsonData["total_prompt_tokens"])
	assert.Equal(t, 200, jsonData["total_completion_tokens"])

	iterDetails := jsonData["iteration_details"].([]map[string]any)
	assert.Len(t, iterDetails, 1)
	assert.Equal(t, 1, iterDetails[0]["number"])
	assert.Equal(t, int64(300), iterDetails[0]["llm_call_ms"])
	assert.Equal(t, true, iterDetails[0]["has_code"])
}

func TestRLMProfile_PrimaryBottleneck(t *testing.T) {
	tests := []struct {
		name     string
		llmTime  time.Duration
		replTime time.Duration
		other    time.Duration
		expected BottleneckType
	}{
		{
			name:     "LLM bottleneck",
			llmTime:  300 * time.Millisecond,
			replTime: 100 * time.Millisecond,
			other:    50 * time.Millisecond,
			expected: BottleneckLLM,
		},
		{
			name:     "REPL bottleneck",
			llmTime:  100 * time.Millisecond,
			replTime: 300 * time.Millisecond,
			other:    50 * time.Millisecond,
			expected: BottleneckREPL,
		},
		{
			name:     "Other bottleneck",
			llmTime:  50 * time.Millisecond,
			replTime: 50 * time.Millisecond,
			other:    200 * time.Millisecond,
			expected: BottleneckOther,
		},
		{
			name:     "LLM wins tie with REPL",
			llmTime:  100 * time.Millisecond,
			replTime: 100 * time.Millisecond,
			other:    50 * time.Millisecond,
			expected: BottleneckLLM, // LLM >= REPL, so LLM wins
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := &RLMProfile{
				TotalLLMTime:   tt.llmTime,
				TotalREPLTime:  tt.replTime,
				TotalOtherTime: tt.other,
			}

			assert.Equal(t, tt.expected, profile.PrimaryBottleneck())
		})
	}
}

func TestRLMProfile_PercentFunctions(t *testing.T) {
	profile := &RLMProfile{
		TotalDuration: 1000 * time.Millisecond,
		TotalLLMTime:  600 * time.Millisecond,
		TotalREPLTime: 300 * time.Millisecond,
	}

	assert.InDelta(t, 60.0, profile.LLMTimePercent(), 0.01)
	assert.InDelta(t, 30.0, profile.REPLTimePercent(), 0.01)
}

func TestRLMProfile_ZeroDuration(t *testing.T) {
	profile := &RLMProfile{
		TotalDuration: 0,
		TotalLLMTime:  100 * time.Millisecond,
	}

	// Should return 0 to avoid division by zero
	assert.Equal(t, 0.0, profile.LLMTimePercent())
	assert.Equal(t, 0.0, profile.REPLTimePercent())
}

func TestRLMProfile_OtherTimeCalculation(t *testing.T) {
	profile := NewRLMProfile()

	iter := profile.StartIteration(1)

	// Simulate real time passing by sleeping
	time.Sleep(50 * time.Millisecond)

	// Set phase times that are LESS than actual elapsed time
	// This leaves room for "other" time (overhead)
	iter.LLMCallDur = 10 * time.Millisecond
	iter.REPLExecDur = 5 * time.Millisecond
	iter.ParseDur = 1 * time.Millisecond

	profile.EndIteration(iter)

	// OtherDur should be calculated as Duration - (LLM + REPL + Parse)
	accounted := iter.LLMCallDur + iter.REPLExecDur + iter.ParseDur
	recorded := profile.Iterations[0]
	assert.True(t, recorded.Duration >= accounted, "Duration should be >= accounted time")
	assert.Equal(t, recorded.Duration-accounted, recorded.OtherDur, "OtherDur should be the difference")
	assert.True(t, recorded.OtherDur > 0, "OtherDur should be positive since there was unaccounted time")
}
