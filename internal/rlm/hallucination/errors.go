// Package hallucination provides information-theoretic hallucination detection.
package hallucination

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Error types for hallucination detection
var (
	// ErrBackendTimeout indicates a verification backend timeout.
	ErrBackendTimeout = errors.New("verification backend timeout")

	// ErrBackendError indicates a general backend error.
	ErrBackendError = errors.New("verification backend error")

	// ErrRateLimited indicates rate limiting was encountered.
	ErrRateLimited = errors.New("verification backend rate limited")

	// ErrBackendUnavailable indicates the backend is unavailable.
	ErrBackendUnavailable = errors.New("verification backend unavailable")

	// ErrInvalidInput indicates invalid input to verification.
	ErrInvalidInput = errors.New("invalid verification input")
)

// ErrorHandler provides graceful error handling for hallucination detection.
// [SPEC-08.42] [SPEC-08.43]
type ErrorHandler struct {
	config ErrorHandlerConfig
	logger *slog.Logger

	// Statistics
	mu                sync.Mutex
	timeouts          int64
	errors            int64
	rateLimits        int64
	recoveries        int64
	queuedOperations  int64
}

// ErrorHandlerConfig configures error handling behavior.
type ErrorHandlerConfig struct {
	// DefaultConfidenceOnTimeout is the confidence multiplier on timeout.
	// [SPEC-08.42] Timeout: Store/return with reduced confidence
	DefaultConfidenceOnTimeout float64

	// DefaultConfidenceOnError is the confidence multiplier on error.
	DefaultConfidenceOnError float64

	// MaxRetries is the maximum number of retries before giving up.
	MaxRetries int

	// RetryDelay is the delay between retries.
	RetryDelay time.Duration

	// EnableQueueing enables queueing for rate-limited operations.
	// [SPEC-08.42] Rate limit: Queue for later verification
	EnableQueueing bool

	// QueueSize is the maximum number of queued operations.
	QueueSize int

	// LogErrors enables logging of all errors.
	LogErrors bool
}

// DefaultErrorHandlerConfig returns sensible defaults.
func DefaultErrorHandlerConfig() ErrorHandlerConfig {
	return ErrorHandlerConfig{
		DefaultConfidenceOnTimeout: 0.5,
		DefaultConfidenceOnError:   0.5,
		MaxRetries:                 2,
		RetryDelay:                 100 * time.Millisecond,
		EnableQueueing:             true,
		QueueSize:                  100,
		LogErrors:                  true,
	}
}

// NewErrorHandler creates a new error handler.
func NewErrorHandler(config ErrorHandlerConfig) *ErrorHandler {
	return &ErrorHandler{
		config: config,
		logger: slog.Default(),
	}
}

// SetLogger sets the logger for the error handler.
func (h *ErrorHandler) SetLogger(logger *slog.Logger) {
	h.logger = logger
}

// GracefulResult represents the result of a gracefully handled operation.
type GracefulResult struct {
	// Success indicates whether the operation succeeded.
	Success bool

	// Result is the operation result if successful.
	Result any

	// FallbackUsed indicates whether a fallback was used.
	FallbackUsed bool

	// FallbackReason explains why the fallback was used.
	FallbackReason string

	// AdjustedConfidence is the confidence after adjustment.
	AdjustedConfidence float64

	// OriginalError is the original error if any.
	OriginalError error

	// Queued indicates if the operation was queued for later.
	Queued bool
}

// HandleVerificationError handles verification errors gracefully.
// [SPEC-08.42] [SPEC-08.43]
func (h *ErrorHandler) HandleVerificationError(err error, originalConfidence float64) *GracefulResult {
	if err == nil {
		return &GracefulResult{
			Success:            true,
			AdjustedConfidence: originalConfidence,
		}
	}

	h.mu.Lock()
	h.errors++
	h.mu.Unlock()

	result := &GracefulResult{
		Success:       false,
		FallbackUsed:  true,
		OriginalError: err,
	}

	// Classify error and determine response
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrBackendTimeout):
		h.mu.Lock()
		h.timeouts++
		h.mu.Unlock()

		// [SPEC-08.42] Timeout: Store/return with reduced confidence
		result.AdjustedConfidence = originalConfidence * h.config.DefaultConfidenceOnTimeout
		result.FallbackReason = "verification timeout - using reduced confidence"

		if h.config.LogErrors {
			h.logger.Warn("verification timeout",
				"original_confidence", originalConfidence,
				"adjusted_confidence", result.AdjustedConfidence,
			)
		}

	case errors.Is(err, ErrRateLimited):
		h.mu.Lock()
		h.rateLimits++
		h.mu.Unlock()

		// [SPEC-08.42] Rate limit: Queue for later verification
		result.AdjustedConfidence = originalConfidence * h.config.DefaultConfidenceOnError
		result.FallbackReason = "rate limited - using reduced confidence"
		result.Queued = h.config.EnableQueueing

		if h.config.LogErrors {
			h.logger.Warn("verification rate limited",
				"original_confidence", originalConfidence,
				"queued", result.Queued,
			)
		}

	default:
		// [SPEC-08.42] Error: Log and continue without verification
		result.AdjustedConfidence = originalConfidence * h.config.DefaultConfidenceOnError
		result.FallbackReason = fmt.Sprintf("verification error: %v", err)

		if h.config.LogErrors {
			h.logger.Warn("verification error",
				"error", err,
				"original_confidence", originalConfidence,
				"adjusted_confidence", result.AdjustedConfidence,
			)
		}
	}

	h.mu.Lock()
	h.recoveries++
	h.mu.Unlock()

	return result
}

// WithRetry executes a function with retry logic.
func (h *ErrorHandler) WithRetry(ctx context.Context, fn func(context.Context) (any, error)) *GracefulResult {
	var lastErr error

	for attempt := 0; attempt <= h.config.MaxRetries; attempt++ {
		result, err := fn(ctx)
		if err == nil {
			return &GracefulResult{
				Success: true,
				Result:  result,
			}
		}

		lastErr = err

		// Don't retry on certain errors
		if errors.Is(err, ErrInvalidInput) {
			break
		}

		// Don't retry on context cancellation
		if errors.Is(err, context.Canceled) {
			break
		}

		// Wait before retry if not last attempt
		if attempt < h.config.MaxRetries {
			select {
			case <-ctx.Done():
				lastErr = ctx.Err()
				break
			case <-time.After(h.config.RetryDelay):
				// Continue to next attempt
			}
		}
	}

	return h.HandleVerificationError(lastErr, 0.9) // Default confidence
}

// WrapBackendCall wraps a backend call with error handling.
// [SPEC-08.43] The system MUST NOT crash or block user operations
func (h *ErrorHandler) WrapBackendCall(ctx context.Context, fn func(context.Context) (float64, error), defaultValue float64) (float64, error) {
	result, err := fn(ctx)
	if err == nil {
		return result, nil
	}

	graceful := h.HandleVerificationError(err, defaultValue)
	return graceful.AdjustedConfidence, nil // Never return error - graceful degradation
}

// SafeVerification ensures verification never panics or blocks.
// [SPEC-08.43]
func (h *ErrorHandler) SafeVerification(ctx context.Context, fn func(context.Context) (*VerificationResult, error), claim *Claim) *VerificationResult {
	// Recover from any panics
	defer func() {
		if r := recover(); r != nil {
			h.logger.Error("verification panic recovered",
				"panic", r,
				"claim", truncate(claim.Content, 50),
			)
		}
	}()

	result, err := fn(ctx)
	if err != nil {
		graceful := h.HandleVerificationError(err, claim.Confidence)
		return &VerificationResult{
			Claim:              claim,
			Status:             StatusUnverifiable,
			AdjustedConfidence: graceful.AdjustedConfidence,
			Explanation:        graceful.FallbackReason,
			Error:              err,
		}
	}

	return result
}

// ErrorHandlerStats contains error handling statistics.
type ErrorHandlerStats struct {
	Timeouts         int64   `json:"timeouts"`
	Errors           int64   `json:"errors"`
	RateLimits       int64   `json:"rate_limits"`
	Recoveries       int64   `json:"recoveries"`
	QueuedOperations int64   `json:"queued_operations"`
	RecoveryRate     float64 `json:"recovery_rate"`
}

// Stats returns current statistics.
func (h *ErrorHandler) Stats() ErrorHandlerStats {
	h.mu.Lock()
	defer h.mu.Unlock()

	stats := ErrorHandlerStats{
		Timeouts:         h.timeouts,
		Errors:           h.errors,
		RateLimits:       h.rateLimits,
		Recoveries:       h.recoveries,
		QueuedOperations: h.queuedOperations,
	}

	if h.errors > 0 {
		stats.RecoveryRate = float64(h.recoveries) / float64(h.errors)
	}

	return stats
}

// CircuitBreaker provides circuit breaker functionality for backend calls.
// Prevents cascading failures when backend is unhealthy.
type CircuitBreaker struct {
	mu            sync.Mutex
	failures      int
	successes     int
	lastFailure   time.Time
	state         CircuitState
	config        CircuitBreakerConfig
	logger        *slog.Logger
}

// CircuitState represents the circuit breaker state.
type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"   // Normal operation
	CircuitOpen     CircuitState = "open"     // Blocking calls
	CircuitHalfOpen CircuitState = "half-open" // Testing recovery
)

// CircuitBreakerConfig configures the circuit breaker.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of failures before opening.
	FailureThreshold int

	// SuccessThreshold is the number of successes in half-open to close.
	SuccessThreshold int

	// OpenDuration is how long to stay open before trying half-open.
	OpenDuration time.Duration

	// HalfOpenMaxCalls is the max calls allowed in half-open state.
	HalfOpenMaxCalls int
}

// DefaultCircuitBreakerConfig returns sensible defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		OpenDuration:     30 * time.Second,
		HalfOpenMaxCalls: 3,
	}
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		state:  CircuitClosed,
		config: config,
		logger: slog.Default(),
	}
}

// SetLogger sets the logger for the circuit breaker.
func (cb *CircuitBreaker) SetLogger(logger *slog.Logger) {
	cb.logger = logger
}

// Allow checks if a call should be allowed.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true

	case CircuitOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastFailure) > cb.config.OpenDuration {
			cb.state = CircuitHalfOpen
			cb.successes = 0
			cb.logger.Info("circuit breaker half-open")
			return true
		}
		return false

	case CircuitHalfOpen:
		return cb.successes < cb.config.HalfOpenMaxCalls
	}

	return true
}

// RecordSuccess records a successful call.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0

	if cb.state == CircuitHalfOpen {
		cb.successes++
		if cb.successes >= cb.config.SuccessThreshold {
			cb.state = CircuitClosed
			cb.logger.Info("circuit breaker closed")
		}
	}
}

// RecordFailure records a failed call.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	if cb.state == CircuitClosed && cb.failures >= cb.config.FailureThreshold {
		cb.state = CircuitOpen
		cb.logger.Warn("circuit breaker opened",
			"failures", cb.failures,
			"threshold", cb.config.FailureThreshold,
		)
	} else if cb.state == CircuitHalfOpen {
		cb.state = CircuitOpen
		cb.logger.Warn("circuit breaker re-opened after half-open failure")
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// Reset resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = CircuitClosed
	cb.failures = 0
	cb.successes = 0
}

// ResilientBackend wraps a VerifierBackend with error handling and circuit breaker.
type ResilientBackend struct {
	backend        VerifierBackend
	errorHandler   *ErrorHandler
	circuitBreaker *CircuitBreaker
	logger         *slog.Logger
}

// NewResilientBackend creates a resilient wrapper around a backend.
func NewResilientBackend(backend VerifierBackend, errorConfig ErrorHandlerConfig, circuitConfig CircuitBreakerConfig) *ResilientBackend {
	return &ResilientBackend{
		backend:        backend,
		errorHandler:   NewErrorHandler(errorConfig),
		circuitBreaker: NewCircuitBreaker(circuitConfig),
		logger:         slog.Default(),
	}
}

// SetLogger sets the logger for the resilient backend.
func (rb *ResilientBackend) SetLogger(logger *slog.Logger) {
	rb.logger = logger
	rb.errorHandler.SetLogger(logger)
	rb.circuitBreaker.SetLogger(logger)
}

// Name returns the backend name.
func (rb *ResilientBackend) Name() string {
	return "resilient:" + rb.backend.Name()
}

// EstimateProbability estimates probability with resilience.
// [SPEC-08.42] [SPEC-08.43]
func (rb *ResilientBackend) EstimateProbability(ctx context.Context, claim, evidence string) (float64, error) {
	// Check circuit breaker
	if !rb.circuitBreaker.Allow() {
		rb.logger.Debug("circuit breaker blocking call")
		return 0.5, nil // Return neutral probability when circuit is open
	}

	// Make the call with error handling
	result, err := rb.errorHandler.WrapBackendCall(ctx, func(ctx context.Context) (float64, error) {
		return rb.backend.EstimateProbability(ctx, claim, evidence)
	}, 0.5)

	if err != nil {
		rb.circuitBreaker.RecordFailure()
		return result, nil // Still return result from graceful handling
	}

	rb.circuitBreaker.RecordSuccess()
	return result, nil
}

// BatchEstimate estimates probabilities in batch with resilience.
func (rb *ResilientBackend) BatchEstimate(ctx context.Context, claims []string, context string) ([]float64, error) {
	if !rb.circuitBreaker.Allow() {
		// Return neutral probabilities when circuit is open
		results := make([]float64, len(claims))
		for i := range results {
			results[i] = 0.5
		}
		return results, nil
	}

	results, err := rb.backend.BatchEstimate(ctx, claims, context)
	if err != nil {
		rb.circuitBreaker.RecordFailure()
		// Return neutral probabilities on error
		fallback := make([]float64, len(claims))
		for i := range fallback {
			fallback[i] = 0.5
		}
		return fallback, nil
	}

	rb.circuitBreaker.RecordSuccess()
	return results, nil
}

// Verify interface compliance
var _ VerifierBackend = (*ResilientBackend)(nil)
