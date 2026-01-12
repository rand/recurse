// Package rlm implements the Recursive Language Model orchestration system.
package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/rlm/decompose"
	"github.com/rand/recurse/internal/rlm/meta"
	"github.com/rand/recurse/internal/rlm/synthesize"
)

// Controller orchestrates RLM operations with integrated memory.
type Controller struct {
	meta        *meta.Controller
	store       *hypergraph.Store
	synthesizer synthesize.Synthesizer
	tracer      TraceRecorder
	config      ControllerConfig
	recovery    *RecoveryManager
}

// ControllerConfig configures the RLM controller.
type ControllerConfig struct {
	// MaxTokenBudget is the maximum tokens per request.
	MaxTokenBudget int

	// MaxRecursionDepth limits decomposition depth.
	MaxRecursionDepth int

	// MemoryQueryLimit is max results from memory queries.
	MemoryQueryLimit int

	// StoreDecisions persists decisions to memory graph.
	StoreDecisions bool

	// TraceEnabled enables trace recording.
	TraceEnabled bool

	// Recovery configures error recovery behavior.
	Recovery RecoveryConfig
}

// DefaultControllerConfig returns sensible defaults.
func DefaultControllerConfig() ControllerConfig {
	return ControllerConfig{
		MaxTokenBudget:    100000,
		MaxRecursionDepth: 5,
		MemoryQueryLimit:  10,
		StoreDecisions:    true,
		TraceEnabled:      true,
		Recovery:          DefaultRecoveryConfig(),
	}
}

// TraceRecorder records RLM execution traces.
type TraceRecorder interface {
	RecordEvent(event TraceEvent) error
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

// NewController creates a new RLM controller with memory integration.
func NewController(
	metaCtrl *meta.Controller,
	store *hypergraph.Store,
	cfg ControllerConfig,
) *Controller {
	return &Controller{
		meta:        metaCtrl,
		store:       store,
		synthesizer: synthesize.NewConcatenateSynthesizer(),
		config:      cfg,
		recovery:    NewRecoveryManager(cfg.Recovery),
	}
}

// SetTracer sets the trace recorder.
func (c *Controller) SetTracer(tracer TraceRecorder) {
	c.tracer = tracer
}

// Tracer returns the trace recorder.
func (c *Controller) Tracer() TraceRecorder {
	return c.tracer
}

// Execute runs the RLM orchestration loop for a task.
func (c *Controller) Execute(ctx context.Context, task string) (*ExecutionResult, error) {
	start := time.Now()
	result := &ExecutionResult{
		Task:      task,
		StartTime: start,
	}

	// Build initial state
	state := meta.State{
		Task:           task,
		ContextTokens:  estimateTokens(task),
		BudgetRemain:   c.config.MaxTokenBudget,
		RecursionDepth: 0,
		MaxDepth:       c.config.MaxRecursionDepth,
	}

	// Check memory for relevant context
	memoryHints, err := c.queryMemoryContext(ctx, task)
	if err == nil && len(memoryHints) > 0 {
		state.MemoryHints = memoryHints
	}

	// Run orchestration loop
	response, tokens, err := c.orchestrate(ctx, state, "")
	if err != nil {
		result.Error = err.Error()
		result.Duration = time.Since(start)
		return result, err
	}

	result.Response = response
	result.TotalTokens = tokens
	result.Duration = time.Since(start)

	// Store execution as decision node
	if c.config.StoreDecisions {
		c.storeExecution(ctx, task, response, tokens)
	}

	return result, nil
}

// orchestrate is the recursive orchestration loop with error recovery.
func (c *Controller) orchestrate(ctx context.Context, state meta.State, parentID string) (string, int, error) {
	eventID := generateID()
	totalTokens := 0

	// Reset retry counter for this orchestration
	c.recovery.ResetRetry()

	// Record trace event start
	if c.tracer != nil && c.config.TraceEnabled {
		c.tracer.RecordEvent(TraceEvent{
			ID:        eventID,
			Type:      "decision",
			Action:    "Evaluating: " + truncate(state.Task, 50),
			Timestamp: time.Now(),
			Depth:     state.RecursionDepth,
			ParentID:  parentID,
			Status:    "running",
		})
	}

	// Get decision from meta-controller
	decision, err := c.meta.Decide(ctx, state)
	if err != nil {
		return "", 0, fmt.Errorf("meta decision: %w", err)
	}

	// Execute the decision with error recovery
	response, totalTokens, err := c.executeWithRecovery(ctx, state, decision, eventID)

	// Record trace event completion
	if c.tracer != nil && c.config.TraceEnabled {
		status := "completed"
		if err != nil {
			status = "failed"
		}
		c.tracer.RecordEvent(TraceEvent{
			ID:       eventID,
			Type:     string(decision.Action),
			Action:   decision.Reasoning,
			Tokens:   totalTokens,
			Depth:    state.RecursionDepth,
			ParentID: parentID,
			Status:   status,
		})
	}

	return response, totalTokens, err
}

// executeWithRecovery executes a decision with retry and degradation support.
func (c *Controller) executeWithRecovery(ctx context.Context, state meta.State, decision *meta.Decision, eventID string) (string, int, error) {
	var response string
	var totalTokens int
	var err error

	for {
		// Execute the decision
		response, totalTokens, err = c.executeAction(ctx, state, decision, eventID)

		if err == nil {
			return response, totalTokens, nil
		}

		// Determine recovery action
		recoveryAction := c.recovery.DetermineAction(err, decision.Action, state)

		// Record the error
		c.recovery.RecordError(ErrorRecord{
			Category:   recoveryAction.Category,
			Action:     string(decision.Action),
			Error:      err.Error(),
			Context:    truncate(state.Task, 200),
			Recovered:  recoveryAction.ShouldRetry || recoveryAction.Degraded,
			RetryCount: c.recovery.RetryCount(),
			Degraded:   recoveryAction.Degraded,
		})

		// Record recovery attempt in trace
		if c.tracer != nil && c.config.TraceEnabled {
			c.tracer.RecordEvent(TraceEvent{
				ID:        generateID(),
				Type:      "recovery",
				Action:    recoveryAction.Message,
				Timestamp: time.Now(),
				Depth:     state.RecursionDepth,
				ParentID:  eventID,
				Status:    "attempting",
			})
		}

		// Handle retry
		if recoveryAction.ShouldRetry {
			c.recovery.IncrementRetry()

			// Add recovery context to state
			if recoveryAction.RetryPrompt != "" {
				state.Task = state.Task + "\n\n" + recoveryAction.RetryPrompt
			}

			// Brief delay before retry
			select {
			case <-ctx.Done():
				return "", totalTokens, ctx.Err()
			case <-time.After(c.config.Recovery.RetryDelay):
			}

			continue
		}

		// Handle degradation to direct mode
		if recoveryAction.Degraded {
			// Record degradation in trace
			if c.tracer != nil && c.config.TraceEnabled {
				c.tracer.RecordEvent(TraceEvent{
					ID:        generateID(),
					Type:      "degradation",
					Action:    "Falling back to direct mode",
					Details:   recoveryAction.Message,
					Timestamp: time.Now(),
					Depth:     state.RecursionDepth,
					ParentID:  eventID,
					Status:    "degraded",
				})
			}

			// Execute in direct mode
			response, totalTokens, err = c.executeDirect(ctx, state)
			if err != nil {
				return "", totalTokens, fmt.Errorf("degraded execution failed: %w", err)
			}
			return response, totalTokens, nil
		}

		// No recovery possible
		return "", totalTokens, WrapWithRecovery(err, recoveryAction)
	}
}

// executeAction executes the appropriate action based on decision.
func (c *Controller) executeAction(ctx context.Context, state meta.State, decision *meta.Decision, eventID string) (string, int, error) {
	switch decision.Action {
	case meta.ActionDirect:
		return c.executeDirect(ctx, state)

	case meta.ActionDecompose:
		return c.executeDecompose(ctx, state, decision, eventID)

	case meta.ActionMemoryQuery:
		return c.executeMemoryQuery(ctx, state, decision)

	case meta.ActionSubcall:
		return c.executeSubcall(ctx, state, decision, eventID)

	case meta.ActionSynthesize:
		return c.executeSynthesize(ctx, state)

	default:
		return "", 0, fmt.Errorf("unknown action: %s", decision.Action)
	}
}

// executeDirect answers directly using current context.
func (c *Controller) executeDirect(ctx context.Context, state meta.State) (string, int, error) {
	// In a full implementation, this would call the main LLM to generate a response.
	// For the CLI demonstration, we acknowledge the task was processed directly.
	tokens := estimateTokens(state.Task) * 2

	// Return the task as the response since it was deemed simple enough for direct handling
	// In production, this would be replaced with an actual LLM call
	response := state.Task
	return response, tokens, nil
}

// executeDecompose breaks task into subtasks and processes them.
func (c *Controller) executeDecompose(ctx context.Context, state meta.State, decision *meta.Decision, parentID string) (string, int, error) {
	totalTokens := 0

	// Select decomposer based on strategy (use Auto for smart selection)
	var decomposer decompose.Decomposer
	switch decision.Params.Strategy {
	case meta.StrategyFunction:
		decomposer = decompose.NewFunctionDecomposer("go") // Default to Go
	case meta.StrategyConcept:
		decomposer = decompose.NewConceptDecomposer(4000, 200)
	case meta.StrategyCustom:
		decomposer = decompose.Auto(state.Task) // Auto-detect
	default:
		decomposer = decompose.NewFileDecomposer()
	}

	// Decompose the task
	chunks, err := decomposer.Decompose(state.Task)
	if err != nil {
		return "", 0, fmt.Errorf("decompose: %w", err)
	}

	// Process each chunk recursively
	var results []synthesize.SubCallResult
	for i, chunk := range chunks {
		childState := meta.State{
			Task:           chunk.Content,
			ContextTokens:  estimateTokens(chunk.Content),
			BudgetRemain:   state.BudgetRemain / len(chunks),
			RecursionDepth: state.RecursionDepth + 1,
			MaxDepth:       state.MaxDepth,
		}

		response, tokens, err := c.orchestrate(ctx, childState, parentID)
		totalTokens += tokens

		result := synthesize.SubCallResult{
			ID:         fmt.Sprintf("chunk-%d", i),
			Name:       chunk.Name,
			Response:   response,
			TokensUsed: tokens,
		}
		if err != nil {
			result.Error = err.Error()
		}
		results = append(results, result)
	}

	// Synthesize results
	synthesized, err := c.synthesizer.Synthesize(ctx, state.Task, results)
	if err != nil {
		return "", totalTokens, fmt.Errorf("synthesize: %w", err)
	}

	return synthesized.Response, totalTokens + synthesized.TotalTokensUsed, nil
}

// executeMemoryQuery retrieves context from hypergraph memory.
func (c *Controller) executeMemoryQuery(ctx context.Context, state meta.State, decision *meta.Decision) (string, int, error) {
	query := decision.Params.Query
	if query == "" {
		query = state.Task
	}

	// Search memory by content
	nodes, err := c.store.ListNodes(ctx, hypergraph.NodeFilter{
		Types: []hypergraph.NodeType{
			hypergraph.NodeTypeFact,
			hypergraph.NodeTypeExperience,
		},
		Limit: c.config.MemoryQueryLimit,
	})
	if err != nil {
		return "", 0, fmt.Errorf("memory query: %w", err)
	}

	// Filter by content relevance (simple substring match for now)
	var relevant []*hypergraph.Node
	queryLower := strings.ToLower(query)
	for _, node := range nodes {
		if strings.Contains(strings.ToLower(node.Content), queryLower) {
			relevant = append(relevant, node)
		}
	}

	if len(relevant) == 0 {
		return "No relevant memory found.", 0, nil
	}

	// Format results
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d relevant memories:\n\n", len(relevant)))
	for _, node := range relevant {
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", node.Type, truncate(node.Content, 200)))
	}

	// Increment access counts
	for _, node := range relevant {
		c.store.IncrementAccess(ctx, node.ID)
	}

	return sb.String(), estimateTokens(sb.String()), nil
}

// executeSubcall processes a specific snippet with focused prompt.
func (c *Controller) executeSubcall(ctx context.Context, state meta.State, decision *meta.Decision, parentID string) (string, int, error) {
	snippet := decision.Params.Snippet
	if snippet == "" {
		snippet = state.Task
	}

	prompt := decision.Params.Prompt
	if prompt == "" {
		prompt = "Process the following"
	}

	// Create child state for subcall
	childState := meta.State{
		Task:           fmt.Sprintf("%s:\n\n%s", prompt, snippet),
		ContextTokens:  estimateTokens(snippet),
		BudgetRemain:   decision.Params.TokenBudget,
		RecursionDepth: state.RecursionDepth + 1,
		MaxDepth:       state.MaxDepth,
	}

	if childState.BudgetRemain == 0 {
		childState.BudgetRemain = state.BudgetRemain / 2
	}

	return c.orchestrate(ctx, childState, parentID)
}

// executeSynthesize combines partial results.
func (c *Controller) executeSynthesize(ctx context.Context, state meta.State) (string, int, error) {
	if len(state.PartialResults) == 0 {
		return "No partial results to synthesize.", 0, nil
	}

	// Convert partial results to SubCallResult format
	var results []synthesize.SubCallResult
	for i, partial := range state.PartialResults {
		results = append(results, synthesize.SubCallResult{
			ID:       fmt.Sprintf("partial-%d", i),
			Name:     fmt.Sprintf("Part %d", i+1),
			Response: partial,
		})
	}

	synthesized, err := c.synthesizer.Synthesize(ctx, state.Task, results)
	if err != nil {
		return "", 0, fmt.Errorf("synthesize: %w", err)
	}

	return synthesized.Response, synthesized.TotalTokensUsed, nil
}

// queryMemoryContext retrieves relevant context from memory.
func (c *Controller) queryMemoryContext(ctx context.Context, task string) ([]string, error) {
	// Query for relevant facts
	nodes, err := c.store.ListNodes(ctx, hypergraph.NodeFilter{
		Types:         []hypergraph.NodeType{hypergraph.NodeTypeFact},
		MinConfidence: 0.5,
		Limit:         5,
	})
	if err != nil {
		return nil, err
	}

	// Filter by relevance
	var hints []string
	taskLower := strings.ToLower(task)
	for _, node := range nodes {
		if strings.Contains(strings.ToLower(node.Content), taskLower[:min(len(taskLower), 20)]) {
			hints = append(hints, truncate(node.Content, 100))
		}
	}

	return hints, nil
}

// storeExecution saves the execution as a decision node.
func (c *Controller) storeExecution(ctx context.Context, task, response string, tokens int) error {
	node := hypergraph.NewNode(hypergraph.NodeTypeDecision, task)
	node.Subtype = "rlm_execution"

	metadata := map[string]any{
		"response": truncate(response, 500),
		"tokens":   tokens,
	}
	metadataJSON, _ := json.Marshal(metadata)
	node.Metadata = metadataJSON

	return c.store.CreateNode(ctx, node)
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

// Helper functions

func estimateTokens(text string) int {
	// Rough estimate: ~4 chars per token
	return len(text) / 4
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

var idCounter uint64

func generateID() string {
	idCounter++
	return fmt.Sprintf("rlm-%d-%d", time.Now().UnixNano(), idCounter)
}
