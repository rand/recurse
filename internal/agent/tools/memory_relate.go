package tools

import (
	"context"
	_ "embed"
	"fmt"

	"charm.land/fantasy"
	"github.com/rand/recurse/internal/memory/tiers"
)

const MemoryRelateToolName = "memory_relate"

//go:embed memory_relate.md
var memoryRelateDescription []byte

// MemoryRelateParams defines parameters for creating relationships.
type MemoryRelateParams struct {
	// The relationship label (e.g., "contains", "calls", "depends_on")
	Label string `json:"label" description:"Relationship type (e.g., contains, calls, depends_on, references)"`

	// Subject node ID (the source of the relationship)
	SubjectID string `json:"subject_id" description:"ID of the subject node (source of relationship)"`

	// Object node ID (the target of the relationship)
	ObjectID string `json:"object_id" description:"ID of the object node (target of relationship)"`
}

// MemoryRelateResult contains the result of creating a relationship.
type MemoryRelateResult struct {
	EdgeID    string `json:"edge_id"`
	Label     string `json:"label"`
	SubjectID string `json:"subject_id"`
	ObjectID  string `json:"object_id"`
}

// NewMemoryRelateTool creates a tool for creating relationships between nodes.
func NewMemoryRelateTool(taskMemory *tiers.TaskMemory) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		MemoryRelateToolName,
		string(memoryRelateDescription),
		func(ctx context.Context, params MemoryRelateParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Label == "" {
				return fantasy.NewTextErrorResponse("label is required"), nil
			}
			if params.SubjectID == "" {
				return fantasy.NewTextErrorResponse("subject_id is required"), nil
			}
			if params.ObjectID == "" {
				return fantasy.NewTextErrorResponse("object_id is required"), nil
			}

			edge, err := taskMemory.Relate(ctx, params.Label, params.SubjectID, params.ObjectID)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("create relation: %w", err)
			}

			result := MemoryRelateResult{
				EdgeID:    edge.ID,
				Label:     params.Label,
				SubjectID: params.SubjectID,
				ObjectID:  params.ObjectID,
			}

			msg := fmt.Sprintf("Created relationship: %s -[%s]-> %s (edge: %s)",
				params.SubjectID[:8], params.Label, params.ObjectID[:8], edge.ID[:8])

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(msg),
				result,
			), nil
		})
}
