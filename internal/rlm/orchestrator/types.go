// Package orchestrator provides the modular RLM orchestration system.
package orchestrator

import (
	"time"

	"github.com/rand/recurse/internal/rlm/meta"
)

// AnalysisResult contains the RLM analysis of a user prompt.
type AnalysisResult struct {
	// OriginalPrompt is the user's original prompt.
	OriginalPrompt string

	// EnhancedPrompt is the prompt with additional context/instructions.
	EnhancedPrompt string

	// Decision is the meta-controller's orchestration decision.
	Decision *meta.Decision

	// ContextNeeds identifies what additional context would help.
	ContextNeeds *ContextNeeds

	// Routing suggests how to handle different parts of the task.
	Routing *TaskRouting

	// LoadedContext contains info about context externalized to REPL.
	LoadedContext *LoadedContext

	// AnalysisTime is how long analysis took.
	AnalysisTime time.Duration

	// ModelUsed is which model performed the analysis.
	ModelUsed string

	// ShouldDecompose indicates if the task should be broken into subtasks.
	ShouldDecompose bool

	// Subtasks are the decomposed subtasks if ShouldDecompose is true.
	Subtasks []Subtask

	// UseExternalizedContext indicates if REPL-based context is available.
	UseExternalizedContext bool
}

// ContextNeeds identifies what context would help with the task.
type ContextNeeds struct {
	// FilePatterns are glob patterns for files that might be relevant.
	FilePatterns []string

	// SearchQueries are code search queries to run.
	SearchQueries []string

	// ConceptsToUnderstand are high-level concepts that need exploration.
	ConceptsToUnderstand []string

	// MemoryQueries are queries to run against hypergraph memory.
	MemoryQueries []string

	// Priority indicates how important gathering this context is (0-10).
	Priority int
}

// TaskRouting suggests how to route different parts of the task.
type TaskRouting struct {
	// PrimaryModel is the recommended model for the main task.
	PrimaryModel string

	// PrimaryTier is the recommended tier for the main task.
	PrimaryTier meta.ModelTier

	// Reasoning explains the routing decision.
	Reasoning string

	// SubtaskRoutes maps subtask types to recommended models.
	SubtaskRoutes map[string]string
}

// Subtask represents a decomposed part of the original task.
type Subtask struct {
	// ID is a unique identifier for the subtask.
	ID string

	// Description describes what this subtask should accomplish.
	Description string

	// Type categorizes the subtask (e.g., "code_search", "analysis", "generation").
	Type string

	// RecommendedModel is the model best suited for this subtask.
	RecommendedModel string

	// RecommendedTier is the tier best suited for this subtask.
	RecommendedTier meta.ModelTier

	// Dependencies are IDs of subtasks that must complete first.
	Dependencies []string

	// Priority determines execution order (higher = sooner).
	Priority int
}

// LoadedContext represents context that has been externalized to the REPL.
type LoadedContext struct {
	// Variables maps variable names to their info.
	Variables map[string]VariableInfo

	// TotalTokens is the approximate token count of all loaded context.
	TotalTokens int

	// LoadTime is when the context was loaded.
	LoadTime time.Time
}

// VariableInfo describes a loaded context variable.
type VariableInfo struct {
	// Name is the Python variable name.
	Name string `json:"name"`

	// Type is the context type.
	Type ContextType `json:"type"`

	// Size is the approximate size in characters (alias: Length).
	Size int `json:"length"`

	// TokenEstimate is the approximate token count (alias: TokenCount).
	TokenEstimate int `json:"token_count"`

	// Description describes the context variable.
	Description string `json:"description,omitempty"`

	// Source indicates where the context came from.
	Source string `json:"source,omitempty"`

	// Metadata contains additional info about the source.
	Metadata map[string]any `json:"-"`
}

// Length returns the size for backwards compatibility.
func (v VariableInfo) Length() int {
	return v.Size
}

// TokenCount returns the token estimate for backwards compatibility.
func (v VariableInfo) TokenCount() int {
	return v.TokenEstimate
}

// ContextType categorizes loaded context.
type ContextType string

const (
	ContextTypeFile    ContextType = "file"
	ContextTypeSearch  ContextType = "search"
	ContextTypeMemory  ContextType = "memory"
	ContextTypeCustom  ContextType = "custom"
	ContextTypePrompt  ContextType = "prompt"
)

// ContextSource defines a source of context to load.
type ContextSource struct {
	// Name is the variable name to use in the REPL.
	Name string

	// Content is the actual content to load.
	Content string

	// Type categorizes the source.
	Type ContextType

	// Metadata contains additional source info.
	Metadata map[string]any
}

// ExecutionResult contains the outcome of an RLM execution.
type ExecutionResult struct {
	Task        string        `json:"task"`
	Response    string        `json:"response"`
	TotalTokens int           `json:"total_tokens"`
	StartTime   time.Time     `json:"start_time"`
	Duration    time.Duration `json:"duration"`
	Error       string        `json:"error,omitempty"`
}

// TraceEvent represents a trace event for the RLM trace view.
type TraceEvent struct {
	ID        string        `json:"id"`
	Type      string        `json:"type"`
	Action    string        `json:"action"`
	Details   string        `json:"details"`
	Tokens    int           `json:"tokens"`
	Duration  time.Duration `json:"duration"`
	Timestamp time.Time     `json:"timestamp"`
	Depth     int           `json:"depth"`
	ParentID  string        `json:"parent_id,omitempty"`
	Status    string        `json:"status"`
}

// TraceRecorder records RLM execution traces.
type TraceRecorder interface {
	RecordEvent(event TraceEvent) error
}
