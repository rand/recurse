package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/memory/tiers"
)

const MemoryStoreToolName = "memory_store"

//go:embed memory_store.md
var memoryStoreDescription []byte

// MemoryStoreParams defines parameters for storing to memory.
type MemoryStoreParams struct {
	// Type of memory to store: fact, entity, snippet, decision, experience
	Type string `json:"type" description:"Type of memory: fact, entity, snippet, decision, or experience"`

	// Content to store
	Content string `json:"content" description:"The content to store in memory"`

	// Optional fields depending on type
	Subtype      string   `json:"subtype,omitempty" description:"Subtype for entities (e.g., file, function, class)"`
	File         string   `json:"file,omitempty" description:"Source file for snippets"`
	Line         int      `json:"line,omitempty" description:"Line number for snippets"`
	Confidence   float64  `json:"confidence,omitempty" description:"Confidence level (0-1) for facts"`
	Rationale    string   `json:"rationale,omitempty" description:"Rationale for decisions"`
	Alternatives []string `json:"alternatives,omitempty" description:"Alternatives considered for decisions"`
	Outcome      string   `json:"outcome,omitempty" description:"Outcome for experiences"`
	Success      bool     `json:"success,omitempty" description:"Whether experience was successful"`
}

// MemoryStoreResult contains the result of storing to memory.
type MemoryStoreResult struct {
	NodeID  string `json:"node_id"`
	Type    string `json:"type"`
	Created bool   `json:"created"` // false if deduplicated
}

// NewMemoryStoreTool creates a tool for storing to memory.
func NewMemoryStoreTool(taskMemory *tiers.TaskMemory) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		MemoryStoreToolName,
		string(memoryStoreDescription),
		func(ctx context.Context, params MemoryStoreParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Content == "" {
				return fantasy.NewTextErrorResponse("content is required"), nil
			}

			var node *hypergraph.Node
			var err error
			var nodeType string

			switch strings.ToLower(params.Type) {
			case "fact":
				confidence := params.Confidence
				if confidence == 0 {
					confidence = 1.0
				}
				node, err = taskMemory.AddFact(ctx, params.Content, confidence)
				nodeType = "fact"

			case "entity":
				subtype := params.Subtype
				if subtype == "" {
					subtype = "unknown"
				}
				node, err = taskMemory.AddEntity(ctx, params.Content, subtype)
				nodeType = "entity"

			case "snippet":
				file := params.File
				if file == "" {
					file = "unknown"
				}
				node, err = taskMemory.AddSnippet(ctx, params.Content, file, params.Line)
				nodeType = "snippet"

			case "decision":
				node, err = taskMemory.AddDecision(ctx, params.Content, params.Rationale, params.Alternatives)
				nodeType = "decision"

			case "experience":
				node, err = taskMemory.AddExperience(ctx, params.Content, params.Outcome, params.Success)
				nodeType = "experience"

			default:
				return fantasy.NewTextErrorResponse(
					fmt.Sprintf("unknown memory type: %s (valid: fact, entity, snippet, decision, experience)", params.Type),
				), nil
			}

			if err != nil {
				// Check if it's a capacity warning (non-fatal)
				if strings.Contains(err.Error(), "capacity") {
					// Node was still created, just warn
					result := MemoryStoreResult{
						NodeID:  node.ID,
						Type:    nodeType,
						Created: true,
					}
					return fantasy.WithResponseMetadata(
						fantasy.NewTextResponse(fmt.Sprintf("Stored %s (id: %s). Warning: %s", nodeType, node.ID, err.Error())),
						result,
					), nil
				}
				return fantasy.ToolResponse{}, fmt.Errorf("store %s: %w", nodeType, err)
			}

			// Check if this was a new node or deduplicated
			created := node.AccessCount == 0

			result := MemoryStoreResult{
				NodeID:  node.ID,
				Type:    nodeType,
				Created: created,
			}

			var msg string
			if created {
				msg = fmt.Sprintf("Stored %s (id: %s)", nodeType, node.ID)
			} else {
				msg = fmt.Sprintf("Found existing %s (id: %s, access_count: %d)", nodeType, node.ID, node.AccessCount)
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(msg),
				result,
			), nil
		})
}
