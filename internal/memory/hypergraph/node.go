package hypergraph

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// NodeType defines the type of a node in the hypergraph.
type NodeType string

const (
	NodeTypeEntity     NodeType = "entity"     // Code elements (files, functions, types, variables)
	NodeTypeFact       NodeType = "fact"       // Extracted knowledge
	NodeTypeExperience NodeType = "experience" // Interaction patterns
	NodeTypeDecision   NodeType = "decision"   // Reasoning trace
	NodeTypeSnippet    NodeType = "snippet"    // Verbatim content with provenance
)

// Tier represents the memory tier for a node.
type Tier string

const (
	TierTask     Tier = "task"     // Working memory for current problem
	TierSession  Tier = "session"  // Accumulated context for coding session
	TierLongterm Tier = "longterm" // Persistent knowledge
	TierArchive  Tier = "archive"  // Archived (excluded from retrieval)
)

// Node represents a node in the hypergraph.
type Node struct {
	ID           string          `json:"id"`
	Type         NodeType        `json:"type"`
	Subtype      string          `json:"subtype,omitempty"` // e.g., file, function, goal, action
	Content      string          `json:"content"`
	Embedding    []byte          `json:"embedding,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	AccessCount  int             `json:"access_count"`
	LastAccessed *time.Time      `json:"last_accessed,omitempty"`
	Tier         Tier            `json:"tier"`
	Confidence   float64         `json:"confidence"`
	Provenance   json.RawMessage `json:"provenance,omitempty"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
}

// Provenance captures the source of a node.
type Provenance struct {
	File       string `json:"file,omitempty"`
	Line       int    `json:"line,omitempty"`
	CommitHash string `json:"commit_hash,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Source     string `json:"source,omitempty"` // e.g., "agent", "user", "system"
}

// NewNode creates a new node with a generated ID.
func NewNode(nodeType NodeType, content string) *Node {
	now := time.Now().UTC()
	return &Node{
		ID:         uuid.New().String(),
		Type:       nodeType,
		Content:    content,
		CreatedAt:  now,
		UpdatedAt:  now,
		Tier:       TierTask,
		Confidence: 1.0,
	}
}

// CreateNode inserts a new node into the database.
func (s *Store) CreateNode(ctx context.Context, node *Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if node.ID == "" {
		node.ID = uuid.New().String()
	}
	if node.CreatedAt.IsZero() {
		node.CreatedAt = time.Now().UTC()
	}
	if node.UpdatedAt.IsZero() {
		node.UpdatedAt = time.Now().UTC()
	}
	if node.Tier == "" {
		node.Tier = TierTask
	}
	if node.Confidence == 0 {
		node.Confidence = 1.0
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO nodes (id, type, subtype, content, embedding, created_at, updated_at,
		                   access_count, last_accessed, tier, confidence, provenance, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		node.ID, node.Type, nullString(node.Subtype), node.Content, node.Embedding,
		node.CreatedAt, node.UpdatedAt, node.AccessCount, nullTime(node.LastAccessed),
		node.Tier, node.Confidence, node.Provenance, node.Metadata,
	)
	if err != nil {
		return fmt.Errorf("insert node: %w", err)
	}

	return nil
}

// GetNode retrieves a node by ID.
func (s *Store) GetNode(ctx context.Context, id string) (*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getNodeLocked(ctx, id)
}

func (s *Store) getNodeLocked(ctx context.Context, id string) (*Node, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, subtype, content, embedding, created_at, updated_at,
		       access_count, last_accessed, tier, confidence, provenance, metadata
		FROM nodes WHERE id = ?
	`, id)

	return scanNode(row)
}

// UpdateNode updates an existing node.
func (s *Store) UpdateNode(ctx context.Context, node *Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	node.UpdatedAt = time.Now().UTC()

	result, err := s.db.ExecContext(ctx, `
		UPDATE nodes SET
			type = ?, subtype = ?, content = ?, embedding = ?, updated_at = ?,
			access_count = ?, last_accessed = ?, tier = ?, confidence = ?,
			provenance = ?, metadata = ?
		WHERE id = ?
	`,
		node.Type, nullString(node.Subtype), node.Content, node.Embedding, node.UpdatedAt,
		node.AccessCount, nullTime(node.LastAccessed), node.Tier, node.Confidence,
		node.Provenance, node.Metadata, node.ID,
	)
	if err != nil {
		return fmt.Errorf("update node: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("node not found: %s", node.ID)
	}

	return nil
}

// DeleteNode removes a node by ID.
func (s *Store) DeleteNode(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.ExecContext(ctx, "DELETE FROM nodes WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete node: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("node not found: %s", id)
	}

	return nil
}

// IncrementAccess increments the access count for a node.
func (s *Store) IncrementAccess(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Pass time.Time directly like CreateNode does for created_at/updated_at
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE nodes SET access_count = access_count + 1, last_accessed = ?
		WHERE id = ?
	`, now, id)
	if err != nil {
		return fmt.Errorf("increment access: %w", err)
	}

	return nil
}

// NodeFilter defines criteria for filtering nodes.
type NodeFilter struct {
	Types    []NodeType
	Subtypes []string
	Tiers    []Tier
	MinConfidence float64
	Limit    int
	Offset   int
}

// ListNodes retrieves nodes matching the given filter.
func (s *Store) ListNodes(ctx context.Context, filter NodeFilter) ([]*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT id, type, subtype, content, embedding, created_at, updated_at, " +
		"access_count, last_accessed, tier, confidence, provenance, metadata FROM nodes WHERE 1=1"
	var args []any

	if len(filter.Types) > 0 {
		query += " AND type IN (?" + repeatString(",?", len(filter.Types)-1) + ")"
		for _, t := range filter.Types {
			args = append(args, t)
		}
	}

	if len(filter.Subtypes) > 0 {
		query += " AND subtype IN (?" + repeatString(",?", len(filter.Subtypes)-1) + ")"
		for _, s := range filter.Subtypes {
			args = append(args, s)
		}
	}

	if len(filter.Tiers) > 0 {
		query += " AND tier IN (?" + repeatString(",?", len(filter.Tiers)-1) + ")"
		for _, t := range filter.Tiers {
			args = append(args, t)
		}
	}

	if filter.MinConfidence > 0 {
		query += " AND confidence >= ?"
		args = append(args, filter.MinConfidence)
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
		return nil, fmt.Errorf("query nodes: %w", err)
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

// CountNodes returns the count of nodes matching the filter.
func (s *Store) CountNodes(ctx context.Context, filter NodeFilter) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT COUNT(*) FROM nodes WHERE 1=1"
	var args []any

	if len(filter.Types) > 0 {
		query += " AND type IN (?" + repeatString(",?", len(filter.Types)-1) + ")"
		for _, t := range filter.Types {
			args = append(args, t)
		}
	}

	if len(filter.Tiers) > 0 {
		query += " AND tier IN (?" + repeatString(",?", len(filter.Tiers)-1) + ")"
		for _, t := range filter.Tiers {
			args = append(args, t)
		}
	}

	if filter.MinConfidence > 0 {
		query += " AND confidence >= ?"
		args = append(args, filter.MinConfidence)
	}

	var count int64
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count nodes: %w", err)
	}

	return count, nil
}

// Helper functions

func scanNode(row *sql.Row) (*Node, error) {
	var node Node
	var subtype, provenance, metadata sql.NullString
	var lastAccessed sql.NullTime

	err := row.Scan(
		&node.ID, &node.Type, &subtype, &node.Content, &node.Embedding,
		&node.CreatedAt, &node.UpdatedAt, &node.AccessCount, &lastAccessed,
		&node.Tier, &node.Confidence, &provenance, &metadata,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("node not found")
	}
	if err != nil {
		return nil, fmt.Errorf("scan node: %w", err)
	}

	node.Subtype = subtype.String
	if lastAccessed.Valid {
		node.LastAccessed = &lastAccessed.Time
	}
	if provenance.Valid {
		node.Provenance = json.RawMessage(provenance.String)
	}
	if metadata.Valid {
		node.Metadata = json.RawMessage(metadata.String)
	}

	return &node, nil
}

func scanNodeRows(rows *sql.Rows) (*Node, error) {
	var node Node
	var subtype, provenance, metadata sql.NullString
	var lastAccessed sql.NullTime

	err := rows.Scan(
		&node.ID, &node.Type, &subtype, &node.Content, &node.Embedding,
		&node.CreatedAt, &node.UpdatedAt, &node.AccessCount, &lastAccessed,
		&node.Tier, &node.Confidence, &provenance, &metadata,
	)
	if err != nil {
		return nil, fmt.Errorf("scan node: %w", err)
	}

	node.Subtype = subtype.String
	if lastAccessed.Valid {
		node.LastAccessed = &lastAccessed.Time
	}
	if provenance.Valid {
		node.Provenance = json.RawMessage(provenance.String)
	}
	if metadata.Valid {
		node.Metadata = json.RawMessage(metadata.String)
	}

	return &node, nil
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func repeatString(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
