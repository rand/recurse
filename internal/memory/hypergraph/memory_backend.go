package hypergraph

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// InMemoryBackend provides an in-memory implementation of Backend for testing.
type InMemoryBackend struct {
	mu          sync.RWMutex
	nodes       map[string]*Node
	hyperedges  map[string]*Hyperedge
	memberships []Membership
}

// NewInMemoryBackend creates a new in-memory backend.
func NewInMemoryBackend() *InMemoryBackend {
	return &InMemoryBackend{
		nodes:       make(map[string]*Node),
		hyperedges:  make(map[string]*Hyperedge),
		memberships: make([]Membership, 0),
	}
}

// CreateNode inserts a new node.
func (b *InMemoryBackend) CreateNode(ctx context.Context, node *Node) error {
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

	// Deep copy to prevent external mutations
	nodeCopy := *node
	b.nodes[node.ID] = &nodeCopy

	return nil
}

// GetNode retrieves a node by ID.
func (b *InMemoryBackend) GetNode(ctx context.Context, id string) (*Node, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	node, ok := b.nodes[id]
	if !ok {
		return nil, &ErrNotFound{Entity: "node", ID: id}
	}

	// Return a copy
	nodeCopy := *node
	return &nodeCopy, nil
}

// UpdateNode updates an existing node.
func (b *InMemoryBackend) UpdateNode(ctx context.Context, node *Node) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.nodes[node.ID]; !ok {
		return &ErrNotFound{Entity: "node", ID: node.ID}
	}

	node.UpdatedAt = time.Now().UTC()
	nodeCopy := *node
	b.nodes[node.ID] = &nodeCopy

	return nil
}

// DeleteNode removes a node by ID.
func (b *InMemoryBackend) DeleteNode(ctx context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.nodes[id]; !ok {
		return &ErrNotFound{Entity: "node", ID: id}
	}

	delete(b.nodes, id)

	// Remove associated memberships
	filtered := make([]Membership, 0)
	for _, m := range b.memberships {
		if m.NodeID != id {
			filtered = append(filtered, m)
		}
	}
	b.memberships = filtered

	return nil
}

// ListNodes retrieves nodes matching the filter.
func (b *InMemoryBackend) ListNodes(ctx context.Context, filter NodeFilter) ([]*Node, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var results []*Node
	for _, node := range b.nodes {
		if b.matchesNodeFilter(node, filter) {
			nodeCopy := *node
			results = append(results, &nodeCopy)
		}
	}

	// Sort by created_at DESC
	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})

	// Apply limit and offset
	if filter.Offset > 0 && filter.Offset < len(results) {
		results = results[filter.Offset:]
	} else if filter.Offset >= len(results) {
		return []*Node{}, nil
	}

	if filter.Limit > 0 && filter.Limit < len(results) {
		results = results[:filter.Limit]
	}

	return results, nil
}

func (b *InMemoryBackend) matchesNodeFilter(node *Node, filter NodeFilter) bool {
	if len(filter.Types) > 0 {
		found := false
		for _, t := range filter.Types {
			if node.Type == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(filter.Subtypes) > 0 {
		found := false
		for _, st := range filter.Subtypes {
			if node.Subtype == st {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(filter.Tiers) > 0 {
		found := false
		for _, t := range filter.Tiers {
			if node.Tier == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if filter.MinConfidence > 0 && node.Confidence < filter.MinConfidence {
		return false
	}

	return true
}

// CountNodes returns the count of nodes matching the filter.
func (b *InMemoryBackend) CountNodes(ctx context.Context, filter NodeFilter) (int64, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var count int64
	for _, node := range b.nodes {
		if b.matchesNodeFilter(node, filter) {
			count++
		}
	}

	return count, nil
}

// IncrementAccess increments the access count for a node.
func (b *InMemoryBackend) IncrementAccess(ctx context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	node, ok := b.nodes[id]
	if !ok {
		return &ErrNotFound{Entity: "node", ID: id}
	}

	node.AccessCount++
	now := time.Now().UTC()
	node.LastAccessed = &now

	return nil
}

// CreateHyperedge inserts a new hyperedge.
func (b *InMemoryBackend) CreateHyperedge(ctx context.Context, edge *Hyperedge) error {
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

	edgeCopy := *edge
	b.hyperedges[edge.ID] = &edgeCopy

	return nil
}

// GetHyperedge retrieves a hyperedge by ID.
func (b *InMemoryBackend) GetHyperedge(ctx context.Context, id string) (*Hyperedge, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	edge, ok := b.hyperedges[id]
	if !ok {
		return nil, &ErrNotFound{Entity: "hyperedge", ID: id}
	}

	edgeCopy := *edge
	return &edgeCopy, nil
}

// UpdateHyperedge updates an existing hyperedge.
func (b *InMemoryBackend) UpdateHyperedge(ctx context.Context, edge *Hyperedge) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.hyperedges[edge.ID]; !ok {
		return &ErrNotFound{Entity: "hyperedge", ID: edge.ID}
	}

	edgeCopy := *edge
	b.hyperedges[edge.ID] = &edgeCopy

	return nil
}

// DeleteHyperedge removes a hyperedge by ID.
func (b *InMemoryBackend) DeleteHyperedge(ctx context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.hyperedges[id]; !ok {
		return &ErrNotFound{Entity: "hyperedge", ID: id}
	}

	delete(b.hyperedges, id)

	// Remove associated memberships
	filtered := make([]Membership, 0)
	for _, m := range b.memberships {
		if m.HyperedgeID != id {
			filtered = append(filtered, m)
		}
	}
	b.memberships = filtered

	return nil
}

// ListHyperedges retrieves hyperedges matching the filter.
func (b *InMemoryBackend) ListHyperedges(ctx context.Context, filter HyperedgeFilter) ([]*Hyperedge, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var results []*Hyperedge
	for _, edge := range b.hyperedges {
		if b.matchesHyperedgeFilter(edge, filter) {
			edgeCopy := *edge
			results = append(results, &edgeCopy)
		}
	}

	// Sort by created_at DESC
	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})

	// Apply limit and offset
	if filter.Offset > 0 && filter.Offset < len(results) {
		results = results[filter.Offset:]
	} else if filter.Offset >= len(results) {
		return []*Hyperedge{}, nil
	}

	if filter.Limit > 0 && filter.Limit < len(results) {
		results = results[:filter.Limit]
	}

	return results, nil
}

func (b *InMemoryBackend) matchesHyperedgeFilter(edge *Hyperedge, filter HyperedgeFilter) bool {
	if len(filter.Types) > 0 {
		found := false
		for _, t := range filter.Types {
			if edge.Type == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if filter.MinWeight > 0 && edge.Weight < filter.MinWeight {
		return false
	}

	return true
}

// AddMember adds a node to a hyperedge.
func (b *InMemoryBackend) AddMember(ctx context.Context, m Membership) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.memberships = append(b.memberships, m)
	return nil
}

// RemoveMember removes a node from a hyperedge.
func (b *InMemoryBackend) RemoveMember(ctx context.Context, hyperedgeID, nodeID string, role MemberRole) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	found := false
	filtered := make([]Membership, 0)
	for _, m := range b.memberships {
		if m.HyperedgeID == hyperedgeID && m.NodeID == nodeID && m.Role == role {
			found = true
			continue
		}
		filtered = append(filtered, m)
	}

	if !found {
		return &ErrNotFound{Entity: "membership", ID: hyperedgeID + ":" + nodeID}
	}

	b.memberships = filtered
	return nil
}

// GetMembers returns all members of a hyperedge.
func (b *InMemoryBackend) GetMembers(ctx context.Context, hyperedgeID string) ([]Membership, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var results []Membership
	for _, m := range b.memberships {
		if m.HyperedgeID == hyperedgeID {
			results = append(results, m)
		}
	}

	// Sort by position
	sort.Slice(results, func(i, j int) bool {
		return results[i].Position < results[j].Position
	})

	return results, nil
}

// GetMemberNodes returns all nodes in a hyperedge.
func (b *InMemoryBackend) GetMemberNodes(ctx context.Context, hyperedgeID string) ([]*Node, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var nodeIDs []string
	positionMap := make(map[string]int)
	for _, m := range b.memberships {
		if m.HyperedgeID == hyperedgeID {
			nodeIDs = append(nodeIDs, m.NodeID)
			positionMap[m.NodeID] = m.Position
		}
	}

	var results []*Node
	for _, id := range nodeIDs {
		if node, ok := b.nodes[id]; ok {
			nodeCopy := *node
			results = append(results, &nodeCopy)
		}
	}

	// Sort by position
	sort.Slice(results, func(i, j int) bool {
		return positionMap[results[i].ID] < positionMap[results[j].ID]
	})

	return results, nil
}

// GetNodeHyperedges returns all hyperedges a node belongs to.
func (b *InMemoryBackend) GetNodeHyperedges(ctx context.Context, nodeID string) ([]*Hyperedge, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	edgeIDs := make(map[string]bool)
	for _, m := range b.memberships {
		if m.NodeID == nodeID {
			edgeIDs[m.HyperedgeID] = true
		}
	}

	var results []*Hyperedge
	for id := range edgeIDs {
		if edge, ok := b.hyperedges[id]; ok {
			edgeCopy := *edge
			results = append(results, &edgeCopy)
		}
	}

	return results, nil
}

// SearchByContent performs text search on node content.
func (b *InMemoryBackend) SearchByContent(ctx context.Context, query string, opts SearchOptions) ([]*SearchResult, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	queryLower := strings.ToLower(query)
	var results []*SearchResult

	for _, node := range b.nodes {
		// Skip archived nodes
		if node.Tier == TierArchive {
			continue
		}

		if !b.matchesSearchOptions(node, opts) {
			continue
		}

		contentLower := strings.ToLower(node.Content)
		if strings.Contains(contentLower, queryLower) {
			nodeCopy := *node
			score := float64(strings.Count(contentLower, queryLower))
			results = append(results, &SearchResult{
				Node:  &nodeCopy,
				Score: score,
			})
		}
	}

	// Sort by access_count DESC, updated_at DESC
	sort.Slice(results, func(i, j int) bool {
		if results[i].Node.AccessCount != results[j].Node.AccessCount {
			return results[i].Node.AccessCount > results[j].Node.AccessCount
		}
		return results[i].Node.UpdatedAt.After(results[j].Node.UpdatedAt)
	})

	if opts.Limit > 0 && opts.Limit < len(results) {
		results = results[:opts.Limit]
	}

	return results, nil
}

func (b *InMemoryBackend) matchesSearchOptions(node *Node, opts SearchOptions) bool {
	if len(opts.Types) > 0 {
		found := false
		for _, t := range opts.Types {
			if node.Type == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(opts.Tiers) > 0 {
		found := false
		for _, t := range opts.Tiers {
			if node.Tier == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(opts.Subtypes) > 0 {
		found := false
		for _, st := range opts.Subtypes {
			if node.Subtype == st {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if opts.MinConfidence > 0 && node.Confidence < opts.MinConfidence {
		return false
	}

	return true
}

// GetConnected finds nodes connected via hyperedges.
func (b *InMemoryBackend) GetConnected(ctx context.Context, nodeID string, opts TraversalOptions) ([]*ConnectedNode, error) {
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

		connected := b.getImmediateConnectionsLocked(current.id, opts)
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

func (b *InMemoryBackend) getImmediateConnectionsLocked(nodeID string, opts TraversalOptions) []*ConnectedNode {
	// Find hyperedges containing this node
	type edgeMembership struct {
		edge *Hyperedge
		role MemberRole
	}
	edgeMemberships := make(map[string]edgeMembership)

	for _, m := range b.memberships {
		if m.NodeID == nodeID {
			// Apply direction filter
			switch opts.Direction {
			case TraverseOutgoing:
				if m.Role != RoleSubject {
					continue
				}
			case TraverseIncoming:
				if m.Role != RoleObject {
					continue
				}
			}

			if edge, ok := b.hyperedges[m.HyperedgeID]; ok {
				// Apply edge type filter
				if len(opts.EdgeTypes) > 0 {
					found := false
					for _, t := range opts.EdgeTypes {
						if edge.Type == t {
							found = true
							break
						}
					}
					if !found {
						continue
					}
				}
				edgeMemberships[m.HyperedgeID] = edgeMembership{edge: edge, role: m.Role}
			}
		}
	}

	// Find connected nodes
	var results []*ConnectedNode
	seen := make(map[string]bool)

	for edgeID, em := range edgeMemberships {
		for _, m := range b.memberships {
			if m.HyperedgeID == edgeID && m.NodeID != nodeID && !seen[m.NodeID] {
				node, ok := b.nodes[m.NodeID]
				if !ok {
					continue
				}

				// Skip archived
				if node.Tier == TierArchive {
					continue
				}

				// Apply node type filter
				if len(opts.NodeTypes) > 0 {
					found := false
					for _, t := range opts.NodeTypes {
						if node.Type == t {
							found = true
							break
						}
					}
					if !found {
						continue
					}
				}

				// Apply tier filter
				if len(opts.Tiers) > 0 {
					found := false
					for _, t := range opts.Tiers {
						if node.Tier == t {
							found = true
							break
						}
					}
					if !found {
						continue
					}
				}

				seen[m.NodeID] = true
				nodeCopy := *node
				conn := &ConnectedNode{
					Node: &nodeCopy,
					Role: m.Role,
				}
				if opts.IncludeEdge {
					edgeCopy := *em.edge
					conn.Edge = &edgeCopy
				}
				results = append(results, conn)
			}
		}
	}

	return results
}

// RecentNodes returns recently accessed nodes.
func (b *InMemoryBackend) RecentNodes(ctx context.Context, limit int, tiers []Tier) ([]*Node, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var results []*Node
	for _, node := range b.nodes {
		if node.Tier == TierArchive {
			continue
		}

		if len(tiers) > 0 {
			found := false
			for _, t := range tiers {
				if node.Tier == t {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		nodeCopy := *node
		results = append(results, &nodeCopy)
	}

	// Sort by last_accessed or updated_at DESC
	sort.Slice(results, func(i, j int) bool {
		ti := results[i].UpdatedAt
		if results[i].LastAccessed != nil {
			ti = *results[i].LastAccessed
		}
		tj := results[j].UpdatedAt
		if results[j].LastAccessed != nil {
			tj = *results[j].LastAccessed
		}
		return ti.After(tj)
	})

	if limit > 0 && limit < len(results) {
		results = results[:limit]
	}

	return results, nil
}

// Stats returns statistics.
func (b *InMemoryBackend) Stats(ctx context.Context) (*Stats, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	stats := &Stats{
		NodeCount:      int64(len(b.nodes)),
		HyperedgeCount: int64(len(b.hyperedges)),
		NodesByTier:    make(map[string]int64),
		NodesByType:    make(map[string]int64),
	}

	for _, node := range b.nodes {
		stats.NodesByTier[string(node.Tier)]++
		stats.NodesByType[string(node.Type)]++
	}

	return stats, nil
}

// Close is a no-op for in-memory backend.
func (b *InMemoryBackend) Close() error {
	return nil
}

// Verify InMemoryBackend implements Backend interface.
var _ Backend = (*InMemoryBackend)(nil)
