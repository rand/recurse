package hallucination

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewErrorHandler(t *testing.T) {
	config := DefaultErrorHandlerConfig()
	handler := NewErrorHandler(config)

	assert.NotNil(t, handler)
	assert.Equal(t, config.DefaultConfidenceOnTimeout, handler.config.DefaultConfidenceOnTimeout)
}

func TestErrorHandler_HandleVerificationError_Nil(t *testing.T) {
	handler := NewErrorHandler(DefaultErrorHandlerConfig())

	result := handler.HandleVerificationError(nil, 0.9)

	assert.True(t, result.Success)
	assert.Equal(t, 0.9, result.AdjustedConfidence)
	assert.False(t, result.FallbackUsed)
}

func TestErrorHandler_HandleVerificationError_Timeout(t *testing.T) {
	config := DefaultErrorHandlerConfig()
	config.DefaultConfidenceOnTimeout = 0.5
	config.LogErrors = false
	handler := NewErrorHandler(config)

	result := handler.HandleVerificationError(context.DeadlineExceeded, 0.9)

	assert.False(t, result.Success)
	assert.True(t, result.FallbackUsed)
	assert.Equal(t, 0.45, result.AdjustedConfidence) // 0.9 * 0.5
	assert.Contains(t, result.FallbackReason, "timeout")

	stats := handler.Stats()
	assert.Equal(t, int64(1), stats.Timeouts)
	assert.Equal(t, int64(1), stats.Recoveries)
}

func TestErrorHandler_HandleVerificationError_RateLimit(t *testing.T) {
	config := DefaultErrorHandlerConfig()
	config.EnableQueueing = true
	config.LogErrors = false
	handler := NewErrorHandler(config)

	result := handler.HandleVerificationError(ErrRateLimited, 0.8)

	assert.False(t, result.Success)
	assert.True(t, result.FallbackUsed)
	assert.True(t, result.Queued)
	assert.Contains(t, result.FallbackReason, "rate limited")

	stats := handler.Stats()
	assert.Equal(t, int64(1), stats.RateLimits)
}

func TestErrorHandler_HandleVerificationError_GenericError(t *testing.T) {
	config := DefaultErrorHandlerConfig()
	config.LogErrors = false
	handler := NewErrorHandler(config)

	result := handler.HandleVerificationError(errors.New("some error"), 0.8)

	assert.False(t, result.Success)
	assert.True(t, result.FallbackUsed)
	assert.Contains(t, result.FallbackReason, "verification error")

	stats := handler.Stats()
	assert.Equal(t, int64(1), stats.Errors)
	assert.Equal(t, int64(1), stats.Recoveries)
}

func TestErrorHandler_WithRetry_Success(t *testing.T) {
	handler := NewErrorHandler(DefaultErrorHandlerConfig())
	ctx := context.Background()

	attempts := 0
	result := handler.WithRetry(ctx, func(ctx context.Context) (any, error) {
		attempts++
		return "success", nil
	})

	assert.True(t, result.Success)
	assert.Equal(t, "success", result.Result)
	assert.Equal(t, 1, attempts)
}

func TestErrorHandler_WithRetry_FailThenSucceed(t *testing.T) {
	config := DefaultErrorHandlerConfig()
	config.RetryDelay = 1 * time.Millisecond
	handler := NewErrorHandler(config)
	ctx := context.Background()

	attempts := 0
	result := handler.WithRetry(ctx, func(ctx context.Context) (any, error) {
		attempts++
		if attempts < 2 {
			return nil, errors.New("temporary error")
		}
		return "success", nil
	})

	assert.True(t, result.Success)
	assert.Equal(t, "success", result.Result)
	assert.Equal(t, 2, attempts)
}

func TestErrorHandler_WithRetry_AllFail(t *testing.T) {
	config := DefaultErrorHandlerConfig()
	config.MaxRetries = 2
	config.RetryDelay = 1 * time.Millisecond
	config.LogErrors = false
	handler := NewErrorHandler(config)
	ctx := context.Background()

	attempts := 0
	result := handler.WithRetry(ctx, func(ctx context.Context) (any, error) {
		attempts++
		return nil, errors.New("persistent error")
	})

	assert.False(t, result.Success)
	assert.True(t, result.FallbackUsed)
	assert.Equal(t, 3, attempts) // Initial + 2 retries
}

func TestErrorHandler_WithRetry_NoRetryOnInvalidInput(t *testing.T) {
	config := DefaultErrorHandlerConfig()
	config.MaxRetries = 3
	config.LogErrors = false
	handler := NewErrorHandler(config)
	ctx := context.Background()

	attempts := 0
	result := handler.WithRetry(ctx, func(ctx context.Context) (any, error) {
		attempts++
		return nil, ErrInvalidInput
	})

	assert.False(t, result.Success)
	assert.Equal(t, 1, attempts) // No retries on invalid input
}

func TestErrorHandler_WrapBackendCall(t *testing.T) {
	config := DefaultErrorHandlerConfig()
	config.LogErrors = false
	handler := NewErrorHandler(config)
	ctx := context.Background()

	// Success case
	result, err := handler.WrapBackendCall(ctx, func(ctx context.Context) (float64, error) {
		return 0.9, nil
	}, 0.5)

	require.NoError(t, err)
	assert.Equal(t, 0.9, result)

	// Error case - should not return error, just adjusted value
	result, err = handler.WrapBackendCall(ctx, func(ctx context.Context) (float64, error) {
		return 0, errors.New("backend error")
	}, 0.5)

	require.NoError(t, err) // Never returns error
	assert.Less(t, result, 0.5) // Adjusted down
}

func TestErrorHandler_SafeVerification(t *testing.T) {
	config := DefaultErrorHandlerConfig()
	config.LogErrors = false
	handler := NewErrorHandler(config)
	ctx := context.Background()

	claim := &Claim{Content: "test claim", Confidence: 0.8}

	// Success case
	result := handler.SafeVerification(ctx, func(ctx context.Context) (*VerificationResult, error) {
		return &VerificationResult{
			Claim:  claim,
			Status: StatusGrounded,
			P1:     0.9,
		}, nil
	}, claim)

	assert.Equal(t, StatusGrounded, result.Status)
	assert.Equal(t, 0.9, result.P1)

	// Error case
	result = handler.SafeVerification(ctx, func(ctx context.Context) (*VerificationResult, error) {
		return nil, errors.New("verification error")
	}, claim)

	assert.Equal(t, StatusUnverifiable, result.Status)
	assert.NotNil(t, result.Error)
}

func TestCircuitBreaker_AllowWhenClosed(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	assert.True(t, cb.Allow())
	assert.Equal(t, CircuitClosed, cb.State())
}

func TestCircuitBreaker_OpensAfterFailures(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	config.FailureThreshold = 3
	cb := NewCircuitBreaker(config)

	// Record failures
	cb.RecordFailure()
	cb.RecordFailure()
	assert.True(t, cb.Allow()) // Still closed

	cb.RecordFailure() // Third failure opens circuit
	assert.Equal(t, CircuitOpen, cb.State())
	assert.False(t, cb.Allow())
}

func TestCircuitBreaker_TransitionsToHalfOpen(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	config.FailureThreshold = 1
	config.OpenDuration = 1 * time.Millisecond
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.RecordFailure()
	assert.Equal(t, CircuitOpen, cb.State())

	// Wait for open duration
	time.Sleep(5 * time.Millisecond)

	// Should transition to half-open
	assert.True(t, cb.Allow())
	assert.Equal(t, CircuitHalfOpen, cb.State())
}

func TestCircuitBreaker_ClosesAfterSuccesses(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	config.FailureThreshold = 1
	config.SuccessThreshold = 2
	config.OpenDuration = 1 * time.Millisecond
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.RecordFailure()
	assert.Equal(t, CircuitOpen, cb.State())

	// Wait and transition to half-open
	time.Sleep(5 * time.Millisecond)
	cb.Allow()
	assert.Equal(t, CircuitHalfOpen, cb.State())

	// Record successes
	cb.RecordSuccess()
	assert.Equal(t, CircuitHalfOpen, cb.State())

	cb.RecordSuccess()
	assert.Equal(t, CircuitClosed, cb.State())
}

func TestCircuitBreaker_ReopensOnHalfOpenFailure(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	config.FailureThreshold = 1
	config.OpenDuration = 1 * time.Millisecond
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.RecordFailure()

	// Wait and transition to half-open
	time.Sleep(5 * time.Millisecond)
	cb.Allow()
	assert.Equal(t, CircuitHalfOpen, cb.State())

	// Failure in half-open reopens
	cb.RecordFailure()
	assert.Equal(t, CircuitOpen, cb.State())
}

func TestCircuitBreaker_Reset(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	config.FailureThreshold = 1
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.RecordFailure()
	assert.Equal(t, CircuitOpen, cb.State())

	// Reset
	cb.Reset()
	assert.Equal(t, CircuitClosed, cb.State())
	assert.True(t, cb.Allow())
}

func TestResilientBackend_EstimateProbability(t *testing.T) {
	backend := NewMockBackend(0.8)
	rb := NewResilientBackend(backend, DefaultErrorHandlerConfig(), DefaultCircuitBreakerConfig())
	ctx := context.Background()

	result, err := rb.EstimateProbability(ctx, "claim", "evidence")

	require.NoError(t, err)
	assert.Equal(t, 0.8, result)
}

func TestResilientBackend_CircuitBreakerBlocking(t *testing.T) {
	backend := NewMockBackend(0.8)
	config := DefaultCircuitBreakerConfig()
	config.FailureThreshold = 1
	rb := NewResilientBackend(backend, DefaultErrorHandlerConfig(), config)
	ctx := context.Background()

	// Manually open the circuit
	rb.circuitBreaker.RecordFailure()
	assert.Equal(t, CircuitOpen, rb.circuitBreaker.State())

	// Should return neutral probability when circuit is open
	result, err := rb.EstimateProbability(ctx, "claim", "evidence")

	require.NoError(t, err)
	assert.Equal(t, 0.5, result) // Neutral value
}

func TestResilientBackend_BatchEstimate(t *testing.T) {
	backend := NewMockBackend(0.8)
	rb := NewResilientBackend(backend, DefaultErrorHandlerConfig(), DefaultCircuitBreakerConfig())
	ctx := context.Background()

	results, err := rb.BatchEstimate(ctx, []string{"claim1", "claim2"}, "context")

	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, 0.8, results[0])
	assert.Equal(t, 0.8, results[1])
}

func TestResilientBackend_BatchEstimate_CircuitOpen(t *testing.T) {
	backend := NewMockBackend(0.8)
	config := DefaultCircuitBreakerConfig()
	config.FailureThreshold = 1
	rb := NewResilientBackend(backend, DefaultErrorHandlerConfig(), config)
	ctx := context.Background()

	// Open the circuit
	rb.circuitBreaker.RecordFailure()

	results, err := rb.BatchEstimate(ctx, []string{"claim1", "claim2"}, "context")

	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, 0.5, results[0]) // Neutral
	assert.Equal(t, 0.5, results[1]) // Neutral
}

func TestDefaultErrorHandlerConfig(t *testing.T) {
	cfg := DefaultErrorHandlerConfig()

	assert.Equal(t, 0.5, cfg.DefaultConfidenceOnTimeout)
	assert.Equal(t, 0.5, cfg.DefaultConfidenceOnError)
	assert.Equal(t, 2, cfg.MaxRetries)
	assert.Equal(t, 100*time.Millisecond, cfg.RetryDelay)
	assert.True(t, cfg.EnableQueueing)
	assert.Equal(t, 100, cfg.QueueSize)
	assert.True(t, cfg.LogErrors)
}

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()

	assert.Equal(t, 5, cfg.FailureThreshold)
	assert.Equal(t, 3, cfg.SuccessThreshold)
	assert.Equal(t, 30*time.Second, cfg.OpenDuration)
	assert.Equal(t, 3, cfg.HalfOpenMaxCalls)
}
