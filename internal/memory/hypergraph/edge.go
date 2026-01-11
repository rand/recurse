package hypergraph

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// HyperedgeType defines the type of a hyperedge.
type HyperedgeType string

const (
	HyperedgeRelation    HyperedgeType = "relation"    // Connects entities with typed relationship
	HyperedgeComposition HyperedgeType = "composition" // Groups related facts into higher-order structure
	HyperedgeCausation   HyperedgeType = "causation"   // Links decisions to outcomes
	HyperedgeContext     HyperedgeType = "context"     // Associates snippets with semantic meaning
)

// MemberRole defines the role of a node in a hyperedge.
type MemberRole string

const (
	RoleSubject     MemberRole = "subject"     // Primary actor/entity
	RoleObject      MemberRole = "object"      // Target of relationship
	RoleContext     MemberRole = "context"     // Contextual information
	RoleParticipant MemberRole = "participant" // General participant
)

// Hyperedge represents an edge connecting multiple nodes.
type Hyperedge struct {
	ID        string          `json:"id"`
	Type      HyperedgeType   `json:"type"`
	Label     string          `json:"label,omitempty"` // Human-readable description
	Weight    float64         `json:"weight"`
	CreatedAt time.Time       `json:"created_at"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

// Membership represents a node's participation in a hyperedge.
type Membership struct {
	HyperedgeID string     `json:"hyperedge_id"`
	NodeID      string     `json:"node_id"`
	Role        MemberRole `json:"role"`
	Position    int        `json:"position"` // Ordering within hyperedge
}

// NewHyperedge creates a new hyperedge with a generated ID.
func NewHyperedge(edgeType HyperedgeType, label string) *Hyperedge {
	return &Hyperedge{
		ID:        uuid.New().String(),
		Type:      edgeType,
		Label:     label,
		Weight:    1.0,
		CreatedAt: time.Now().UTC(),
	}
}

// CreateHyperedge inserts a new hyperedge into the database.
func (s *Store) CreateHyperedge(ctx context.Context, edge *Hyperedge) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if edge.ID == "" {
		edge.ID = uuid.New().String()
	}
	if edge.CreatedAt.IsZero() {
		edge.CreatedAt = time.Now().UTC()
	}
	if edge.Weight == 0 {
		edge.Weight = 1.0
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO hyperedges (id, type, label, weight, created_at, metadata)
		VALUES (?, ?, ?, ?, ?, ?)
	`, edge.ID, edge.Type, nullString(edge.Label), edge.Weight, edge.CreatedAt, edge.Metadata)
	if err != nil {
		return fmt.Errorf("insert hyperedge: %w", err)
	}

	return nil
}

// GetHyperedge retrieves a hyperedge by ID.
func (s *Store) GetHyperedge(ctx context.Context, id string) (*Hyperedge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, label, weight, created_at, metadata
		FROM hyperedges WHERE id = ?
	`, id)

	return scanHyperedge(row)
}

// UpdateHyperedge updates an existing hyperedge.
func (s *Store) UpdateHyperedge(ctx context.Context, edge *Hyperedge) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.ExecContext(ctx, `
		UPDATE hyperedges SET type = ?, label = ?, weight = ?, metadata = ?
		WHERE id = ?
	`, edge.Type, nullString(edge.Label), edge.Weight, edge.Metadata, edge.ID)
	if err != nil {
		return fmt.Errorf("update hyperedge: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("hyperedge not found: %s", edge.ID)
	}

	return nil
}

// DeleteHyperedge removes a hyperedge by ID.
func (s *Store) DeleteHyperedge(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.ExecContext(ctx, "DELETE FROM hyperedges WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete hyperedge: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("hyperedge not found: %s", id)
	}

	return nil
}

// AddMember adds a node to a hyperedge with the specified role.
func (s *Store) AddMember(ctx context.Context, m Membership) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO membership (hyperedge_id, node_id, role, position)
		VALUES (?, ?, ?, ?)
	`, m.HyperedgeID, m.NodeID, m.Role, m.Position)
	if err != nil {
		return fmt.Errorf("add member: %w", err)
	}

	return nil
}

// RemoveMember removes a node from a hyperedge.
func (s *Store) RemoveMember(ctx context.Context, hyperedgeID, nodeID string, role MemberRole) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.ExecContext(ctx, `
		DELETE FROM membership WHERE hyperedge_id = ? AND node_id = ? AND role = ?
	`, hyperedgeID, nodeID, role)
	if err != nil {
		return fmt.Errorf("remove member: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("membership not found")
	}

	return nil
}

// GetMembers returns all members of a hyperedge.
func (s *Store) GetMembers(ctx context.Context, hyperedgeID string) ([]Membership, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT hyperedge_id, node_id, role, position
		FROM membership WHERE hyperedge_id = ?
		ORDER BY position
	`, hyperedgeID)
	if err != nil {
		return nil, fmt.Errorf("query members: %w", err)
	}
	defer rows.Close()

	var members []Membership
	for rows.Next() {
		var m Membership
		var role sql.NullString
		var position sql.NullInt64
		if err := rows.Scan(&m.HyperedgeID, &m.NodeID, &role, &position); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		m.Role = MemberRole(role.String)
		m.Position = int(position.Int64)
		members = append(members, m)
	}

	return members, rows.Err()
}

// GetMemberNodes returns all nodes that are members of a hyperedge.
func (s *Store) GetMemberNodes(ctx context.Context, hyperedgeID string) ([]*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT n.id, n.type, n.subtype, n.content, n.embedding, n.created_at, n.updated_at,
		       n.access_count, n.last_accessed, n.tier, n.confidence, n.provenance, n.metadata
		FROM nodes n
		JOIN membership m ON n.id = m.node_id
		WHERE m.hyperedge_id = ?
		ORDER BY m.position
	`, hyperedgeID)
	if err != nil {
		return nil, fmt.Errorf("query member nodes: %w", err)
	}
	defer rows.Close()

	var nodes []*Node
	for rows.Next() {
		node, err := scanNodeRows(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}

	return nodes, rows.Err()
}

// GetNodeHyperedges returns all hyperedges that a node belongs to.
func (s *Store) GetNodeHyperedges(ctx context.Context, nodeID string) ([]*Hyperedge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT h.id, h.type, h.label, h.weight, h.created_at, h.metadata
		FROM hyperedges h
		JOIN membership m ON h.id = m.hyperedge_id
		WHERE m.node_id = ?
	`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("query node hyperedges: %w", err)
	}
	defer rows.Close()

	var edges []*Hyperedge
	for rows.Next() {
		edge, err := scanHyperedgeRows(rows)
		if err != nil {
			return nil, err
		}
		edges = append(edges, edge)
	}

	return edges, rows.Err()
}

// HyperedgeFilter defines criteria for filtering hyperedges.
type HyperedgeFilter struct {
	Types     []HyperedgeType
	MinWeight float64
	Limit     int
	Offset    int
}

// ListHyperedges retrieves hyperedges matching the given filter.
func (s *Store) ListHyperedges(ctx context.Context, filter HyperedgeFilter) ([]*Hyperedge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT id, type, label, weight, created_at, metadata FROM hyperedges WHERE 1=1"
	var args []any

	if len(filter.Types) > 0 {
		query += " AND type IN (?" + repeatString(",?", len(filter.Types)-1) + ")"
		for _, t := range filter.Types {
			args = append(args, t)
		}
	}

	if filter.MinWeight > 0 {
		query += " AND weight >= ?"
		args = append(args, filter.MinWeight)
	}

	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query hyperedges: %w", err)
	}
	defer rows.Close()

	var edges []*Hyperedge
	for rows.Next() {
		edge, err := scanHyperedgeRows(rows)
		if err != nil {
			return nil, err
		}
		edges = append(edges, edge)
	}

	return edges, rows.Err()
}

// CreateRelation creates a hyperedge connecting nodes with a typed relationship.
// This is a convenience method for creating common relation patterns.
func (s *Store) CreateRelation(ctx context.Context, label string, subjectID, objectID string) (*Hyperedge, error) {
	edge := NewHyperedge(HyperedgeRelation, label)

	if err := s.CreateHyperedge(ctx, edge); err != nil {
		return nil, err
	}

	// Add subject
	if err := s.AddMember(ctx, Membership{
		HyperedgeID: edge.ID,
		NodeID:      subjectID,
		Role:        RoleSubject,
		Position:    0,
	}); err != nil {
		return nil, err
	}

	// Add object
	if err := s.AddMember(ctx, Membership{
		HyperedgeID: edge.ID,
		NodeID:      objectID,
		Role:        RoleObject,
		Position:    1,
	}); err != nil {
		return nil, err
	}

	return edge, nil
}

// Helper functions

func scanHyperedge(row *sql.Row) (*Hyperedge, error) {
	var edge Hyperedge
	var label, metadata sql.NullString

	err := row.Scan(&edge.ID, &edge.Type, &label, &edge.Weight, &edge.CreatedAt, &metadata)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("hyperedge not found")
	}
	if err != nil {
		return nil, fmt.Errorf("scan hyperedge: %w", err)
	}

	edge.Label = label.String
	if metadata.Valid {
		edge.Metadata = json.RawMessage(metadata.String)
	}

	return &edge, nil
}

func scanHyperedgeRows(rows *sql.Rows) (*Hyperedge, error) {
	var edge Hyperedge
	var label, metadata sql.NullString

	err := rows.Scan(&edge.ID, &edge.Type, &label, &edge.Weight, &edge.CreatedAt, &metadata)
	if err != nil {
		return nil, fmt.Errorf("scan hyperedge: %w", err)
	}

	edge.Label = label.String
	if metadata.Valid {
		edge.Metadata = json.RawMessage(metadata.String)
	}

	return &edge, nil
}
