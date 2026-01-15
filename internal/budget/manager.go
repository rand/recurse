package budget

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Manager coordinates budget tracking, persistence, and enforcement.
type Manager struct {
	mu sync.RWMutex

	tracker   *Tracker
	store     *Store
	projectID string
	config    ManagerConfig
	logger    *slog.Logger

	// Session tracking
	sessionID    string
	sessionStart time.Time
	taskCount    int
	violations   []Violation

	// Event callbacks
	onEvent func(Event)
}

// ManagerConfig configures the budget manager.
type ManagerConfig struct {
	// ProjectID identifies the project for per-project budgets.
	ProjectID string

	// Limits are the default limits.
	Limits Limits

	// Enforcement configuration.
	Enforcement EnforcementConfig

	// Logger for events.
	Logger *slog.Logger
}

// EnforcementConfig configures budget enforcement behavior.
type EnforcementConfig struct {
	// WarnAt is the percentage (0-1) to emit warnings.
	WarnAt float64

	// BlockAt is the percentage (0-1) to block operations.
	BlockAt float64

	// OnWarning is the action to take on warnings.
	OnWarning EnforcementAction

	// OnBlock is the action to take when blocked.
	OnBlock EnforcementAction

	// DegradeModel specifies which model to degrade to on budget pressure.
	DegradeModel string
}

// EnforcementAction specifies what to do on limit violations.
type EnforcementAction string

const (
	ActionLog      EnforcementAction = "log"
	ActionNotify   EnforcementAction = "notify"
	ActionDegrade  EnforcementAction = "degrade"
	ActionBlock    EnforcementAction = "block"
	ActionEscalate EnforcementAction = "escalate"
)

// DefaultEnforcementConfig returns sensible enforcement defaults.
func DefaultEnforcementConfig() EnforcementConfig {
	return EnforcementConfig{
		WarnAt:    0.80,
		BlockAt:   1.00,
		OnWarning: ActionLog,
		OnBlock:   ActionBlock,
	}
}

// Event represents a budget-related event.
type Event struct {
	Type      EventType     `json:"type"`
	Timestamp time.Time     `json:"timestamp"`
	State     State         `json:"state"`
	Limits    Limits        `json:"limits"`
	Violation *Violation    `json:"violation,omitempty"`
	ProjectID string        `json:"project_id"`
	SessionID string        `json:"session_id"`
	Message   string        `json:"message"`
}

// EventType categorizes budget events.
type EventType string

const (
	EventSessionStart     EventType = "session_start"
	EventSessionEnd       EventType = "session_end"
	EventTaskStart        EventType = "task_start"
	EventTaskEnd          EventType = "task_end"
	EventWarningThreshold EventType = "warning_threshold"
	EventLimitExceeded    EventType = "limit_exceeded"
	EventDegraded         EventType = "degraded"
	EventTokensAdded      EventType = "tokens_added"
)

// NewManager creates a new budget manager.
func NewManager(store *Store, cfg ManagerConfig) *Manager {
	if cfg.Limits.MaxInputTokens == 0 {
		cfg.Limits = DefaultLimits()
	}
	if cfg.Enforcement.WarnAt == 0 {
		cfg.Enforcement = DefaultEnforcementConfig()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	tracker := NewTracker(cfg.Limits)

	m := &Manager{
		tracker:   tracker,
		store:     store,
		projectID: cfg.ProjectID,
		config:    cfg,
		logger:    cfg.Logger,
	}

	// Set up limit callback
	tracker.SetLimitCallback(m.handleViolation)

	return m
}

// StartSession begins a new tracking session.
func (m *Manager) StartSession(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	m.sessionStart = time.Now()
	m.taskCount = 0
	m.violations = nil
	m.tracker.Reset()

	// Load project limits if available
	if m.store != nil && m.projectID != "" {
		pb, err := m.store.GetOrCreateProjectBudget(ctx, m.projectID, m.config.Limits)
		if err != nil {
			m.logger.Warn("failed to load project budget", "error", err)
		} else if pb != nil {
			m.tracker.UpdateLimits(pb.Limits)
		}
	}

	m.emitEvent(Event{
		Type:      EventSessionStart,
		Timestamp: m.sessionStart,
		State:     m.tracker.State(),
		Limits:    m.tracker.Limits(),
		ProjectID: m.projectID,
		SessionID: m.sessionID,
		Message:   "Session started",
	})

	return nil
}

// EndSession completes the current session and persists the record.
func (m *Manager) EndSession(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sessionID == "" {
		return nil // No active session
	}

	state := m.tracker.State()
	endTime := time.Now()

	// Save session record
	if m.store != nil {
		record := &SessionRecord{
			ID:         m.sessionID,
			ProjectID:  m.projectID,
			StartTime:  m.sessionStart,
			EndTime:    endTime,
			FinalState: state,
			Limits:     m.tracker.Limits(),
			Violations: m.violations,
			TaskCount:  m.taskCount,
		}

		if err := m.store.SaveSession(ctx, record); err != nil {
			m.logger.Error("failed to save session", "error", err)
		}

		// Update project spending
		if m.projectID != "" {
			tokens := state.InputTokens + state.OutputTokens
			if err := m.store.UpdateProjectSpending(ctx, m.projectID, state.TotalCost, tokens); err != nil {
				m.logger.Error("failed to update project spending", "error", err)
			}
			if err := m.store.IncrementSessionCount(ctx, m.projectID); err != nil {
				m.logger.Error("failed to increment session count", "error", err)
			}
		}
	}

	m.emitEvent(Event{
		Type:      EventSessionEnd,
		Timestamp: endTime,
		State:     state,
		Limits:    m.tracker.Limits(),
		ProjectID: m.projectID,
		SessionID: m.sessionID,
		Message:   fmt.Sprintf("Session ended: $%.4f, %d tokens", state.TotalCost, state.InputTokens+state.OutputTokens),
	})

	m.sessionID = ""
	return nil
}

// StartTask marks the beginning of a task.
func (m *Manager) StartTask() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.tracker.StartTask()
	m.taskCount++

	m.emitEvent(Event{
		Type:      EventTaskStart,
		Timestamp: time.Now(),
		State:     m.tracker.State(),
		Limits:    m.tracker.Limits(),
		ProjectID: m.projectID,
		SessionID: m.sessionID,
		Message:   fmt.Sprintf("Task %d started", m.taskCount),
	})
}

// EndTask marks the end of a task.
func (m *Manager) EndTask() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.tracker.EndTask()

	m.emitEvent(Event{
		Type:      EventTaskEnd,
		Timestamp: time.Now(),
		State:     m.tracker.State(),
		Limits:    m.tracker.Limits(),
		ProjectID: m.projectID,
		SessionID: m.sessionID,
		Message:   fmt.Sprintf("Task %d ended", m.taskCount),
	})
}

// AddTokens records token usage. Returns error if hard limit exceeded.
func (m *Manager) AddTokens(input, output, cached int64, inputCost, outputCost float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	err := m.tracker.AddTokens(input, output, cached, inputCost, outputCost)

	m.emitEvent(Event{
		Type:      EventTokensAdded,
		Timestamp: time.Now(),
		State:     m.tracker.State(),
		Limits:    m.tracker.Limits(),
		ProjectID: m.projectID,
		SessionID: m.sessionID,
		Message:   fmt.Sprintf("Added %d input, %d output tokens", input, output),
	})

	return err
}

// IncrementSubCall records a sub-call. Returns error if limit exceeded.
func (m *Manager) IncrementSubCall(depth int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tracker.IncrementSubCall(depth)
}

// IncrementREPLExecution records a REPL execution.
func (m *Manager) IncrementREPLExecution() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tracker.IncrementREPLExecution()
}

// AddCompressionSavings records tokens saved by context compression.
func (m *Manager) AddCompressionSavings(tokensSaved int64, ratio float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tracker.AddCompressionSavings(tokensSaved, ratio)
}

// CheckBudget checks if an operation can proceed given estimated cost.
func (m *Manager) CheckBudget(estimatedInputTokens, estimatedOutputTokens int64, inputCost, outputCost float64) *BudgetCheck {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state := m.tracker.State()
	limits := m.tracker.Limits()

	estimatedCost := float64(estimatedInputTokens)*inputCost + float64(estimatedOutputTokens)*outputCost
	projectedCost := state.TotalCost + estimatedCost
	projectedInputTokens := state.InputTokens + estimatedInputTokens
	projectedOutputTokens := state.OutputTokens + estimatedOutputTokens

	check := &BudgetCheck{
		CanProceed:     true,
		EstimatedCost:  estimatedCost,
		ProjectedCost:  projectedCost,
		RemainingCost:  limits.MaxTotalCost - state.TotalCost,
		CostPercent:    projectedCost / limits.MaxTotalCost * 100,
		ShouldDegrade:  false,
	}

	// Check cost limit
	if limits.MaxTotalCost > 0 {
		if projectedCost >= limits.MaxTotalCost {
			check.CanProceed = false
			check.BlockReason = "cost limit would be exceeded"
		} else if projectedCost >= limits.MaxTotalCost*m.config.Enforcement.WarnAt {
			check.ShouldDegrade = true
			check.DegradeReason = "approaching cost limit"
		}
	}

	// Check token limits
	if limits.MaxInputTokens > 0 && projectedInputTokens >= limits.MaxInputTokens {
		check.CanProceed = false
		check.BlockReason = "input token limit would be exceeded"
	}
	if limits.MaxOutputTokens > 0 && projectedOutputTokens >= limits.MaxOutputTokens {
		check.CanProceed = false
		check.BlockReason = "output token limit would be exceeded"
	}

	return check
}

// BudgetCheck contains the result of a pre-flight budget check.
type BudgetCheck struct {
	CanProceed    bool    `json:"can_proceed"`
	BlockReason   string  `json:"block_reason,omitempty"`
	ShouldDegrade bool    `json:"should_degrade"`
	DegradeReason string  `json:"degrade_reason,omitempty"`
	EstimatedCost float64 `json:"estimated_cost"`
	ProjectedCost float64 `json:"projected_cost"`
	RemainingCost float64 `json:"remaining_cost"`
	CostPercent   float64 `json:"cost_percent"`
}

// handleViolation processes a limit violation.
// Note: This is called from within the tracker's locked context, so we must not
// call back into tracker methods that acquire locks (State(), Limits()).
func (m *Manager) handleViolation(v Violation) {
	m.violations = append(m.violations, v)

	eventType := EventWarningThreshold
	if v.Hard {
		eventType = EventLimitExceeded
	}

	// Emit event with violation info only - no tracker calls to avoid deadlock
	m.emitEvent(Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Violation: &v,
		ProjectID: m.projectID,
		SessionID: m.sessionID,
		Message:   v.Message,
	})

	// Log based on severity
	if v.Hard {
		m.logger.Error("budget limit exceeded",
			"metric", v.Metric,
			"current", v.Current,
			"limit", v.Limit)
	} else if v.Warning {
		m.logger.Warn("budget warning",
			"metric", v.Metric,
			"current", v.Current,
			"limit", v.Limit,
			"percent", v.Percent)
	}
}

// emitEvent sends an event to the callback if registered.
func (m *Manager) emitEvent(e Event) {
	if m.onEvent != nil {
		m.onEvent(e)
	}
}

// SetEventCallback sets the callback for budget events.
func (m *Manager) SetEventCallback(cb func(Event)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onEvent = cb
}

// State returns the current budget state.
func (m *Manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tracker.State()
}

// Limits returns the current limits.
func (m *Manager) Limits() Limits {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tracker.Limits()
}

// UpdateLimits updates the current limits.
func (m *Manager) UpdateLimits(limits Limits) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tracker.UpdateLimits(limits)
}

// Usage returns current usage percentages.
func (m *Manager) Usage() Usage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tracker.Usage()
}

// Report generates a budget report.
func (m *Manager) Report() Report {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return NewReport(m.tracker)
}

// SessionID returns the current session ID.
func (m *Manager) SessionID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionID
}

// ProjectID returns the project ID.
func (m *Manager) ProjectID() string {
	return m.projectID
}

// Tracker returns the underlying tracker for direct access.
func (m *Manager) Tracker() *Tracker {
	return m.tracker
}
