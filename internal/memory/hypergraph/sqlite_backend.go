package hypergraph

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

//go:embed schema.sql
var schemaSQLEmbed string

// SQLiteBackend provides a SQLite implementation of Backend.
type SQLiteBackend struct {
	db   *sql.DB
	mu   sync.RWMutex
	path string
}

// SQLiteBackendOptions configures the SQLite backend.
type SQLiteBackendOptions struct {
	// Path to the SQLite database file.
	// If empty, uses in-memory database.
	Path string

	// CreateIfNotExists creates the database file if it doesn't exist.
	CreateIfNotExists bool
}

// NewSQLiteBackend creates a new SQLite backend.
func NewSQLiteBackend(opts SQLiteBackendOptions) (*SQLiteBackend, error) {
	var dsn string

	if opts.Path == "" {
		dsn = "file::memory:?cache=shared"
	} else {
		if opts.CreateIfNotExists {
			dir := filepath.Dir(opts.Path)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("create database directory: %w", err)
			}
		}
		dsn = "file:" + opts.Path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	backend := &SQLiteBackend{
		db:   db,
		path: opts.Path,
	}

	if err := backend.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return backend, nil
}

func (b *SQLiteBackend) initSchema() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	_, err := b.db.Exec(schemaSQLEmbed)
	if err != nil {
		return fmt.Errorf("execute schema: %w", err)
	}

	return nil
}

// DB returns the underlying database connection.
func (b *SQLiteBackend) DB() *sql.DB {
	return b.db
}

// Path returns the database file path.
func (b *SQLiteBackend) Path() string {
	return b.path
}

// CreateNode inserts a new node.
func (b *SQLiteBackend) CreateNode(ctx context.Context, node *Node) error {
	b.mu.Lock()
	defer b.mu.Unlock()

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

	_, err := b.db.ExecContext(ctx, `
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
func (b *SQLiteBackend) GetNode(ctx context.Context, id string) (*Node, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	row := b.db.QueryRowContext(ctx, `
		SELECT id, type, subtype, content, embedding, created_at, updated_at,
		       access_count, last_accessed, tier, confidence, provenance, metadata
		FROM nodes WHERE id = ?
	`, id)

	node, err := scanNodeRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, &ErrNotFound{Entity: "node", ID: id}
		}
		return nil, err
	}

	return node, nil
}

// UpdateNode updates an existing node.
func (b *SQLiteBackend) UpdateNode(ctx context.Context, node *Node) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	node.UpdatedAt = time.Now().UTC()

	result, err := b.db.ExecContext(ctx, `
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
		return &ErrNotFound{Entity: "node", ID: node.ID}
	}

	return nil
}

// DeleteNode removes a node by ID.
func (b *SQLiteBackend) DeleteNode(ctx context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	result, err := b.db.ExecContext(ctx, "DELETE FROM nodes WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete node: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return &ErrNotFound{Entity: "node", ID: id}
	}

	return nil
}

// ListNodes retrieves nodes matching the filter.
func (b *SQLiteBackend) ListNodes(ctx context.Context, filter NodeFilter) ([]*Node, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	query := "SELECT id, type, subtype, content, embedding, created_at, updated_at, " +
		"access_count, last_accessed, tier, confidence, provenance, metadata FROM nodes WHERE 1=1"
	var args []any

	if len(filter.Types) > 0 {
		query += " AND type IN (?" + strings.Repeat(",?", len(filter.Types)-1) + ")"
		for _, t := range filter.Types {
			args = append(args, t)
		}
	}

	if len(filter.Subtypes) > 0 {
		query += " AND subtype IN (?" + strings.Repeat(",?", len(filter.Subtypes)-1) + ")"
		for _, s := range filter.Subtypes {
			args = append(args, s)
		}
	}

	if len(filter.Tiers) > 0 {
		query += " AND tier IN (?" + strings.Repeat(",?", len(filter.Tiers)-1) + ")"
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

	rows, err := b.db.QueryContext(ctx, query, args...)
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
func (b *SQLiteBackend) CountNodes(ctx context.Context, filter NodeFilter) (int64, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	query := "SELECT COUNT(*) FROM nodes WHERE 1=1"
	var args []any

	if len(filter.Types) > 0 {
		query += " AND type IN (?" + strings.Repeat(",?", len(filter.Types)-1) + ")"
		for _, t := range filter.Types {
			args = append(args, t)
		}
	}

	if len(filter.Tiers) > 0 {
		query += " AND tier IN (?" + strings.Repeat(",?", len(filter.Tiers)-1) + ")"
		for _, t := range filter.Tiers {
			args = append(args, t)
		}
	}

	if filter.MinConfidence > 0 {
		query += " AND confidence >= ?"
		args = append(args, filter.MinConfidence)
	}

	var count int64
	err := b.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count nodes: %w", err)
	}

	return count, nil
}

// IncrementAccess increments the access count for a node.
func (b *SQLiteBackend) IncrementAccess(ctx context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now().UTC()
	result, err := b.db.ExecContext(ctx, `
		UPDATE nodes SET access_count = access_count + 1, last_accessed = ?
		WHERE id = ?
	`, now, id)
	if err != nil {
		return fmt.Errorf("increment access: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return &ErrNotFound{Entity: "node", ID: id}
	}

	return nil
}

// CreateHyperedge inserts a new hyperedge.
func (b *SQLiteBackend) CreateHyperedge(ctx context.Context, edge *Hyperedge) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if edge.ID == "" {
		edge.ID = uuid.New().String()
	}
	if edge.CreatedAt.IsZero() {
		edge.CreatedAt = time.Now().UTC()
	}
	if edge.Weight == 0 {
		edge.Weight = 1.0
	}

	_, err := b.db.ExecContext(ctx, `
		INSERT INTO hyperedges (id, type, label, weight, created_at, metadata)
		VALUES (?, ?, ?, ?, ?, ?)
	`, edge.ID, edge.Type, nullString(edge.Label), edge.Weight, edge.CreatedAt, edge.Metadata)
	if err != nil {
		return fmt.Errorf("insert hyperedge: %w", err)
	}

	return nil
}

// GetHyperedge retrieves a hyperedge by ID.
func (b *SQLiteBackend) GetHyperedge(ctx context.Context, id string) (*Hyperedge, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	row := b.db.QueryRowContext(ctx, `
		SELECT id, type, label, weight, created_at, metadata
		FROM hyperedges WHERE id = ?
	`, id)

	edge, err := scanHyperedgeRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, &ErrNotFound{Entity: "hyperedge", ID: id}
		}
		return nil, err
	}

	return edge, nil
}

// UpdateHyperedge updates an existing hyperedge.
func (b *SQLiteBackend) UpdateHyperedge(ctx context.Context, edge *Hyperedge) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	result, err := b.db.ExecContext(ctx, `
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
		return &ErrNotFound{Entity: "hyperedge", ID: edge.ID}
	}

	return nil
}

// DeleteHyperedge removes a hyperedge by ID.
func (b *SQLiteBackend) DeleteHyperedge(ctx context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	result, err := b.db.ExecContext(ctx, "DELETE FROM hyperedges WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete hyperedge: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return &ErrNotFound{Entity: "hyperedge", ID: id}
	}

	return nil
}

// ListHyperedges retrieves hyperedges matching the filter.
func (b *SQLiteBackend) ListHyperedges(ctx context.Context, filter HyperedgeFilter) ([]*Hyperedge, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	query := "SELECT id, type, label, weight, created_at, metadata FROM hyperedges WHERE 1=1"
	var args []any

	if len(filter.Types) > 0 {
		query += " AND type IN (?" + strings.Repeat(",?", len(filter.Types)-1) + ")"
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

	rows, err := b.db.QueryContext(ctx, query, args...)
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

// AddMember adds a node to a hyperedge.
func (b *SQLiteBackend) AddMember(ctx context.Context, m Membership) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	_, err := b.db.ExecContext(ctx, `
		INSERT INTO membership (hyperedge_id, node_id, role, position)
		VALUES (?, ?, ?, ?)
	`, m.HyperedgeID, m.NodeID, m.Role, m.Position)
	if err != nil {
		return fmt.Errorf("add member: %w", err)
	}

	return nil
}

// RemoveMember removes a node from a hyperedge.
func (b *SQLiteBackend) RemoveMember(ctx context.Context, hyperedgeID, nodeID string, role MemberRole) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	result, err := b.db.ExecContext(ctx, `
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
		return &ErrNotFound{Entity: "membership", ID: hyperedgeID + ":" + nodeID}
	}

	return nil
}

// GetMembers returns all members of a hyperedge.
func (b *SQLiteBackend) GetMembers(ctx context.Context, hyperedgeID string) ([]Membership, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	rows, err := b.db.QueryContext(ctx, `
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

// GetMemberNodes returns all nodes in a hyperedge.
func (b *SQLiteBackend) GetMemberNodes(ctx context.Context, hyperedgeID string) ([]*Node, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	rows, err := b.db.QueryContext(ctx, `
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

// GetNodeHyperedges returns all hyperedges a node belongs to.
func (b *SQLiteBackend) GetNodeHyperedges(ctx context.Context, nodeID string) ([]*Hyperedge, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	rows, err := b.db.QueryContext(ctx, `
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

// SearchByContent performs text search on node content.
func (b *SQLiteBackend) SearchByContent(ctx context.Context, query string, opts SearchOptions) ([]*SearchResult, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	sqlQuery := `
		SELECT id, type, subtype, content, embedding, created_at, updated_at,
		       access_count, last_accessed, tier, confidence, provenance, metadata
		FROM nodes WHERE content LIKE ?
	`
	args := []any{"%" + query + "%"}

	if len(opts.Types) > 0 {
		sqlQuery += " AND type IN (?" + strings.Repeat(",?", len(opts.Types)-1) + ")"
		for _, t := range opts.Types {
			args = append(args, t)
		}
	}

	if len(opts.Tiers) > 0 {
		sqlQuery += " AND tier IN (?" + strings.Repeat(",?", len(opts.Tiers)-1) + ")"
		for _, t := range opts.Tiers {
			args = append(args, t)
		}
	}

	if len(opts.Subtypes) > 0 {
		sqlQuery += " AND subtype IN (?" + strings.Repeat(",?", len(opts.Subtypes)-1) + ")"
		for _, st := range opts.Subtypes {
			args = append(args, st)
		}
	}

	if opts.MinConfidence > 0 {
		sqlQuery += " AND confidence >= ?"
		args = append(args, opts.MinConfidence)
	}

	sqlQuery += " AND tier != 'archive'"
	sqlQuery += " ORDER BY access_count DESC, updated_at DESC"

	if opts.Limit > 0 {
		sqlQuery += " LIMIT ?"
		args = append(args, opts.Limit)
	}

	rows, err := b.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("search by content: %w", err)
	}
	defer rows.Close()

	var results []*SearchResult
	for rows.Next() {
		node, err := scanNodeRows(rows)
		if err != nil {
			return nil, err
		}
		score := float64(strings.Count(strings.ToLower(node.Content), strings.ToLower(query)))
		results = append(results, &SearchResult{Node: node, Score: score})
	}

	return results, rows.Err()
}

// GetConnected finds nodes connected via hyperedges.
func (b *SQLiteBackend) GetConnected(ctx context.Context, nodeID string, opts TraversalOptions) ([]*ConnectedNode, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if opts.MaxDepth == 0 {
		opts.MaxDepth = 1
	}

	visited := make(map[string]bool)
	visited[nodeID] = true

	var results []*ConnectedNode
	queue := []struct {
		id    string
		depth int
	}{{nodeID, 0}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.depth >= opts.MaxDepth {
			continue
		}

		connected, err := b.getImmediateConnectionsLocked(ctx, current.id, opts)
		if err != nil {
			return nil, err
		}

		for _, conn := range connected {
			if visited[conn.Node.ID] {
				continue
			}
			visited[conn.Node.ID] = true

			conn.Depth = current.depth + 1
			results = append(results, conn)

			if opts.MaxResults > 0 && len(results) >= opts.MaxResults {
				return results, nil
			}

			if conn.Depth < opts.MaxDepth {
				queue = append(queue, struct {
					id    string
					depth int
				}{conn.Node.ID, conn.Depth})
			}
		}
	}

	return results, nil
}

func (b *SQLiteBackend) getImmediateConnectionsLocked(ctx context.Context, nodeID string, opts TraversalOptions) ([]*ConnectedNode, error) {
	baseSelect := `
		SELECT DISTINCT n.id, n.type, n.subtype, n.content, n.embedding, n.created_at, n.updated_at,
		       n.access_count, n.last_accessed, n.tier, n.confidence, n.provenance, n.metadata,
		       h.id, h.type, h.label, h.weight, h.created_at, h.metadata,
		       m2.role
		FROM nodes n
		JOIN membership m2 ON n.id = m2.node_id
		JOIN hyperedges h ON m2.hyperedge_id = h.id
		JOIN membership m1 ON h.id = m1.hyperedge_id
		WHERE m1.node_id = ? AND n.id != ?
	`

	var query string
	args := []any{nodeID, nodeID}

	switch opts.Direction {
	case TraverseOutgoing:
		query = baseSelect + " AND m1.role = 'subject'"
	case TraverseIncoming:
		query = baseSelect + " AND m1.role = 'object'"
	default:
		query = baseSelect
	}

	if len(opts.EdgeTypes) > 0 {
		query += " AND h.type IN (?" + strings.Repeat(",?", len(opts.EdgeTypes)-1) + ")"
		for _, t := range opts.EdgeTypes {
			args = append(args, t)
		}
	}

	if len(opts.NodeTypes) > 0 {
		query += " AND n.type IN (?" + strings.Repeat(",?", len(opts.NodeTypes)-1) + ")"
		for _, t := range opts.NodeTypes {
			args = append(args, t)
		}
	}

	if len(opts.Tiers) > 0 {
		query += " AND n.tier IN (?" + strings.Repeat(",?", len(opts.Tiers)-1) + ")"
		for _, t := range opts.Tiers {
			args = append(args, t)
		}
	}

	query += " AND n.tier != 'archive'"

	rows, err := b.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get connections: %w", err)
	}
	defer rows.Close()

	var results []*ConnectedNode
	for rows.Next() {
		conn, err := scanConnectedNodeRow(rows, opts.IncludeEdge)
		if err != nil {
			return nil, err
		}
		results = append(results, conn)
	}

	return results, rows.Err()
}

// RecentNodes returns recently accessed nodes.
func (b *SQLiteBackend) RecentNodes(ctx context.Context, limit int, tiers []Tier) ([]*Node, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	query := `
		SELECT id, type, subtype, content, embedding, created_at, updated_at,
		       access_count, last_accessed, tier, confidence, provenance, metadata
		FROM nodes WHERE tier != 'archive'
	`
	var args []any

	if len(tiers) > 0 {
		query += " AND tier IN (?" + strings.Repeat(",?", len(tiers)-1) + ")"
		for _, t := range tiers {
			args = append(args, t)
		}
	}

	query += " ORDER BY julianday(COALESCE(last_accessed, updated_at)) DESC, id ASC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := b.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("recent nodes: %w", err)
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

// Stats returns database statistics.
func (b *SQLiteBackend) Stats(ctx context.Context) (*Stats, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	stats := &Stats{
		NodesByTier: make(map[string]int64),
		NodesByType: make(map[string]int64),
	}

	err := b.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM nodes").Scan(&stats.NodeCount)
	if err != nil {
		return nil, fmt.Errorf("count nodes: %w", err)
	}

	err = b.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM hyperedges").Scan(&stats.HyperedgeCount)
	if err != nil {
		return nil, fmt.Errorf("count hyperedges: %w", err)
	}

	rows, err := b.db.QueryContext(ctx, "SELECT tier, COUNT(*) FROM nodes GROUP BY tier")
	if err != nil {
		return nil, fmt.Errorf("count by tier: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tier string
		var count int64
		if err := rows.Scan(&tier, &count); err != nil {
			return nil, fmt.Errorf("scan tier count: %w", err)
		}
		stats.NodesByTier[tier] = count
	}

	rows, err = b.db.QueryContext(ctx, "SELECT type, COUNT(*) FROM nodes GROUP BY type")
	if err != nil {
		return nil, fmt.Errorf("count by type: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var nodeType string
		var count int64
		if err := rows.Scan(&nodeType, &count); err != nil {
			return nil, fmt.Errorf("scan type count: %w", err)
		}
		stats.NodesByType[nodeType] = count
	}

	return stats, nil
}

// Close closes the database connection.
func (b *SQLiteBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.db != nil {
		return b.db.Close()
	}
	return nil
}

// Helper scan functions

func scanNodeRow(row *sql.Row) (*Node, error) {
	var node Node
	var subtype, provenance, metadata sql.NullString
	var lastAccessed sql.NullTime

	err := row.Scan(
		&node.ID, &node.Type, &subtype, &node.Content, &node.Embedding,
		&node.CreatedAt, &node.UpdatedAt, &node.AccessCount, &lastAccessed,
		&node.Tier, &node.Confidence, &provenance, &metadata,
	)
	if err != nil {
		return nil, err
	}

	node.Subtype = subtype.String
	if lastAccessed.Valid {
		node.LastAccessed = &lastAccessed.Time
	}
	if provenance.Valid {
		node.Provenance = []byte(provenance.String)
	}
	if metadata.Valid {
		node.Metadata = []byte(metadata.String)
	}

	return &node, nil
}

func scanHyperedgeRow(row *sql.Row) (*Hyperedge, error) {
	var edge Hyperedge
	var label, metadata sql.NullString

	err := row.Scan(&edge.ID, &edge.Type, &label, &edge.Weight, &edge.CreatedAt, &metadata)
	if err != nil {
		return nil, err
	}

	edge.Label = label.String
	if metadata.Valid {
		edge.Metadata = []byte(metadata.String)
	}

	return &edge, nil
}

func scanConnectedNodeRow(rows *sql.Rows, includeEdge bool) (*ConnectedNode, error) {
	var node Node
	var edge Hyperedge
	var subtype, nodeProvenance, nodeMetadata sql.NullString
	var lastAccessed sql.NullTime
	var edgeLabel, edgeMetadata sql.NullString
	var role sql.NullString

	err := rows.Scan(
		&node.ID, &node.Type, &subtype, &node.Content, &node.Embedding,
		&node.CreatedAt, &node.UpdatedAt, &node.AccessCount, &lastAccessed,
		&node.Tier, &node.Confidence, &nodeProvenance, &nodeMetadata,
		&edge.ID, &edge.Type, &edgeLabel, &edge.Weight, &edge.CreatedAt, &edgeMetadata,
		&role,
	)
	if err != nil {
		return nil, fmt.Errorf("scan connected node: %w", err)
	}

	node.Subtype = subtype.String
	if lastAccessed.Valid {
		node.LastAccessed = &lastAccessed.Time
	}
	if nodeProvenance.Valid {
		node.Provenance = []byte(nodeProvenance.String)
	}
	if nodeMetadata.Valid {
		node.Metadata = []byte(nodeMetadata.String)
	}

	edge.Label = edgeLabel.String
	if edgeMetadata.Valid {
		edge.Metadata = []byte(edgeMetadata.String)
	}

	result := &ConnectedNode{
		Node: &node,
		Role: MemberRole(role.String),
	}

	if includeEdge {
		result.Edge = &edge
	}

	return result, nil
}

// Verify SQLiteBackend implements Backend interface.
var _ Backend = (*SQLiteBackend)(nil)
