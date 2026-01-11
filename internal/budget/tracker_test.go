package budget

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTracker(t *testing.T) {
	limits := DefaultLimits()
	tracker := NewTracker(limits)

	assert.NotNil(t, tracker)
	state := tracker.State()
	assert.False(t, state.SessionStart.IsZero())
	assert.Equal(t, int64(0), state.InputTokens)
	assert.Equal(t, int64(0), state.OutputTokens)
	assert.Equal(t, float64(0), state.TotalCost)
}

func TestTrackerAddTokens(t *testing.T) {
	limits := DefaultLimits()
	tracker := NewTracker(limits)

	// Add some tokens
	err := tracker.AddTokens(1000, 500, 100, SonnetInputCost, SonnetOutputCost)
	require.NoError(t, err)

	state := tracker.State()
	assert.Equal(t, int64(1000), state.InputTokens)
	assert.Equal(t, int64(500), state.OutputTokens)
	assert.Equal(t, int64(100), state.CachedTokens)

	// Cost should be (input - cached) * inputCost + output * outputCost
	expectedCost := float64(900)*SonnetInputCost + float64(500)*SonnetOutputCost
	assert.InDelta(t, expectedCost, state.TotalCost, 0.0001)
}

func TestTrackerLimitExceeded(t *testing.T) {
	limits := Limits{
		MaxInputTokens:  1000,
		MaxOutputTokens: 500,
		MaxTotalCost:    0.01,
	}
	tracker := NewTracker(limits)

	// First addition should succeed
	err := tracker.AddTokens(500, 200, 0, SonnetInputCost, SonnetOutputCost)
	require.NoError(t, err)

	// Exceed input token limit
	err = tracker.AddTokens(600, 0, 0, SonnetInputCost, SonnetOutputCost)
	require.Error(t, err)

	var violation Violation
	require.ErrorAs(t, err, &violation)
	assert.Equal(t, "input_tokens", violation.Metric)
	assert.True(t, violation.Hard)
}

func TestTrackerWarningThreshold(t *testing.T) {
	limits := Limits{
		MaxInputTokens:        1000,
		TokenWarningThreshold: 0.80,
	}
	tracker := NewTracker(limits)

	var warnings []Violation
	tracker.SetLimitCallback(func(v Violation) {
		warnings = append(warnings, v)
	})

	// Add tokens at 85% of limit - should trigger warning
	err := tracker.AddTokens(850, 0, 0, SonnetInputCost, SonnetOutputCost)
	require.NoError(t, err) // Warnings don't return errors

	require.Len(t, warnings, 1)
	assert.True(t, warnings[0].Warning)
	assert.Equal(t, "input_tokens", warnings[0].Metric)
}

func TestTrackerRecursionDepth(t *testing.T) {
	limits := Limits{
		MaxRecursionDepth: 3,
	}
	tracker := NewTracker(limits)

	// Increment depth
	err := tracker.IncrementSubCall(1)
	require.NoError(t, err)

	err = tracker.IncrementSubCall(2)
	require.NoError(t, err)

	// Reach limit
	err = tracker.IncrementSubCall(3)
	require.Error(t, err)

	var violation Violation
	require.ErrorAs(t, err, &violation)
	assert.Equal(t, "recursion_depth", violation.Metric)
}

func TestTrackerSubCalls(t *testing.T) {
	limits := Limits{
		MaxSubCalls: 5,
	}
	tracker := NewTracker(limits)

	// Add sub-calls up to limit
	for i := 0; i < 4; i++ {
		err := tracker.IncrementSubCall(1)
		require.NoError(t, err)
	}

	// Fifth call should hit the limit
	err := tracker.IncrementSubCall(1)
	require.Error(t, err)

	var violation Violation
	require.ErrorAs(t, err, &violation)
	assert.Equal(t, "sub_calls", violation.Metric)
}

func TestTrackerTask(t *testing.T) {
	limits := DefaultLimits()
	tracker := NewTracker(limits)

	// Start task
	tracker.StartTask()
	state := tracker.State()
	assert.False(t, state.TaskStart.IsZero())

	// Add some activity
	_ = tracker.IncrementSubCall(1)
	_ = tracker.IncrementSubCall(2)
	tracker.IncrementREPLExecution()

	state = tracker.State()
	assert.Equal(t, 2, state.SubCallCount)
	assert.Equal(t, 2, state.RecursionDepth)
	assert.Equal(t, 1, state.REPLExecutions)

	// End task resets per-task counters
	tracker.EndTask()
	state = tracker.State()
	assert.True(t, state.TaskStart.IsZero())
	assert.Equal(t, 0, state.SubCallCount)
	assert.Equal(t, 0, state.RecursionDepth)
	// REPL executions persist across tasks
	assert.Equal(t, 1, state.REPLExecutions)
}

func TestTrackerReset(t *testing.T) {
	limits := DefaultLimits()
	tracker := NewTracker(limits)

	// Add some state
	_ = tracker.AddTokens(1000, 500, 0, SonnetInputCost, SonnetOutputCost)
	_ = tracker.IncrementSubCall(1)
	tracker.IncrementREPLExecution()

	// Reset
	tracker.Reset()

	state := tracker.State()
	assert.Equal(t, int64(0), state.InputTokens)
	assert.Equal(t, int64(0), state.OutputTokens)
	assert.Equal(t, float64(0), state.TotalCost)
	assert.Equal(t, 0, state.SubCallCount)
	assert.Equal(t, 0, state.REPLExecutions)
	assert.False(t, state.SessionStart.IsZero()) // Reset creates new session
}

func TestTrackerUsage(t *testing.T) {
	limits := Limits{
		MaxInputTokens:    1000,
		MaxOutputTokens:   500,
		MaxTotalCost:      1.0,
		MaxRecursionDepth: 5,
		MaxSubCalls:       10,
		MaxSessionTime:    1 * time.Hour,
	}
	tracker := NewTracker(limits)

	_ = tracker.AddTokens(500, 250, 0, 0.001, 0.002)
	_ = tracker.IncrementSubCall(2)

	usage := tracker.Usage()

	assert.InDelta(t, 50.0, usage.InputTokensPercent, 0.1)
	assert.InDelta(t, 50.0, usage.OutputTokensPercent, 0.1)
	assert.InDelta(t, 40.0, usage.RecursionPercent, 0.1)
	assert.InDelta(t, 10.0, usage.SubCallsPercent, 0.1)
}

func TestLimitsCheck(t *testing.T) {
	tests := []struct {
		name       string
		limits     Limits
		state      State
		wantHard   bool
		wantWarn   bool
		wantMetric string
	}{
		{
			name:   "no violations",
			limits: DefaultLimits(),
			state: State{
				InputTokens: 1000,
				TotalCost:   0.01,
			},
			wantHard: false,
			wantWarn: false,
		},
		{
			name: "input token exceeded",
			limits: Limits{
				MaxInputTokens: 1000,
			},
			state: State{
				InputTokens: 1500,
			},
			wantHard:   true,
			wantMetric: "input_tokens",
		},
		{
			name: "cost warning",
			limits: Limits{
				MaxTotalCost:         1.0,
				CostWarningThreshold: 0.80,
			},
			state: State{
				TotalCost: 0.85,
			},
			wantWarn:   true,
			wantMetric: "total_cost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violations := tt.limits.Check(tt.state)

			if tt.wantHard {
				assert.True(t, HasHardViolation(violations))
				found := false
				for _, v := range violations {
					if v.Hard && v.Metric == tt.wantMetric {
						found = true
						break
					}
				}
				assert.True(t, found, "expected hard violation for metric %s", tt.wantMetric)
			}

			if tt.wantWarn {
				assert.True(t, HasWarning(violations))
				found := false
				for _, v := range violations {
					if v.Warning && v.Metric == tt.wantMetric {
						found = true
						break
					}
				}
				assert.True(t, found, "expected warning for metric %s", tt.wantMetric)
			}

			if !tt.wantHard && !tt.wantWarn {
				assert.Empty(t, violations)
			}
		})
	}
}
