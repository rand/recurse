package budget

import (
	"fmt"
	"time"
)

// Limits defines budget constraints.
type Limits struct {
	// Token limits (per task)
	MaxInputTokens  int64 `json:"max_input_tokens" yaml:"max_input_tokens"`
	MaxOutputTokens int64 `json:"max_output_tokens" yaml:"max_output_tokens"`

	// Cost limit (per session, USD)
	MaxTotalCost float64 `json:"max_total_cost" yaml:"max_total_cost"`

	// RLM limits
	MaxRecursionDepth int `json:"max_recursion_depth" yaml:"max_recursion_depth"`
	MaxSubCalls       int `json:"max_subcalls_per_task" yaml:"max_subcalls_per_task"`

	// Time limit
	MaxSessionTime time.Duration `json:"max_session_time" yaml:"max_session_hours"`

	// Warning thresholds (0-1)
	CostWarningThreshold  float64 `json:"cost_warning_threshold" yaml:"cost_warning_threshold"`
	TokenWarningThreshold float64 `json:"token_warning_threshold" yaml:"token_warning_threshold"`
}

// DefaultLimits returns sensible default limits.
func DefaultLimits() Limits {
	return Limits{
		MaxInputTokens:        100000,
		MaxOutputTokens:       50000,
		MaxTotalCost:          5.00,
		MaxRecursionDepth:     5,
		MaxSubCalls:           20,
		MaxSessionTime:        8 * time.Hour,
		CostWarningThreshold:  0.80,
		TokenWarningThreshold: 0.75,
	}
}

// Violation represents a limit that has been exceeded or is near being exceeded.
type Violation struct {
	Metric   string  `json:"metric"`
	Current  float64 `json:"current"`
	Limit    float64 `json:"limit"`
	Percent  float64 `json:"percent"`
	Hard     bool    `json:"hard"`     // true if this is a hard limit (blocks operation)
	Warning  bool    `json:"warning"`  // true if this is a warning threshold
	Message  string  `json:"message"`
}

func (v Violation) Error() string {
	return v.Message
}

// Check evaluates the current state against limits and returns any violations.
func (l Limits) Check(state State) []Violation {
	var violations []Violation

	// Check input tokens
	if l.MaxInputTokens > 0 {
		percent := float64(state.InputTokens) / float64(l.MaxInputTokens)
		if percent >= 1.0 {
			violations = append(violations, Violation{
				Metric:  "input_tokens",
				Current: float64(state.InputTokens),
				Limit:   float64(l.MaxInputTokens),
				Percent: percent * 100,
				Hard:    true,
				Message: fmt.Sprintf("Input token limit exceeded: %d/%d", state.InputTokens, l.MaxInputTokens),
			})
		} else if l.TokenWarningThreshold > 0 && percent >= l.TokenWarningThreshold {
			violations = append(violations, Violation{
				Metric:  "input_tokens",
				Current: float64(state.InputTokens),
				Limit:   float64(l.MaxInputTokens),
				Percent: percent * 100,
				Warning: true,
				Message: fmt.Sprintf("Input tokens at %.0f%% of limit", percent*100),
			})
		}
	}

	// Check output tokens
	if l.MaxOutputTokens > 0 {
		percent := float64(state.OutputTokens) / float64(l.MaxOutputTokens)
		if percent >= 1.0 {
			violations = append(violations, Violation{
				Metric:  "output_tokens",
				Current: float64(state.OutputTokens),
				Limit:   float64(l.MaxOutputTokens),
				Percent: percent * 100,
				Hard:    true,
				Message: fmt.Sprintf("Output token limit exceeded: %d/%d", state.OutputTokens, l.MaxOutputTokens),
			})
		} else if l.TokenWarningThreshold > 0 && percent >= l.TokenWarningThreshold {
			violations = append(violations, Violation{
				Metric:  "output_tokens",
				Current: float64(state.OutputTokens),
				Limit:   float64(l.MaxOutputTokens),
				Percent: percent * 100,
				Warning: true,
				Message: fmt.Sprintf("Output tokens at %.0f%% of limit", percent*100),
			})
		}
	}

	// Check cost
	if l.MaxTotalCost > 0 {
		percent := state.TotalCost / l.MaxTotalCost
		if percent >= 1.0 {
			violations = append(violations, Violation{
				Metric:  "total_cost",
				Current: state.TotalCost,
				Limit:   l.MaxTotalCost,
				Percent: percent * 100,
				Hard:    true,
				Message: fmt.Sprintf("Cost limit exceeded: $%.4f/$%.2f", state.TotalCost, l.MaxTotalCost),
			})
		} else if l.CostWarningThreshold > 0 && percent >= l.CostWarningThreshold {
			violations = append(violations, Violation{
				Metric:  "total_cost",
				Current: state.TotalCost,
				Limit:   l.MaxTotalCost,
				Percent: percent * 100,
				Warning: true,
				Message: fmt.Sprintf("Cost at %.0f%% of limit ($%.4f/$%.2f)", percent*100, state.TotalCost, l.MaxTotalCost),
			})
		}
	}

	// Check recursion depth
	if l.MaxRecursionDepth > 0 && state.RecursionDepth >= l.MaxRecursionDepth {
		violations = append(violations, Violation{
			Metric:  "recursion_depth",
			Current: float64(state.RecursionDepth),
			Limit:   float64(l.MaxRecursionDepth),
			Percent: float64(state.RecursionDepth) / float64(l.MaxRecursionDepth) * 100,
			Hard:    true,
			Message: fmt.Sprintf("Max recursion depth reached: %d/%d", state.RecursionDepth, l.MaxRecursionDepth),
		})
	}

	// Check sub-calls
	if l.MaxSubCalls > 0 && state.SubCallCount >= l.MaxSubCalls {
		violations = append(violations, Violation{
			Metric:  "sub_calls",
			Current: float64(state.SubCallCount),
			Limit:   float64(l.MaxSubCalls),
			Percent: float64(state.SubCallCount) / float64(l.MaxSubCalls) * 100,
			Hard:    true,
			Message: fmt.Sprintf("Max sub-calls reached: %d/%d", state.SubCallCount, l.MaxSubCalls),
		})
	}

	// Check session time
	if l.MaxSessionTime > 0 {
		duration := state.SessionDuration()
		percent := float64(duration) / float64(l.MaxSessionTime)
		if percent >= 1.0 {
			violations = append(violations, Violation{
				Metric:  "session_time",
				Current: float64(duration),
				Limit:   float64(l.MaxSessionTime),
				Percent: percent * 100,
				Hard:    true,
				Message: fmt.Sprintf("Session time limit exceeded: %v/%v", duration.Round(time.Minute), l.MaxSessionTime),
			})
		}
	}

	return violations
}

// HasHardViolation returns true if any violations are hard limits.
func HasHardViolation(violations []Violation) bool {
	for _, v := range violations {
		if v.Hard {
			return true
		}
	}
	return false
}

// HasWarning returns true if any violations are warnings.
func HasWarning(violations []Violation) bool {
	for _, v := range violations {
		if v.Warning {
			return true
		}
	}
	return false
}
