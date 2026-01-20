package rlm

// RLMCoreCostBridge provides a bridge between the Go budget package
// and rlm-core's CostTracker. When enabled (RLM_USE_CORE=true),
// it provides shared cost calculation and token tracking logic.
//
// The Go budget package handles:
// - Budget limits and enforcement
// - REPL tracking and limits
// - Violation detection
//
// The rlm-core CostTracker provides:
// - Token usage accumulation
// - Per-model cost breakdown
// - Cost calculation for known models
//
// This bridge integrates them by using rlm-core for cost calculations
// while keeping budget enforcement in Go.

import (
	"log/slog"

	"github.com/rand/recurse/internal/rlmcore"
)

// RLMCoreCostBridge wraps rlm-core cost tracking functions.
type RLMCoreCostBridge struct {
	tracker *rlmcore.CostTracker
}

// NewRLMCoreCostBridge creates a new cost tracking bridge using rlm-core.
// Returns nil, nil if rlm-core is not available.
func NewRLMCoreCostBridge() (*RLMCoreCostBridge, error) {
	if !rlmcore.Available() {
		return nil, nil
	}

	tracker, err := rlmcore.NewCostTracker()
	if err != nil {
		return nil, err
	}

	slog.Info("rlm-core cost bridge initialized")
	return &RLMCoreCostBridge{
		tracker: tracker,
	}, nil
}

// NewRLMCoreCostBridgeFromJSON creates a bridge from serialized JSON state.
func NewRLMCoreCostBridgeFromJSON(jsonStr string) (*RLMCoreCostBridge, error) {
	if !rlmcore.Available() {
		return nil, nil
	}

	tracker, err := rlmcore.NewCostTrackerFromJSON(jsonStr)
	if err != nil {
		return nil, err
	}

	slog.Info("rlm-core cost bridge restored from JSON")
	return &RLMCoreCostBridge{
		tracker: tracker,
	}, nil
}

// Record records token usage from a completion.
// Pass a negative cost value if cost is unknown.
func (b *RLMCoreCostBridge) Record(model string, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens uint64, cost float64) error {
	return b.tracker.Record(model, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens, cost)
}

// Merge merges another bridge's tracker into this one.
func (b *RLMCoreCostBridge) Merge(other *RLMCoreCostBridge) error {
	if other == nil || other.tracker == nil {
		return nil
	}
	return b.tracker.Merge(other.tracker)
}

// TotalInputTokens returns the total input tokens tracked.
func (b *RLMCoreCostBridge) TotalInputTokens() uint64 {
	return b.tracker.TotalInputTokens()
}

// TotalOutputTokens returns the total output tokens tracked.
func (b *RLMCoreCostBridge) TotalOutputTokens() uint64 {
	return b.tracker.TotalOutputTokens()
}

// TotalCacheReadTokens returns the total cache read tokens tracked.
func (b *RLMCoreCostBridge) TotalCacheReadTokens() uint64 {
	return b.tracker.TotalCacheReadTokens()
}

// TotalCacheCreationTokens returns the total cache creation tokens tracked.
func (b *RLMCoreCostBridge) TotalCacheCreationTokens() uint64 {
	return b.tracker.TotalCacheCreationTokens()
}

// TotalCost returns the total cost in USD.
func (b *RLMCoreCostBridge) TotalCost() float64 {
	return b.tracker.TotalCost()
}

// RequestCount returns the total number of requests tracked.
func (b *RLMCoreCostBridge) RequestCount() uint64 {
	return b.tracker.RequestCount()
}

// ByModel returns the per-model cost breakdown.
func (b *RLMCoreCostBridge) ByModel() (map[string]rlmcore.ModelCost, error) {
	return b.tracker.ByModel()
}

// ToJSON serializes the tracker to JSON.
func (b *RLMCoreCostBridge) ToJSON() (string, error) {
	return b.tracker.ToJSON()
}

// Close releases all resources.
func (b *RLMCoreCostBridge) Close() error {
	if b.tracker != nil {
		b.tracker.Free()
		b.tracker = nil
	}
	return nil
}

// UseRLMCoreCost returns true if rlm-core cost tracking should be used.
func UseRLMCoreCost() bool {
	return rlmcore.Available()
}

// CalculateCostByName calculates cost using well-known model names via rlm-core.
// Supported: "claude-opus", "claude-sonnet", "claude-haiku", "gpt-4o", "gpt-4o-mini"
// Returns cost in USD, or -1.0 on error (unknown model or rlm-core unavailable).
func CalculateCostByName(modelName string, inputTokens, outputTokens uint64) float64 {
	return rlmcore.CalculateCostByName(modelName, inputTokens, outputTokens)
}

// EffectiveInputTokens calculates effective input tokens accounting for cache reads.
// Cache reads are typically 90% cheaper, so we count them at 10%.
func EffectiveInputTokens(inputTokens, cacheReadTokens uint64) uint64 {
	return rlmcore.EffectiveInputTokens(inputTokens, cacheReadTokens)
}

// ModelSpecJSON returns default model spec JSON for a well-known model.
// Supported: "claude-opus", "claude-sonnet", "claude-haiku", "gpt-4o", "gpt-4o-mini"
func ModelSpecJSON(modelName string) (string, error) {
	return rlmcore.ModelSpecJSON(modelName)
}
