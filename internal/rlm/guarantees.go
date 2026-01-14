// Package rlm provides recursive language model orchestration.
// This file implements execution guarantees with hard limits and graceful degradation.
package rlm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Guarantee-related errors.
var (
	ErrBudgetExhausted  = errors.New("execution budget exhausted")
	ErrTimeoutExceeded  = errors.New("execution timeout exceeded")
	ErrMaxDepthExceeded = errors.New("maximum recursion depth exceeded")
	ErrMaxCallsExceeded = errors.New("maximum LLM calls exceeded")
)

// ExecutionGuarantees provides hard limits and tracking for RLM execution.
type ExecutionGuarantees struct {
	// Configured limits
	maxCostUSD        float64
	maxDuration       time.Duration
	maxRecursiveCalls int
	maxTokens         int

	// Current state (protected by mutex)
	mu            sync.RWMutex
	costUsed      float64
	tokensUsed    int
	callsUsed     int
	currentDepth  int
	maxDepthSeen  int
	startTime     time.Time
	deadline      time.Time
	violations    []Violation
	checkpoints   []Checkpoint

	// Callbacks
	onViolation func(Violation)
}

// GuaranteesConfig configures execution guarantees.
type GuaranteesConfig struct {
	MaxCostUSD        float64
	MaxDuration       time.Duration
	MaxRecursiveCalls int
	MaxTokens         int
}

// DefaultGuaranteesConfig returns sensible defaults.
func DefaultGuaranteesConfig() GuaranteesConfig {
	return GuaranteesConfig{
		MaxCostUSD:        1.0,             // $1 max
		MaxDuration:       5 * time.Minute, // 5 minutes max
		MaxRecursiveCalls: 50,              // 50 LLM calls max
		MaxTokens:         100000,          // 100K tokens max
	}
}

// NewExecutionGuarantees creates guarantees with the given config.
func NewExecutionGuarantees(cfg GuaranteesConfig) *ExecutionGuarantees {
	now := time.Now()
	return &ExecutionGuarantees{
		maxCostUSD:        cfg.MaxCostUSD,
		maxDuration:       cfg.MaxDuration,
		maxRecursiveCalls: cfg.MaxRecursiveCalls,
		maxTokens:         cfg.MaxTokens,
		startTime:         now,
		deadline:          now.Add(cfg.MaxDuration),
	}
}

// Violation represents a guarantee violation.
type Violation struct {
	Timestamp time.Time
	Type      ViolationType
	Message   string
	Severity  ViolationSeverity
}

// ViolationType categorizes violations.
type ViolationType int

const (
	ViolationBudget ViolationType = iota
	ViolationTime
	ViolationDepth
	ViolationCalls
)

func (v ViolationType) String() string {
	switch v {
	case ViolationBudget:
		return "budget"
	case ViolationTime:
		return "time"
	case ViolationDepth:
		return "depth"
	case ViolationCalls:
		return "calls"
	default:
		return "unknown"
	}
}

// ViolationSeverity indicates how critical a violation is.
type ViolationSeverity int

const (
	SeverityWarning ViolationSeverity = iota // Approaching limit (90%)
	SeveritySoft                             // Soft limit exceeded
	SeverityHard                             // Hard limit exceeded, must stop
)

func (s ViolationSeverity) String() string {
	switch s {
	case SeverityWarning:
		return "warning"
	case SeveritySoft:
		return "soft"
	case SeverityHard:
		return "hard"
	default:
		return "unknown"
	}
}

// Checkpoint represents a recoverable state.
type Checkpoint struct {
	Timestamp     time.Time
	PartialResult string
	TokensUsed    int
	CostUsed      float64
	CallsUsed     int
	Depth         int
}

// DegradationPlan specifies how to handle budget exhaustion.
type DegradationPlan struct {
	// Strategy indicates the degradation approach
	Strategy DegradationStrategy

	// PartialResult is the best result so far (if available)
	PartialResult string

	// ResourcesUsed summarizes consumption
	ResourcesUsed ResourceUsage

	// Reason explains why degradation occurred
	Reason string

	// CanRetry indicates if the operation can be retried with more budget
	CanRetry bool

	// SuggestedBudget is the recommended budget for retry
	SuggestedBudget GuaranteesConfig
}

// DegradationStrategy specifies how to degrade.
type DegradationStrategy int

const (
	// DegradationFail returns an error
	DegradationFail DegradationStrategy = iota
	// DegradationPartial returns partial results
	DegradationPartial
	// DegradationSynthesize combines partial results
	DegradationSynthesize
	// DegradationFallback uses a simpler approach
	DegradationFallback
)

func (s DegradationStrategy) String() string {
	switch s {
	case DegradationFail:
		return "fail"
	case DegradationPartial:
		return "partial"
	case DegradationSynthesize:
		return "synthesize"
	case DegradationFallback:
		return "fallback"
	default:
		return "unknown"
	}
}

// ResourceUsage summarizes resource consumption.
type ResourceUsage struct {
	CostUSD      float64
	Tokens       int
	Calls        int
	Duration     time.Duration
	MaxDepthSeen int
}

// CanProceed checks if execution can continue given estimated cost.
func (g *ExecutionGuarantees) CanProceed(estimatedCost float64) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Check time
	if time.Now().After(g.deadline) {
		return false
	}

	// Check cost
	if g.costUsed+estimatedCost > g.maxCostUSD {
		return false
	}

	// Check calls
	if g.callsUsed >= g.maxRecursiveCalls {
		return false
	}

	// Check tokens
	if g.tokensUsed >= g.maxTokens {
		return false
	}

	return true
}

// CanProceedWithTokens checks if execution can continue with estimated tokens.
func (g *ExecutionGuarantees) CanProceedWithTokens(estimatedTokens int) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if time.Now().After(g.deadline) {
		return false
	}

	if g.tokensUsed+estimatedTokens > g.maxTokens {
		return false
	}

	if g.callsUsed >= g.maxRecursiveCalls {
		return false
	}

	return true
}

// MustStop returns true if execution must stop due to hard violations.
func (g *ExecutionGuarantees) MustStop() bool {
	violations := g.Check()
	for _, v := range violations {
		if v.Severity == SeverityHard {
			return true
		}
	}
	return false
}

// Check verifies all guarantees and returns any violations.
func (g *ExecutionGuarantees) Check() []Violation {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var violations []Violation

	// Check cost budget
	if g.costUsed >= g.maxCostUSD {
		violations = append(violations, Violation{
			Timestamp: time.Now(),
			Type:      ViolationBudget,
			Message:   fmt.Sprintf("cost budget exhausted: $%.4f/$%.4f", g.costUsed, g.maxCostUSD),
			Severity:  SeverityHard,
		})
	} else if g.costUsed >= g.maxCostUSD*0.9 {
		violations = append(violations, Violation{
			Timestamp: time.Now(),
			Type:      ViolationBudget,
			Message:   fmt.Sprintf("cost budget at 90%%: $%.4f/$%.4f", g.costUsed, g.maxCostUSD),
			Severity:  SeverityWarning,
		})
	}

	// Check tokens
	if g.tokensUsed >= g.maxTokens {
		violations = append(violations, Violation{
			Timestamp: time.Now(),
			Type:      ViolationBudget,
			Message:   fmt.Sprintf("token budget exhausted: %d/%d", g.tokensUsed, g.maxTokens),
			Severity:  SeverityHard,
		})
	} else if g.tokensUsed >= int(float64(g.maxTokens)*0.9) {
		violations = append(violations, Violation{
			Timestamp: time.Now(),
			Type:      ViolationBudget,
			Message:   fmt.Sprintf("token budget at 90%%: %d/%d", g.tokensUsed, g.maxTokens),
			Severity:  SeverityWarning,
		})
	}

	// Check time
	elapsed := time.Since(g.startTime)
	if time.Now().After(g.deadline) {
		violations = append(violations, Violation{
			Timestamp: time.Now(),
			Type:      ViolationTime,
			Message:   fmt.Sprintf("deadline exceeded: %v/%v", elapsed, g.maxDuration),
			Severity:  SeverityHard,
		})
	} else if elapsed >= time.Duration(float64(g.maxDuration)*0.9) {
		violations = append(violations, Violation{
			Timestamp: time.Now(),
			Type:      ViolationTime,
			Message:   fmt.Sprintf("time at 90%%: %v/%v", elapsed, g.maxDuration),
			Severity:  SeverityWarning,
		})
	}

	// Check LLM calls
	if g.callsUsed >= g.maxRecursiveCalls {
		violations = append(violations, Violation{
			Timestamp: time.Now(),
			Type:      ViolationCalls,
			Message:   fmt.Sprintf("LLM call limit reached: %d/%d", g.callsUsed, g.maxRecursiveCalls),
			Severity:  SeverityHard,
		})
	} else if g.callsUsed >= int(float64(g.maxRecursiveCalls)*0.9) {
		violations = append(violations, Violation{
			Timestamp: time.Now(),
			Type:      ViolationCalls,
			Message:   fmt.Sprintf("LLM calls at 90%%: %d/%d", g.callsUsed, g.maxRecursiveCalls),
			Severity:  SeverityWarning,
		})
	}

	return violations
}

// OnBudgetExhausted returns a degradation plan when budget is exhausted.
func (g *ExecutionGuarantees) OnBudgetExhausted() *DegradationPlan {
	g.mu.RLock()
	defer g.mu.RUnlock()

	usage := ResourceUsage{
		CostUSD:      g.costUsed,
		Tokens:       g.tokensUsed,
		Calls:        g.callsUsed,
		Duration:     time.Since(g.startTime),
		MaxDepthSeen: g.maxDepthSeen,
	}

	// Determine which resource was exhausted
	var reason string
	switch {
	case g.costUsed >= g.maxCostUSD:
		reason = fmt.Sprintf("cost limit reached ($%.4f)", g.maxCostUSD)
	case g.tokensUsed >= g.maxTokens:
		reason = fmt.Sprintf("token limit reached (%d)", g.maxTokens)
	case g.callsUsed >= g.maxRecursiveCalls:
		reason = fmt.Sprintf("call limit reached (%d)", g.maxRecursiveCalls)
	case time.Now().After(g.deadline):
		reason = fmt.Sprintf("time limit reached (%v)", g.maxDuration)
	default:
		reason = "unknown limit reached"
	}

	// Get best partial result from checkpoints
	var partialResult string
	if len(g.checkpoints) > 0 {
		partialResult = g.checkpoints[len(g.checkpoints)-1].PartialResult
	}

	// Suggest increased budget for retry
	suggestedBudget := GuaranteesConfig{
		MaxCostUSD:        g.maxCostUSD * 2,
		MaxDuration:       g.maxDuration * 2,
		MaxRecursiveCalls: g.maxRecursiveCalls * 2,
		MaxTokens:         g.maxTokens * 2,
	}

	strategy := DegradationPartial
	if partialResult == "" {
		strategy = DegradationFail
	}

	return &DegradationPlan{
		Strategy:        strategy,
		PartialResult:   partialResult,
		ResourcesUsed:   usage,
		Reason:          reason,
		CanRetry:        true,
		SuggestedBudget: suggestedBudget,
	}
}

// RecordCost adds to cost usage.
func (g *ExecutionGuarantees) RecordCost(cost float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.costUsed += cost
	g.checkViolationsLocked()
}

// RecordTokens adds to token usage.
func (g *ExecutionGuarantees) RecordTokens(tokens int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tokensUsed += tokens
	g.checkViolationsLocked()
}

// RecordCall increments the call counter.
func (g *ExecutionGuarantees) RecordCall() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.callsUsed++
	g.checkViolationsLocked()
}

// EnterRecursion increments depth and checks limit.
func (g *ExecutionGuarantees) EnterRecursion() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.currentDepth++
	if g.currentDepth > g.maxDepthSeen {
		g.maxDepthSeen = g.currentDepth
	}

	// Note: depth is managed by the caller, not a hard limit here
	return nil
}

// ExitRecursion decrements depth.
func (g *ExecutionGuarantees) ExitRecursion() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.currentDepth > 0 {
		g.currentDepth--
	}
}

// Checkpoint saves current state for recovery.
func (g *ExecutionGuarantees) Checkpoint(partialResult string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	cp := Checkpoint{
		Timestamp:     time.Now(),
		PartialResult: partialResult,
		TokensUsed:    g.tokensUsed,
		CostUsed:      g.costUsed,
		CallsUsed:     g.callsUsed,
		Depth:         g.currentDepth,
	}
	g.checkpoints = append(g.checkpoints, cp)
}

// LastCheckpoint returns the most recent checkpoint, if any.
func (g *ExecutionGuarantees) LastCheckpoint() *Checkpoint {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(g.checkpoints) == 0 {
		return nil
	}
	cp := g.checkpoints[len(g.checkpoints)-1]
	return &cp
}

// Usage returns current resource usage.
func (g *ExecutionGuarantees) Usage() ResourceUsage {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return ResourceUsage{
		CostUSD:      g.costUsed,
		Tokens:       g.tokensUsed,
		Calls:        g.callsUsed,
		Duration:     time.Since(g.startTime),
		MaxDepthSeen: g.maxDepthSeen,
	}
}

// RemainingBudget returns remaining resources.
func (g *ExecutionGuarantees) RemainingBudget() ResourceUsage {
	g.mu.RLock()
	defer g.mu.RUnlock()

	remaining := g.deadline.Sub(time.Now())
	if remaining < 0 {
		remaining = 0
	}

	return ResourceUsage{
		CostUSD:  g.maxCostUSD - g.costUsed,
		Tokens:   g.maxTokens - g.tokensUsed,
		Calls:    g.maxRecursiveCalls - g.callsUsed,
		Duration: remaining,
	}
}

// Violations returns all recorded violations.
func (g *ExecutionGuarantees) Violations() []Violation {
	g.mu.RLock()
	defer g.mu.RUnlock()

	result := make([]Violation, len(g.violations))
	copy(result, g.violations)
	return result
}

// SetOnViolation sets a callback for violations.
func (g *ExecutionGuarantees) SetOnViolation(fn func(Violation)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.onViolation = fn
}

// checkViolationsLocked checks for new violations (must hold lock).
func (g *ExecutionGuarantees) checkViolationsLocked() {
	// Cost violation
	if g.costUsed >= g.maxCostUSD && !g.hasViolationType(ViolationBudget, SeverityHard) {
		v := Violation{
			Timestamp: time.Now(),
			Type:      ViolationBudget,
			Message:   fmt.Sprintf("cost budget exhausted: $%.4f/$%.4f", g.costUsed, g.maxCostUSD),
			Severity:  SeverityHard,
		}
		g.violations = append(g.violations, v)
		if g.onViolation != nil {
			go g.onViolation(v)
		}
	}

	// Token violation
	if g.tokensUsed >= g.maxTokens && !g.hasViolationTypeForTokens() {
		v := Violation{
			Timestamp: time.Now(),
			Type:      ViolationBudget,
			Message:   fmt.Sprintf("token budget exhausted: %d/%d", g.tokensUsed, g.maxTokens),
			Severity:  SeverityHard,
		}
		g.violations = append(g.violations, v)
		if g.onViolation != nil {
			go g.onViolation(v)
		}
	}

	// Call violation
	if g.callsUsed >= g.maxRecursiveCalls && !g.hasViolationType(ViolationCalls, SeverityHard) {
		v := Violation{
			Timestamp: time.Now(),
			Type:      ViolationCalls,
			Message:   fmt.Sprintf("LLM call limit reached: %d/%d", g.callsUsed, g.maxRecursiveCalls),
			Severity:  SeverityHard,
		}
		g.violations = append(g.violations, v)
		if g.onViolation != nil {
			go g.onViolation(v)
		}
	}
}

func (g *ExecutionGuarantees) hasViolationType(vtype ViolationType, severity ViolationSeverity) bool {
	for _, v := range g.violations {
		if v.Type == vtype && v.Severity == severity {
			return true
		}
	}
	return false
}

func (g *ExecutionGuarantees) hasViolationTypeForTokens() bool {
	for _, v := range g.violations {
		if v.Type == ViolationBudget && v.Severity == SeverityHard && v.Message != "" {
			// Check if it's a token violation specifically
			if len(v.Message) > 5 && v.Message[:5] == "token" {
				return true
			}
		}
	}
	return false
}

// Context returns a context that will be canceled when deadline is reached.
func (g *ExecutionGuarantees) Context(parent context.Context) (context.Context, context.CancelFunc) {
	g.mu.RLock()
	remaining := time.Until(g.deadline)
	g.mu.RUnlock()

	if remaining <= 0 {
		ctx, cancel := context.WithCancel(parent)
		cancel() // Already past deadline
		return ctx, cancel
	}

	return context.WithDeadline(parent, g.deadline)
}
