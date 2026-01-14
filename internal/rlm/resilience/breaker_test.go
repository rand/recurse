package resilience

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errTest = errors.New("test error")

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		state    CircuitState
		expected string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{CircuitState(99), "unknown"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.state.String())
	}
}

func TestCircuitBreaker_StartsInClosedState(t *testing.T) {
	cb := NewCircuitBreaker(DefaultBreakerConfig())
	assert.Equal(t, StateClosed, cb.State())
}

func TestCircuitBreaker_ClosedToOpenOnThreshold(t *testing.T) {
	config := BreakerConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  time.Second,
	}
	cb := NewCircuitBreaker(config)

	// First two failures - still closed
	for i := 0; i < 2; i++ {
		err := cb.Call(func() error { return errTest })
		assert.ErrorIs(t, err, errTest)
		assert.Equal(t, StateClosed, cb.State())
	}

	// Third failure - opens the circuit
	err := cb.Call(func() error { return errTest })
	assert.ErrorIs(t, err, errTest)
	assert.Equal(t, StateOpen, cb.State())
}

func TestCircuitBreaker_OpenRejectsCalls(t *testing.T) {
	config := BreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  time.Hour, // Long timeout to stay open
	}
	cb := NewCircuitBreaker(config)

	// Trip the circuit
	_ = cb.Call(func() error { return errTest })
	assert.Equal(t, StateOpen, cb.State())

	// All subsequent calls should be rejected
	for i := 0; i < 5; i++ {
		err := cb.Call(func() error { return nil })
		assert.ErrorIs(t, err, ErrCircuitOpen)
	}

	// Check metrics
	metrics := cb.Metrics()
	assert.Equal(t, int64(5), metrics.TotalRejections)
}

func TestCircuitBreaker_OpenToHalfOpenAfterTimeout(t *testing.T) {
	config := BreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  50 * time.Millisecond,
	}
	cb := NewCircuitBreaker(config)

	// Trip the circuit
	_ = cb.Call(func() error { return errTest })
	assert.Equal(t, StateOpen, cb.State())

	// Wait for recovery timeout
	time.Sleep(60 * time.Millisecond)

	// State check should transition to half-open
	assert.Equal(t, StateHalfOpen, cb.State())
}

func TestCircuitBreaker_HalfOpenToClosedOnSuccess(t *testing.T) {
	config := BreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  10 * time.Millisecond,
		SuccessThreshold: 1,
	}
	cb := NewCircuitBreaker(config)

	// Trip the circuit
	_ = cb.Call(func() error { return errTest })

	// Wait for recovery
	time.Sleep(20 * time.Millisecond)

	// Successful call should close the circuit
	err := cb.Call(func() error { return nil })
	assert.NoError(t, err)
	assert.Equal(t, StateClosed, cb.State())
}

func TestCircuitBreaker_HalfOpenToOpenOnFailure(t *testing.T) {
	config := BreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  10 * time.Millisecond,
	}
	cb := NewCircuitBreaker(config)

	// Trip the circuit
	_ = cb.Call(func() error { return errTest })

	// Wait for recovery
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, StateHalfOpen, cb.State())

	// Failed call should reopen the circuit
	err := cb.Call(func() error { return errTest })
	assert.ErrorIs(t, err, errTest)
	assert.Equal(t, StateOpen, cb.State())
}

func TestCircuitBreaker_HalfOpenAllowsOnlyOneCall(t *testing.T) {
	config := BreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  10 * time.Millisecond,
	}
	cb := NewCircuitBreaker(config)

	// Trip the circuit
	_ = cb.Call(func() error { return errTest })

	// Wait for recovery
	time.Sleep(20 * time.Millisecond)

	// Start a slow call
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		_ = cb.Call(func() error {
			time.Sleep(50 * time.Millisecond)
			return nil
		})
		close(done)
	}()

	<-started
	time.Sleep(5 * time.Millisecond) // Give time for call to start

	// Second call should be rejected while first is in flight
	err := cb.Call(func() error { return nil })
	assert.ErrorIs(t, err, ErrCircuitOpen)

	<-done
}

func TestCircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	config := BreakerConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  time.Second,
	}
	cb := NewCircuitBreaker(config)

	// Two failures
	_ = cb.Call(func() error { return errTest })
	_ = cb.Call(func() error { return errTest })

	// Success resets count
	_ = cb.Call(func() error { return nil })

	// Two more failures - still closed (count was reset)
	_ = cb.Call(func() error { return errTest })
	_ = cb.Call(func() error { return errTest })
	assert.Equal(t, StateClosed, cb.State())

	// Third failure opens
	_ = cb.Call(func() error { return errTest })
	assert.Equal(t, StateOpen, cb.State())
}

func TestCircuitBreaker_MultipleSuccessThreshold(t *testing.T) {
	config := BreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  10 * time.Millisecond,
		SuccessThreshold: 3,
	}
	cb := NewCircuitBreaker(config)

	// Trip the circuit
	_ = cb.Call(func() error { return errTest })

	// Wait for recovery
	time.Sleep(20 * time.Millisecond)

	// First success - still half-open
	_ = cb.Call(func() error { return nil })
	assert.Equal(t, StateHalfOpen, cb.State())

	// Second success - still half-open
	_ = cb.Call(func() error { return nil })
	assert.Equal(t, StateHalfOpen, cb.State())

	// Third success - closes
	_ = cb.Call(func() error { return nil })
	assert.Equal(t, StateClosed, cb.State())
}

func TestCircuitBreaker_Reset(t *testing.T) {
	config := BreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  time.Hour,
	}
	cb := NewCircuitBreaker(config)

	// Trip the circuit
	_ = cb.Call(func() error { return errTest })
	assert.Equal(t, StateOpen, cb.State())

	// Reset
	cb.Reset()
	assert.Equal(t, StateClosed, cb.State())

	// Should allow calls again
	err := cb.Call(func() error { return nil })
	assert.NoError(t, err)
}

func TestCircuitBreaker_CallWithResult(t *testing.T) {
	cb := NewCircuitBreaker(DefaultBreakerConfig())

	result, err := cb.CallWithResult(func() (any, error) {
		return "success", nil
	})
	assert.NoError(t, err)
	assert.Equal(t, "success", result)

	// With error
	result, err = cb.CallWithResult(func() (any, error) {
		return nil, errTest
	})
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, result)
}

func TestCircuitBreaker_OnStateChange(t *testing.T) {
	var transitions []struct{ from, to CircuitState }
	var mu sync.Mutex

	config := BreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  10 * time.Millisecond,
		OnStateChange: func(from, to CircuitState) {
			mu.Lock()
			transitions = append(transitions, struct{ from, to CircuitState }{from, to})
			mu.Unlock()
		},
	}
	cb := NewCircuitBreaker(config)

	// Trip the circuit
	_ = cb.Call(func() error { return errTest })

	// Wait for callback (runs in goroutine)
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	require.Len(t, transitions, 1)
	assert.Equal(t, StateClosed, transitions[0].from)
	assert.Equal(t, StateOpen, transitions[0].to)
	mu.Unlock()
}

func TestCircuitBreaker_Metrics(t *testing.T) {
	config := BreakerConfig{
		FailureThreshold: 2,
		RecoveryTimeout:  time.Hour,
	}
	cb := NewCircuitBreaker(config)

	// Some successes
	_ = cb.Call(func() error { return nil })
	_ = cb.Call(func() error { return nil })

	// Some failures to trip
	_ = cb.Call(func() error { return errTest })
	_ = cb.Call(func() error { return errTest })

	// Rejections
	_ = cb.Call(func() error { return nil })
	_ = cb.Call(func() error { return nil })

	metrics := cb.Metrics()
	assert.Equal(t, int64(4), metrics.TotalCalls)
	assert.Equal(t, int64(2), metrics.TotalSuccesses)
	assert.Equal(t, int64(2), metrics.TotalFailures)
	assert.Equal(t, int64(2), metrics.TotalRejections)
	assert.Equal(t, StateOpen, metrics.State)
}

func TestCircuitBreaker_ConcurrentCalls(t *testing.T) {
	config := BreakerConfig{
		FailureThreshold: 100, // High threshold to stay closed
		RecoveryTimeout:  time.Second,
	}
	cb := NewCircuitBreaker(config)

	var wg sync.WaitGroup
	numGoroutines := 100
	callsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				_ = cb.Call(func() error { return nil })
			}
		}()
	}

	wg.Wait()

	metrics := cb.Metrics()
	expectedCalls := int64(numGoroutines * callsPerGoroutine)
	assert.Equal(t, expectedCalls, metrics.TotalCalls)
	assert.Equal(t, expectedCalls, metrics.TotalSuccesses)
	assert.Equal(t, int64(0), metrics.TotalFailures)
}

func TestCircuitBreaker_ConcurrentFailures(t *testing.T) {
	config := BreakerConfig{
		FailureThreshold: 5,
		RecoveryTimeout:  time.Hour,
	}
	cb := NewCircuitBreaker(config)

	var wg sync.WaitGroup
	numGoroutines := 20
	var failedCalls int64

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := cb.Call(func() error { return errTest })
			if errors.Is(err, errTest) {
				atomic.AddInt64(&failedCalls, 1)
			}
		}()
	}

	wg.Wait()

	// After threshold, circuit should be open
	assert.Equal(t, StateOpen, cb.State())

	// Some calls succeeded (before threshold), some rejected (after)
	metrics := cb.Metrics()
	assert.GreaterOrEqual(t, failedCalls, int64(config.FailureThreshold))
	assert.Greater(t, metrics.TotalRejections, int64(0))
}

// Property-based tests

func TestCircuitBreaker_FailureCountNeverExceedsThresholdPlusOne(t *testing.T) {
	config := BreakerConfig{
		FailureThreshold: 5,
		RecoveryTimeout:  time.Hour,
	}
	cb := NewCircuitBreaker(config)

	// Send many failures
	for i := 0; i < 100; i++ {
		_ = cb.Call(func() error { return errTest })
	}

	cb.mu.Lock()
	failureCount := cb.failureCount
	cb.mu.Unlock()

	// Failure count should be at threshold (resets not possible in open state)
	assert.LessOrEqual(t, failureCount, config.FailureThreshold)
}

func TestCircuitBreaker_OpenCircuitRejectsAllCalls(t *testing.T) {
	config := BreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  time.Hour, // Long timeout
	}
	cb := NewCircuitBreaker(config)

	// Trip the circuit
	_ = cb.Call(func() error { return errTest })
	require.Equal(t, StateOpen, cb.State())

	// All calls should be rejected
	for i := 0; i < 100; i++ {
		err := cb.Call(func() error {
			t.Fatal("call should not have been made")
			return nil
		})
		assert.ErrorIs(t, err, ErrCircuitOpen)
	}
}

// BreakerRegistry tests

func TestBreakerRegistry_Get(t *testing.T) {
	reg := NewBreakerRegistry(DefaultBreakerConfig())

	cb1 := reg.Get("tier1")
	assert.NotNil(t, cb1)

	// Same tier returns same breaker
	cb2 := reg.Get("tier1")
	assert.Same(t, cb1, cb2)

	// Different tier returns different breaker
	cb3 := reg.Get("tier2")
	assert.NotSame(t, cb1, cb3)
}

func TestBreakerRegistry_GetOrCreate(t *testing.T) {
	reg := NewBreakerRegistry(DefaultBreakerConfig())

	customConfig := BreakerConfig{
		FailureThreshold: 10,
		RecoveryTimeout:  time.Minute,
	}

	cb := reg.GetOrCreate("custom", customConfig)
	assert.NotNil(t, cb)

	// Verify custom config was used
	assert.Equal(t, 10, cb.config.FailureThreshold)
}

func TestBreakerRegistry_All(t *testing.T) {
	reg := NewBreakerRegistry(DefaultBreakerConfig())

	reg.Get("tier1")
	reg.Get("tier2")
	reg.Get("tier3")

	all := reg.All()
	assert.Len(t, all, 3)
	assert.Contains(t, all, "tier1")
	assert.Contains(t, all, "tier2")
	assert.Contains(t, all, "tier3")
}

func TestBreakerRegistry_ResetAll(t *testing.T) {
	reg := NewBreakerRegistry(BreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  time.Hour,
	})

	// Trip all breakers
	for _, tier := range []string{"tier1", "tier2", "tier3"} {
		cb := reg.Get(tier)
		_ = cb.Call(func() error { return errTest })
		assert.Equal(t, StateOpen, cb.State())
	}

	// Reset all
	reg.ResetAll()

	// All should be closed
	for tier, cb := range reg.All() {
		assert.Equal(t, StateClosed, cb.State(), "tier %s should be closed", tier)
	}
}

func TestBreakerRegistry_AggregateMetrics(t *testing.T) {
	reg := NewBreakerRegistry(DefaultBreakerConfig())

	// Make some calls on different tiers
	cb1 := reg.Get("tier1")
	_ = cb1.Call(func() error { return nil })
	_ = cb1.Call(func() error { return nil })

	cb2 := reg.Get("tier2")
	_ = cb2.Call(func() error { return errTest })

	metrics := reg.AggregateMetrics()
	assert.Len(t, metrics, 2)
	assert.Equal(t, int64(2), metrics["tier1"].TotalSuccesses)
	assert.Equal(t, int64(1), metrics["tier2"].TotalFailures)
}

func TestBreakerRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewBreakerRegistry(DefaultBreakerConfig())

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			tier := []string{"tier1", "tier2", "tier3"}[id%3]
			cb := reg.Get(tier)
			_ = cb.Call(func() error { return nil })
		}(i)
	}

	wg.Wait()

	// Should have exactly 3 breakers
	assert.Len(t, reg.All(), 3)
}

func TestDefaultRegistry(t *testing.T) {
	reg := DefaultRegistry()

	// Should have pre-registered tiers
	all := reg.All()
	assert.Contains(t, all, TierHaiku)
	assert.Contains(t, all, TierSonnet)
	assert.Contains(t, all, TierOpus)

	// Verify tier-specific configs
	haiku := reg.Get(TierHaiku)
	assert.Equal(t, 10, haiku.config.FailureThreshold)

	sonnet := reg.Get(TierSonnet)
	assert.Equal(t, 5, sonnet.config.FailureThreshold)

	opus := reg.Get(TierOpus)
	assert.Equal(t, 3, opus.config.FailureThreshold)
	assert.Equal(t, 2, opus.config.SuccessThreshold)
}

func TestCircuitBreaker_RaceCondition(t *testing.T) {
	config := BreakerConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  10 * time.Millisecond,
	}
	cb := NewCircuitBreaker(config)

	var wg sync.WaitGroup

	// Concurrent failures to trip circuit
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cb.Call(func() error { return errTest })
		}()
	}

	// Concurrent state checks
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cb.State()
		}()
	}

	// Concurrent metric reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cb.Metrics()
		}()
	}

	wg.Wait()
	// Test passes if no race detector errors
}
