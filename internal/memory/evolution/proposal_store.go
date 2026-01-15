package evolution

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

// SQLiteProposalStore implements ProposalStore using the hypergraph database.
type SQLiteProposalStore struct {
	store *hypergraph.Store
}

// NewSQLiteProposalStore creates a new SQLite-backed proposal store.
func NewSQLiteProposalStore(store *hypergraph.Store) *SQLiteProposalStore {
	return &SQLiteProposalStore{store: store}
}

// Save stores a proposal.
func (s *SQLiteProposalStore) Save(ctx context.Context, proposal *Proposal) error {
	db := s.store.DB()
	if db == nil {
		return fmt.Errorf("database not available")
	}

	evidenceJSON, err := json.Marshal(proposal.Evidence)
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}

	impactJSON, err := json.Marshal(proposal.Impact)
	if err != nil {
		return fmt.Errorf("marshal impact: %w", err)
	}

	changesJSON, err := json.Marshal(proposal.Changes)
	if err != nil {
		return fmt.Errorf("marshal changes: %w", err)
	}

	query := `
		INSERT INTO proposals (
			id, type, title, description, rationale, evidence, impact, changes,
			confidence, priority, status, status_note, source_pattern,
			defer_until, applied_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	var deferUntil, appliedAt interface{}
	if !proposal.DeferUntil.IsZero() {
		deferUntil = proposal.DeferUntil
	}
	if !proposal.AppliedAt.IsZero() {
		appliedAt = proposal.AppliedAt
	}

	_, err = db.ExecContext(ctx, query,
		proposal.ID,
		string(proposal.Type),
		proposal.Title,
		proposal.Description,
		proposal.Rationale,
		string(evidenceJSON),
		string(impactJSON),
		string(changesJSON),
		proposal.Confidence,
		proposal.Priority,
		string(proposal.Status),
		proposal.StatusNote,
		string(proposal.SourcePattern),
		deferUntil,
		appliedAt,
		proposal.CreatedAt,
		proposal.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert proposal: %w", err)
	}

	return nil
}

// Get retrieves a proposal by ID.
func (s *SQLiteProposalStore) Get(ctx context.Context, id string) (*Proposal, error) {
	db := s.store.DB()
	if db == nil {
		return nil, fmt.Errorf("database not available")
	}

	query := `
		SELECT id, type, title, description, rationale, evidence, impact, changes,
		       confidence, priority, status, status_note, source_pattern,
		       defer_until, applied_at, created_at, updated_at
		FROM proposals
		WHERE id = ?
	`

	row := db.QueryRowContext(ctx, query, id)
	return scanProposal(row)
}

// List returns proposals matching the filter.
func (s *SQLiteProposalStore) List(ctx context.Context, filter ProposalFilter) ([]*Proposal, error) {
	db := s.store.DB()
	if db == nil {
		return nil, fmt.Errorf("database not available")
	}

	// Build query dynamically
	query := `
		SELECT id, type, title, description, rationale, evidence, impact, changes,
		       confidence, priority, status, status_note, source_pattern,
		       defer_until, applied_at, created_at, updated_at
		FROM proposals
	`

	var conditions []string
	var args []interface{}

	if len(filter.Status) > 0 {
		placeholders := make([]string, len(filter.Status))
		for i, status := range filter.Status {
			placeholders[i] = "?"
			args = append(args, string(status))
		}
		conditions = append(conditions, fmt.Sprintf("status IN (%s)", strings.Join(placeholders, ",")))
	}

	if len(filter.Type) > 0 {
		placeholders := make([]string, len(filter.Type))
		for i, t := range filter.Type {
			placeholders[i] = "?"
			args = append(args, string(t))
		}
		conditions = append(conditions, fmt.Sprintf("type IN (%s)", strings.Join(placeholders, ",")))
	}

	if !filter.Since.IsZero() {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, filter.Since)
	}

	if !filter.Until.IsZero() {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, filter.Until)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Sort order
	sortBy := "created_at"
	if filter.SortBy != "" {
		switch filter.SortBy {
		case "priority", "confidence", "created_at", "updated_at":
			sortBy = filter.SortBy
		}
	}

	sortOrder := "DESC"
	if filter.SortOrder == "asc" {
		sortOrder = "ASC"
	}

	query += fmt.Sprintf(" ORDER BY %s %s", sortBy, sortOrder)

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query proposals: %w", err)
	}
	defer rows.Close()

	var proposals []*Proposal
	for rows.Next() {
		p, err := scanProposalRow(rows)
		if err != nil {
			return nil, err
		}
		proposals = append(proposals, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return proposals, nil
}

// Update updates an existing proposal.
func (s *SQLiteProposalStore) Update(ctx context.Context, proposal *Proposal) error {
	db := s.store.DB()
	if db == nil {
		return fmt.Errorf("database not available")
	}

	evidenceJSON, err := json.Marshal(proposal.Evidence)
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}

	impactJSON, err := json.Marshal(proposal.Impact)
	if err != nil {
		return fmt.Errorf("marshal impact: %w", err)
	}

	changesJSON, err := json.Marshal(proposal.Changes)
	if err != nil {
		return fmt.Errorf("marshal changes: %w", err)
	}

	query := `
		UPDATE proposals SET
			type = ?, title = ?, description = ?, rationale = ?,
			evidence = ?, impact = ?, changes = ?,
			confidence = ?, priority = ?, status = ?, status_note = ?,
			source_pattern = ?, defer_until = ?, applied_at = ?, updated_at = ?
		WHERE id = ?
	`

	proposal.UpdatedAt = time.Now()

	var deferUntil, appliedAt interface{}
	if !proposal.DeferUntil.IsZero() {
		deferUntil = proposal.DeferUntil
	}
	if !proposal.AppliedAt.IsZero() {
		appliedAt = proposal.AppliedAt
	}

	result, err := db.ExecContext(ctx, query,
		string(proposal.Type),
		proposal.Title,
		proposal.Description,
		proposal.Rationale,
		string(evidenceJSON),
		string(impactJSON),
		string(changesJSON),
		proposal.Confidence,
		proposal.Priority,
		string(proposal.Status),
		proposal.StatusNote,
		string(proposal.SourcePattern),
		deferUntil,
		appliedAt,
		proposal.UpdatedAt,
		proposal.ID,
	)
	if err != nil {
		return fmt.Errorf("update proposal: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("proposal not found: %s", proposal.ID)
	}

	return nil
}

// Delete removes a proposal.
func (s *SQLiteProposalStore) Delete(ctx context.Context, id string) error {
	db := s.store.DB()
	if db == nil {
		return fmt.Errorf("database not available")
	}

	query := `DELETE FROM proposals WHERE id = ?`

	result, err := db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete proposal: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("proposal not found: %s", id)
	}

	return nil
}

// GetPending returns all pending proposals.
func (s *SQLiteProposalStore) GetPending(ctx context.Context) ([]*Proposal, error) {
	return s.List(ctx, ProposalFilter{
		Status:  []ProposalStatus{ProposalStatusPending},
		SortBy:  "priority",
		SortOrder: "desc",
	})
}

// GetStats returns proposal statistics.
func (s *SQLiteProposalStore) GetStats(ctx context.Context) (*ProposalStats, error) {
	proposals, err := s.List(ctx, ProposalFilter{})
	if err != nil {
		return nil, err
	}
	return CalculateStats(proposals), nil
}

// scanProposal scans a single row into a Proposal.
func scanProposal(row *sql.Row) (*Proposal, error) {
	var p Proposal
	var proposalType, status, sourcePattern string
	var evidenceJSON, impactJSON, changesJSON string
	var deferUntil, appliedAt sql.NullTime

	err := row.Scan(
		&p.ID,
		&proposalType,
		&p.Title,
		&p.Description,
		&p.Rationale,
		&evidenceJSON,
		&impactJSON,
		&changesJSON,
		&p.Confidence,
		&p.Priority,
		&status,
		&p.StatusNote,
		&sourcePattern,
		&deferUntil,
		&appliedAt,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan proposal: %w", err)
	}

	p.Type = ProposalType(proposalType)
	p.Status = ProposalStatus(status)
	p.SourcePattern = PatternType(sourcePattern)

	if deferUntil.Valid {
		p.DeferUntil = deferUntil.Time
	}
	if appliedAt.Valid {
		p.AppliedAt = appliedAt.Time
	}

	if err := json.Unmarshal([]byte(evidenceJSON), &p.Evidence); err != nil {
		return nil, fmt.Errorf("unmarshal evidence: %w", err)
	}
	if err := json.Unmarshal([]byte(impactJSON), &p.Impact); err != nil {
		return nil, fmt.Errorf("unmarshal impact: %w", err)
	}
	if err := json.Unmarshal([]byte(changesJSON), &p.Changes); err != nil {
		return nil, fmt.Errorf("unmarshal changes: %w", err)
	}

	return &p, nil
}

// scanProposalRow scans a rows result into a Proposal.
func scanProposalRow(rows *sql.Rows) (*Proposal, error) {
	var p Proposal
	var proposalType, status, sourcePattern string
	var evidenceJSON, impactJSON, changesJSON string
	var deferUntil, appliedAt sql.NullTime

	err := rows.Scan(
		&p.ID,
		&proposalType,
		&p.Title,
		&p.Description,
		&p.Rationale,
		&evidenceJSON,
		&impactJSON,
		&changesJSON,
		&p.Confidence,
		&p.Priority,
		&status,
		&p.StatusNote,
		&sourcePattern,
		&deferUntil,
		&appliedAt,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan proposal row: %w", err)
	}

	p.Type = ProposalType(proposalType)
	p.Status = ProposalStatus(status)
	p.SourcePattern = PatternType(sourcePattern)

	if deferUntil.Valid {
		p.DeferUntil = deferUntil.Time
	}
	if appliedAt.Valid {
		p.AppliedAt = appliedAt.Time
	}

	if err := json.Unmarshal([]byte(evidenceJSON), &p.Evidence); err != nil {
		return nil, fmt.Errorf("unmarshal evidence: %w", err)
	}
	if err := json.Unmarshal([]byte(impactJSON), &p.Impact); err != nil {
		return nil, fmt.Errorf("unmarshal impact: %w", err)
	}
	if err := json.Unmarshal([]byte(changesJSON), &p.Changes); err != nil {
		return nil, fmt.Errorf("unmarshal changes: %w", err)
	}

	return &p, nil
}
