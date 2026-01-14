// Package hypergraph provides a hypergraph memory store with pluggable backends.
package hypergraph

import (
	"context"
)

// Backend defines the interface for hypergraph storage backends.
// Implementations include SQLiteBackend for production and InMemoryBackend for testing.
type Backend interface {
	// Node operations
	CreateNode(ctx context.Context, node *Node) error
	GetNode(ctx context.Context, id string) (*Node, error)
	UpdateNode(ctx context.Context, node *Node) error
	DeleteNode(ctx context.Context, id string) error
	ListNodes(ctx context.Context, filter NodeFilter) ([]*Node, error)
	CountNodes(ctx context.Context, filter NodeFilter) (int64, error)
	IncrementAccess(ctx context.Context, id string) error

	// Hyperedge operations
	CreateHyperedge(ctx context.Context, edge *Hyperedge) error
	GetHyperedge(ctx context.Context, id string) (*Hyperedge, error)
	UpdateHyperedge(ctx context.Context, edge *Hyperedge) error
	DeleteHyperedge(ctx context.Context, id string) error
	ListHyperedges(ctx context.Context, filter HyperedgeFilter) ([]*Hyperedge, error)

	// Membership operations
	AddMember(ctx context.Context, m Membership) error
	RemoveMember(ctx context.Context, hyperedgeID, nodeID string, role MemberRole) error
	GetMembers(ctx context.Context, hyperedgeID string) ([]Membership, error)
	GetMemberNodes(ctx context.Context, hyperedgeID string) ([]*Node, error)
	GetNodeHyperedges(ctx context.Context, nodeID string) ([]*Hyperedge, error)

	// Search operations
	SearchByContent(ctx context.Context, query string, opts SearchOptions) ([]*SearchResult, error)
	GetConnected(ctx context.Context, nodeID string, opts TraversalOptions) ([]*ConnectedNode, error)
	RecentNodes(ctx context.Context, limit int, tiers []Tier) ([]*Node, error)

	// Stats returns database statistics.
	Stats(ctx context.Context) (*Stats, error)

	// Close releases backend resources.
	Close() error
}

// ErrNotFound is returned when an entity is not found.
type ErrNotFound struct {
	Entity string
	ID     string
}

func (e *ErrNotFound) Error() string {
	return e.Entity + " not found: " + e.ID
}

// IsNotFound returns true if the error is an ErrNotFound.
func IsNotFound(err error) bool {
	_, ok := err.(*ErrNotFound)
	return ok
}
