package budget

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestReportSummary(t *testing.T) {
	report := Report{
		State: State{
			InputTokens:    50000,
			TotalCost:      2.5,
			RecursionDepth: 3,
		},
		Limits: Limits{
			MaxInputTokens:    100000,
			MaxTotalCost:      5.0,
			MaxRecursionDepth: 5,
		},
	}

	summary := report.Summary()

	assert.Contains(t, summary, "50k/100k")
	assert.Contains(t, summary, "$2.5000/$5.00")
	assert.Contains(t, summary, "3/5")
}

func TestReportStatusBar(t *testing.T) {
	report := Report{
		State: State{
			InputTokens:    50000,
			TotalCost:      2.5,
			RecursionDepth: 3,
			SessionStart:   time.Now().Add(-30 * time.Minute),
		},
		Limits: Limits{
			MaxInputTokens:    100000,
			MaxTotalCost:      5.0,
			MaxRecursionDepth: 5,
		},
		Usage: Usage{
			InputTokensPercent: 50.0,
			CostPercent:        50.0,
		},
	}

	statusBar := report.StatusBar()

	assert.Contains(t, statusBar, "Tokens:")
	assert.Contains(t, statusBar, "Cost:")
	assert.Contains(t, statusBar, "Depth:")
	assert.Contains(t, statusBar, "▓") // Progress bar filled
	assert.Contains(t, statusBar, "░") // Progress bar empty
}

func TestReportDetailed(t *testing.T) {
	report := Report{
		State: State{
			InputTokens:    50000,
			OutputTokens:   25000,
			CachedTokens:   10000,
			TotalCost:      2.5,
			RecursionDepth: 3,
			SubCallCount:   8,
			REPLExecutions: 15,
			SessionStart:   time.Now().Add(-1 * time.Hour),
		},
		Limits: Limits{
			MaxInputTokens:    100000,
			MaxOutputTokens:   50000,
			MaxTotalCost:      5.0,
			MaxRecursionDepth: 5,
			MaxSubCalls:       20,
			MaxSessionTime:    8 * time.Hour,
		},
		Usage: Usage{
			InputTokensPercent:  50.0,
			OutputTokensPercent: 50.0,
			CostPercent:         50.0,
		},
	}

	detailed := report.Detailed()

	assert.Contains(t, detailed, "Budget Report")
	assert.Contains(t, detailed, "Token Usage")
	assert.Contains(t, detailed, "Input:")
	assert.Contains(t, detailed, "Output:")
	assert.Contains(t, detailed, "Cached:")
	assert.Contains(t, detailed, "Cost:")
	assert.Contains(t, detailed, "RLM Metrics")
	assert.Contains(t, detailed, "Recursion Depth:")
	assert.Contains(t, detailed, "Sub-calls:")
	assert.Contains(t, detailed, "REPL Executions:")
	assert.Contains(t, detailed, "Session Duration:")
}

func TestProgressBar(t *testing.T) {
	tests := []struct {
		percent  float64
		width    int
		expected string
	}{
		{0, 10, "░░░░░░░░░░"},
		{50, 10, "▓▓▓▓▓░░░░░"},
		{100, 10, "▓▓▓▓▓▓▓▓▓▓"},
		{150, 10, "▓▓▓▓▓▓▓▓▓▓"}, // Capped at 100%
		{-10, 10, "░░░░░░░░░░"}, // Capped at 0%
		{25, 8, "▓▓░░░░░░"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := progressBar(tt.percent, tt.width)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m"},
		{45 * time.Minute, "45m"},
		{1 * time.Hour, "1h"},
		{90 * time.Minute, "1h30m"},
		{2*time.Hour + 15*time.Minute, "2h15m"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatDuration(tt.duration)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCostEstimate(t *testing.T) {
	estimate := CostEstimate{
		InputTokens:        1000000, // 1M
		OutputTokens:       500000,  // 500k
		CostPerInputToken:  SonnetInputCost,
		CostPerOutputToken: SonnetOutputCost,
	}

	cost := estimate.Estimate()

	// 1M * $3/1M + 500k * $15/1M = $3 + $7.5 = $10.5
	assert.InDelta(t, 10.5, cost, 0.01)
}

func TestNewReport(t *testing.T) {
	limits := DefaultLimits()
	tracker := NewTracker(limits)

	_ = tracker.AddTokens(1000, 500, 100, SonnetInputCost, SonnetOutputCost)

	report := NewReport(tracker)

	assert.Equal(t, int64(1000), report.State.InputTokens)
	assert.Equal(t, int64(500), report.State.OutputTokens)
	assert.Equal(t, limits.MaxInputTokens, report.Limits.MaxInputTokens)
	assert.Greater(t, report.Usage.InputTokensPercent, float64(0))
}
