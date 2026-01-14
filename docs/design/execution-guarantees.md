# Execution Guarantees Design

> Design document for `recurse-kum`: [SPEC] Execution Guarantees Design

## Overview

This document specifies formal execution guarantees for the RLM system, ensuring predictable behavior around termination, resource bounds, and output quality. These guarantees enable users to trust the system's behavior and enable automated orchestration.

## Problem Statement

### Current State

The RLM system lacks formal guarantees about:
- When/if execution will terminate
- Maximum resource consumption
- Minimum output quality
- Error recovery behavior

**Issues**:
- Runaway recursion can consume unlimited resources
- No SLA for response time
- Quality varies unpredictably
- Difficult to reason about system behavior

### Desired Guarantees

| Property | Guarantee |
|----------|-----------|
| Termination | Always terminates within budget |
| Resource bounds | Never exceeds configured limits |
| Quality floor | Minimum acceptable output quality |
| Progress | Measurable progress toward completion |

## Design Goals

1. **Bounded execution**: Guaranteed termination within limits
2. **Resource caps**: Hard limits on tokens, time, calls
3. **Quality gates**: Minimum quality thresholds
4. **Progress tracking**: Observable execution progress
5. **Formal properties**: Provable guarantees where possible

## Core Types

### Execution Contract

```go
// internal/rlm/guarantees/contract.go

// Contract specifies execution guarantees.
type Contract struct {
    // Resource limits
    MaxTokens       int           // Maximum total tokens
    MaxWallTime     time.Duration // Maximum wall-clock time
    MaxLLMCalls     int           // Maximum LLM API calls
    MaxRecursionDepth int         // Maximum recursion depth

    // Quality requirements
    MinConfidence   float64       // Minimum confidence score
    MinCompleteness float64       // Minimum task completeness

    // Behavior specifications
    OnBudgetExhaust BudgetPolicy  // What to do when budget runs out
    OnQualityFail   QualityPolicy // What to do when quality fails
    OnTimeout       TimeoutPolicy // What to do on timeout

    // Checkpointing
    CheckpointInterval time.Duration // How often to checkpoint
}

type BudgetPolicy int

const (
    BudgetPolicyFail      BudgetPolicy = iota // Return error
    BudgetPolicyBestEffort                     // Return best result so far
    BudgetPolicySynthesize                     // Synthesize partial results
)

type QualityPolicy int

const (
    QualityPolicyFail    QualityPolicy = iota // Return error
    QualityPolicyRetry                         // Retry with different approach
    QualityPolicyDegrade                       // Return with warning
)

type TimeoutPolicy int

const (
    TimeoutPolicyCancel TimeoutPolicy = iota // Cancel and return error
    TimeoutPolicyGraceful                     // Allow graceful completion
    TimeoutPolicyCheckpoint                   // Return checkpoint state
)

func DefaultContract() Contract {
    return Contract{
        MaxTokens:          100000,
        MaxWallTime:        5 * time.Minute,
        MaxLLMCalls:        50,
        MaxRecursionDepth:  10,
        MinConfidence:      0.5,
        MinCompleteness:    0.8,
        OnBudgetExhaust:    BudgetPolicySynthesize,
        OnQualityFail:      QualityPolicyDegrade,
        OnTimeout:          TimeoutPolicyGraceful,
        CheckpointInterval: 30 * time.Second,
    }
}
```

### Execution State

```go
// internal/rlm/guarantees/state.go

// State tracks execution progress and resource usage.
type State struct {
    // Resource consumption
    TokensUsed     int
    WallTimeUsed   time.Duration
    LLMCallsUsed   int
    CurrentDepth   int
    MaxDepthSeen   int

    // Progress tracking
    Completeness   float64 // 0.0 to 1.0
    Confidence     float64 // 0.0 to 1.0
    SubtasksTotal  int
    SubtasksDone   int

    // Checkpoints
    LastCheckpoint time.Time
    Checkpoints    []Checkpoint

    // Execution status
    Phase          ExecutionPhase
    Violations     []Violation
}

type ExecutionPhase int

const (
    PhaseInitializing ExecutionPhase = iota
    PhaseDecomposing
    PhaseExecuting
    PhaseSynthesizing
    PhaseCompleted
    PhaseFailed
)

type Checkpoint struct {
    Timestamp    time.Time
    State        *State
    PartialResult string
    Recoverable  bool
}

type Violation struct {
    Timestamp time.Time
    Type      ViolationType
    Message   string
    Severity  ViolationSeverity
}

type ViolationType int

const (
    ViolationBudget ViolationType = iota
    ViolationTime
    ViolationDepth
    ViolationQuality
)

type ViolationSeverity int

const (
    SeverityWarning  ViolationSeverity = iota // Approaching limit
    SeveritySoft                               // Limit exceeded, can continue
    SeverityHard                               // Limit exceeded, must stop
)
```

### Execution Monitor

```go
// internal/rlm/guarantees/monitor.go

// Monitor enforces contract guarantees.
type Monitor struct {
    contract   Contract
    state      *State
    startTime  time.Time
    mu         sync.RWMutex

    // Callbacks
    onViolation func(Violation)
    onCheckpoint func(Checkpoint)
}

func NewMonitor(contract Contract) *Monitor {
    return &Monitor{
        contract:  contract,
        state:     &State{Phase: PhaseInitializing},
        startTime: time.Now(),
    }
}

// Check verifies all guarantees and returns any violations.
func (m *Monitor) Check() []Violation {
    m.mu.RLock()
    defer m.mu.RUnlock()

    var violations []Violation

    // Check token budget
    if m.state.TokensUsed >= m.contract.MaxTokens {
        violations = append(violations, Violation{
            Timestamp: time.Now(),
            Type:      ViolationBudget,
            Message:   fmt.Sprintf("token budget exhausted: %d/%d", m.state.TokensUsed, m.contract.MaxTokens),
            Severity:  SeverityHard,
        })
    } else if float64(m.state.TokensUsed) >= float64(m.contract.MaxTokens)*0.9 {
        violations = append(violations, Violation{
            Timestamp: time.Now(),
            Type:      ViolationBudget,
            Message:   fmt.Sprintf("token budget at 90%%: %d/%d", m.state.TokensUsed, m.contract.MaxTokens),
            Severity:  SeverityWarning,
        })
    }

    // Check wall time
    elapsed := time.Since(m.startTime)
    if elapsed >= m.contract.MaxWallTime {
        violations = append(violations, Violation{
            Timestamp: time.Now(),
            Type:      ViolationTime,
            Message:   fmt.Sprintf("wall time exceeded: %v/%v", elapsed, m.contract.MaxWallTime),
            Severity:  SeverityHard,
        })
    }

    // Check recursion depth
    if m.state.CurrentDepth >= m.contract.MaxRecursionDepth {
        violations = append(violations, Violation{
            Timestamp: time.Now(),
            Type:      ViolationDepth,
            Message:   fmt.Sprintf("max recursion depth: %d/%d", m.state.CurrentDepth, m.contract.MaxRecursionDepth),
            Severity:  SeverityHard,
        })
    }

    // Check LLM calls
    if m.state.LLMCallsUsed >= m.contract.MaxLLMCalls {
        violations = append(violations, Violation{
            Timestamp: time.Now(),
            Type:      ViolationBudget,
            Message:   fmt.Sprintf("LLM call limit: %d/%d", m.state.LLMCallsUsed, m.contract.MaxLLMCalls),
            Severity:  SeverityHard,
        })
    }

    return violations
}

// MustContinue returns false if execution must stop.
func (m *Monitor) MustContinue() bool {
    violations := m.Check()
    for _, v := range violations {
        if v.Severity == SeverityHard {
            return false
        }
    }
    return true
}

// RecordTokens updates token usage.
func (m *Monitor) RecordTokens(tokens int) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.state.TokensUsed += tokens
}

// RecordLLMCall updates LLM call count.
func (m *Monitor) RecordLLMCall() {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.state.LLMCallsUsed++
}

// EnterRecursion increments depth.
func (m *Monitor) EnterRecursion() error {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.state.CurrentDepth++
    if m.state.CurrentDepth > m.state.MaxDepthSeen {
        m.state.MaxDepthSeen = m.state.CurrentDepth
    }

    if m.state.CurrentDepth > m.contract.MaxRecursionDepth {
        return ErrMaxDepthExceeded
    }

    return nil
}

// ExitRecursion decrements depth.
func (m *Monitor) ExitRecursion() {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.state.CurrentDepth--
}

// Checkpoint saves current state.
func (m *Monitor) Checkpoint(partialResult string) {
    m.mu.Lock()
    defer m.mu.Unlock()

    checkpoint := Checkpoint{
        Timestamp:     time.Now(),
        State:         m.state.Clone(),
        PartialResult: partialResult,
        Recoverable:   true,
    }

    m.state.Checkpoints = append(m.state.Checkpoints, checkpoint)
    m.state.LastCheckpoint = checkpoint.Timestamp

    if m.onCheckpoint != nil {
        go m.onCheckpoint(checkpoint)
    }
}
```

## Progress Tracking

### Progress Estimator

```go
// internal/rlm/guarantees/progress.go

// Progress tracks task completion.
type Progress struct {
    monitor *Monitor
}

// UpdateCompleteness estimates task completeness.
func (p *Progress) UpdateCompleteness(subtasksDone, subtasksTotal int) {
    p.monitor.mu.Lock()
    defer p.monitor.mu.Unlock()

    p.monitor.state.SubtasksDone = subtasksDone
    p.monitor.state.SubtasksTotal = subtasksTotal

    if subtasksTotal > 0 {
        p.monitor.state.Completeness = float64(subtasksDone) / float64(subtasksTotal)
    }
}

// UpdateConfidence updates the confidence estimate.
func (p *Progress) UpdateConfidence(confidence float64) {
    p.monitor.mu.Lock()
    defer p.monitor.mu.Unlock()
    p.monitor.state.Confidence = confidence
}

// EstimatedCompletion estimates when execution will complete.
func (p *Progress) EstimatedCompletion() time.Duration {
    p.monitor.mu.RLock()
    defer p.monitor.mu.RUnlock()

    elapsed := time.Since(p.monitor.startTime)
    completeness := p.monitor.state.Completeness

    if completeness <= 0 {
        return p.monitor.contract.MaxWallTime // Unknown, return max
    }

    // Linear extrapolation
    estimated := time.Duration(float64(elapsed) / completeness)

    // Cap at max wall time
    remaining := p.monitor.contract.MaxWallTime - elapsed
    if estimated > remaining {
        return remaining
    }

    return estimated - elapsed
}

// RemainingBudget returns remaining resources.
func (p *Progress) RemainingBudget() Budget {
    p.monitor.mu.RLock()
    defer p.monitor.mu.RUnlock()

    return Budget{
        Tokens:   p.monitor.contract.MaxTokens - p.monitor.state.TokensUsed,
        WallTime: p.monitor.contract.MaxWallTime - time.Since(p.monitor.startTime),
        LLMCalls: p.monitor.contract.MaxLLMCalls - p.monitor.state.LLMCallsUsed,
        Depth:    p.monitor.contract.MaxRecursionDepth - p.monitor.state.CurrentDepth,
    }
}

type Budget struct {
    Tokens   int
    WallTime time.Duration
    LLMCalls int
    Depth    int
}
```

## Quality Gates

### Quality Checker

```go
// internal/rlm/guarantees/quality.go

// QualityChecker verifies output quality.
type QualityChecker struct {
    contract Contract
    scorer   ConfidenceScorer
}

type QualityResult struct {
    Passed      bool
    Confidence  float64
    Completeness float64
    Issues      []QualityIssue
}

type QualityIssue struct {
    Type        QualityIssueType
    Description string
    Severity    float64
}

type QualityIssueType int

const (
    QualityIssueLowConfidence QualityIssueType = iota
    QualityIssueIncomplete
    QualityIssueInconsistent
    QualityIssueMissingInfo
)

func (c *QualityChecker) Check(ctx context.Context, result string, state *State) (*QualityResult, error) {
    qr := &QualityResult{
        Confidence:   state.Confidence,
        Completeness: state.Completeness,
    }

    // Check confidence threshold
    if state.Confidence < c.contract.MinConfidence {
        qr.Issues = append(qr.Issues, QualityIssue{
            Type:        QualityIssueLowConfidence,
            Description: fmt.Sprintf("confidence %.2f below threshold %.2f", state.Confidence, c.contract.MinConfidence),
            Severity:    c.contract.MinConfidence - state.Confidence,
        })
    }

    // Check completeness threshold
    if state.Completeness < c.contract.MinCompleteness {
        qr.Issues = append(qr.Issues, QualityIssue{
            Type:        QualityIssueIncomplete,
            Description: fmt.Sprintf("completeness %.2f below threshold %.2f", state.Completeness, c.contract.MinCompleteness),
            Severity:    c.contract.MinCompleteness - state.Completeness,
        })
    }

    qr.Passed = len(qr.Issues) == 0

    return qr, nil
}
```

## Guaranteed Executor

### Executor

```go
// internal/rlm/guarantees/executor.go

// Executor wraps the RLM controller with guarantees.
type Executor struct {
    controller *Controller
    contract   Contract
}

func NewExecutor(controller *Controller, contract Contract) *Executor {
    return &Executor{
        controller: controller,
        contract:   contract,
    }
}

// Execute runs with guarantee enforcement.
func (e *Executor) Execute(ctx context.Context, query string) (*ExecutionResult, error) {
    monitor := NewMonitor(e.contract)
    progress := &Progress{monitor: monitor}
    qualityChecker := &QualityChecker{contract: e.contract}

    // Create context with timeout
    ctx, cancel := context.WithTimeout(ctx, e.contract.MaxWallTime)
    defer cancel()

    // Start checkpoint ticker
    checkpointTicker := time.NewTicker(e.contract.CheckpointInterval)
    defer checkpointTicker.Stop()

    // Result channel
    resultCh := make(chan *ExecutionResult, 1)
    errCh := make(chan error, 1)

    go func() {
        result, err := e.executeWithMonitor(ctx, query, monitor, progress)
        if err != nil {
            errCh <- err
        } else {
            resultCh <- result
        }
    }()

    // Monitor loop
    var lastCheckpointResult string
    for {
        select {
        case result := <-resultCh:
            // Check quality
            qr, _ := qualityChecker.Check(ctx, result.Content, monitor.State())
            if !qr.Passed {
                result = e.handleQualityFailure(ctx, result, qr, monitor)
            }
            return result, nil

        case err := <-errCh:
            return nil, err

        case <-checkpointTicker.C:
            // Auto-checkpoint
            if lastCheckpointResult != "" {
                monitor.Checkpoint(lastCheckpointResult)
            }

        case <-ctx.Done():
            return e.handleTimeout(monitor)
        }
    }
}

func (e *Executor) executeWithMonitor(
    ctx context.Context,
    query string,
    monitor *Monitor,
    progress *Progress,
) (*ExecutionResult, error) {
    // Wrap controller methods to track resources
    wrappedClient := &MonitoredLLMClient{
        inner:   e.controller.llmClient,
        monitor: monitor,
    }

    // Execute with monitoring
    result, tokens, err := e.controller.ExecuteWithClient(ctx, query, wrappedClient)
    if err != nil {
        return nil, err
    }

    return &ExecutionResult{
        Content:      result,
        TokensUsed:   tokens,
        State:        monitor.State(),
        Contract:     e.contract,
    }, nil
}

func (e *Executor) handleQualityFailure(
    ctx context.Context,
    result *ExecutionResult,
    qr *QualityResult,
    monitor *Monitor,
) *ExecutionResult {
    switch e.contract.OnQualityFail {
    case QualityPolicyFail:
        result.Error = ErrQualityBelowThreshold
        return result

    case QualityPolicyRetry:
        // Could implement retry logic here
        result.Warnings = append(result.Warnings, "Quality below threshold, retry not implemented")
        return result

    case QualityPolicyDegrade:
        result.Warnings = append(result.Warnings,
            fmt.Sprintf("Quality below threshold: confidence=%.2f, completeness=%.2f",
                qr.Confidence, qr.Completeness))
        return result

    default:
        return result
    }
}

func (e *Executor) handleTimeout(monitor *Monitor) (*ExecutionResult, error) {
    switch e.contract.OnTimeout {
    case TimeoutPolicyCancel:
        return nil, ErrTimeout

    case TimeoutPolicyGraceful:
        // Return last checkpoint if available
        checkpoints := monitor.State().Checkpoints
        if len(checkpoints) > 0 {
            last := checkpoints[len(checkpoints)-1]
            return &ExecutionResult{
                Content:  last.PartialResult,
                State:    last.State,
                Partial:  true,
                Warnings: []string{"Execution timed out, returning partial result"},
            }, nil
        }
        return nil, ErrTimeout

    case TimeoutPolicyCheckpoint:
        return &ExecutionResult{
            Content:  "",
            State:    monitor.State(),
            Partial:  true,
            Warnings: []string{"Execution timed out, returning checkpoint state"},
        }, nil

    default:
        return nil, ErrTimeout
    }
}
```

### Monitored Client

```go
// internal/rlm/guarantees/monitored_client.go

type MonitoredLLMClient struct {
    inner   LLMClient
    monitor *Monitor
}

func (c *MonitoredLLMClient) Complete(ctx context.Context, params anthropic.MessageParams) (string, *Usage, error) {
    // Check if we can continue
    if !c.monitor.MustContinue() {
        return "", nil, ErrBudgetExhausted
    }

    // Record the call
    c.monitor.RecordLLMCall()

    // Make the call
    response, usage, err := c.inner.Complete(ctx, params)
    if err != nil {
        return "", nil, err
    }

    // Record token usage
    c.monitor.RecordTokens(usage.InputTokens + usage.OutputTokens)

    return response, usage, nil
}
```

## Errors

```go
// internal/rlm/guarantees/errors.go

var (
    ErrBudgetExhausted       = errors.New("execution budget exhausted")
    ErrTimeout               = errors.New("execution timed out")
    ErrMaxDepthExceeded      = errors.New("maximum recursion depth exceeded")
    ErrQualityBelowThreshold = errors.New("output quality below threshold")
    ErrMaxCallsExceeded      = errors.New("maximum LLM calls exceeded")
)
```

## Observability

### Metrics

```go
var (
    guaranteeViolations = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rlm_guarantee_violations_total",
            Help: "Guarantee violations by type",
        },
        []string{"type", "severity"},
    )

    executionCompleteness = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "rlm_execution_completeness",
            Help:    "Task completeness at end of execution",
            Buckets: prometheus.LinearBuckets(0, 0.1, 11),
        },
    )

    budgetUtilization = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "rlm_budget_utilization",
            Help:    "Resource utilization as fraction of budget",
            Buckets: prometheus.LinearBuckets(0, 0.1, 11),
        },
        []string{"resource"}, // tokens, time, calls
    )
)
```

## Testing Strategy

### Unit Tests

```go
func TestMonitor_Check(t *testing.T) {
    contract := Contract{
        MaxTokens:   1000,
        MaxLLMCalls: 10,
        MaxWallTime: time.Minute,
    }

    monitor := NewMonitor(contract)

    // No violations initially
    violations := monitor.Check()
    assert.Empty(t, violations)

    // Add tokens near limit
    monitor.RecordTokens(950)
    violations = monitor.Check()
    assert.Len(t, violations, 1)
    assert.Equal(t, SeverityWarning, violations[0].Severity)

    // Exceed limit
    monitor.RecordTokens(100)
    violations = monitor.Check()
    assert.Len(t, violations, 1)
    assert.Equal(t, SeverityHard, violations[0].Severity)
}

func TestExecutor_BudgetExhaust(t *testing.T) {
    contract := Contract{
        MaxTokens:       100,
        OnBudgetExhaust: BudgetPolicySynthesize,
    }

    executor := NewExecutor(mockController, contract)
    result, err := executor.Execute(context.Background(), "test query")

    require.NoError(t, err)
    assert.True(t, result.Partial)
    assert.Contains(t, result.Warnings[0], "budget")
}

func TestExecutor_Timeout(t *testing.T) {
    contract := Contract{
        MaxWallTime: 100 * time.Millisecond,
        OnTimeout:   TimeoutPolicyGraceful,
    }

    // Mock controller that takes too long
    slowController := &MockController{delay: time.Second}

    executor := NewExecutor(slowController, contract)
    result, err := executor.Execute(context.Background(), "test query")

    require.NoError(t, err)
    assert.True(t, result.Partial)
}
```

## Success Criteria

1. **Termination**: 100% of executions terminate within configured limits
2. **Predictability**: Resource usage predictable within 10% of estimates
3. **Quality**: Quality gate catches >90% of low-quality outputs
4. **Recovery**: Checkpoint recovery works for >95% of interrupted executions
5. **Observability**: All violations visible in metrics

## Appendix: Formal Properties

### Bounded Termination

**Property**: For any input and contract C, execution terminates in at most C.MaxWallTime with at most C.MaxTokens consumed.

**Enforcement**: Monitor checks before every LLM call. Context cancellation enforces wall time.

### Progress Guarantee

**Property**: If completeness increases monotonically and resources decrease monotonically, execution will either complete or exhaust budget.

**Enforcement**: Progress tracking with checkpoint fallback.

### Quality Floor

**Property**: Output quality is either above threshold or explicitly marked as degraded.

**Enforcement**: Quality checker runs before returning any result.
