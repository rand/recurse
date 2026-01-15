package hallucination

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDetector(t *testing.T) {
	backend := NewMockBackend(0.8)
	config := DefaultDetectorConfig()

	detector := NewDetector(backend, config)

	assert.NotNil(t, detector)
	assert.NotNil(t, detector.extractor)
	assert.NotNil(t, detector.scrubber)
}

func TestDetector_VerifyClaimWithEvidence(t *testing.T) {
	t.Run("grounded claim", func(t *testing.T) {
		// p0 = 0.3, p1 = 0.9, confidence = 0.8
		// ObservedBits > RequiredBits -> negative gap -> well supported
		backend := NewMockBackendWithHandler(func(claim, context string) float64 {
			// Scrubbed context contains "[EVIDENCE REMOVED]" or "[No specific evidence provided]"
			if context == "[EVIDENCE REMOVED]" || context == "[No specific evidence provided]" {
				return 0.3 // Low without evidence
			}
			return 0.9 // High with evidence
		})

		detector := NewDetector(backend, DefaultDetectorConfig())
		ctx := context.Background()

		claim := &Claim{
			Content:    "The Earth is round",
			Confidence: 0.8, // Lower than p1 (0.9) so evidence exceeds requirement
		}

		result, err := detector.VerifyClaimWithEvidence(ctx, claim, "Scientific evidence shows...")

		require.NoError(t, err)
		assert.Equal(t, StatusGrounded, result.Status)
		assert.InDelta(t, 0.3, result.P0, 0.01)
		assert.InDelta(t, 0.9, result.P1, 0.01)
		assert.Less(t, result.BudgetGap, 0.0, "should have negative gap for well-supported claim")
		assert.Contains(t, result.Explanation, "Grounded")
	})

	t.Run("unsupported claim", func(t *testing.T) {
		// p0 = 0.7, p1 = 0.7 -> evidence doesn't help (hallucination)
		backend := NewMockBackend(0.7)

		detector := NewDetector(backend, DefaultDetectorConfig())
		ctx := context.Background()

		claim := &Claim{
			Content:    "The model knows this without evidence",
			Confidence: 0.95, // High claimed confidence
		}

		result, err := detector.VerifyClaimWithEvidence(ctx, claim, "Some context")

		require.NoError(t, err)
		assert.Equal(t, StatusUnsupported, result.Status)
		assert.Greater(t, result.BudgetGap, 0.0, "should have positive gap for unsupported claim")
	})

	t.Run("contradicted claim", func(t *testing.T) {
		// p1 = 0.1 -> evidence contradicts
		backend := NewMockBackendWithHandler(func(claim, context string) float64 {
			if context == "[No specific evidence provided]" {
				return 0.6
			}
			return 0.1 // Low with evidence - contradiction
		})

		detector := NewDetector(backend, DefaultDetectorConfig())
		ctx := context.Background()

		claim := &Claim{
			Content:    "The sky is green",
			Confidence: 0.9,
		}

		result, err := detector.VerifyClaimWithEvidence(ctx, claim, "The sky appears blue due to Rayleigh scattering")

		require.NoError(t, err)
		assert.Equal(t, StatusContradicted, result.Status)
		assert.InDelta(t, 0.1, result.P1, 0.01)
		assert.Contains(t, result.Explanation, "Contradicted")
	})

	t.Run("records duration", func(t *testing.T) {
		backend := NewMockBackend(0.8)
		detector := NewDetector(backend, DefaultDetectorConfig())
		ctx := context.Background()

		claim := &Claim{Content: "Test claim", Confidence: 0.9}
		result, _ := detector.VerifyClaimWithEvidence(ctx, claim, "evidence")

		assert.Greater(t, result.Duration, time.Duration(0))
	})
}

func TestDetector_VerifyText(t *testing.T) {
	t.Run("extracts and verifies claims", func(t *testing.T) {
		backend := NewMockBackend(0.8)
		detector := NewDetector(backend, DefaultDetectorConfig())
		ctx := context.Background()

		text := "Go is a programming language. It was created at Google. It is statically typed."
		evidence := "Go documentation and history"

		results, err := detector.VerifyText(ctx, text, evidence)

		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 2, "should extract multiple claims")

		for _, r := range results {
			assert.NotNil(t, r.Claim)
			assert.NotEmpty(t, r.Claim.Content)
		}
	})

	t.Run("filters non-assertive text", func(t *testing.T) {
		backend := NewMockBackend(0.8)
		detector := NewDetector(backend, DefaultDetectorConfig())
		ctx := context.Background()

		text := "What is Go? Let me explain. Go is a language."
		evidence := "Go documentation"

		results, err := detector.VerifyText(ctx, text, evidence)

		require.NoError(t, err)
		// Should only verify "Go is a language", not the question or meta-commentary
		assert.LessOrEqual(t, len(results), 2)
	})

	t.Run("empty text returns nil", func(t *testing.T) {
		backend := NewMockBackend(0.8)
		detector := NewDetector(backend, DefaultDetectorConfig())
		ctx := context.Background()

		results, err := detector.VerifyText(ctx, "", "evidence")

		require.NoError(t, err)
		assert.Nil(t, results)
	})
}

func TestDetector_VerifyTextBatch(t *testing.T) {
	t.Run("batch verifies efficiently", func(t *testing.T) {
		backend := NewMockBackend(0.8)
		detector := NewDetector(backend, DefaultDetectorConfig())
		ctx := context.Background()

		text := "Claim one. Claim two. Claim three."
		evidence := "Context"

		results, err := detector.VerifyTextBatch(ctx, text, evidence)

		require.NoError(t, err)
		assert.Len(t, results, 3)
	})
}

func TestDetector_AuditTrace(t *testing.T) {
	t.Run("valid trace", func(t *testing.T) {
		// Each step is supported by prior context
		callNum := 0
		backend := NewMockBackendWithHandler(func(claim, context string) float64 {
			callNum++
			// Simulate increasing support as context accumulates
			if context == "[No prior context]" {
				return 0.3 // Low without prior steps
			}
			return 0.9 // High with prior steps
		})

		detector := NewDetector(backend, DefaultDetectorConfig())
		ctx := context.Background()

		steps := []TraceStep{
			{Content: "Given: x = 5", StepType: "assumption", Confidence: 1.0, Index: 0},
			{Content: "Therefore: x + 1 = 6", StepType: "inference", Confidence: 0.95, Index: 1},
			{Content: "Conclusion: x is less than 10", StepType: "inference", Confidence: 0.9, Index: 2},
		}

		audit, err := detector.AuditTrace(ctx, steps, "Math problem context")

		require.NoError(t, err)
		assert.True(t, audit.Valid)
		assert.Empty(t, audit.FlaggedSteps)
		assert.Equal(t, 3, audit.TotalSteps)
		assert.Greater(t, audit.Duration, time.Duration(0))
	})

	t.Run("invalid trace with contradiction", func(t *testing.T) {
		backend := NewMockBackendWithHandler(func(claim, context string) float64 {
			// Step 2 contradicts prior context
			if claim == "Therefore: x = 100" {
				return 0.1 // Very low - contradicts
			}
			return 0.8
		})

		detector := NewDetector(backend, DefaultDetectorConfig())
		ctx := context.Background()

		steps := []TraceStep{
			{Content: "Given: x = 5", StepType: "assumption", Confidence: 1.0, Index: 0},
			{Content: "Therefore: x = 100", StepType: "inference", Confidence: 0.9, Index: 1}, // Wrong!
		}

		audit, err := detector.AuditTrace(ctx, steps, "")

		require.NoError(t, err)
		assert.False(t, audit.Valid)
		assert.Contains(t, audit.FlaggedSteps, 1)
	})

	t.Run("empty trace", func(t *testing.T) {
		backend := NewMockBackend(0.8)
		detector := NewDetector(backend, DefaultDetectorConfig())
		ctx := context.Background()

		audit, err := detector.AuditTrace(ctx, []TraceStep{}, "context")

		require.NoError(t, err)
		assert.True(t, audit.Valid)
		assert.Equal(t, 0, audit.TotalSteps)
	})
}

func TestSummary(t *testing.T) {
	t.Run("calculates statistics", func(t *testing.T) {
		results := []VerificationResult{
			{Status: StatusGrounded, BudgetGap: -1.0, Duration: 100 * time.Millisecond},
			{Status: StatusGrounded, BudgetGap: -0.5, Duration: 100 * time.Millisecond},
			{Status: StatusUnsupported, BudgetGap: 2.5, Duration: 100 * time.Millisecond},
			{Status: StatusContradicted, BudgetGap: 3.0, Duration: 100 * time.Millisecond},
			{Status: StatusUnverifiable, BudgetGap: 0, Duration: 100 * time.Millisecond},
		}

		summary := Summary(results)

		assert.Equal(t, 5, summary.TotalClaims)
		assert.Equal(t, 2, summary.Grounded)
		assert.Equal(t, 1, summary.Unsupported)
		assert.Equal(t, 1, summary.Contradicted)
		assert.Equal(t, 1, summary.Unverifiable)
		assert.Equal(t, 3.0, summary.MaxBudgetGap)
		assert.InDelta(t, 0.4, summary.OverallRisk, 0.01) // 2/5 flagged
		assert.Equal(t, 500*time.Millisecond, summary.TotalDuration)
	})

	t.Run("HasHallucinations", func(t *testing.T) {
		noHallucinations := VerificationSummary{Grounded: 5, Unsupported: 0, Contradicted: 0}
		assert.False(t, noHallucinations.HasHallucinations())

		withUnsupported := VerificationSummary{Grounded: 3, Unsupported: 2, Contradicted: 0}
		assert.True(t, withUnsupported.HasHallucinations())

		withContradicted := VerificationSummary{Grounded: 4, Unsupported: 0, Contradicted: 1}
		assert.True(t, withContradicted.HasHallucinations())
	})
}

func TestDefaultDetectorConfig(t *testing.T) {
	cfg := DefaultDetectorConfig()

	assert.False(t, cfg.MemoryGateEnabled)
	assert.False(t, cfg.OutputVerifyEnabled)
	assert.False(t, cfg.TraceAuditEnabled)
	assert.Equal(t, 2.0, cfg.FlagThresholdBits)
	assert.Equal(t, 0.5, cfg.MinConfidence)
	assert.Equal(t, 2*time.Second, cfg.Timeout)
	assert.False(t, cfg.RejectUnsupported)
}

func TestDetector_Timeout(t *testing.T) {
	t.Run("respects timeout", func(t *testing.T) {
		// Create a slow backend
		backend := &slowMockBackend{
			delay:       100 * time.Millisecond,
			probability: 0.8,
		}

		config := DefaultDetectorConfig()
		config.Timeout = 50 * time.Millisecond // Very short timeout

		detector := NewDetector(backend, config)
		ctx := context.Background()

		claim := &Claim{Content: "Test", Confidence: 0.9}
		result, err := detector.VerifyClaimWithEvidence(ctx, claim, "evidence")

		// Should complete but with unverifiable status due to timeout
		require.NoError(t, err) // Non-fatal
		assert.Equal(t, StatusUnverifiable, result.Status)
		assert.NotNil(t, result.Error)
	})
}

// slowMockBackend simulates a slow backend for timeout testing.
type slowMockBackend struct {
	delay       time.Duration
	probability float64
}

func (b *slowMockBackend) Name() string { return "slow-mock" }

func (b *slowMockBackend) EstimateProbability(ctx context.Context, claim, context string) (float64, error) {
	select {
	case <-time.After(b.delay):
		return b.probability, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (b *slowMockBackend) BatchEstimate(ctx context.Context, claims []string, context string) ([]float64, error) {
	results := make([]float64, len(claims))
	for i := range claims {
		p, err := b.EstimateProbability(ctx, claims[i], context)
		if err != nil {
			return nil, err
		}
		results[i] = p
	}
	return results, nil
}

// Integration test with all components.
func TestDetector_Integration(t *testing.T) {
	t.Run("end-to-end verification", func(t *testing.T) {
		// Simulate a realistic scenario:
		// - Claim: "Python was created in 1991"
		// - Evidence: mentions Python and 1991
		// - Expected: grounded

		backend := NewMockBackendWithHandler(func(claim, context string) float64 {
			// If context mentions both "Python" and "1991", high probability
			if context != "[No specific evidence provided]" {
				return 0.95
			}
			// Without evidence, moderate probability (common knowledge)
			return 0.6
		})

		config := DefaultDetectorConfig()
		detector := NewDetector(backend, config)
		ctx := context.Background()

		text := "Python was created in 1991 by Guido van Rossum. It is a high-level programming language."
		evidence := "According to the Python documentation, Python was first released in 1991. Guido van Rossum began working on it in the late 1980s."

		results, err := detector.VerifyText(ctx, text, evidence)

		require.NoError(t, err)
		assert.NotEmpty(t, results)

		// Check summary
		summary := Summary(results)
		assert.Equal(t, summary.TotalClaims, len(results))

		// At least one claim should be grounded
		assert.Greater(t, summary.Grounded, 0)
	})
}
