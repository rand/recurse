# Circuit Breaker Pattern Design

> Design document for `recurse-rxw`: [SPEC] Circuit Breaker Pattern Design

## Overview

This document specifies the circuit breaker pattern for the RLM system to handle failures gracefully and prevent cascade failures. Circuit breakers protect against repeated failures by temporarily disabling calls to failing services, allowing them to recover while providing fallback behavior.

## Problem Statement

### Current Failure Handling

```go
response, err := c.llmClient.Complete(ctx, prompt)
if err != nil {
    return "", 0, err // Error propagates up
}
```

**Issues**:
- Repeated calls to failing services waste resources
- No backoff between retries
- No fallback behavior during outages
- Failures cascade through the system
- No visibility into service health

### Failure Scenarios

| Scenario | Impact Without Circuit Breaker |
|----------|-------------------------------|
| LLM API timeout | Requests queue up, tokens wasted |
| Rate limiting | 429s accumulate, budget depleted |
| REPL crash | Repeated restart attempts |
| Embedding API down | All semantic searches fail |
| Network partition | Requests hang until timeout |

## Design Goals

1. **Fail fast**: Stop calling failing services immediately
2. **Graceful degradation**: Provide fallback behavior
3. **Self-healing**: Automatically recover when service returns
4. **Visibility**: Expose circuit state for monitoring
5. **Configurability**: Tune thresholds per service

## Core Types

### Circuit State

```go
// internal/resilience/circuit/state.go

// State represents the circuit breaker state.
type State int

const (
    StateClosed   State = iota // Normal operation, calls allowed
    StateOpen                   // Failing, calls blocked
    StateHalfOpen              // Testing if service recovered
)

func (s State) String() string {
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
```

### Circuit Breaker

```go
// internal/resilience/circuit/breaker.go

// Breaker implements the circuit breaker pattern.
type Breaker struct {
    name    string
    config  Config
    state   State
    metrics *Metrics
    mu      sync.RWMutex

    // State tracking
    consecutiveFailures int
    consecutiveSuccesses int
    lastFailure         time.Time
    lastStateChange     time.Time
}

type Config struct {
    // Failure threshold to open circuit
    FailureThreshold int

    // Success threshold to close circuit (in half-open)
    SuccessThreshold int

    // Time to wait before trying half-open
    OpenTimeout time.Duration

    // Maximum requests allowed in half-open state
    HalfOpenMaxRequests int

    // Failure rate threshold (alternative to consecutive failures)
    FailureRateThreshold float64

    // Window for failure rate calculation
    FailureRateWindow time.Duration

    // Fallback function
    Fallback func(error) (any, error)

    // OnStateChange callback
    OnStateChange func(from, to State)
}

func DefaultConfig() Config {
    return Config{
        FailureThreshold:    5,
        SuccessThreshold:    2,
        OpenTimeout:         30 * time.Second,
        HalfOpenMaxRequests: 3,
        FailureRateThreshold: 0.5,
        FailureRateWindow:   time.Minute,
    }
}

func NewBreaker(name string, config Config) *Breaker {
    return &Breaker{
        name:            name,
        config:          config,
        state:           StateClosed,
        metrics:         NewMetrics(name),
        lastStateChange: time.Now(),
    }
}
```

### Execute Pattern

```go
// internal/resilience/circuit/execute.go

// Execute runs the function through the circuit breaker.
func (b *Breaker) Execute(fn func() (any, error)) (any, error) {
    // Check if call is allowed
    if !b.allowRequest() {
        b.metrics.RecordRejection()
        if b.config.Fallback != nil {
            return b.config.Fallback(ErrCircuitOpen)
        }
        return nil, ErrCircuitOpen
    }

    // Execute the function
    start := time.Now()
    result, err := fn()
    duration := time.Since(start)

    // Record outcome
    b.recordResult(err, duration)

    if err != nil && b.config.Fallback != nil {
        return b.config.Fallback(err)
    }

    return result, err
}

func (b *Breaker) allowRequest() bool {
    b.mu.RLock()
    state := b.state
    b.mu.RUnlock()

    switch state {
    case StateClosed:
        return true

    case StateOpen:
        // Check if timeout has passed
        b.mu.Lock()
        defer b.mu.Unlock()

        if time.Since(b.lastFailure) > b.config.OpenTimeout {
            b.transitionTo(StateHalfOpen)
            return true
        }
        return false

    case StateHalfOpen:
        b.mu.Lock()
        defer b.mu.Unlock()

        // Allow limited requests in half-open
        if b.metrics.HalfOpenRequests() < b.config.HalfOpenMaxRequests {
            return true
        }
        return false

    default:
        return false
    }
}

func (b *Breaker) recordResult(err error, duration time.Duration) {
    b.mu.Lock()
    defer b.mu.Unlock()

    b.metrics.RecordCall(err, duration)

    if err != nil {
        b.recordFailure()
    } else {
        b.recordSuccess()
    }
}

func (b *Breaker) recordFailure() {
    b.consecutiveFailures++
    b.consecutiveSuccesses = 0
    b.lastFailure = time.Now()

    switch b.state {
    case StateClosed:
        if b.consecutiveFailures >= b.config.FailureThreshold {
            b.transitionTo(StateOpen)
        } else if b.metrics.FailureRate(b.config.FailureRateWindow) >= b.config.FailureRateThreshold {
            b.transitionTo(StateOpen)
        }

    case StateHalfOpen:
        // Any failure in half-open returns to open
        b.transitionTo(StateOpen)
    }
}

func (b *Breaker) recordSuccess() {
    b.consecutiveSuccesses++
    b.consecutiveFailures = 0

    switch b.state {
    case StateHalfOpen:
        if b.consecutiveSuccesses >= b.config.SuccessThreshold {
            b.transitionTo(StateClosed)
        }
    }
}

func (b *Breaker) transitionTo(newState State) {
    oldState := b.state
    b.state = newState
    b.lastStateChange = time.Now()
    b.metrics.RecordStateChange(oldState, newState)

    if b.config.OnStateChange != nil {
        go b.config.OnStateChange(oldState, newState)
    }
}
```

### Errors

```go
// internal/resilience/circuit/errors.go

var (
    ErrCircuitOpen     = errors.New("circuit breaker is open")
    ErrTooManyRequests = errors.New("too many requests in half-open state")
)

// IsCircuitError checks if error is circuit-related.
func IsCircuitError(err error) bool {
    return errors.Is(err, ErrCircuitOpen) || errors.Is(err, ErrTooManyRequests)
}
```

## Metrics

### Circuit Metrics

```go
// internal/resilience/circuit/metrics.go

type Metrics struct {
    name string

    // Counters
    totalCalls       int64
    successCalls     int64
    failedCalls      int64
    rejectedCalls    int64
    halfOpenRequests int64

    // State tracking
    stateChanges []StateChange

    // Sliding window for failure rate
    callHistory []CallRecord
    historyMu   sync.Mutex
}

type CallRecord struct {
    Timestamp time.Time
    Success   bool
    Duration  time.Duration
}

type StateChange struct {
    Timestamp time.Time
    From      State
    To        State
}

func (m *Metrics) RecordCall(err error, duration time.Duration) {
    atomic.AddInt64(&m.totalCalls, 1)

    if err != nil {
        atomic.AddInt64(&m.failedCalls, 1)
    } else {
        atomic.AddInt64(&m.successCalls, 1)
    }

    m.historyMu.Lock()
    m.callHistory = append(m.callHistory, CallRecord{
        Timestamp: time.Now(),
        Success:   err == nil,
        Duration:  duration,
    })
    // Trim old entries
    m.pruneHistory(5 * time.Minute)
    m.historyMu.Unlock()
}

func (m *Metrics) FailureRate(window time.Duration) float64 {
    m.historyMu.Lock()
    defer m.historyMu.Unlock()

    cutoff := time.Now().Add(-window)
    var total, failures int

    for _, record := range m.callHistory {
        if record.Timestamp.After(cutoff) {
            total++
            if !record.Success {
                failures++
            }
        }
    }

    if total == 0 {
        return 0
    }

    return float64(failures) / float64(total)
}
```

### Prometheus Integration

```go
// internal/resilience/circuit/prometheus.go

var (
    circuitState = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "circuit_breaker_state",
            Help: "Current state of circuit breaker (0=closed, 1=open, 2=half-open)",
        },
        []string{"name"},
    )

    circuitCalls = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "circuit_breaker_calls_total",
            Help: "Total circuit breaker calls",
        },
        []string{"name", "result"}, // result: success, failure, rejected
    )

    circuitStateChanges = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "circuit_breaker_state_changes_total",
            Help: "Circuit breaker state transitions",
        },
        []string{"name", "from", "to"},
    )

    circuitCallDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "circuit_breaker_call_duration_seconds",
            Help:    "Duration of calls through circuit breaker",
            Buckets: prometheus.ExponentialBuckets(0.01, 2, 10),
        },
        []string{"name", "result"},
    )
)

func (m *Metrics) RegisterPrometheus() {
    prometheus.MustRegister(circuitState, circuitCalls, circuitStateChanges, circuitCallDuration)
}
```

## Circuit Breaker Registry

### Registry Pattern

```go
// internal/resilience/circuit/registry.go

// Registry manages multiple circuit breakers.
type Registry struct {
    breakers map[string]*Breaker
    mu       sync.RWMutex
    defaults Config
}

func NewRegistry(defaults Config) *Registry {
    return &Registry{
        breakers: make(map[string]*Breaker),
        defaults: defaults,
    }
}

func (r *Registry) Get(name string) *Breaker {
    r.mu.RLock()
    breaker, ok := r.breakers[name]
    r.mu.RUnlock()

    if ok {
        return breaker
    }

    r.mu.Lock()
    defer r.mu.Unlock()

    // Double-check after acquiring write lock
    if breaker, ok := r.breakers[name]; ok {
        return breaker
    }

    breaker = NewBreaker(name, r.defaults)
    r.breakers[name] = breaker
    return breaker
}

func (r *Registry) GetWithConfig(name string, config Config) *Breaker {
    r.mu.Lock()
    defer r.mu.Unlock()

    if breaker, ok := r.breakers[name]; ok {
        return breaker
    }

    breaker := NewBreaker(name, config)
    r.breakers[name] = breaker
    return breaker
}

// HealthCheck returns the state of all breakers.
func (r *Registry) HealthCheck() map[string]State {
    r.mu.RLock()
    defer r.mu.RUnlock()

    states := make(map[string]State)
    for name, breaker := range r.breakers {
        states[name] = breaker.State()
    }
    return states
}
```

## Fallback Strategies

### Fallback Types

```go
// internal/resilience/circuit/fallback.go

// FallbackFunc is a function that provides fallback behavior.
type FallbackFunc func(err error) (any, error)

// StaticFallback returns a fixed value.
func StaticFallback(value any) FallbackFunc {
    return func(err error) (any, error) {
        return value, nil
    }
}

// ErrorFallback wraps the error with context.
func ErrorFallback(message string) FallbackFunc {
    return func(err error) (any, error) {
        return nil, fmt.Errorf("%s: %w", message, err)
    }
}

// CacheFallback returns cached value if available.
func CacheFallback(cache Cache) FallbackFunc {
    return func(err error) (any, error) {
        value, ok := cache.GetLast()
        if !ok {
            return nil, fmt.Errorf("no cached fallback: %w", err)
        }
        return value, nil
    }
}

// DegradedFallback runs a simpler version of the operation.
func DegradedFallback(degradedFn func() (any, error)) FallbackFunc {
    return func(err error) (any, error) {
        return degradedFn()
    }
}

// ChainedFallback tries multiple fallbacks in order.
func ChainedFallback(fallbacks ...FallbackFunc) FallbackFunc {
    return func(err error) (any, error) {
        var lastErr error = err
        for _, fb := range fallbacks {
            result, fbErr := fb(lastErr)
            if fbErr == nil {
                return result, nil
            }
            lastErr = fbErr
        }
        return nil, lastErr
    }
}
```

## Integration Points

### LLM Client Integration

```go
// internal/rlm/client.go

type ResilientLLMClient struct {
    inner    LLMClient
    breakers *circuit.Registry
}

func NewResilientLLMClient(inner LLMClient) *ResilientLLMClient {
    registry := circuit.NewRegistry(circuit.Config{
        FailureThreshold: 3,
        SuccessThreshold: 2,
        OpenTimeout:      30 * time.Second,
        Fallback:         circuit.ErrorFallback("LLM service unavailable"),
    })

    return &ResilientLLMClient{
        inner:    inner,
        breakers: registry,
    }
}

func (c *ResilientLLMClient) Complete(
    ctx context.Context,
    params anthropic.MessageParams,
) (string, *Usage, error) {
    breaker := c.breakers.Get("llm-complete")

    result, err := breaker.Execute(func() (any, error) {
        return c.inner.Complete(ctx, params)
    })

    if err != nil {
        return "", nil, err
    }

    resp := result.(*CompletionResponse)
    return resp.Content, resp.Usage, nil
}
```

### REPL Integration

```go
// internal/rlm/repl_resilient.go

type ResilientREPL struct {
    inner   *repl.Manager
    breaker *circuit.Breaker
}

func NewResilientREPL(inner *repl.Manager) *ResilientREPL {
    return &ResilientREPL{
        inner: inner,
        breaker: circuit.NewBreaker("repl", circuit.Config{
            FailureThreshold: 3,
            SuccessThreshold: 1,
            OpenTimeout:      10 * time.Second,
            Fallback: circuit.DegradedFallback(func() (any, error) {
                return &repl.Result{
                    Output: "REPL temporarily unavailable, falling back to direct response",
                    Error:  "circuit open",
                }, nil
            }),
        }),
    }
}

func (r *ResilientREPL) Execute(ctx context.Context, code string) (*repl.Result, error) {
    result, err := r.breaker.Execute(func() (any, error) {
        return r.inner.Execute(ctx, code)
    })

    if err != nil {
        return nil, err
    }

    return result.(*repl.Result), nil
}
```

### Embedding Integration

```go
// internal/memory/embeddings/resilient.go

type ResilientProvider struct {
    primary  Provider
    fallback Provider // Local/cached provider
    breaker  *circuit.Breaker
}

func NewResilientProvider(primary, fallback Provider) *ResilientProvider {
    return &ResilientProvider{
        primary:  primary,
        fallback: fallback,
        breaker: circuit.NewBreaker("embeddings", circuit.Config{
            FailureThreshold: 5,
            OpenTimeout:      time.Minute,
            Fallback: func(err error) (any, error) {
                // Will use fallback provider
                return nil, err
            },
        }),
    }
}

func (p *ResilientProvider) Embed(ctx context.Context, texts []string) ([]Vector, error) {
    result, err := p.breaker.Execute(func() (any, error) {
        return p.primary.Embed(ctx, texts)
    })

    if err != nil {
        // Try fallback provider
        if p.fallback != nil {
            return p.fallback.Embed(ctx, texts)
        }
        return nil, err
    }

    return result.([]Vector), nil
}
```

## Health Endpoint

### HTTP Health Check

```go
// internal/api/health.go

type HealthHandler struct {
    circuits *circuit.Registry
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    states := h.circuits.HealthCheck()

    health := struct {
        Status   string            `json:"status"`
        Circuits map[string]string `json:"circuits"`
    }{
        Status:   "healthy",
        Circuits: make(map[string]string),
    }

    for name, state := range states {
        health.Circuits[name] = state.String()
        if state == circuit.StateOpen {
            health.Status = "degraded"
        }
    }

    w.Header().Set("Content-Type", "application/json")
    if health.Status != "healthy" {
        w.WriteHeader(http.StatusServiceUnavailable)
    }
    json.NewEncoder(w).Encode(health)
}
```

## Testing Strategy

### Unit Tests

```go
func TestBreaker_StateTransitions(t *testing.T) {
    config := Config{
        FailureThreshold: 3,
        SuccessThreshold: 2,
        OpenTimeout:      100 * time.Millisecond,
    }

    breaker := NewBreaker("test", config)

    // Should start closed
    assert.Equal(t, StateClosed, breaker.State())

    // Fail 3 times to open
    for i := 0; i < 3; i++ {
        _, _ = breaker.Execute(func() (any, error) {
            return nil, errors.New("fail")
        })
    }
    assert.Equal(t, StateOpen, breaker.State())

    // Calls should be rejected
    _, err := breaker.Execute(func() (any, error) {
        return "success", nil
    })
    assert.Equal(t, ErrCircuitOpen, err)

    // Wait for timeout
    time.Sleep(150 * time.Millisecond)

    // Should transition to half-open on next call
    _, err = breaker.Execute(func() (any, error) {
        return "success", nil
    })
    assert.NoError(t, err)
    assert.Equal(t, StateHalfOpen, breaker.State())

    // One more success should close
    _, err = breaker.Execute(func() (any, error) {
        return "success", nil
    })
    assert.NoError(t, err)
    assert.Equal(t, StateClosed, breaker.State())
}

func TestBreaker_FailureRate(t *testing.T) {
    config := Config{
        FailureThreshold:     100, // High threshold
        FailureRateThreshold: 0.5,
        FailureRateWindow:    time.Second,
    }

    breaker := NewBreaker("test", config)

    // 3 failures, 2 successes = 60% failure rate
    for i := 0; i < 3; i++ {
        breaker.Execute(func() (any, error) { return nil, errors.New("fail") })
    }
    for i := 0; i < 2; i++ {
        breaker.Execute(func() (any, error) { return "ok", nil })
    }

    // Should open due to failure rate
    assert.Equal(t, StateOpen, breaker.State())
}

func TestBreaker_Fallback(t *testing.T) {
    config := Config{
        FailureThreshold: 1,
        Fallback: func(err error) (any, error) {
            return "fallback-value", nil
        },
    }

    breaker := NewBreaker("test", config)

    // First failure opens circuit
    breaker.Execute(func() (any, error) { return nil, errors.New("fail") })

    // Next call should use fallback
    result, err := breaker.Execute(func() (any, error) {
        return "should-not-reach", nil
    })

    assert.NoError(t, err)
    assert.Equal(t, "fallback-value", result)
}
```

### Integration Tests

```go
func TestResilientLLMClient_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    // Mock client that fails intermittently
    mockClient := &MockLLMClient{
        failCount: 3,
    }

    client := NewResilientLLMClient(mockClient)

    // First 3 calls should fail and open circuit
    for i := 0; i < 3; i++ {
        _, _, err := client.Complete(context.Background(), anthropic.MessageParams{})
        assert.Error(t, err)
    }

    // Circuit should be open
    _, _, err := client.Complete(context.Background(), anthropic.MessageParams{})
    assert.True(t, circuit.IsCircuitError(err))

    // Wait for recovery
    time.Sleep(35 * time.Second)

    // Should work now
    _, _, err = client.Complete(context.Background(), anthropic.MessageParams{})
    assert.NoError(t, err)
}
```

## Configuration

### Per-Service Configuration

```go
// internal/resilience/config.go

type CircuitConfig struct {
    LLM struct {
        FailureThreshold int           `yaml:"failure_threshold"`
        OpenTimeout      time.Duration `yaml:"open_timeout"`
    } `yaml:"llm"`

    REPL struct {
        FailureThreshold int           `yaml:"failure_threshold"`
        OpenTimeout      time.Duration `yaml:"open_timeout"`
    } `yaml:"repl"`

    Embeddings struct {
        FailureThreshold int           `yaml:"failure_threshold"`
        OpenTimeout      time.Duration `yaml:"open_timeout"`
        FallbackProvider string        `yaml:"fallback_provider"`
    } `yaml:"embeddings"`
}

func LoadCircuitConfig(path string) (*CircuitConfig, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }

    var config CircuitConfig
    if err := yaml.Unmarshal(data, &config); err != nil {
        return nil, err
    }

    return &config, nil
}
```

## Success Criteria

1. **Fail fast**: Circuit opens within 5 failures
2. **Recovery**: System recovers automatically when service returns
3. **Fallback**: Degraded operation during outages
4. **Visibility**: Circuit state exposed in metrics and health checks
5. **Configurability**: Per-service tuning possible

## Appendix: State Machine

```
        ┌─────────────┐
        │   CLOSED    │◄────────────────────────┐
        │  (normal)   │                         │
        └──────┬──────┘                         │
               │                                │
               │ failure_count >= threshold     │
               │ OR failure_rate >= threshold   │
               ▼                                │
        ┌─────────────┐                         │
        │    OPEN     │                         │
        │  (failing)  │                         │
        └──────┬──────┘                         │
               │                                │
               │ timeout elapsed                │ success_count >= threshold
               ▼                                │
        ┌─────────────┐                         │
        │  HALF-OPEN  │─────────────────────────┘
        │  (testing)  │
        └──────┬──────┘
               │
               │ any failure
               ▼
        ┌─────────────┐
        │    OPEN     │
        │  (failing)  │
        └─────────────┘
```
