package rlm

import (
	"context"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/memory/tiers"
	"github.com/rand/recurse/internal/rlm/repl"
)

// MemoryCallbackHandler implements repl.MemoryCallbackHandler using the hypergraph memory system.
type MemoryCallbackHandler struct {
	store   *hypergraph.Store
	taskMem *tiers.TaskMemory
	ctx     context.Context
}

// NewMemoryCallbackHandler creates a new memory callback handler.
func NewMemoryCallbackHandler(store *hypergraph.Store) *MemoryCallbackHandler {
	var taskMem *tiers.TaskMemory
	if store != nil {
		taskMem = tiers.NewTaskMemory(store, tiers.DefaultTaskConfig())
	}
	return &MemoryCallbackHandler{
		store:   store,
		taskMem: taskMem,
		ctx:     context.Background(),
	}
}

// WithContext returns a copy with the given context.
func (h *MemoryCallbackHandler) WithContext(ctx context.Context) *MemoryCallbackHandler {
	return &MemoryCallbackHandler{
		store:   h.store,
		taskMem: h.taskMem,
		ctx:     ctx,
	}
}

// SetFactVerifier sets the fact verifier for hallucination detection.
// [SPEC-08.15] Integrates memory gate with handler.
func (h *MemoryCallbackHandler) SetFactVerifier(verifier tiers.FactVerifier) {
	if h.taskMem != nil {
		h.taskMem.SetFactVerifier(verifier)
	}
}

// TaskMemory returns the underlying task memory for direct access.
func (h *MemoryCallbackHandler) TaskMemory() *tiers.TaskMemory {
	return h.taskMem
}

// MemoryQuery searches memory for relevant nodes.
func (h *MemoryCallbackHandler) MemoryQuery(query string, limit int) ([]repl.MemoryNode, error) {
	if h.taskMem == nil {
		return []repl.MemoryNode{}, nil
	}

	if limit <= 0 {
		limit = 10
	}

	results, err := h.taskMem.Search(h.ctx, query, limit)
	if err != nil {
		return nil, err
	}

	nodes := make([]repl.MemoryNode, 0, len(results))
	for _, r := range results {
		nodes = append(nodes, repl.MemoryNode{
			ID:         r.Node.ID,
			Type:       string(r.Node.Type),
			Content:    r.Node.Content,
			Confidence: r.Node.Confidence,
			Tier:       string(r.Node.Tier),
		})
	}
	return nodes, nil
}

// MemoryAddFact adds a fact to memory.
func (h *MemoryCallbackHandler) MemoryAddFact(content string, confidence float64) (string, error) {
	if h.taskMem == nil {
		return "", nil
	}

	node, err := h.taskMem.AddFact(h.ctx, content, confidence)
	if err != nil {
		return "", err
	}
	return node.ID, nil
}

// MemoryAddFactWithEvidence adds a fact to memory with evidence for verification.
// [SPEC-08.15] Verifies fact before storage if hallucination detection is enabled.
func (h *MemoryCallbackHandler) MemoryAddFactWithEvidence(content string, confidence float64, evidence string) (string, error) {
	if h.taskMem == nil {
		return "", nil
	}

	node, err := h.taskMem.AddFactWithEvidence(h.ctx, content, confidence, evidence)
	if err != nil {
		return "", err
	}
	return node.ID, nil
}

// MemoryAddExperience adds an experience to memory.
func (h *MemoryCallbackHandler) MemoryAddExperience(content, outcome string, success bool) (string, error) {
	if h.taskMem == nil {
		return "", nil
	}

	node, err := h.taskMem.AddExperience(h.ctx, content, outcome, success)
	if err != nil {
		return "", err
	}
	return node.ID, nil
}

// MemoryAddExperienceWithOptions adds an experience with extended metadata.
// [SPEC-09.02] Supports rich context for better memory retrieval.
func (h *MemoryCallbackHandler) MemoryAddExperienceWithOptions(params repl.MemoryAddExperienceParams) (string, error) {
	if h.taskMem == nil {
		return "", nil
	}

	var opts *tiers.ExperienceOptions
	if params.HasExtendedFields() {
		opts = &tiers.ExperienceOptions{
			TaskDescription:  params.TaskDescription,
			Approach:         params.Approach,
			FilesModified:    params.FilesModified,
			BlockersHit:      params.BlockersHit,
			InsightsGained:   params.InsightsGained,
			RelatedDecisions: params.RelatedDecisions,
		}
		if params.DurationSecs > 0 {
			opts.Duration = time.Duration(params.DurationSecs) * time.Second
		}
	}

	node, err := h.taskMem.AddExperienceWithOptions(h.ctx, params.Content, params.Outcome, params.Success, opts)
	if err != nil {
		return "", err
	}
	return node.ID, nil
}

// MemoryGetContext retrieves recent context nodes.
func (h *MemoryCallbackHandler) MemoryGetContext(limit int) ([]repl.MemoryNode, error) {
	if h.taskMem == nil {
		return []repl.MemoryNode{}, nil
	}

	if limit <= 0 {
		limit = 10
	}

	contextNodes, err := h.taskMem.GetContext(h.ctx, limit)
	if err != nil {
		return nil, err
	}

	nodes := make([]repl.MemoryNode, 0, len(contextNodes))
	for _, n := range contextNodes {
		nodes = append(nodes, repl.MemoryNode{
			ID:         n.ID,
			Type:       string(n.Type),
			Content:    n.Content,
			Confidence: n.Confidence,
			Tier:       string(n.Tier),
		})
	}
	return nodes, nil
}

// MemoryRelate creates a relationship between nodes.
func (h *MemoryCallbackHandler) MemoryRelate(label, subjectID, objectID string) (string, error) {
	if h.taskMem == nil {
		return "", nil
	}

	edge, err := h.taskMem.Relate(h.ctx, label, subjectID, objectID)
	if err != nil {
		return "", err
	}
	return edge.ID, nil
}

// Verify interface compliance
var _ repl.MemoryCallbackHandler = (*MemoryCallbackHandler)(nil)
