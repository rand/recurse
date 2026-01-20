package rlmcore

import (
	core "github.com/rand/rlm-core/go/rlmcore"
)

// CostTracker wraps the rlm-core CostTracker for token/cost tracking.
type CostTracker struct {
	inner *core.CostTracker
}

// ModelCost is an alias for the rlmcore type.
type ModelCost = core.ModelCost

// NewCostTracker creates a new cost tracker.
// Returns nil, nil if rlm-core is not available.
func NewCostTracker() (*CostTracker, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner := core.NewCostTracker()
	return &CostTracker{inner: inner}, nil
}

// NewCostTrackerFromJSON deserializes a tracker from JSON.
// Returns nil, nil if rlm-core is not available.
func NewCostTrackerFromJSON(jsonStr string) (*CostTracker, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner, err := core.NewCostTrackerFromJSON(jsonStr)
	if err != nil {
		return nil, err
	}
	return &CostTracker{inner: inner}, nil
}

// Free releases the cost tracker resources.
func (ct *CostTracker) Free() {
	if ct.inner != nil {
		ct.inner.Free()
		ct.inner = nil
	}
}

// Record records token usage from a completion.
// Pass a negative cost value if cost is unknown.
func (ct *CostTracker) Record(model string, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens uint64, cost float64) error {
	return ct.inner.Record(model, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens, cost)
}

// Merge merges another tracker into this one.
func (ct *CostTracker) Merge(other *CostTracker) error {
	if other == nil || other.inner == nil {
		return nil
	}
	return ct.inner.Merge(other.inner)
}

// TotalInputTokens returns the total input tokens tracked.
func (ct *CostTracker) TotalInputTokens() uint64 {
	return ct.inner.TotalInputTokens()
}

// TotalOutputTokens returns the total output tokens tracked.
func (ct *CostTracker) TotalOutputTokens() uint64 {
	return ct.inner.TotalOutputTokens()
}

// TotalCacheReadTokens returns the total cache read tokens tracked.
func (ct *CostTracker) TotalCacheReadTokens() uint64 {
	return ct.inner.TotalCacheReadTokens()
}

// TotalCacheCreationTokens returns the total cache creation tokens tracked.
func (ct *CostTracker) TotalCacheCreationTokens() uint64 {
	return ct.inner.TotalCacheCreationTokens()
}

// TotalCost returns the total cost in USD.
func (ct *CostTracker) TotalCost() float64 {
	return ct.inner.TotalCost()
}

// RequestCount returns the total number of requests tracked.
func (ct *CostTracker) RequestCount() uint64 {
	return ct.inner.RequestCount()
}

// ByModel returns the per-model cost breakdown.
func (ct *CostTracker) ByModel() (map[string]ModelCost, error) {
	return ct.inner.ByModel()
}

// ToJSON serializes the tracker to JSON.
func (ct *CostTracker) ToJSON() (string, error) {
	return ct.inner.ToJSON()
}

// ============================================================================
// Cost Calculation Helpers
// ============================================================================

// CalculateCost calculates cost for given token usage with a model spec JSON.
// Returns cost in USD, or -1.0 on error.
func CalculateCost(modelJSON string, inputTokens, outputTokens uint64) float64 {
	if !Available() {
		return -1.0
	}
	return core.CalculateCost(modelJSON, inputTokens, outputTokens)
}

// CalculateCostByName calculates cost using well-known model names.
// Supported: "claude-opus", "claude-sonnet", "claude-haiku", "gpt-4o", "gpt-4o-mini"
// Returns cost in USD, or -1.0 on error (unknown model).
func CalculateCostByName(modelName string, inputTokens, outputTokens uint64) float64 {
	if !Available() {
		return -1.0
	}
	return core.CalculateCostByName(modelName, inputTokens, outputTokens)
}

// ModelSpecJSON returns default model spec JSON for a well-known model.
// Supported: "claude-opus", "claude-sonnet", "claude-haiku", "gpt-4o", "gpt-4o-mini"
func ModelSpecJSON(modelName string) (string, error) {
	if !Available() {
		return "", ErrNotAvailable
	}
	return core.ModelSpecJSON(modelName)
}

// EffectiveInputTokens calculates effective input tokens accounting for cache reads.
// Cache reads are typically 90% cheaper, so we count them at 10%.
func EffectiveInputTokens(inputTokens, cacheReadTokens uint64) uint64 {
	if !Available() {
		// Fall back to simple calculation
		if cacheReadTokens == 0 {
			return inputTokens
		}
		return inputTokens - cacheReadTokens + (cacheReadTokens / 10)
	}
	return core.EffectiveInputTokens(inputTokens, cacheReadTokens)
}
