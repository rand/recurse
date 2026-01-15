# SPEC-07: Budget Management

> Comprehensive budget tracking, enforcement, and persistence for RLM operations.

## Overview

The budget management system tracks resource usage (tokens, cost, time, recursion), enforces limits, persists history across sessions, and provides per-project budget controls. It integrates with the RLM controller to enforce limits and optimize resource allocation.

## Current State

- Basic `internal/budget/` package with Tracker, Limits, Reporter
- Not fully integrated with RLM service
- No persistence across sessions
- No per-project budget support
- No cost forecasting or optimization

## Requirements

### [SPEC-07.01] Budget Store

Persist budget data using the hypergraph:

```go
type BudgetStore struct {
    graph *hypergraph.Store
}

// Session records
type SessionRecord struct {
    ID            string
    ProjectID     string
    StartTime     time.Time
    EndTime       time.Time
    FinalState    budget.State
    Limits        budget.Limits
    Violations    []budget.Violation
    TaskCount     int
}

// Project budgets
type ProjectBudget struct {
    ProjectID     string
    Limits        budget.Limits
    SpentToDate   float64
    MonthlyLimit  float64
    ResetDay      int  // Day of month to reset
}
```

Features:
- Store session records with final state
- Track per-project spending
- Monthly budget cycles with reset
- Query historical usage

### [SPEC-07.02] RLM Integration

Wire budget tracker into RLM service:

```go
type Service struct {
    // ... existing fields
    budget        *budget.Tracker
    budgetStore   *BudgetStore
}

// Integration points
- Track tokens on each LLM call
- Track recursion depth on sub-calls
- Track REPL executions
- Enforce limits before operations
- Emit events on warnings/violations
```

Features:
- Automatic token tracking from LLM responses
- Recursion depth tracking in decomposition
- Pre-flight cost estimation
- Configurable enforcement (warn vs block)

### [SPEC-07.03] Budget Enforcement

Enforce budget limits with configurable behavior:

```go
type EnforcementConfig struct {
    // Hard limits block operations
    HardLimits    budget.Limits

    // Soft limits emit warnings
    SoftLimits    budget.Limits

    // Actions on violation
    OnWarning     EnforcementAction  // log, notify, degrade
    OnHardLimit   EnforcementAction  // block, degrade, escalate
}

type EnforcementAction string
const (
    ActionLog      EnforcementAction = "log"
    ActionNotify   EnforcementAction = "notify"
    ActionDegrade  EnforcementAction = "degrade"   // Switch to cheaper model
    ActionBlock    EnforcementAction = "block"
    ActionEscalate EnforcementAction = "escalate"  // Ask user
)
```

Features:
- Configurable warning vs hard limits
- Graceful degradation (switch to cheaper models)
- User escalation for budget decisions
- Pre-operation budget check

### [SPEC-07.04] Per-Project Budgets

Support project-specific budget configurations:

```go
type ProjectBudgetManager struct {
    store         *BudgetStore
    defaultLimits budget.Limits
}

func (m *ProjectBudgetManager) GetLimits(projectID string) budget.Limits
func (m *ProjectBudgetManager) SetLimits(projectID string, limits budget.Limits) error
func (m *ProjectBudgetManager) GetSpending(projectID string, period time.Duration) float64
func (m *ProjectBudgetManager) ResetMonthly(projectID string) error
```

Features:
- Per-project limit configuration
- Default limits for new projects
- Monthly spending tracking
- Automatic monthly reset

### [SPEC-07.05] Budget Events

Emit events for budget state changes:

```go
type BudgetEvent struct {
    Type      BudgetEventType
    Timestamp time.Time
    State     budget.State
    Limits    budget.Limits
    Violation *budget.Violation  // If applicable
    ProjectID string
}

type BudgetEventType string
const (
    EventWarningThreshold BudgetEventType = "warning_threshold"
    EventLimitExceeded    BudgetEventType = "limit_exceeded"
    EventSessionStart     BudgetEventType = "session_start"
    EventSessionEnd       BudgetEventType = "session_end"
    EventTaskComplete     BudgetEventType = "task_complete"
)
```

Features:
- Event emission for observability
- Integration with TUI for live updates
- Webhook support for external notifications
- Metrics export

### [SPEC-07.06] Budget Analytics

Provide analytics on budget usage:

```go
type BudgetAnalytics struct {
    store *BudgetStore
}

type UsageSummary struct {
    Period        time.Duration
    TotalCost     float64
    TotalTokens   int64
    SessionCount  int
    TaskCount     int
    TopCategories []CategoryUsage
    DailyBreakdown []DailyUsage
}

func (a *BudgetAnalytics) GetSummary(projectID string, period time.Duration) (*UsageSummary, error)
func (a *BudgetAnalytics) GetTrend(projectID string, periods int) ([]UsageSummary, error)
func (a *BudgetAnalytics) ForecastMonthly(projectID string) (float64, error)
```

Features:
- Usage summaries by period
- Trend analysis
- Monthly forecasting
- Category breakdown

## Implementation Tasks

- [x] Create BudgetStore with hypergraph backend (`internal/budget/store.go`)
- [x] Add SessionRecord and ProjectBudget types (`internal/budget/store.go`)
- [x] Wire budget.Tracker into RLM Service (`internal/rlm/service.go`)
- [x] Add pre-flight cost estimation (`internal/budget/manager.go` CheckBudget)
- [x] Implement enforcement callbacks (`internal/budget/manager.go` SetEventCallback)
- [x] Add graceful degradation support (`internal/budget/limits.go` EnforcementConfig)
- [x] Create ProjectBudgetManager (`internal/budget/manager.go`)
- [x] Add budget event emission (`internal/budget/tracker.go`)
- [x] Implement BudgetAnalytics (`internal/budget/reporter.go`)
- [x] Add tests for all components (`internal/budget/*_test.go`)
- [x] Update TUI budget panel integration (`internal/tui/components/core/status/budget.go`)

## Dependencies

- `internal/budget/` - Existing tracker/limits/reporter
- `internal/memory/hypergraph/` - Storage backend
- `internal/rlm/` - RLM service integration
- `internal/rlm/routing/` - Cost-aware model selection

## Acceptance Criteria

1. Budget state persists across sessions
2. Per-project limits configurable and enforced
3. Warnings at 80% of limits, hard stop at 100%
4. Graceful degradation switches to cheaper models
5. Monthly spending tracked and reset automatically
6. Usage analytics queryable for any time period
7. <1ms overhead for budget checks
