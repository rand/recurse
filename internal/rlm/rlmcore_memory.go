package rlm

import (
	"log/slog"

	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/rlmcore"
)

// RLMCoreMemoryBridge provides optional integration between hypergraph.Store
// and rlm-core's MemoryStore. When enabled (RLM_USE_CORE=true), it can be used
// for specific memory operations while the primary hypergraph.Store handles
// the full feature set.
type RLMCoreMemoryBridge struct {
	store *rlmcore.MemoryStore
}

// NewRLMCoreMemoryBridge creates a new memory bridge if rlm-core is available.
// Returns nil if rlm-core is not available.
func NewRLMCoreMemoryBridge(path string) *RLMCoreMemoryBridge {
	if !rlmcore.Available() {
		return nil
	}

	var store *rlmcore.MemoryStore
	var err error

	if path == "" {
		store, err = rlmcore.NewMemoryStoreInMemory()
	} else {
		store, err = rlmcore.OpenMemoryStore(path)
	}

	if err != nil {
		slog.Warn("Failed to create rlm-core memory store", "error", err)
		return nil
	}

	slog.Info("rlm-core memory bridge enabled", "path", path)
	return &RLMCoreMemoryBridge{store: store}
}

// Available returns true if the memory bridge is available.
func (b *RLMCoreMemoryBridge) Available() bool {
	return b != nil && b.store != nil
}

// Store returns the underlying rlm-core memory store.
func (b *RLMCoreMemoryBridge) Store() *rlmcore.MemoryStore {
	if b == nil {
		return nil
	}
	return b.store
}

// AddNode adds a node to the rlm-core store.
// This creates a simplified representation suitable for rlm-core.
func (b *RLMCoreMemoryBridge) AddNode(node *hypergraph.Node) error {
	if b == nil || b.store == nil {
		return nil
	}

	// Convert hypergraph node type to rlmcore node type
	nodeType := convertNodeType(node.Type)
	tier := convertTier(node.Tier)

	// Create rlm-core node
	coreNode := rlmcore.NewNodeFull(nodeType, node.Content, tier, node.Confidence)
	if node.Subtype != "" {
		_ = coreNode.SetSubtype(node.Subtype)
	}

	return b.store.AddNode(coreNode)
}

// SearchContent searches for nodes by content in the rlm-core store.
func (b *RLMCoreMemoryBridge) SearchContent(query string, limit int64) ([]string, error) {
	if b == nil || b.store == nil {
		return nil, nil
	}
	return b.store.SearchContent(query, limit)
}

// QueryByType queries nodes by type in the rlm-core store.
func (b *RLMCoreMemoryBridge) QueryByType(nodeType hypergraph.NodeType, limit int64) ([]string, error) {
	if b == nil || b.store == nil {
		return nil, nil
	}
	coreType := convertNodeType(nodeType)
	return b.store.QueryByType(coreType, limit)
}

// Stats returns statistics from the rlm-core store.
func (b *RLMCoreMemoryBridge) Stats() (*rlmcore.MemoryStats, error) {
	if b == nil || b.store == nil {
		return nil, nil
	}
	return b.store.Stats()
}

// Free releases the memory bridge resources.
func (b *RLMCoreMemoryBridge) Free() {
	if b == nil || b.store == nil {
		return
	}
	b.store.Free()
	b.store = nil
}

// convertNodeType converts hypergraph.NodeType to rlmcore.NodeType.
func convertNodeType(t hypergraph.NodeType) rlmcore.NodeType {
	switch t {
	case hypergraph.NodeTypeEntity:
		return rlmcore.NodeTypeEntity
	case hypergraph.NodeTypeFact:
		return rlmcore.NodeTypeFact
	case hypergraph.NodeTypeExperience:
		return rlmcore.NodeTypeExperience
	case hypergraph.NodeTypeDecision:
		return rlmcore.NodeTypeDecision
	case hypergraph.NodeTypeSnippet:
		return rlmcore.NodeTypeSnippet
	default:
		return rlmcore.NodeTypeFact
	}
}

// convertTier converts hypergraph.Tier to rlmcore.Tier.
func convertTier(t hypergraph.Tier) rlmcore.Tier {
	switch t {
	case hypergraph.TierTask:
		return rlmcore.TierTask
	case hypergraph.TierSession:
		return rlmcore.TierSession
	case hypergraph.TierLongterm:
		return rlmcore.TierLongTerm
	case hypergraph.TierArchive:
		return rlmcore.TierArchive
	default:
		return rlmcore.TierTask
	}
}

// UseRLMCoreMemory returns true if rlm-core should be used for memory operations.
func UseRLMCoreMemory() bool {
	return rlmcore.Available()
}
