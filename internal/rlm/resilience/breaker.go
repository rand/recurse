// Package resilience provides resilience patterns for RLM operations.
package resilience

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// CircuitState represents the current state of a circuit breaker.
type CircuitState int

const (
	// StateClosed allows all calls through (normal operation).
	StateClosed CircuitState = iota

	// StateOpen rejects all calls immediately (circuit tripped).
	StateOpen

	// StateHalfOpen allows a single test call through to probe recovery.
	StateHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Errors returned by circuit breaker operations.
var (
	// ErrCircuitOpen is returned when the circuit is open and rejecting calls.
	ErrCircuitOpen = errors.New("circuit breaker is open")

	// ErrTooManyRequests is returned when half-open circuit already has a test in progress.
	ErrTooManyRequests = errors.New("circuit breaker half-open: test in progress")
)

// BreakerConfig configures a circuit breaker.
type BreakerConfig struct {
	// FailureThreshold is the number of consecutive failures before opening.
	// Default: 5
	FailureThreshold int

	// RecoveryTimeout is how long to wait before attempting recovery.
	// Default: 30 seconds
	RecoveryTimeout time.Duration

	// SuccessThreshold is the number of consecutive successes in half-open
	// state before closing the circuit.
	// Default: 1
	SuccessThreshold int

	// OnStateChange is called when the circuit state changes.
	OnStateChange func(from, to CircuitState)
}

// DefaultBreakerConfig returns the default configuration.
func DefaultBreakerConfig() BreakerConfig {
	return BreakerConfig{
		FailureThreshold: 5,
		RecoveryTimeout:  30 * time.Second,
		SuccessThreshold: 1,
	}
}

// CircuitBreaker implements the circuit breaker pattern for resilience.
// It prevents cascading failures by failing fast when a downstream service is unhealthy.
type CircuitBreaker struct {
	config BreakerConfig

	state            CircuitState
	failureCount     int
	successCount     int
	lastFailureTime  time.Time
	lastStateChange  time.Time
	halfOpenInFlight bool // whether a test call is in progress

	// Metrics
	totalCalls      int64
	totalFailures   int64
	totalSuccesses  int64
	totalRejections int64

	mu sync.Mutex
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration.
func NewCircuitBreaker(config BreakerConfig) *CircuitBreaker {
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = 5
	}
	if config.RecoveryTimeout <= 0 {
		config.RecoveryTimeout = 30 * time.Second
	}
	if config.SuccessThreshold <= 0 {
		config.SuccessThreshold = 1
	}

	return &CircuitBreaker{
		config:          config,
		state:           StateClosed,
		lastStateChange: time.Now(),
	}
}

// Call executes the given function if the circuit allows it.
// Returns ErrCircuitOpen if the circuit is open, or the function's error otherwise.
func (cb *CircuitBreaker) Call(fn func() error) error {
	if !cb.allowRequest() {
		atomic.AddInt64(&cb.totalRejections, 1)
		return ErrCircuitOpen
	}

	atomic.AddInt64(&cb.totalCalls, 1)

	err := fn()

	if err != nil {
		cb.recordFailure()
		return err
	}

	cb.recordSuccess()
	return nil
}

// CallWithResult executes a function that returns a value and error.
func (cb *CircuitBreaker) CallWithResult(fn func() (any, error)) (any, error) {
	if !cb.allowRequest() {
		atomic.AddInt64(&cb.totalRejections, 1)
		return nil, ErrCircuitOpen
	}

	atomic.AddInt64(&cb.totalCalls, 1)

	result, err := fn()

	if err != nil {
		cb.recordFailure()
		return nil, err
	}

	cb.recordSuccess()
	return result, nil
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Check for automatic transition from open to half-open
	if cb.state == StateOpen && time.Since(cb.lastFailureTime) >= cb.config.RecoveryTimeout {
		cb.transitionTo(StateHalfOpen)
	}

	return cb.state
}

// Reset manually resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.transitionTo(StateClosed)
	cb.failureCount = 0
	cb.successCount = 0
	cb.halfOpenInFlight = false
}

// Metrics returns current circuit breaker metrics.
func (cb *CircuitBreaker) Metrics() BreakerMetrics {
	cb.mu.Lock()
	state := cb.state
	failureCount := cb.failureCount
	lastStateChange := cb.lastStateChange
	cb.mu.Unlock()

	return BreakerMetrics{
		State:           state,
		TotalCalls:      atomic.LoadInt64(&cb.totalCalls),
		TotalFailures:   atomic.LoadInt64(&cb.totalFailures),
		TotalSuccesses:  atomic.LoadInt64(&cb.totalSuccesses),
		TotalRejections: atomic.LoadInt64(&cb.totalRejections),
		FailureCount:    failureCount,
		LastStateChange: lastStateChange,
	}
}

// BreakerMetrics contains circuit breaker statistics.
type BreakerMetrics struct {
	State           CircuitState
	TotalCalls      int64
	TotalFailures   int64
	TotalSuccesses  int64
	TotalRejections int64
	FailureCount    int
	LastStateChange time.Time
}

// allowRequest determines whether a request should be allowed through.
func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true

	case StateOpen:
		// Check if recovery timeout has elapsed
		if time.Since(cb.lastFailureTime) >= cb.config.RecoveryTimeout {
			cb.transitionTo(StateHalfOpen)
			cb.halfOpenInFlight = true
			return true
		}
		return false

	case StateHalfOpen:
		// Only allow one request at a time in half-open state
		if cb.halfOpenInFlight {
			return false
		}
		cb.halfOpenInFlight = true
		return true

	default:
		return false
	}
}

// recordSuccess records a successful call.
func (cb *CircuitBreaker) recordSuccess() {
	atomic.AddInt64(&cb.totalSuccesses, 1)

	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		// Reset failure count on success
		cb.failureCount = 0

	case StateHalfOpen:
		cb.successCount++
		cb.halfOpenInFlight = false

		// Check if we've had enough successes to close the circuit
		if cb.successCount >= cb.config.SuccessThreshold {
			cb.transitionTo(StateClosed)
			cb.failureCount = 0
			cb.successCount = 0
		}
	}
}

// recordFailure records a failed call.
func (cb *CircuitBreaker) recordFailure() {
	atomic.AddInt64(&cb.totalFailures, 1)

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		cb.failureCount++

		// Check if we've exceeded the failure threshold
		if cb.failureCount >= cb.config.FailureThreshold {
			cb.transitionTo(StateOpen)
		}

	case StateHalfOpen:
		// Single failure in half-open returns to open
		cb.halfOpenInFlight = false
		cb.successCount = 0
		cb.transitionTo(StateOpen)
	}
}

// transitionTo changes the circuit state. Must be called with lock held.
func (cb *CircuitBreaker) transitionTo(newState CircuitState) {
	if cb.state == newState {
		return
	}

	oldState := cb.state
	cb.state = newState
	cb.lastStateChange = time.Now()

	if cb.config.OnStateChange != nil {
		// Call callback without holding the lock
		go cb.config.OnStateChange(oldState, newState)
	}
}

// BreakerRegistry manages circuit breakers for different model tiers.
type BreakerRegistry struct {
	breakers map[string]*CircuitBreaker
	config   BreakerConfig
	mu       sync.RWMutex
}

// NewBreakerRegistry creates a registry with the given default config.
func NewBreakerRegistry(config BreakerConfig) *BreakerRegistry {
	return &BreakerRegistry{
		breakers: make(map[string]*CircuitBreaker),
		config:   config,
	}
}

// Get returns the circuit breaker for a model tier, creating one if necessary.
func (r *BreakerRegistry) Get(modelTier string) *CircuitBreaker {
	// Try read lock first
	r.mu.RLock()
	if cb, ok := r.breakers[modelTier]; ok {
		r.mu.RUnlock()
		return cb
	}
	r.mu.RUnlock()

	// Need to create - acquire write lock
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if cb, ok := r.breakers[modelTier]; ok {
		return cb
	}

	cb := NewCircuitBreaker(r.config)
	r.breakers[modelTier] = cb
	return cb
}

// GetOrCreate returns existing breaker or creates one with custom config.
func (r *BreakerRegistry) GetOrCreate(modelTier string, config BreakerConfig) *CircuitBreaker {
	r.mu.Lock()
	defer r.mu.Unlock()

	if cb, ok := r.breakers[modelTier]; ok {
		return cb
	}

	cb := NewCircuitBreaker(config)
	r.breakers[modelTier] = cb
	return cb
}

// All returns all registered circuit breakers.
func (r *BreakerRegistry) All() map[string]*CircuitBreaker {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]*CircuitBreaker, len(r.breakers))
	for k, v := range r.breakers {
		result[k] = v
	}
	return result
}

// ResetAll resets all circuit breakers to closed state.
func (r *BreakerRegistry) ResetAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, cb := range r.breakers {
		cb.Reset()
	}
}

// AggregateMetrics returns combined metrics across all breakers.
func (r *BreakerRegistry) AggregateMetrics() map[string]BreakerMetrics {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]BreakerMetrics, len(r.breakers))
	for tier, cb := range r.breakers {
		result[tier] = cb.Metrics()
	}
	return result
}

// Common model tiers for pre-registration.
const (
	TierHaiku  = "haiku"
	TierSonnet = "sonnet"
	TierOpus   = "opus"
)

// DefaultRegistry creates a registry with breakers for common model tiers.
func DefaultRegistry() *BreakerRegistry {
	reg := NewBreakerRegistry(DefaultBreakerConfig())

	// Pre-register common tiers with tier-specific configs
	reg.GetOrCreate(TierHaiku, BreakerConfig{
		FailureThreshold: 10, // More tolerant for cheap calls
		RecoveryTimeout:  15 * time.Second,
		SuccessThreshold: 1,
	})

	reg.GetOrCreate(TierSonnet, BreakerConfig{
		FailureThreshold: 5,
		RecoveryTimeout:  30 * time.Second,
		SuccessThreshold: 1,
	})

	reg.GetOrCreate(TierOpus, BreakerConfig{
		FailureThreshold: 3, // Less tolerant for expensive calls
		RecoveryTimeout:  60 * time.Second,
		SuccessThreshold: 2, // Require 2 successes before trusting
	})

	return reg
}
