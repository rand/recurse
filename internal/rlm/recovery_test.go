package rlm

import (
	"context"
	"errors"
	"testing"

	"github.com/rand/recurse/internal/rlm/meta"
	"github.com/rand/recurse/internal/rlm/repl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRecoveryConfig(t *testing.T) {
	cfg := DefaultRecoveryConfig()

	assert.Equal(t, 1, cfg.MaxRetries)
	assert.Equal(t, 100*1000*1000, int(cfg.RetryDelay)) // 100ms in nanoseconds
	assert.True(t, cfg.EnableDegradation)
	assert.True(t, cfg.LogErrors)
}

func TestNewRecoveryManager(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	mgr := NewRecoveryManager(cfg)

	require.NotNil(t, mgr)
	assert.Equal(t, cfg.MaxRetries, mgr.config.MaxRetries)
	assert.Equal(t, 0, mgr.retryCount)
	assert.Empty(t, mgr.errorHistory)
}

func TestRecoveryManager_ClassifyError_Nil(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	category := mgr.ClassifyError(nil)
	assert.Equal(t, ErrorCategoryRetryable, category)
}

func TestRecoveryManager_ClassifyError_Timeout(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	tests := []struct {
		name string
		err  error
	}{
		{"context deadline", context.DeadlineExceeded},
		{"timeout string", errors.New("operation timeout")},
		{"deadline exceeded string", errors.New("deadline exceeded for request")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			category := mgr.ClassifyError(tt.err)
			assert.Equal(t, ErrorCategoryTimeout, category)
		})
	}
}

func TestRecoveryManager_ClassifyError_Retryable(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	tests := []struct {
		name string
		err  error
	}{
		{"connection error", errors.New("connection refused")},
		{"temporary error", errors.New("temporary failure")},
		{"retry suggestion", errors.New("please retry")},
		{"unavailable", errors.New("service unavailable")},
		{"python error", errors.New("python: SyntaxError")},
		{"syntax error", errors.New("syntax error at line 5")},
		{"nameerror", errors.New("NameError: name 'x' is not defined")},
		{"typeerror", errors.New("TypeError: unsupported operand")},
		{"repl error", errors.New("repl execution failed")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			category := mgr.ClassifyError(tt.err)
			assert.Equal(t, ErrorCategoryRetryable, category)
		})
	}
}

func TestRecoveryManager_ClassifyError_Degradable(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	tests := []struct {
		name string
		err  error
	}{
		{"decompose error", errors.New("failed to decompose task")},
		{"synthesize error", errors.New("synthesize failed")},
		{"orchestration error", errors.New("orchestration error")},
		{"unknown error", errors.New("something went wrong")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			category := mgr.ClassifyError(tt.err)
			assert.Equal(t, ErrorCategoryDegradable, category)
		})
	}
}

func TestRecoveryManager_ClassifyError_Terminal(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	tests := []struct {
		name string
		err  error
	}{
		{"invalid", errors.New("invalid request")},
		{"not found", errors.New("resource not found")},
		{"permission denied", errors.New("permission denied")},
		{"unauthorized", errors.New("unauthorized access")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			category := mgr.ClassifyError(tt.err)
			assert.Equal(t, ErrorCategoryTerminal, category)
		})
	}
}

func TestRecoveryManager_ClassifyError_Resource(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	resourceErr := repl.NewResourceError(
		&repl.ResourceViolation{Resource: "memory", Hard: true},
		&repl.ResourceStats{PeakMemoryMB: 1100},
	)

	category := mgr.ClassifyError(resourceErr)
	assert.Equal(t, ErrorCategoryResource, category)
}

func TestRecoveryManager_DetermineAction_Retryable(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	mgr := NewRecoveryManager(cfg)

	err := errors.New("syntax error in code")
	action := mgr.DetermineAction(err, meta.ActionDirect, meta.State{})

	assert.True(t, action.ShouldRetry)
	assert.False(t, action.Degraded)
	assert.NotEmpty(t, action.RetryPrompt)
	assert.Contains(t, action.Message, "Retrying")
}

func TestRecoveryManager_DetermineAction_RetryExhausted(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	mgr := NewRecoveryManager(cfg)
	mgr.retryCount = cfg.MaxRetries // Already at max

	err := errors.New("syntax error in code")
	action := mgr.DetermineAction(err, meta.ActionDirect, meta.State{})

	assert.False(t, action.ShouldRetry)
	assert.True(t, action.Degraded) // Falls back to degraded mode
	assert.Contains(t, action.Message, "Max retries reached")
}

func TestRecoveryManager_DetermineAction_RetryExhausted_NoDegradation(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	cfg.EnableDegradation = false
	mgr := NewRecoveryManager(cfg)
	mgr.retryCount = cfg.MaxRetries

	err := errors.New("syntax error in code")
	action := mgr.DetermineAction(err, meta.ActionDirect, meta.State{})

	assert.False(t, action.ShouldRetry)
	assert.False(t, action.Degraded)
	assert.Contains(t, action.Message, "Max retries reached")
}

func TestRecoveryManager_DetermineAction_Degradable(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	mgr := NewRecoveryManager(cfg)

	err := errors.New("decomposition failed")
	action := mgr.DetermineAction(err, meta.ActionDecompose, meta.State{})

	assert.False(t, action.ShouldRetry)
	assert.True(t, action.Degraded)
	assert.Contains(t, action.Message, "falling back")
}

func TestRecoveryManager_DetermineAction_Terminal(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	mgr := NewRecoveryManager(cfg)

	err := errors.New("permission denied")
	action := mgr.DetermineAction(err, meta.ActionDirect, meta.State{})

	assert.False(t, action.ShouldRetry)
	assert.False(t, action.Degraded)
	assert.Contains(t, action.Message, "Unrecoverable")
}

func TestRecoveryManager_DetermineAction_Timeout(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	mgr := NewRecoveryManager(cfg)

	action := mgr.DetermineAction(context.DeadlineExceeded, meta.ActionDirect, meta.State{})

	assert.True(t, action.ShouldRetry)
	assert.Contains(t, action.RetryPrompt, "timed out")
}

func TestRecoveryManager_DetermineAction_TimeoutExhausted(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	mgr := NewRecoveryManager(cfg)
	mgr.retryCount = cfg.MaxRetries

	action := mgr.DetermineAction(context.DeadlineExceeded, meta.ActionDirect, meta.State{})

	assert.False(t, action.ShouldRetry)
	assert.True(t, action.Degraded)
	assert.Contains(t, action.Message, "Timeout")
}

func TestRecoveryManager_DetermineAction_Resource(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	mgr := NewRecoveryManager(cfg)

	resourceErr := repl.NewResourceError(
		&repl.ResourceViolation{Resource: "memory", Hard: true},
		&repl.ResourceStats{PeakMemoryMB: 1100},
	)

	action := mgr.DetermineAction(resourceErr, meta.ActionDirect, meta.State{})

	assert.False(t, action.ShouldRetry)
	assert.True(t, action.Degraded)
	assert.Contains(t, action.Message, "Resource limit")
}

func TestRecoveryManager_RetryCounter(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	assert.Equal(t, 0, mgr.RetryCount())

	mgr.IncrementRetry()
	assert.Equal(t, 1, mgr.RetryCount())

	mgr.IncrementRetry()
	assert.Equal(t, 2, mgr.RetryCount())

	mgr.ResetRetry()
	assert.Equal(t, 0, mgr.RetryCount())
}

func TestRecoveryManager_RecordError(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	mgr := NewRecoveryManager(cfg)

	record := ErrorRecord{
		Category:  ErrorCategoryRetryable,
		Action:    "REPL",
		Error:     "syntax error",
		Context:   "test context",
		Recovered: true,
	}

	mgr.RecordError(record)

	history := mgr.ErrorHistory()
	require.Len(t, history, 1)
	assert.Equal(t, ErrorCategoryRetryable, history[0].Category)
	assert.True(t, history[0].Recovered)
	assert.NotZero(t, history[0].Timestamp)
}

func TestRecoveryManager_RecordError_Disabled(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	cfg.LogErrors = false
	mgr := NewRecoveryManager(cfg)

	record := ErrorRecord{
		Category: ErrorCategoryRetryable,
		Error:    "syntax error",
	}

	mgr.RecordError(record)

	history := mgr.ErrorHistory()
	assert.Empty(t, history)
}

func TestRecoveryManager_ErrorStats(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	// Add some errors
	mgr.RecordError(ErrorRecord{Category: ErrorCategoryRetryable, Recovered: true})
	mgr.RecordError(ErrorRecord{Category: ErrorCategoryRetryable, Recovered: false})
	mgr.RecordError(ErrorRecord{Category: ErrorCategoryTimeout, Recovered: true, Degraded: true})
	mgr.RecordError(ErrorRecord{Category: ErrorCategoryTerminal, Recovered: false})

	stats := mgr.ErrorStats()

	assert.Equal(t, 4, stats.TotalErrors)
	assert.Equal(t, 2, stats.RecoveredCount)
	assert.Equal(t, 1, stats.DegradedCount)
	assert.Equal(t, 0.5, stats.RecoveryRate)
	assert.Equal(t, 2, stats.CategoryCounts[ErrorCategoryRetryable])
	assert.Equal(t, 1, stats.CategoryCounts[ErrorCategoryTimeout])
	assert.Equal(t, 1, stats.CategoryCounts[ErrorCategoryTerminal])
}

func TestRecoveryManager_ErrorStats_Empty(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	stats := mgr.ErrorStats()

	assert.Equal(t, 0, stats.TotalErrors)
	assert.Equal(t, float64(0), stats.RecoveryRate)
}

func TestRecoveryManager_HistoryBounding(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	// Add more than 1000 errors
	for i := 0; i < 1050; i++ {
		mgr.RecordError(ErrorRecord{Category: ErrorCategoryRetryable})
	}

	// History should be bounded
	history := mgr.ErrorHistory()
	assert.LessOrEqual(t, len(history), 1000)
}

func TestBuildRetryPrompt(t *testing.T) {
	mgr := NewRecoveryManager(DefaultRecoveryConfig())

	tests := []struct {
		name     string
		err      error
		contains string
	}{
		{"syntax error", errors.New("SyntaxError: invalid syntax"), "syntax error"},
		{"name error", errors.New("NameError: name 'x'"), "variable was not defined"},
		{"type error", errors.New("TypeError: unsupported"), "type error occurred"},
		{"timeout", errors.New("timeout exceeded"), "timed out"},
		{"memory", errors.New("memory limit reached"), "smaller chunks"},
		{"generic", errors.New("something failed"), "Previous attempt failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := mgr.buildRetryPrompt(tt.err, meta.ActionDirect)
			assert.Contains(t, prompt, "RECOVERY CONTEXT")
			assert.Contains(t, prompt, tt.contains)
		})
	}
}

func TestTruncateError(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		max      int
		expected string
	}{
		{"short", "short error", 20, "short error"},
		{"exact", "12345", 5, "12345"},
		{"long", "this is a very long error message", 10, "this is..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateError(tt.input, tt.max)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRecoverableError(t *testing.T) {
	original := errors.New("original error")
	action := &RecoveryAction{
		ShouldRetry: true,
		RetryPrompt: "try again",
	}

	wrapped := WrapWithRecovery(original, action)

	assert.Equal(t, "original error", wrapped.Error())
	assert.Equal(t, original, wrapped.Unwrap())
	assert.Equal(t, action, wrapped.Action)
}

func TestIsRecoverable(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		recoverable bool
	}{
		{
			"retryable",
			WrapWithRecovery(errors.New("err"), &RecoveryAction{ShouldRetry: true}),
			true,
		},
		{
			"degradable",
			WrapWithRecovery(errors.New("err"), &RecoveryAction{Degraded: true}),
			true,
		},
		{
			"terminal",
			WrapWithRecovery(errors.New("err"), &RecoveryAction{ShouldRetry: false, Degraded: false}),
			false,
		},
		{
			"plain error",
			errors.New("plain error"),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.recoverable, IsRecoverable(tt.err))
		})
	}
}

func TestShouldDegrade(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		degrade bool
	}{
		{
			"degradable",
			WrapWithRecovery(errors.New("err"), &RecoveryAction{Degraded: true}),
			true,
		},
		{
			"not degradable",
			WrapWithRecovery(errors.New("err"), &RecoveryAction{Degraded: false}),
			false,
		},
		{
			"plain error",
			errors.New("plain error"),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.degrade, ShouldDegrade(tt.err))
		})
	}
}
