package budget

import (
	"fmt"
	"strings"
	"time"
)

// Report generates a human-readable report of current budget state.
type Report struct {
	State  State  `json:"state"`
	Limits Limits `json:"limits"`
	Usage  Usage  `json:"usage"`
}

// NewReport creates a report from a tracker.
func NewReport(t *Tracker) Report {
	return Report{
		State:  t.State(),
		Limits: t.Limits(),
		Usage:  t.Usage(),
	}
}

// Summary returns a brief one-line summary.
func (r Report) Summary() string {
	return fmt.Sprintf("Tokens: %dk/%dk | Cost: $%.4f/$%.2f | Depth: %d/%d",
		r.State.InputTokens/1000, r.Limits.MaxInputTokens/1000,
		r.State.TotalCost, r.Limits.MaxTotalCost,
		r.State.RecursionDepth, r.Limits.MaxRecursionDepth,
	)
}

// StatusBar returns a formatted status suitable for display in a TUI status bar.
func (r Report) StatusBar() string {
	var parts []string

	// Tokens
	tokensBar := progressBar(r.Usage.InputTokensPercent, 10)
	parts = append(parts, fmt.Sprintf("[Tokens: %.1fk/%dk %s]",
		float64(r.State.InputTokens)/1000,
		r.Limits.MaxInputTokens/1000,
		tokensBar))

	// Cost
	costBar := progressBar(r.Usage.CostPercent, 10)
	parts = append(parts, fmt.Sprintf("[Cost: $%.2f/$%.2f %s]",
		r.State.TotalCost,
		r.Limits.MaxTotalCost,
		costBar))

	// Recursion depth
	parts = append(parts, fmt.Sprintf("[Depth: %d/%d]",
		r.State.RecursionDepth,
		r.Limits.MaxRecursionDepth))

	// Session time
	sessionDur := r.State.SessionDuration().Round(time.Minute)
	parts = append(parts, fmt.Sprintf("[⏱ %s]", formatDuration(sessionDur)))

	return strings.Join(parts, " ")
}

// Detailed returns a multi-line detailed report.
func (r Report) Detailed() string {
	var sb strings.Builder

	sb.WriteString("=== Budget Report ===\n\n")

	// Token usage
	sb.WriteString("Token Usage:\n")
	sb.WriteString(fmt.Sprintf("  Input:  %d / %d (%.1f%%)\n",
		r.State.InputTokens, r.Limits.MaxInputTokens, r.Usage.InputTokensPercent))
	sb.WriteString(fmt.Sprintf("  Output: %d / %d (%.1f%%)\n",
		r.State.OutputTokens, r.Limits.MaxOutputTokens, r.Usage.OutputTokensPercent))
	sb.WriteString(fmt.Sprintf("  Cached: %d\n", r.State.CachedTokens))
	sb.WriteString("\n")

	// Cost
	sb.WriteString("Cost:\n")
	sb.WriteString(fmt.Sprintf("  Total: $%.4f / $%.2f (%.1f%%)\n",
		r.State.TotalCost, r.Limits.MaxTotalCost, r.Usage.CostPercent))
	sb.WriteString("\n")

	// RLM metrics
	sb.WriteString("RLM Metrics:\n")
	sb.WriteString(fmt.Sprintf("  Recursion Depth: %d / %d\n",
		r.State.RecursionDepth, r.Limits.MaxRecursionDepth))
	sb.WriteString(fmt.Sprintf("  Sub-calls: %d / %d\n",
		r.State.SubCallCount, r.Limits.MaxSubCalls))
	sb.WriteString(fmt.Sprintf("  REPL Executions: %d\n", r.State.REPLExecutions))
	sb.WriteString("\n")

	// Time
	sb.WriteString("Time:\n")
	sb.WriteString(fmt.Sprintf("  Session Duration: %s / %s (%.1f%%)\n",
		formatDuration(r.State.SessionDuration()),
		formatDuration(r.Limits.MaxSessionTime),
		r.Usage.SessionTimePercent))
	if !r.State.TaskStart.IsZero() {
		sb.WriteString(fmt.Sprintf("  Task Duration: %s\n",
			formatDuration(r.State.TaskDuration())))
	}

	return sb.String()
}

// progressBar creates a simple ASCII progress bar.
func progressBar(percent float64, width int) string {
	if percent > 100 {
		percent = 100
	}
	if percent < 0 {
		percent = 0
	}

	filled := int(percent / 100 * float64(width))
	empty := width - filled

	return strings.Repeat("▓", filled) + strings.Repeat("░", empty)
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, minutes)
}

// CostEstimate estimates cost for a given number of tokens.
type CostEstimate struct {
	InputTokens       int64
	OutputTokens      int64
	CostPerInputToken float64
	CostPerOutputToken float64
}

// Estimate returns the estimated cost.
func (e CostEstimate) Estimate() float64 {
	return float64(e.InputTokens)*e.CostPerInputToken +
		float64(e.OutputTokens)*e.CostPerOutputToken
}

// Common pricing (USD per token) - these are approximate
var (
	// Claude Sonnet 4 pricing
	SonnetInputCost  = 0.000003  // $3 per 1M tokens
	SonnetOutputCost = 0.000015  // $15 per 1M tokens

	// Claude Haiku 4.5 pricing
	HaikuInputCost  = 0.0000008 // $0.80 per 1M tokens
	HaikuOutputCost = 0.000004  // $4 per 1M tokens

	// Claude Opus 4 pricing
	OpusInputCost  = 0.000015 // $15 per 1M tokens
	OpusOutputCost = 0.000075 // $75 per 1M tokens
)
