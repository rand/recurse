package rlm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rand/recurse/internal/rlm/meta"
	"github.com/rand/recurse/internal/rlm/repl"
)

// RecoveryConfig configures error recovery behavior.
type RecoveryConfig struct {
	// MaxRetries is the maximum number of retries for recoverable errors.
	MaxRetries int

	// RetryDelay is the base delay between retries.
	RetryDelay time.Duration

	// EnableDegradation allows falling back to direct mode on failure.
	EnableDegradation bool

	// LogErrors enables error logging for meta-analysis.
	LogErrors bool
}

// DefaultRecoveryConfig returns sensible defaults for error recovery.
func DefaultRecoveryConfig() RecoveryConfig {
	return RecoveryConfig{
		MaxRetries:        1,
		RetryDelay:        100 * time.Millisecond,
		EnableDegradation: true,
		LogErrors:         true,
	}
}

// ErrorCategory classifies errors for recovery decisions.
type ErrorCategory int

const (
	// ErrorCategoryRetryable indicates the error can be retried.
	ErrorCategoryRetryable ErrorCategory = iota

	// ErrorCategoryDegradable indicates fallback to direct mode.
	ErrorCategoryDegradable

	// ErrorCategoryTerminal indicates the error is fatal.
	ErrorCategoryTerminal

	// ErrorCategoryTimeout indicates a timeout occurred.
	ErrorCategoryTimeout

	// ErrorCategoryResource indicates a resource limit was hit.
	ErrorCategoryResource
)

// RecoveryAction describes what action to take for an error.
type RecoveryAction struct {
	Category    ErrorCategory
	ShouldRetry bool
	RetryPrompt string // Additional context for retry
	Degraded    bool   // If true, fell back to degraded mode
	Message     string // User-facing message
}

// ErrorRecord tracks an error for meta-analysis.
type ErrorRecord struct {
	Timestamp   time.Time
	Category    ErrorCategory
	Action      string
	Error       string
	Context     string
	Recovered   bool
	RetryCount  int
	Degraded    bool
}

// RecoveryManager handles error recovery in the RLM loop.
type RecoveryManager struct {
	config       RecoveryConfig
	errorHistory []ErrorRecord
	retryCount   int
}

// NewRecoveryManager creates a new recovery manager.
func NewRecoveryManager(config RecoveryConfig) *RecoveryManager {
	return &RecoveryManager{
		config:       config,
		errorHistory: make([]ErrorRecord, 0, 100),
	}
}

// ClassifyError determines the category of an error.
func (m *RecoveryManager) ClassifyError(err error) ErrorCategory {
	if err == nil {
		return ErrorCategoryRetryable
	}

	errStr := strings.ToLower(err.Error())

	// Check for resource errors
	var resourceErr *repl.ResourceError
	if errors.As(err, &resourceErr) {
		return ErrorCategoryResource
	}

	// Check for timeout errors
	if errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "deadline exceeded") {
		return ErrorCategoryTimeout
	}

	// Check for retryable transient errors
	if strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "temporary") ||
		strings.Contains(errStr, "retry") ||
		strings.Contains(errStr, "unavailable") {
		return ErrorCategoryRetryable
	}

	// Check for REPL execution errors (often retryable with better prompt)
	if strings.Contains(errStr, "repl") ||
		strings.Contains(errStr, "python") ||
		strings.Contains(errStr, "syntax") ||
		strings.Contains(errStr, "nameerror") ||
		strings.Contains(errStr, "typeerror") {
		return ErrorCategoryRetryable
	}

	// Check for decomposition/orchestration errors
	if strings.Contains(errStr, "decompose") ||
		strings.Contains(errStr, "synthesize") ||
		strings.Contains(errStr, "orchestrat") {
		return ErrorCategoryDegradable
	}

	// Check for terminal errors
	if strings.Contains(errStr, "invalid") ||
		strings.Contains(errStr, "not found") ||
		strings.Contains(errStr, "permission denied") ||
		strings.Contains(errStr, "unauthorized") {
		return ErrorCategoryTerminal
	}

	// Default: treat as degradable
	return ErrorCategoryDegradable
}

// DetermineAction decides what action to take for an error.
func (m *RecoveryManager) DetermineAction(err error, action meta.Action, state meta.State) *RecoveryAction {
	category := m.ClassifyError(err)

	result := &RecoveryAction{
		Category: category,
		Message:  err.Error(),
	}

	switch category {
	case ErrorCategoryRetryable:
		if m.retryCount < m.config.MaxRetries {
			result.ShouldRetry = true
			result.RetryPrompt = m.buildRetryPrompt(err, action)
			result.Message = fmt.Sprintf("Retrying after error: %v (attempt %d/%d)",
				err, m.retryCount+1, m.config.MaxRetries)
		} else if m.config.EnableDegradation {
			result.Degraded = true
			result.Message = fmt.Sprintf("Max retries reached, degrading to direct mode: %v", err)
		} else {
			result.Message = fmt.Sprintf("Max retries reached: %v", err)
		}

	case ErrorCategoryDegradable:
		if m.config.EnableDegradation {
			result.Degraded = true
			result.Message = fmt.Sprintf("Error in %s, falling back to direct mode: %v", action, err)
		}

	case ErrorCategoryTimeout:
		if m.retryCount < m.config.MaxRetries {
			result.ShouldRetry = true
			result.RetryPrompt = "The previous operation timed out. Please try a simpler approach."
			result.Message = fmt.Sprintf("Timeout, retrying with simpler approach")
		} else if m.config.EnableDegradation {
			result.Degraded = true
			result.Message = "Timeout after retries, degrading to direct mode"
		}

	case ErrorCategoryResource:
		// Resource errors should degrade, not retry
		if m.config.EnableDegradation {
			result.Degraded = true
			result.Message = fmt.Sprintf("Resource limit hit, falling back to direct mode: %v", err)
		}

	case ErrorCategoryTerminal:
		result.Message = fmt.Sprintf("Unrecoverable error: %v", err)
	}

	return result
}

// RecordError logs an error for meta-analysis.
func (m *RecoveryManager) RecordError(record ErrorRecord) {
	if !m.config.LogErrors {
		return
	}

	record.Timestamp = time.Now()
	m.errorHistory = append(m.errorHistory, record)

	// Keep history bounded
	if len(m.errorHistory) > 1000 {
		m.errorHistory = m.errorHistory[100:]
	}
}

// IncrementRetry increments the retry counter.
func (m *RecoveryManager) IncrementRetry() {
	m.retryCount++
}

// ResetRetry resets the retry counter for a new operation.
func (m *RecoveryManager) ResetRetry() {
	m.retryCount = 0
}

// RetryCount returns the current retry count.
func (m *RecoveryManager) RetryCount() int {
	return m.retryCount
}

// ErrorHistory returns the error history for analysis.
func (m *RecoveryManager) ErrorHistory() []ErrorRecord {
	return m.errorHistory
}

// ErrorStats returns statistics about errors.
func (m *RecoveryManager) ErrorStats() ErrorStats {
	stats := ErrorStats{
		CategoryCounts: make(map[ErrorCategory]int),
	}

	for _, record := range m.errorHistory {
		stats.TotalErrors++
		stats.CategoryCounts[record.Category]++
		if record.Recovered {
			stats.RecoveredCount++
		}
		if record.Degraded {
			stats.DegradedCount++
		}
	}

	if stats.TotalErrors > 0 {
		stats.RecoveryRate = float64(stats.RecoveredCount) / float64(stats.TotalErrors)
	}

	return stats
}

// ErrorStats contains error statistics.
type ErrorStats struct {
	TotalErrors    int
	RecoveredCount int
	DegradedCount  int
	RecoveryRate   float64
	CategoryCounts map[ErrorCategory]int
}

// buildRetryPrompt creates additional context for a retry attempt.
func (m *RecoveryManager) buildRetryPrompt(err error, action meta.Action) string {
	errStr := strings.ToLower(err.Error())

	// Build context based on error type
	var hints []string

	if strings.Contains(errStr, "syntax") {
		hints = append(hints, "The previous code had a syntax error. Please use valid Python syntax.")
	}
	if strings.Contains(errStr, "nameerror") {
		hints = append(hints, "A variable was not defined. Make sure to define all variables before use.")
	}
	if strings.Contains(errStr, "typeerror") {
		hints = append(hints, "A type error occurred. Check that operations are valid for the data types.")
	}
	if strings.Contains(errStr, "timeout") {
		hints = append(hints, "The operation timed out. Try a more efficient approach.")
	}
	if strings.Contains(errStr, "memory") {
		hints = append(hints, "Memory limit was approached. Process data in smaller chunks.")
	}

	if len(hints) == 0 {
		hints = append(hints, fmt.Sprintf("Previous attempt failed with: %s", truncateError(errStr, 100)))
	}

	return fmt.Sprintf("RECOVERY CONTEXT:\n%s\n\nPlease try an alternative approach.",
		strings.Join(hints, "\n"))
}

// truncateError truncates an error message for display.
func truncateError(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// RecoverableError wraps an error with recovery metadata.
type RecoverableError struct {
	Original error
	Action   *RecoveryAction
}

func (e *RecoverableError) Error() string {
	return e.Original.Error()
}

func (e *RecoverableError) Unwrap() error {
	return e.Original
}

// WrapWithRecovery wraps an error with recovery information.
func WrapWithRecovery(err error, action *RecoveryAction) *RecoverableError {
	return &RecoverableError{
		Original: err,
		Action:   action,
	}
}

// IsRecoverable checks if an error can be recovered from.
func IsRecoverable(err error) bool {
	var recoverable *RecoverableError
	if errors.As(err, &recoverable) {
		return recoverable.Action.ShouldRetry || recoverable.Action.Degraded
	}
	return false
}

// ShouldDegrade checks if error handling should degrade to direct mode.
func ShouldDegrade(err error) bool {
	var recoverable *RecoverableError
	if errors.As(err, &recoverable) {
		return recoverable.Action.Degraded
	}
	return false
}
