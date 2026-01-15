package hallucination

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMemoryGate(t *testing.T) {
	backend := NewMockBackend(0.8)
	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultMemoryGateConfig()

	gate := NewMemoryGate(detector, config)

	assert.NotNil(t, gate)
	assert.Equal(t, config.Enabled, gate.config.Enabled)
	assert.Equal(t, config.MinConfidence, gate.config.MinConfidence)
}

func TestMemoryGate_Disabled(t *testing.T) {
	backend := NewMockBackend(0.8)
	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultMemoryGateConfig()
	config.Enabled = false

	gate := NewMemoryGate(detector, config)
	ctx := context.Background()

	result, err := gate.VerifyFact(ctx, "Test fact", "evidence", 0.9)

	require.NoError(t, err)
	assert.True(t, result.Allowed)
	assert.Equal(t, 0.9, result.AdjustedConfidence)
	assert.Equal(t, StatusUnverifiable, result.Status)
	assert.Contains(t, result.Explanation, "disabled")
}

func TestMemoryGate_GroundedFact(t *testing.T) {
	// p0 = 0.3, p1 = 0.9 -> well supported
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[EVIDENCE REMOVED]" || context == "[No specific evidence provided]" {
			return 0.3
		}
		return 0.9
	})

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultMemoryGateConfig()
	config.Enabled = true

	gate := NewMemoryGate(detector, config)
	ctx := context.Background()

	result, err := gate.VerifyFact(ctx, "The Earth is round", "Scientific evidence", 0.8)

	require.NoError(t, err)
	assert.True(t, result.Allowed)
	assert.Equal(t, StatusGrounded, result.Status)
	assert.LessOrEqual(t, result.BudgetGap, 0.0)
}

func TestMemoryGate_UnsupportedFact_Rejected(t *testing.T) {
	// p0 = 0.7, p1 = 0.7 -> evidence doesn't help
	backend := NewMockBackend(0.7)

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultMemoryGateConfig()
	config.Enabled = true
	config.RejectUnsupported = true
	config.FlagThresholdBits = 0.1 // Low threshold to trigger rejection

	gate := NewMemoryGate(detector, config)
	ctx := context.Background()

	result, err := gate.VerifyFact(ctx, "Unsupported claim", "Some context", 0.95)

	require.NoError(t, err)
	assert.False(t, result.Allowed)
	assert.Equal(t, StatusUnsupported, result.Status)
	assert.Greater(t, result.BudgetGap, 0.0)

	// Check that fact was logged
	stats := gate.Stats()
	assert.Equal(t, int64(1), stats.FactsRejected)
}

func TestMemoryGate_UnsupportedFact_Adjusted(t *testing.T) {
	// p0 = 0.7, p1 = 0.7 -> evidence doesn't help, but we don't reject
	backend := NewMockBackend(0.7)

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultMemoryGateConfig()
	config.Enabled = true
	config.RejectUnsupported = false // Don't reject, just adjust

	gate := NewMemoryGate(detector, config)
	ctx := context.Background()

	result, err := gate.VerifyFact(ctx, "Partially supported", "Some context", 0.95)

	require.NoError(t, err)
	assert.True(t, result.Allowed)
	assert.Less(t, result.AdjustedConfidence, 0.95, "confidence should be reduced")

	stats := gate.Stats()
	assert.Equal(t, int64(1), stats.FactsAdjusted)
}

func TestMemoryGate_ContradictedFact(t *testing.T) {
	// p1 = 0.1 -> evidence contradicts
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[No specific evidence provided]" {
			return 0.5
		}
		return 0.1 // Low with evidence - contradiction
	})

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultMemoryGateConfig()
	config.Enabled = true

	gate := NewMemoryGate(detector, config)
	ctx := context.Background()

	result, err := gate.VerifyFact(ctx, "The sky is green", "The sky appears blue", 0.9)

	require.NoError(t, err)
	assert.False(t, result.Allowed)
	assert.Equal(t, StatusContradicted, result.Status)
	assert.Equal(t, 0.0, result.AdjustedConfidence)

	// Check rejected facts log
	rejected := gate.RejectedFacts()
	require.Len(t, rejected, 1)
	assert.Equal(t, "The sky is green", rejected[0].Content)
	assert.Equal(t, StatusContradicted, rejected[0].Status)
}

func TestMemoryGate_BelowMinConfidence(t *testing.T) {
	// Fact passes verification but adjusted confidence is too low
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[No specific evidence provided]" {
			return 0.4
		}
		return 0.5 // Modest support
	})

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultMemoryGateConfig()
	config.Enabled = true
	config.MinConfidence = 0.7 // High threshold
	config.RejectUnsupported = false

	gate := NewMemoryGate(detector, config)
	ctx := context.Background()

	result, err := gate.VerifyFact(ctx, "Weak claim", "Weak evidence", 0.6)

	require.NoError(t, err)
	assert.False(t, result.Allowed, "should be rejected due to low adjusted confidence")
}

func TestMemoryGate_GracefulDegradation(t *testing.T) {
	// Create a slow backend that will timeout
	backend := &slowMockBackend{
		delay:       100 * time.Millisecond,
		probability: 0.8,
	}

	detectorConfig := DefaultDetectorConfig()
	detectorConfig.Timeout = 10 * time.Millisecond // Very short timeout
	detector := NewDetector(backend, detectorConfig)

	config := DefaultMemoryGateConfig()
	config.Enabled = true
	config.GracefulDegradation = true
	config.ConfidenceReductionOnError = 0.5
	config.MinConfidence = 0.3 // Low threshold so reduced confidence still passes

	gate := NewMemoryGate(detector, config)
	ctx := context.Background()

	result, err := gate.VerifyFact(ctx, "Test fact", "evidence", 0.9)

	require.NoError(t, err)
	assert.True(t, result.Allowed, "should allow with graceful degradation")
	assert.Equal(t, 0.45, result.AdjustedConfidence, "should reduce confidence on error")
	assert.NotNil(t, result.Error)
}

func TestMemoryGate_Stats(t *testing.T) {
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[No specific evidence provided]" {
			return 0.3
		}
		return 0.9
	})

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultMemoryGateConfig()
	config.Enabled = true

	gate := NewMemoryGate(detector, config)
	ctx := context.Background()

	// Verify multiple facts
	gate.VerifyFact(ctx, "Fact 1", "evidence", 0.8)
	gate.VerifyFact(ctx, "Fact 2", "evidence", 0.8)
	gate.VerifyFact(ctx, "Fact 3", "evidence", 0.8)

	stats := gate.Stats()
	assert.Equal(t, int64(3), stats.FactsVerified)
}

func TestMemoryGate_RejectedFactsLog(t *testing.T) {
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[No specific evidence provided]" {
			return 0.5
		}
		return 0.1 // Contradiction
	})

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultMemoryGateConfig()
	config.Enabled = true
	config.LogRejectedFacts = true
	config.MaxRejectedLog = 3

	gate := NewMemoryGate(detector, config)
	ctx := context.Background()

	// Reject multiple facts
	gate.VerifyFact(ctx, "Bad fact 1", "evidence", 0.9)
	gate.VerifyFact(ctx, "Bad fact 2", "evidence", 0.9)
	gate.VerifyFact(ctx, "Bad fact 3", "evidence", 0.9)
	gate.VerifyFact(ctx, "Bad fact 4", "evidence", 0.9)

	// Should only keep last 3
	rejected := gate.RejectedFacts()
	assert.Len(t, rejected, 3)
	assert.Equal(t, "Bad fact 2", rejected[0].Content)
	assert.Equal(t, "Bad fact 4", rejected[2].Content)

	// Clear log
	gate.ClearRejectedLog()
	assert.Empty(t, gate.RejectedFacts())
}

func TestDefaultMemoryGateConfig(t *testing.T) {
	cfg := DefaultMemoryGateConfig()

	assert.False(t, cfg.Enabled)
	assert.Equal(t, 0.5, cfg.MinConfidence)
	assert.True(t, cfg.RejectUnsupported)
	assert.Equal(t, 2.0, cfg.FlagThresholdBits)
	assert.True(t, cfg.LogRejectedFacts)
	assert.Equal(t, 100, cfg.MaxRejectedLog)
	assert.True(t, cfg.GracefulDegradation)
	assert.Equal(t, 0.5, cfg.ConfidenceReductionOnError)
}

func TestMemoryGate_NilDetector(t *testing.T) {
	config := DefaultMemoryGateConfig()
	config.Enabled = true

	gate := NewMemoryGate(nil, config)
	ctx := context.Background()

	result, err := gate.VerifyFact(ctx, "Test fact", "evidence", 0.9)

	require.NoError(t, err)
	assert.True(t, result.Allowed)
	assert.Equal(t, 0.9, result.AdjustedConfidence)
}
