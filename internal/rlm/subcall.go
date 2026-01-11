package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rand/recurse/internal/rlm/meta"
)

// SubCallRouter routes sub-LLM calls from the REPL to appropriate models.
// This is the bridge between Python's llm_call() and the OpenRouter backend.
type SubCallRouter struct {
	client      meta.LLMClient
	models      []meta.ModelSpec
	selector    meta.ModelSelector
	maxDepth    int
	budgetLimit int

	// Statistics
	mu             sync.RWMutex
	totalCalls     int64
	totalTokens    int64
	totalCost      float64
	callsByTier    map[meta.ModelTier]int64
	callsByModel   map[string]int64
	errors         int64
	currentDepth   int32
	maxDepthSeen   int32
}

// SubCallConfig configures the sub-call router.
type SubCallConfig struct {
	// Client is the LLM client for making calls.
	Client meta.LLMClient

	// Models is the model catalog (uses defaults if empty).
	Models []meta.ModelSpec

	// MaxDepth limits recursion depth (default 5).
	MaxDepth int

	// BudgetLimit is the total token budget (default 100000).
	BudgetLimit int
}

// NewSubCallRouter creates a new sub-call router.
func NewSubCallRouter(cfg SubCallConfig) *SubCallRouter {
	models := cfg.Models
	if len(models) == 0 {
		models = meta.DefaultModels()
	}

	maxDepth := cfg.MaxDepth
	if maxDepth == 0 {
		maxDepth = 5
	}

	budgetLimit := cfg.BudgetLimit
	if budgetLimit == 0 {
		budgetLimit = 100000
	}

	return &SubCallRouter{
		client:       cfg.Client,
		models:       models,
		selector:     &meta.AdaptiveSelector{},
		maxDepth:     maxDepth,
		budgetLimit:  budgetLimit,
		callsByTier:  make(map[meta.ModelTier]int64),
		callsByModel: make(map[string]int64),
	}
}

// SubCallRequest represents a request from the REPL.
type SubCallRequest struct {
	// Prompt is the instruction for the sub-LLM.
	Prompt string `json:"prompt"`

	// Context is the content to process.
	Context string `json:"context"`

	// Model tier hint: "fast", "balanced", "powerful", "reasoning", or "auto".
	Model string `json:"model"`

	// Depth is the current recursion depth.
	Depth int `json:"depth"`

	// Budget is the remaining token budget for this call.
	Budget int `json:"budget"`

	// MaxTokens limits the response length.
	MaxTokens int `json:"max_tokens"`
}

// SubCallResponse is returned to the REPL.
type SubCallResponse struct {
	// Response is the LLM's text response.
	Response string `json:"response"`

	// ModelUsed is the model ID that handled the request.
	ModelUsed string `json:"model_used"`

	// TokensUsed is the estimated tokens consumed.
	TokensUsed int `json:"tokens_used"`

	// Cost is the estimated cost in USD.
	Cost float64 `json:"cost"`

	// Duration is how long the call took.
	Duration time.Duration `json:"duration"`

	// Error is set if the call failed.
	Error string `json:"error,omitempty"`
}

// Call makes a sub-LLM call with intelligent routing.
func (r *SubCallRouter) Call(ctx context.Context, req SubCallRequest) *SubCallResponse {
	start := time.Now()
	resp := &SubCallResponse{}

	// Check depth limit
	currentDepth := atomic.AddInt32(&r.currentDepth, 1)
	defer atomic.AddInt32(&r.currentDepth, -1)

	if int(currentDepth) > r.maxDepth {
		resp.Error = fmt.Sprintf("max recursion depth exceeded: %d > %d", currentDepth, r.maxDepth)
		atomic.AddInt64(&r.errors, 1)
		return resp
	}

	// Track max depth seen
	for {
		seen := atomic.LoadInt32(&r.maxDepthSeen)
		if currentDepth <= seen {
			break
		}
		if atomic.CompareAndSwapInt32(&r.maxDepthSeen, seen, currentDepth) {
			break
		}
	}

	// Build the full prompt with context
	fullPrompt := r.buildPrompt(req)

	// Select model based on tier hint and content
	model := r.selectModel(ctx, req)
	resp.ModelUsed = model.ID

	// Make the call
	if r.client == nil {
		resp.Error = "LLM client not configured"
		atomic.AddInt64(&r.errors, 1)
		return resp
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 1000
	}

	response, err := r.client.Complete(ctx, fullPrompt, maxTokens)
	if err != nil {
		resp.Error = fmt.Sprintf("LLM call failed: %v", err)
		atomic.AddInt64(&r.errors, 1)
		return resp
	}

	resp.Response = response
	resp.Duration = time.Since(start)

	// Estimate tokens and cost
	inputTokens := len(fullPrompt) / 4
	outputTokens := len(response) / 4
	resp.TokensUsed = inputTokens + outputTokens
	resp.Cost = (float64(inputTokens) * model.InputCost / 1_000_000) +
		(float64(outputTokens) * model.OutputCost / 1_000_000)

	// Update statistics
	r.recordStats(model, resp)

	return resp
}

// BatchCall makes multiple sub-LLM calls, potentially in parallel.
func (r *SubCallRouter) BatchCall(ctx context.Context, requests []SubCallRequest) []*SubCallResponse {
	responses := make([]*SubCallResponse, len(requests))

	// For now, execute sequentially (could parallelize with goroutines)
	// Sequential is safer for budget/depth tracking
	for i, req := range requests {
		responses[i] = r.Call(ctx, req)
	}

	return responses
}

// buildPrompt constructs the full prompt from request parts.
func (r *SubCallRouter) buildPrompt(req SubCallRequest) string {
	var sb strings.Builder

	// Add metadata for routing
	sb.WriteString(fmt.Sprintf("Recursion depth: %d\n", req.Depth))
	if req.Budget > 0 {
		sb.WriteString(fmt.Sprintf("Budget remaining: %d\n", req.Budget))
	}
	sb.WriteString("\n")

	// Add instruction
	sb.WriteString("## Task\n")
	sb.WriteString(req.Prompt)
	sb.WriteString("\n\n")

	// Add context if provided
	if req.Context != "" {
		sb.WriteString("## Context\n")
		sb.WriteString(req.Context)
		sb.WriteString("\n")
	}

	return sb.String()
}

// selectModel chooses the best model based on the request.
func (r *SubCallRouter) selectModel(ctx context.Context, req SubCallRequest) *meta.ModelSpec {
	// Honor explicit tier hint
	var targetTier meta.ModelTier
	switch strings.ToLower(req.Model) {
	case "fast":
		targetTier = meta.TierFast
	case "balanced":
		targetTier = meta.TierBalanced
	case "powerful":
		targetTier = meta.TierPowerful
	case "reasoning":
		targetTier = meta.TierReasoning
	default:
		// Auto-select based on content
		return r.autoSelectModel(ctx, req)
	}

	// Find model for specified tier
	for i := range r.models {
		if r.models[i].Tier == targetTier {
			return &r.models[i]
		}
	}

	// Fallback
	return &r.models[0]
}

// autoSelectModel uses the adaptive selector for smart routing.
func (r *SubCallRouter) autoSelectModel(ctx context.Context, req SubCallRequest) *meta.ModelSpec {
	task := req.Prompt + " " + req.Context[:min(500, len(req.Context))]

	budget := req.Budget
	if budget == 0 {
		budget = r.budgetLimit
	}

	if r.selector != nil {
		if spec := r.selector.SelectModel(ctx, task, budget, req.Depth); spec != nil {
			return spec
		}
	}

	// Default to fast for sub-calls
	for i := range r.models {
		if r.models[i].Tier == meta.TierFast {
			return &r.models[i]
		}
	}
	return &r.models[0]
}

// recordStats updates call statistics.
func (r *SubCallRouter) recordStats(model *meta.ModelSpec, resp *SubCallResponse) {
	r.mu.Lock()
	defer r.mu.Unlock()

	atomic.AddInt64(&r.totalCalls, 1)
	atomic.AddInt64(&r.totalTokens, int64(resp.TokensUsed))
	r.totalCost += resp.Cost
	r.callsByTier[model.Tier]++
	r.callsByModel[model.ID]++
}

// Stats returns current statistics.
func (r *SubCallRouter) Stats() SubCallStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tierCopy := make(map[meta.ModelTier]int64)
	for k, v := range r.callsByTier {
		tierCopy[k] = v
	}

	modelCopy := make(map[string]int64)
	for k, v := range r.callsByModel {
		modelCopy[k] = v
	}

	return SubCallStats{
		TotalCalls:   atomic.LoadInt64(&r.totalCalls),
		TotalTokens:  atomic.LoadInt64(&r.totalTokens),
		TotalCost:    r.totalCost,
		Errors:       atomic.LoadInt64(&r.errors),
		MaxDepthSeen: int(atomic.LoadInt32(&r.maxDepthSeen)),
		CallsByTier:  tierCopy,
		CallsByModel: modelCopy,
	}
}

// SubCallStats contains statistics about sub-calls.
type SubCallStats struct {
	TotalCalls   int64                   `json:"total_calls"`
	TotalTokens  int64                   `json:"total_tokens"`
	TotalCost    float64                 `json:"total_cost"`
	Errors       int64                   `json:"errors"`
	MaxDepthSeen int                     `json:"max_depth_seen"`
	CallsByTier  map[meta.ModelTier]int64 `json:"calls_by_tier"`
	CallsByModel map[string]int64        `json:"calls_by_model"`
}

// ToJSON returns stats as JSON string.
func (s SubCallStats) ToJSON() string {
	data, _ := json.MarshalIndent(s, "", "  ")
	return string(data)
}

// ResetStats clears all statistics.
func (r *SubCallRouter) ResetStats() {
	r.mu.Lock()
	defer r.mu.Unlock()

	atomic.StoreInt64(&r.totalCalls, 0)
	atomic.StoreInt64(&r.totalTokens, 0)
	atomic.StoreInt64(&r.errors, 0)
	atomic.StoreInt32(&r.maxDepthSeen, 0)
	r.totalCost = 0
	r.callsByTier = make(map[meta.ModelTier]int64)
	r.callsByModel = make(map[string]int64)
}

// SetClient sets the LLM client (used for late initialization).
func (r *SubCallRouter) SetClient(client meta.LLMClient) {
	r.client = client
}

// IsConfigured returns true if the router has an LLM client.
func (r *SubCallRouter) IsConfigured() bool {
	return r.client != nil
}
