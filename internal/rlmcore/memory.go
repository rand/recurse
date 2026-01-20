package rlmcore

import (
	core "github.com/rand/rlm-core/go/rlmcore"
)

// MemoryStore wraps the rlm-core SqliteMemoryStore.
type MemoryStore struct {
	inner *core.MemoryStore
}

// NewMemoryStoreInMemory creates an in-memory store.
func NewMemoryStoreInMemory() (*MemoryStore, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner, err := core.NewMemoryStoreInMemory()
	if err != nil {
		return nil, err
	}
	return &MemoryStore{inner: inner}, nil
}

// OpenMemoryStore opens or creates a persistent store.
func OpenMemoryStore(path string) (*MemoryStore, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner, err := core.OpenMemoryStore(path)
	if err != nil {
		return nil, err
	}
	return &MemoryStore{inner: inner}, nil
}

// AddNode adds a node to the store.
func (s *MemoryStore) AddNode(node *Node) error {
	return s.inner.AddNode(node.inner)
}

// GetNode retrieves a node by ID.
func (s *MemoryStore) GetNode(id string) (*Node, error) {
	inner, err := s.inner.GetNode(id)
	if err != nil {
		return nil, err
	}
	if inner == nil {
		return nil, nil // Not found
	}
	return &Node{inner: inner}, nil
}

// UpdateNode updates a node in the store.
func (s *MemoryStore) UpdateNode(node *Node) error {
	return s.inner.UpdateNode(node.inner)
}

// DeleteNode deletes a node from the store.
func (s *MemoryStore) DeleteNode(id string) (bool, error) {
	return s.inner.DeleteNode(id)
}

// QueryByType queries nodes by type.
func (s *MemoryStore) QueryByType(nodeType core.NodeType, limit int64) ([]string, error) {
	return s.inner.QueryByType(nodeType, limit)
}

// QueryByTier queries nodes by tier.
func (s *MemoryStore) QueryByTier(tier core.Tier, limit int64) ([]string, error) {
	return s.inner.QueryByTier(tier, limit)
}

// SearchContent searches nodes by content.
func (s *MemoryStore) SearchContent(query string, limit int64) ([]string, error) {
	return s.inner.SearchContent(query, limit)
}

// Promote promotes nodes to the next tier.
func (s *MemoryStore) Promote(nodeIDs []string, reason string) ([]string, error) {
	return s.inner.Promote(nodeIDs, reason)
}

// Decay applies confidence decay to nodes.
func (s *MemoryStore) Decay(factor, minConfidence float64) ([]string, error) {
	return s.inner.Decay(factor, minConfidence)
}

// Stats returns store statistics.
func (s *MemoryStore) Stats() (*core.MemoryStats, error) {
	return s.inner.Stats()
}

// AddEdge adds a hyperedge to the store.
func (s *MemoryStore) AddEdge(edge *HyperEdge) error {
	return s.inner.AddEdge(edge.inner)
}

// GetEdgesForNode retrieves all edges connected to a node.
func (s *MemoryStore) GetEdgesForNode(nodeID string) ([]core.EdgeData, error) {
	return s.inner.GetEdgesForNode(nodeID)
}

// Free releases store resources.
func (s *MemoryStore) Free() {
	if s.inner != nil {
		s.inner.Free()
		s.inner = nil
	}
}

// Node wraps the rlm-core Node.
type Node struct {
	inner *core.Node
}

// Re-export node types from core
type NodeType = core.NodeType

const (
	NodeTypeEntity     = core.NodeTypeEntity
	NodeTypeFact       = core.NodeTypeFact
	NodeTypeExperience = core.NodeTypeExperience
	NodeTypeDecision   = core.NodeTypeDecision
	NodeTypeSnippet    = core.NodeTypeSnippet
)

// Re-export tier types from core
type Tier = core.Tier

const (
	TierTask     = core.TierTask
	TierSession  = core.TierSession
	TierLongTerm = core.TierLongTerm
	TierArchive  = core.TierArchive
)

// NewNode creates a new node.
func NewNode(nodeType core.NodeType, content string) *Node {
	return &Node{inner: core.NewNode(nodeType, content)}
}

// NewNodeFull creates a node with all parameters.
func NewNodeFull(nodeType core.NodeType, content string, tier core.Tier, confidence float64) *Node {
	return &Node{inner: core.NewNodeFull(nodeType, content, tier, confidence)}
}

// ID returns the node ID.
func (n *Node) ID() string {
	return n.inner.ID()
}

// Content returns the node content.
func (n *Node) Content() string {
	return n.inner.Content()
}

// Type returns the node type.
func (n *Node) Type() core.NodeType {
	return n.inner.Type()
}

// Tier returns the node tier.
func (n *Node) Tier() core.Tier {
	return n.inner.Tier()
}

// Confidence returns the node confidence.
func (n *Node) Confidence() float64 {
	return n.inner.Confidence()
}

// Subtype returns the node subtype.
func (n *Node) Subtype() string {
	return n.inner.Subtype()
}

// SetSubtype sets the node subtype.
func (n *Node) SetSubtype(subtype string) error {
	return n.inner.SetSubtype(subtype)
}

// SetTier sets the node tier.
func (n *Node) SetTier(tier core.Tier) error {
	return n.inner.SetTier(tier)
}

// SetConfidence sets the node confidence.
func (n *Node) SetConfidence(confidence float64) error {
	return n.inner.SetConfidence(confidence)
}

// RecordAccess records an access to the node.
func (n *Node) RecordAccess() error {
	return n.inner.RecordAccess()
}

// AccessCount returns the number of times the node has been accessed.
func (n *Node) AccessCount() uint64 {
	return n.inner.AccessCount()
}

// IsDecayed returns true if the node's confidence is below the threshold.
func (n *Node) IsDecayed(minConfidence float64) bool {
	return n.inner.IsDecayed(minConfidence)
}

// AgeHours returns the node's age in hours.
func (n *Node) AgeHours() int64 {
	return n.inner.AgeHours()
}

// ToJSON serializes the node to JSON.
func (n *Node) ToJSON() (string, error) {
	return n.inner.ToJSON()
}

// NodeFromJSON deserializes a node from JSON.
func NodeFromJSON(jsonStr string) (*Node, error) {
	inner, err := core.NodeFromJSON(jsonStr)
	if err != nil {
		return nil, err
	}
	return &Node{inner: inner}, nil
}

// Free releases node resources.
func (n *Node) Free() {
	if n.inner != nil {
		n.inner.Free()
		n.inner = nil
	}
}

// HyperEdge wraps the rlm-core HyperEdge.
type HyperEdge struct {
	inner *core.HyperEdge
}

// NewHyperEdge creates a new hyperedge.
func NewHyperEdge(edgeType string) (*HyperEdge, error) {
	inner, err := core.NewHyperEdge(edgeType)
	if err != nil {
		return nil, err
	}
	return &HyperEdge{inner: inner}, nil
}

// NewBinaryEdge creates a binary edge between two nodes.
func NewBinaryEdge(edgeType, subjectID, objectID, label string) (*HyperEdge, error) {
	inner, err := core.NewBinaryEdge(edgeType, subjectID, objectID, label)
	if err != nil {
		return nil, err
	}
	return &HyperEdge{inner: inner}, nil
}

// ID returns the edge ID.
func (e *HyperEdge) ID() string {
	return e.inner.ID()
}

// Type returns the edge type.
func (e *HyperEdge) Type() string {
	return e.inner.Type()
}

// Label returns the edge label.
func (e *HyperEdge) Label() string {
	return e.inner.Label()
}

// Weight returns the edge weight.
func (e *HyperEdge) Weight() float64 {
	return e.inner.Weight()
}

// NodeIDs returns the IDs of all member nodes.
func (e *HyperEdge) NodeIDs() ([]string, error) {
	return e.inner.NodeIDs()
}

// Contains returns true if the node is a member of this edge.
func (e *HyperEdge) Contains(nodeID string) bool {
	return e.inner.Contains(nodeID)
}

// Free releases edge resources.
func (e *HyperEdge) Free() {
	if e.inner != nil {
		e.inner.Free()
		e.inner = nil
	}
}

// Re-export types from core
type MemoryStats = core.MemoryStats
type EdgeData = core.EdgeData
