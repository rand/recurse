package budget

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rand/recurse/internal/memory/hypergraph"
)

const (
	subtypeSession = "budget_session"
	subtypeProject = "budget_project"
)

// Store persists budget data using the hypergraph.
type Store struct {
	graph *hypergraph.Store
}

// NewStore creates a new budget store.
func NewStore(graph *hypergraph.Store) *Store {
	return &Store{graph: graph}
}

// SessionRecord represents a completed session's budget data.
type SessionRecord struct {
	ID         string      `json:"id"`
	ProjectID  string      `json:"project_id"`
	StartTime  time.Time   `json:"start_time"`
	EndTime    time.Time   `json:"end_time"`
	Duration   string      `json:"duration"`
	FinalState State       `json:"final_state"`
	Limits     Limits      `json:"limits"`
	Violations []Violation `json:"violations"`
	TaskCount  int         `json:"task_count"`
}

// ProjectBudget represents per-project budget configuration.
type ProjectBudget struct {
	ProjectID       string    `json:"project_id"`
	Limits          Limits    `json:"limits"`
	SpentThisMonth  float64   `json:"spent_this_month"`
	SpentAllTime    float64   `json:"spent_all_time"`
	TokensThisMonth int64     `json:"tokens_this_month"`
	TokensAllTime   int64     `json:"tokens_all_time"`
	SessionCount    int       `json:"session_count"`
	MonthlyLimit    float64   `json:"monthly_limit"`
	ResetDay        int       `json:"reset_day"` // Day of month to reset (1-28)
	LastReset       time.Time `json:"last_reset"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// SaveSession records a completed session.
func (s *Store) SaveSession(ctx context.Context, record *SessionRecord) error {
	if record.ID == "" {
		record.ID = fmt.Sprintf("session:%s", uuid.New().String())
	}
	if record.EndTime.IsZero() {
		record.EndTime = time.Now()
	}
	record.Duration = record.EndTime.Sub(record.StartTime).String()

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	node := &hypergraph.Node{
		ID:        record.ID,
		Type:      hypergraph.NodeTypeExperience,
		Subtype:   subtypeSession,
		Content:   string(data),
		CreatedAt: record.StartTime,
		UpdatedAt: record.EndTime,
		Tier:      hypergraph.TierLongterm,
	}

	return s.graph.CreateNode(ctx, node)
}

// GetSession retrieves a session record by ID.
func (s *Store) GetSession(ctx context.Context, id string) (*SessionRecord, error) {
	node, err := s.graph.GetNode(ctx, id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, err
	}
	if node == nil {
		return nil, nil
	}

	var record SessionRecord
	if err := json.Unmarshal([]byte(node.Content), &record); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return &record, nil
}

// ListSessions returns session records, optionally filtered by project.
func (s *Store) ListSessions(ctx context.Context, projectID string, limit int) ([]*SessionRecord, error) {
	if limit <= 0 {
		limit = 100
	}

	nodes, err := s.graph.ListNodes(ctx, hypergraph.NodeFilter{
		Types:    []hypergraph.NodeType{hypergraph.NodeTypeExperience},
		Subtypes: []string{subtypeSession},
		Limit:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	records := make([]*SessionRecord, 0, len(nodes))
	for _, node := range nodes {
		var record SessionRecord
		if err := json.Unmarshal([]byte(node.Content), &record); err != nil {
			continue
		}
		if projectID != "" && record.ProjectID != projectID {
			continue
		}
		records = append(records, &record)
	}
	return records, nil
}

// GetSessionsInPeriod returns sessions within a time period.
func (s *Store) GetSessionsInPeriod(ctx context.Context, projectID string, start, end time.Time) ([]*SessionRecord, error) {
	sessions, err := s.ListSessions(ctx, projectID, 1000)
	if err != nil {
		return nil, err
	}

	filtered := make([]*SessionRecord, 0)
	for _, session := range sessions {
		if session.StartTime.After(start) && session.StartTime.Before(end) {
			filtered = append(filtered, session)
		}
	}
	return filtered, nil
}

// SaveProjectBudget saves or updates a project budget.
func (s *Store) SaveProjectBudget(ctx context.Context, pb *ProjectBudget) error {
	if pb.ProjectID == "" {
		return fmt.Errorf("project ID required")
	}
	if pb.CreatedAt.IsZero() {
		pb.CreatedAt = time.Now()
	}
	pb.UpdatedAt = time.Now()

	// Default reset day
	if pb.ResetDay <= 0 || pb.ResetDay > 28 {
		pb.ResetDay = 1
	}

	data, err := json.Marshal(pb)
	if err != nil {
		return fmt.Errorf("marshal project budget: %w", err)
	}

	nodeID := fmt.Sprintf("project:%s", pb.ProjectID)
	node := &hypergraph.Node{
		ID:        nodeID,
		Type:      hypergraph.NodeTypeFact,
		Subtype:   subtypeProject,
		Content:   string(data),
		CreatedAt: pb.CreatedAt,
		UpdatedAt: pb.UpdatedAt,
		Tier:      hypergraph.TierLongterm,
	}

	// Check if exists
	existing, err := s.graph.GetNode(ctx, nodeID)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return fmt.Errorf("check existing: %w", err)
	}

	if existing != nil {
		existing.Content = string(data)
		existing.UpdatedAt = pb.UpdatedAt
		return s.graph.UpdateNode(ctx, existing)
	}

	return s.graph.CreateNode(ctx, node)
}

// GetProjectBudget retrieves a project's budget configuration.
func (s *Store) GetProjectBudget(ctx context.Context, projectID string) (*ProjectBudget, error) {
	node, err := s.graph.GetNode(ctx, fmt.Sprintf("project:%s", projectID))
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, err
	}
	if node == nil {
		return nil, nil
	}

	var pb ProjectBudget
	if err := json.Unmarshal([]byte(node.Content), &pb); err != nil {
		return nil, fmt.Errorf("unmarshal project budget: %w", err)
	}
	return &pb, nil
}

// GetOrCreateProjectBudget gets existing or creates default budget.
func (s *Store) GetOrCreateProjectBudget(ctx context.Context, projectID string, defaultLimits Limits) (*ProjectBudget, error) {
	pb, err := s.GetProjectBudget(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if pb != nil {
		return pb, nil
	}

	// Create new with defaults
	pb = &ProjectBudget{
		ProjectID:    projectID,
		Limits:       defaultLimits,
		MonthlyLimit: defaultLimits.MaxTotalCost * 30, // Monthly = 30x daily
		ResetDay:     1,
		CreatedAt:    time.Now(),
	}

	if err := s.SaveProjectBudget(ctx, pb); err != nil {
		return nil, err
	}
	return pb, nil
}

// ListProjectBudgets returns all project budgets.
func (s *Store) ListProjectBudgets(ctx context.Context) ([]*ProjectBudget, error) {
	nodes, err := s.graph.ListNodes(ctx, hypergraph.NodeFilter{
		Types:    []hypergraph.NodeType{hypergraph.NodeTypeFact},
		Subtypes: []string{subtypeProject},
	})
	if err != nil {
		return nil, fmt.Errorf("list project budgets: %w", err)
	}

	budgets := make([]*ProjectBudget, 0, len(nodes))
	for _, node := range nodes {
		var pb ProjectBudget
		if err := json.Unmarshal([]byte(node.Content), &pb); err != nil {
			continue
		}
		budgets = append(budgets, &pb)
	}
	return budgets, nil
}

// UpdateProjectSpending adds spending to a project's budget.
func (s *Store) UpdateProjectSpending(ctx context.Context, projectID string, cost float64, tokens int64) error {
	pb, err := s.GetProjectBudget(ctx, projectID)
	if err != nil {
		return err
	}
	if pb == nil {
		return fmt.Errorf("project budget not found: %s", projectID)
	}

	// Check if we need monthly reset
	if s.shouldResetMonthly(pb) {
		pb.SpentThisMonth = 0
		pb.TokensThisMonth = 0
		pb.LastReset = time.Now()
	}

	pb.SpentThisMonth += cost
	pb.SpentAllTime += cost
	pb.TokensThisMonth += tokens
	pb.TokensAllTime += tokens

	return s.SaveProjectBudget(ctx, pb)
}

// IncrementSessionCount increments a project's session count.
func (s *Store) IncrementSessionCount(ctx context.Context, projectID string) error {
	pb, err := s.GetProjectBudget(ctx, projectID)
	if err != nil {
		return err
	}
	if pb == nil {
		return fmt.Errorf("project budget not found: %s", projectID)
	}

	pb.SessionCount++
	return s.SaveProjectBudget(ctx, pb)
}

// shouldResetMonthly checks if monthly reset is due.
func (s *Store) shouldResetMonthly(pb *ProjectBudget) bool {
	if pb.LastReset.IsZero() {
		return false
	}

	now := time.Now()
	lastResetMonth := pb.LastReset.Month()
	currentMonth := now.Month()

	// Different month
	if currentMonth != lastResetMonth {
		// Check if we're past the reset day
		return now.Day() >= pb.ResetDay
	}

	return false
}

// ResetMonthly forces a monthly reset for a project.
func (s *Store) ResetMonthly(ctx context.Context, projectID string) error {
	pb, err := s.GetProjectBudget(ctx, projectID)
	if err != nil {
		return err
	}
	if pb == nil {
		return fmt.Errorf("project budget not found: %s", projectID)
	}

	pb.SpentThisMonth = 0
	pb.TokensThisMonth = 0
	pb.LastReset = time.Now()

	return s.SaveProjectBudget(ctx, pb)
}

// GetSpendingSummary returns spending summary for a project.
func (s *Store) GetSpendingSummary(ctx context.Context, projectID string) (*SpendingSummary, error) {
	pb, err := s.GetProjectBudget(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if pb == nil {
		return nil, nil
	}

	// Get recent sessions
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	sessions, err := s.GetSessionsInPeriod(ctx, projectID, monthStart, now)
	if err != nil {
		return nil, err
	}

	summary := &SpendingSummary{
		ProjectID:         projectID,
		SpentThisMonth:    pb.SpentThisMonth,
		SpentAllTime:      pb.SpentAllTime,
		TokensThisMonth:   pb.TokensThisMonth,
		TokensAllTime:     pb.TokensAllTime,
		MonthlyLimit:      pb.MonthlyLimit,
		MonthlyRemaining:  pb.MonthlyLimit - pb.SpentThisMonth,
		SessionCount:      pb.SessionCount,
		SessionsThisMonth: len(sessions),
		LastReset:         pb.LastReset,
	}

	if pb.MonthlyLimit > 0 {
		summary.MonthlyPercent = (pb.SpentThisMonth / pb.MonthlyLimit) * 100
	}

	return summary, nil
}

// SpendingSummary contains spending analytics.
type SpendingSummary struct {
	ProjectID         string    `json:"project_id"`
	SpentThisMonth    float64   `json:"spent_this_month"`
	SpentAllTime      float64   `json:"spent_all_time"`
	TokensThisMonth   int64     `json:"tokens_this_month"`
	TokensAllTime     int64     `json:"tokens_all_time"`
	MonthlyLimit      float64   `json:"monthly_limit"`
	MonthlyRemaining  float64   `json:"monthly_remaining"`
	MonthlyPercent    float64   `json:"monthly_percent"`
	SessionCount      int       `json:"session_count"`
	SessionsThisMonth int       `json:"sessions_this_month"`
	LastReset         time.Time `json:"last_reset"`
}

// DeleteSession removes a session record.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	return s.graph.DeleteNode(ctx, id)
}

// DeleteProjectBudget removes a project budget.
func (s *Store) DeleteProjectBudget(ctx context.Context, projectID string) error {
	return s.graph.DeleteNode(ctx, fmt.Sprintf("project:%s", projectID))
}
