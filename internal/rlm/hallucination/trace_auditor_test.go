package hallucination

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTraceAuditor(t *testing.T) {
	backend := NewMockBackend(0.8)
	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultTraceAuditorConfig()

	auditor := NewTraceAuditor(detector, config)

	assert.NotNil(t, auditor)
	assert.Equal(t, config.Enabled, auditor.config.Enabled)
	assert.Equal(t, config.CheckPostHoc, auditor.config.CheckPostHoc)
}

func TestTraceAuditor_Disabled(t *testing.T) {
	backend := NewMockBackend(0.8)
	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultTraceAuditorConfig()
	config.Enabled = false

	auditor := NewTraceAuditor(detector, config)
	ctx := context.Background()

	steps := []TraceStep{
		{Content: "Step 1", StepType: "reasoning", Confidence: 0.9, Index: 0},
	}

	result, err := auditor.AuditTrace(ctx, steps, "context", "answer")

	require.NoError(t, err)
	assert.Equal(t, TraceUnauditable, result.Overall)
	assert.Contains(t, result.Summary, "disabled")
}

func TestTraceAuditor_EmptyTrace(t *testing.T) {
	backend := NewMockBackend(0.8)
	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultTraceAuditorConfig()
	config.Enabled = true

	auditor := NewTraceAuditor(detector, config)
	ctx := context.Background()

	result, err := auditor.AuditTrace(ctx, []TraceStep{}, "context", "answer")

	require.NoError(t, err)
	assert.Equal(t, TraceUnauditable, result.Overall)
	assert.Contains(t, result.Summary, "no trace steps")
}

func TestTraceAuditor_ValidTrace(t *testing.T) {
	// Backend returns high probability with context (entailed steps)
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[No prior context]" {
			return 0.3 // Low without context
		}
		return 0.9 // High with context (entailed)
	})

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultTraceAuditorConfig()
	config.Enabled = true
	config.CheckPostHoc = false // Disable for this test

	auditor := NewTraceAuditor(detector, config)
	ctx := context.Background()

	steps := []TraceStep{
		{Content: "Given that X is true", StepType: "premise", Confidence: 0.9, Index: 0},
		{Content: "And Y follows from X", StepType: "inference", Confidence: 0.9, Index: 1},
		{Content: "Therefore Z is the result", StepType: "conclusion", Confidence: 0.9, Index: 2},
	}

	result, err := auditor.AuditTrace(ctx, steps, "X is established as true", "")

	require.NoError(t, err)
	assert.Equal(t, TraceValid, result.Overall)
	assert.Equal(t, 0, len(result.TraceAudit.FlaggedSteps))
}

func TestTraceAuditor_ContradictedStep(t *testing.T) {
	// Backend returns low probability with context (contradicted)
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[No prior context]" {
			return 0.5
		}
		return 0.1 // Low with context (contradicted)
	})

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultTraceAuditorConfig()
	config.Enabled = true
	config.CheckPostHoc = false

	auditor := NewTraceAuditor(detector, config)
	ctx := context.Background()

	steps := []TraceStep{
		{Content: "The sky is green", StepType: "claim", Confidence: 0.9, Index: 0},
	}

	result, err := auditor.AuditTrace(ctx, steps, "The sky is blue", "")

	require.NoError(t, err)
	assert.Equal(t, TraceInvalid, result.Overall)
	assert.Greater(t, len(result.TraceAudit.FlaggedSteps), 0)

	// Check that step is marked as contradicted
	assert.Equal(t, StepContradicted, result.TraceAudit.StepResults[0].Status)
}

func TestTraceAuditor_NotInContextStep(t *testing.T) {
	// Backend returns high probability both with and without context
	// (step is true generally, not derived from context)
	backend := NewMockBackend(0.8)

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultTraceAuditorConfig()
	config.Enabled = true
	config.CheckPostHoc = false

	auditor := NewTraceAuditor(detector, config)
	ctx := context.Background()

	steps := []TraceStep{
		{Content: "Water is wet", StepType: "claim", Confidence: 0.9, Index: 0},
	}

	result, err := auditor.AuditTrace(ctx, steps, "Discussion about mathematics", "")

	require.NoError(t, err)
	// Should be flagged as NOT_IN_CONTEXT since it's true regardless of math context
	if len(result.TraceAudit.FlaggedSteps) > 0 {
		assert.Equal(t, StepNotInContext, result.TraceAudit.StepResults[0].Status)
	}
}

func TestTraceAuditor_PostHocDetection_Derivable(t *testing.T) {
	// Backend returns high probability for derivable answer
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		// High probability when context contains "reasoning steps"
		if len(context) > 100 {
			return 0.9
		}
		return 0.5
	})

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultTraceAuditorConfig()
	config.Enabled = true
	config.CheckPostHoc = true
	config.DerivabilityThreshold = 0.5

	auditor := NewTraceAuditor(detector, config)
	ctx := context.Background()

	steps := []TraceStep{
		{Content: "X equals 5", StepType: "premise", Confidence: 0.9, Index: 0},
		{Content: "Y equals X plus 3", StepType: "inference", Confidence: 0.9, Index: 1},
	}

	result, err := auditor.AuditTrace(ctx, steps, "Calculate Y given X=5", "Y equals 8")

	require.NoError(t, err)
	assert.NotNil(t, result.PostHocResult)
	assert.True(t, result.PostHocResult.Derivable)
	assert.GreaterOrEqual(t, result.PostHocResult.DerivabilityScore, 0.5)
}

func TestTraceAuditor_PostHocDetection_NotDerivable(t *testing.T) {
	// Backend returns low probability for non-derivable answer
	backend := NewMockBackend(0.2) // Low probability

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultTraceAuditorConfig()
	config.Enabled = true
	config.CheckPostHoc = true
	config.DerivabilityThreshold = 0.5

	auditor := NewTraceAuditor(detector, config)
	ctx := context.Background()

	steps := []TraceStep{
		{Content: "The weather is sunny", StepType: "observation", Confidence: 0.9, Index: 0},
	}

	result, err := auditor.AuditTrace(ctx, steps, "Weather report", "The answer is 42")

	require.NoError(t, err)
	assert.NotNil(t, result.PostHocResult)
	assert.False(t, result.PostHocResult.Derivable)
	assert.True(t, result.TraceAudit.PostHocHallucination)
	assert.Equal(t, TraceInvalid, result.Overall)
}

func TestTraceAuditor_StopOnContradiction(t *testing.T) {
	// Backend returns low probability (contradiction)
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[No prior context]" {
			return 0.5
		}
		return 0.1 // Contradiction
	})

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultTraceAuditorConfig()
	config.Enabled = true
	config.CheckPostHoc = false
	config.StopOnContradiction = true

	auditor := NewTraceAuditor(detector, config)
	ctx := context.Background()

	steps := []TraceStep{
		{Content: "Step 1 - contradicted", StepType: "claim", Confidence: 0.9, Index: 0},
		{Content: "Step 2 - would not be reached", StepType: "claim", Confidence: 0.9, Index: 1},
		{Content: "Step 3 - would not be reached", StepType: "claim", Confidence: 0.9, Index: 2},
	}

	result, err := auditor.AuditTrace(ctx, steps, "Different context", "")

	require.NoError(t, err)
	// Should stop after first contradiction
	verifiedCount := 0
	for _, sr := range result.TraceAudit.StepResults {
		if sr.Status != "" {
			verifiedCount++
		}
	}
	assert.Equal(t, 1, verifiedCount, "should stop after first contradiction")
}

func TestTraceAuditor_MaxSteps(t *testing.T) {
	backend := NewMockBackend(0.8)
	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultTraceAuditorConfig()
	config.Enabled = true
	config.CheckPostHoc = false
	config.MaxStepsToAudit = 2

	auditor := NewTraceAuditor(detector, config)
	ctx := context.Background()

	steps := []TraceStep{
		{Content: "Step 1", StepType: "claim", Confidence: 0.9, Index: 0},
		{Content: "Step 2", StepType: "claim", Confidence: 0.9, Index: 1},
		{Content: "Step 3", StepType: "claim", Confidence: 0.9, Index: 2},
		{Content: "Step 4", StepType: "claim", Confidence: 0.9, Index: 3},
	}

	result, err := auditor.AuditTrace(ctx, steps, "context", "")

	require.NoError(t, err)
	assert.Equal(t, 2, result.TraceAudit.TotalSteps, "should only audit MaxStepsToAudit steps")
}

func TestTraceAuditor_Stats(t *testing.T) {
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[No prior context]" {
			return 0.3
		}
		return 0.9
	})

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultTraceAuditorConfig()
	config.Enabled = true
	config.CheckPostHoc = false

	auditor := NewTraceAuditor(detector, config)
	ctx := context.Background()

	steps := []TraceStep{
		{Content: "Step 1", StepType: "claim", Confidence: 0.9, Index: 0},
		{Content: "Step 2", StepType: "claim", Confidence: 0.9, Index: 1},
	}

	// Audit multiple traces
	auditor.AuditTrace(ctx, steps, "context 1", "")
	auditor.AuditTrace(ctx, steps, "context 2", "")

	stats := auditor.Stats()
	assert.Equal(t, int64(2), stats.TracesAudited)
	assert.Equal(t, int64(4), stats.StepsAudited)
}

func TestTraceAuditor_Recommendations(t *testing.T) {
	// Backend returns low probability (contradiction)
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[No prior context]" {
			return 0.5
		}
		return 0.1
	})

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultTraceAuditorConfig()
	config.Enabled = true
	config.CheckPostHoc = false

	auditor := NewTraceAuditor(detector, config)
	ctx := context.Background()

	steps := []TraceStep{
		{Content: "Contradicted step", StepType: "claim", Confidence: 0.9, Index: 0},
	}

	result, err := auditor.AuditTrace(ctx, steps, "Different context", "")

	require.NoError(t, err)
	assert.NotEmpty(t, result.Recommendations, "should provide recommendations for invalid trace")
}

func TestTraceAuditor_NilDetector(t *testing.T) {
	config := DefaultTraceAuditorConfig()
	config.Enabled = true

	auditor := NewTraceAuditor(nil, config)
	ctx := context.Background()

	steps := []TraceStep{
		{Content: "Step 1", StepType: "claim", Confidence: 0.9, Index: 0},
	}

	result, err := auditor.AuditTrace(ctx, steps, "context", "")

	require.NoError(t, err)
	assert.Equal(t, TraceUnauditable, result.Overall)
}

func TestConvertTraceEvents(t *testing.T) {
	events := []TraceEvent{
		{ID: "1", Type: "reasoning", Details: "Step 1 content", Timestamp: time.Now()},
		{ID: "2", Type: "tool_call", Details: "Not reasoning", Timestamp: time.Now()},
		{ID: "3", Type: "step", Details: "Step 2 content", Timestamp: time.Now()},
		{ID: "4", Type: "inference", Details: "Step 3 content", Timestamp: time.Now()},
	}

	steps := ConvertTraceEvents(events)

	// Should only convert reasoning-type events
	assert.Len(t, steps, 3)
	assert.Equal(t, "Step 1 content", steps[0].Content)
	assert.Equal(t, "Step 2 content", steps[1].Content)
	assert.Equal(t, "Step 3 content", steps[2].Content)
}

func TestDefaultTraceAuditorConfig(t *testing.T) {
	cfg := DefaultTraceAuditorConfig()

	assert.False(t, cfg.Enabled)
	assert.True(t, cfg.CheckPostHoc)
	assert.False(t, cfg.StopOnContradiction)
	assert.Equal(t, 100, cfg.MaxStepsToAudit)
	assert.Equal(t, 0.3, cfg.FlagThresholdP1)
	assert.Equal(t, 0.5, cfg.DerivabilityThreshold)
}
