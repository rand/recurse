package hypergraph

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// SearchResult represents a search result with relevance score.
type SearchResult struct {
	Node  *Node   `json:"node"`
	Score float64 `json:"score"`
}

// SearchOptions configures search behavior.
type SearchOptions struct {
	// Filter results by node attributes
	Types    []NodeType
	Tiers    []Tier
	Subtypes []string

	// Minimum confidence threshold
	MinConfidence float64

	// Maximum number of results
	Limit int

	// Query type for meta-evolution tracking (computational, retrieval, analytical, transformational)
	QueryType string
}

// SearchByContent performs a text search on node content.
// This is a simple LIKE-based search; for semantic search, use SearchByEmbedding.
func (s *Store) SearchByContent(ctx context.Context, query string, opts SearchOptions) ([]*SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sqlQuery := `
		SELECT id, type, subtype, content, embedding, created_at, updated_at,
		       access_count, last_accessed, tier, confidence, provenance, metadata
		FROM nodes WHERE content LIKE ?
	`
	args := []any{"%" + query + "%"}

	if len(opts.Types) > 0 {
		sqlQuery += " AND type IN (?" + repeatString(",?", len(opts.Types)-1) + ")"
		for _, t := range opts.Types {
			args = append(args, t)
		}
	}

	if len(opts.Tiers) > 0 {
		sqlQuery += " AND tier IN (?" + repeatString(",?", len(opts.Tiers)-1) + ")"
		for _, t := range opts.Tiers {
			args = append(args, t)
		}
	}

	if len(opts.Subtypes) > 0 {
		sqlQuery += " AND subtype IN (?" + repeatString(",?", len(opts.Subtypes)-1) + ")"
		for _, st := range opts.Subtypes {
			args = append(args, st)
		}
	}

	if opts.MinConfidence > 0 {
		sqlQuery += " AND confidence >= ?"
		args = append(args, opts.MinConfidence)
	}

	// Exclude archived nodes by default
	sqlQuery += " AND tier != 'archive'"

	sqlQuery += " ORDER BY access_count DESC, updated_at DESC"

	if opts.Limit > 0 {
		sqlQuery += " LIMIT ?"
		args = append(args, opts.Limit)
	}

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
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
		// Simple relevance: count occurrences
		score := float64(strings.Count(strings.ToLower(node.Content), strings.ToLower(query)))
		results = append(results, &SearchResult{Node: node, Score: score})
	}

	return results, rows.Err()
}

// TraversalDirection specifies the direction of graph traversal.
type TraversalDirection int

const (
	TraverseOutgoing TraversalDirection = iota // Follow edges where node is subject
	TraverseIncoming                           // Follow edges where node is object
	TraverseBoth                               // Follow edges in both directions
)

// TraversalOptions configures graph traversal.
type TraversalOptions struct {
	Direction   TraversalDirection
	MaxDepth    int             // Maximum traversal depth (0 = unlimited)
	EdgeTypes   []HyperedgeType // Filter by edge types
	NodeTypes   []NodeType      // Filter result nodes by type
	Tiers       []Tier          // Filter result nodes by tier
	MaxResults  int             // Maximum number of results
	IncludeEdge bool            // Include the connecting edge in results
}

// ConnectedNode represents a node found during traversal.
type ConnectedNode struct {
	Node  *Node      `json:"node"`
	Edge  *Hyperedge `json:"edge,omitempty"`
	Depth int        `json:"depth"`
	Role  MemberRole `json:"role"`
}

// GetConnected finds nodes connected to the given node via hyperedges.
func (s *Store) GetConnected(ctx context.Context, nodeID string, opts TraversalOptions) ([]*ConnectedNode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if opts.MaxDepth == 0 {
		opts.MaxDepth = 1 // Default to immediate neighbors
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

		// Find connected nodes at this level
		connected, err := s.getImmediateConnections(ctx, current.id, opts)
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

			// Add to queue for further traversal
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

// getImmediateConnections finds nodes directly connected to the given node.
func (s *Store) getImmediateConnections(ctx context.Context, nodeID string, opts TraversalOptions) ([]*ConnectedNode, error) {
	// Build query based on direction
	var query string
	var args []any

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

	args = append(args, nodeID, nodeID)

	// Direction filter
	switch opts.Direction {
	case TraverseOutgoing:
		query = baseSelect + " AND m1.role = 'subject'"
	case TraverseIncoming:
		query = baseSelect + " AND m1.role = 'object'"
	default:
		query = baseSelect
	}

	// Edge type filter
	if len(opts.EdgeTypes) > 0 {
		query += " AND h.type IN (?" + repeatString(",?", len(opts.EdgeTypes)-1) + ")"
		for _, t := range opts.EdgeTypes {
			args = append(args, t)
		}
	}

	// Node type filter
	if len(opts.NodeTypes) > 0 {
		query += " AND n.type IN (?" + repeatString(",?", len(opts.NodeTypes)-1) + ")"
		for _, t := range opts.NodeTypes {
			args = append(args, t)
		}
	}

	// Tier filter
	if len(opts.Tiers) > 0 {
		query += " AND n.tier IN (?" + repeatString(",?", len(opts.Tiers)-1) + ")"
		for _, t := range opts.Tiers {
			args = append(args, t)
		}
	}

	// Exclude archived by default
	query += " AND n.tier != 'archive'"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get connections: %w", err)
	}
	defer rows.Close()

	var results []*ConnectedNode
	for rows.Next() {
		conn, err := scanConnectedNode(rows, opts.IncludeEdge)
		if err != nil {
			return nil, err
		}
		results = append(results, conn)
	}

	return results, rows.Err()
}

func scanConnectedNode(rows *sql.Rows, includeEdge bool) (*ConnectedNode, error) {
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

// Subgraph represents a portion of the hypergraph.
type Subgraph struct {
	Nodes      []*Node      `json:"nodes"`
	Hyperedges []*Hyperedge `json:"hyperedges"`
	Membership []Membership `json:"membership"`
}

// GetSubgraph extracts a subgraph around the given node IDs.
func (s *Store) GetSubgraph(ctx context.Context, nodeIDs []string, depth int) (*Subgraph, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if depth == 0 {
		depth = 1
	}

	// Collect all node IDs to include
	allNodeIDs := make(map[string]bool)
	for _, id := range nodeIDs {
		allNodeIDs[id] = true
	}

	// Expand by traversing connections
	for d := 0; d < depth; d++ {
		currentIDs := make([]string, 0, len(allNodeIDs))
		for id := range allNodeIDs {
			currentIDs = append(currentIDs, id)
		}

		for _, id := range currentIDs {
			connected, err := s.getImmediateConnections(ctx, id, TraversalOptions{
				Direction: TraverseBoth,
				MaxDepth:  1,
			})
			if err != nil {
				return nil, err
			}
			for _, c := range connected {
				allNodeIDs[c.Node.ID] = true
			}
		}
	}

	// Fetch all nodes
	var nodes []*Node
	for id := range allNodeIDs {
		node, err := s.getNodeLocked(ctx, id)
		if err != nil {
			continue // Skip nodes that no longer exist
		}
		nodes = append(nodes, node)
	}

	// Find all hyperedges connecting these nodes
	edgeIDs := make(map[string]bool)
	var membership []Membership

	for id := range allNodeIDs {
		rows, err := s.db.QueryContext(ctx, `
			SELECT hyperedge_id, node_id, role, position
			FROM membership WHERE node_id = ?
		`, id)
		if err != nil {
			return nil, fmt.Errorf("get membership: %w", err)
		}

		for rows.Next() {
			var m Membership
			var role sql.NullString
			var position sql.NullInt64
			if err := rows.Scan(&m.HyperedgeID, &m.NodeID, &role, &position); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan membership: %w", err)
			}
			m.Role = MemberRole(role.String)
			m.Position = int(position.Int64)

			// Only include if both endpoints are in our subgraph
			edgeIDs[m.HyperedgeID] = true
			membership = append(membership, m)
		}
		rows.Close()
	}

	// Fetch hyperedges
	var hyperedges []*Hyperedge
	for id := range edgeIDs {
		row := s.db.QueryRowContext(ctx, `
			SELECT id, type, label, weight, created_at, metadata
			FROM hyperedges WHERE id = ?
		`, id)
		edge, err := scanHyperedge(row)
		if err != nil {
			continue // Skip edges that no longer exist
		}
		hyperedges = append(hyperedges, edge)
	}

	return &Subgraph{
		Nodes:      nodes,
		Hyperedges: hyperedges,
		Membership: membership,
	}, nil
}

// RecentNodes returns the most recently accessed or updated nodes.
func (s *Store) RecentNodes(ctx context.Context, limit int, tiers []Tier) ([]*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, type, subtype, content, embedding, created_at, updated_at,
		       access_count, last_accessed, tier, confidence, provenance, metadata
		FROM nodes WHERE tier != 'archive'
	`
	var args []any

	if len(tiers) > 0 {
		query += " AND tier IN (?" + repeatString(",?", len(tiers)-1) + ")"
		for _, t := range tiers {
			args = append(args, t)
		}
	}

	// Use julianday() to normalize different timestamp formats for correct sorting
	query += " ORDER BY julianday(COALESCE(last_accessed, updated_at)) DESC, id ASC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
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
