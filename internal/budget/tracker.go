// Package budget provides cost and resource tracking for RLM operations.
package budget

import (
	"sync"
	"time"
)

// State tracks current resource usage.
type State struct {
	// Token counts
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	CachedTokens int64 `json:"cached_tokens"`

	// Cost (USD)
	TotalCost float64 `json:"total_cost"`

	// RLM-specific
	RecursionDepth int `json:"recursion_depth"`
	SubCallCount   int `json:"sub_call_count"`
	REPLExecutions int `json:"repl_executions"`

	// Time
	SessionStart time.Time     `json:"session_start"`
	TaskStart    time.Time     `json:"task_start,omitempty"`
}

// SessionDuration returns the time since session start.
func (s *State) SessionDuration() time.Duration {
	if s.SessionStart.IsZero() {
		return 0
	}
	return time.Since(s.SessionStart)
}

// TaskDuration returns the time since task start.
func (s *State) TaskDuration() time.Duration {
	if s.TaskStart.IsZero() {
		return 0
	}
	return time.Since(s.TaskStart)
}

// Tracker tracks budget usage across a session.
type Tracker struct {
	mu     sync.RWMutex
	state  State
	limits Limits

	// Callbacks for limit violations
	onLimitExceeded func(violation Violation)
}

// NewTracker creates a new budget tracker with the given limits.
func NewTracker(limits Limits) *Tracker {
	return &Tracker{
		state: State{
			SessionStart: time.Now(),
		},
		limits: limits,
	}
}

// SetLimitCallback sets a callback for when limits are exceeded.
func (t *Tracker) SetLimitCallback(cb func(Violation)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onLimitExceeded = cb
}

// State returns a copy of the current state.
func (t *Tracker) State() State {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.state
}

// Limits returns a copy of the current limits.
func (t *Tracker) Limits() Limits {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.limits
}

// UpdateLimits updates the limits.
func (t *Tracker) UpdateLimits(limits Limits) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.limits = limits
}

// StartTask marks the start of a new task.
func (t *Tracker) StartTask() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.TaskStart = time.Now()
}

// EndTask marks the end of a task and resets per-task counters.
func (t *Tracker) EndTask() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.TaskStart = time.Time{}
	t.state.SubCallCount = 0
	t.state.RecursionDepth = 0
}

// AddTokens records token usage and updates cost.
func (t *Tracker) AddTokens(input, output, cached int64, costPerInputToken, costPerOutputToken float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.state.InputTokens += input
	t.state.OutputTokens += output
	t.state.CachedTokens += cached

	// Calculate cost (cached tokens typically don't count toward input cost)
	cost := float64(input-cached)*costPerInputToken + float64(output)*costPerOutputToken
	t.state.TotalCost += cost

	// Check limits
	return t.checkLimitsLocked()
}

// IncrementSubCall increments the sub-call counter and recursion depth.
func (t *Tracker) IncrementSubCall(depth int) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.state.SubCallCount++
	if depth > t.state.RecursionDepth {
		t.state.RecursionDepth = depth
	}

	return t.checkLimitsLocked()
}

// IncrementREPLExecution increments the REPL execution counter.
func (t *Tracker) IncrementREPLExecution() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.REPLExecutions++
}

// checkLimitsLocked checks if any limits are exceeded. Must be called with lock held.
func (t *Tracker) checkLimitsLocked() error {
	violations := t.limits.Check(t.state)
	if len(violations) == 0 {
		return nil
	}

	// Notify callback for each violation
	if t.onLimitExceeded != nil {
		for _, v := range violations {
			t.onLimitExceeded(v)
		}
	}

	// Return the first hard violation as an error
	for _, v := range violations {
		if v.Hard {
			return v
		}
	}

	return nil
}

// CheckLimits checks current state against limits.
func (t *Tracker) CheckLimits() []Violation {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.limits.Check(t.state)
}

// Reset resets all counters but keeps limits.
func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = State{
		SessionStart: time.Now(),
	}
}

// Usage returns a summary of current usage as percentages of limits.
func (t *Tracker) Usage() Usage {
	t.mu.RLock()
	defer t.mu.RUnlock()

	u := Usage{}

	if t.limits.MaxInputTokens > 0 {
		u.InputTokensPercent = float64(t.state.InputTokens) / float64(t.limits.MaxInputTokens) * 100
	}
	if t.limits.MaxOutputTokens > 0 {
		u.OutputTokensPercent = float64(t.state.OutputTokens) / float64(t.limits.MaxOutputTokens) * 100
	}
	if t.limits.MaxTotalCost > 0 {
		u.CostPercent = t.state.TotalCost / t.limits.MaxTotalCost * 100
	}
	if t.limits.MaxRecursionDepth > 0 {
		u.RecursionPercent = float64(t.state.RecursionDepth) / float64(t.limits.MaxRecursionDepth) * 100
	}
	if t.limits.MaxSubCalls > 0 {
		u.SubCallsPercent = float64(t.state.SubCallCount) / float64(t.limits.MaxSubCalls) * 100
	}
	if t.limits.MaxSessionTime > 0 {
		u.SessionTimePercent = float64(t.state.SessionDuration()) / float64(t.limits.MaxSessionTime) * 100
	}

	return u
}

// Usage represents resource usage as percentages of limits.
type Usage struct {
	InputTokensPercent  float64 `json:"input_tokens_percent"`
	OutputTokensPercent float64 `json:"output_tokens_percent"`
	CostPercent         float64 `json:"cost_percent"`
	RecursionPercent    float64 `json:"recursion_percent"`
	SubCallsPercent     float64 `json:"sub_calls_percent"`
	SessionTimePercent  float64 `json:"session_time_percent"`
}
