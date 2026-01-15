package hallucination

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOutputVerifier(t *testing.T) {
	backend := NewMockBackend(0.8)
	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultOutputVerifierConfig()

	verifier := NewOutputVerifier(detector, config)

	assert.NotNil(t, verifier)
	assert.Equal(t, config.Enabled, verifier.config.Enabled)
	assert.Equal(t, config.FlagThresholdBits, verifier.config.FlagThresholdBits)
}

func TestOutputVerifier_Disabled(t *testing.T) {
	backend := NewMockBackend(0.8)
	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultOutputVerifierConfig()
	config.Enabled = false

	verifier := NewOutputVerifier(detector, config)
	ctx := context.Background()

	result, err := verifier.VerifyOutput(ctx, "The sky is blue. Water is wet.", "context")

	require.NoError(t, err)
	assert.True(t, result.Skipped)
	assert.Contains(t, result.SkipReason, "disabled")
}

func TestOutputVerifier_ShortResponse(t *testing.T) {
	backend := NewMockBackend(0.8)
	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultOutputVerifierConfig()
	config.Enabled = true
	config.SkipShortResponses = 100

	verifier := NewOutputVerifier(detector, config)
	ctx := context.Background()

	result, err := verifier.VerifyOutput(ctx, "Short response.", "context")

	require.NoError(t, err)
	assert.True(t, result.Skipped)
	assert.Contains(t, result.SkipReason, "too short")
}

func TestOutputVerifier_GroundedClaims(t *testing.T) {
	// Backend returns high probability with evidence (grounded)
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[EVIDENCE REMOVED]" || context == "[No specific evidence provided]" {
			return 0.3 // Low without evidence
		}
		return 0.9 // High with evidence (grounded)
	})

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultOutputVerifierConfig()
	config.Enabled = true
	config.SkipShortResponses = 10

	verifier := NewOutputVerifier(detector, config)
	ctx := context.Background()

	response := "The Earth orbits the Sun. Water boils at 100 degrees Celsius at sea level."
	context := "Scientific facts: The Earth orbits the Sun. Water boils at 100°C at standard pressure."

	result, err := verifier.VerifyOutput(ctx, response, context)

	require.NoError(t, err)
	assert.False(t, result.Skipped)
	assert.False(t, result.Flagged, "grounded claims should not be flagged")
	assert.Equal(t, 0, result.FlaggedClaims)
}

func TestOutputVerifier_UnsupportedClaims(t *testing.T) {
	// Backend returns same probability regardless of evidence (unsupported)
	backend := NewMockBackend(0.5) // Same probability with or without evidence

	detectorConfig := DefaultDetectorConfig()
	detector := NewDetector(backend, detectorConfig)

	config := DefaultOutputVerifierConfig()
	config.Enabled = true
	config.SkipShortResponses = 10
	config.FlagThresholdBits = 0.1 // Low threshold to ensure flagging

	verifier := NewOutputVerifier(detector, config)
	ctx := context.Background()

	response := "The universe contains exactly 42 galaxies. Bananas are purple on the inside."
	context := "General knowledge about space and fruits."

	result, err := verifier.VerifyOutput(ctx, response, context)

	require.NoError(t, err)
	assert.False(t, result.Skipped)
	assert.Greater(t, result.FlaggedClaims, 0, "unsupported claims should be flagged")
}

func TestOutputVerifier_ContradictedClaims(t *testing.T) {
	// Backend returns low probability with evidence (contradicted)
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[EVIDENCE REMOVED]" || context == "[No specific evidence provided]" {
			return 0.5 // Moderate without evidence
		}
		return 0.1 // Low with evidence (contradicted)
	})

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultOutputVerifierConfig()
	config.Enabled = true
	config.SkipShortResponses = 10
	config.FlagThresholdBits = 0.1 // Low threshold to ensure contradicted claims are flagged

	verifier := NewOutputVerifier(detector, config)
	ctx := context.Background()

	response := "The sky is definitely green according to all sources. Grass is certainly blue everywhere."
	context := "The sky appears blue. Grass is green."

	result, err := verifier.VerifyOutput(ctx, response, context)

	require.NoError(t, err)
	assert.False(t, result.Skipped)

	// Check that some claims have contradicted status
	hasContradicted := false
	for _, cr := range result.ClaimResults {
		if cr.Status == StatusContradicted {
			hasContradicted = true
			break
		}
	}
	assert.True(t, hasContradicted, "should have contradicted claims")

	// Contradicted claims should be flagged with low threshold
	if hasContradicted {
		assert.Greater(t, result.FlaggedClaims, 0, "contradicted claims should be flagged")
	}
}

func TestOutputVerifier_SelfCorrectionHints(t *testing.T) {
	// Backend returns low probability with evidence (flagged)
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[No specific evidence provided]" {
			return 0.5
		}
		return 0.2 // Low with evidence
	})

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultOutputVerifierConfig()
	config.Enabled = true
	config.SkipShortResponses = 10
	config.FlagThresholdBits = 0.5
	config.IncludeExplanations = true

	verifier := NewOutputVerifier(detector, config)
	ctx := context.Background()

	response := "Quantum computers can solve all NP-complete problems instantly."
	context := "Quantum computers have limitations and cannot solve all problems."

	result, err := verifier.VerifyOutput(ctx, response, context)
	require.NoError(t, err)

	// Get self-correction hints
	hint := verifier.GetVerificationResultForSelfCorrection(result)

	if result.Flagged {
		assert.NotNil(t, hint, "should provide hints for flagged responses")
		assert.True(t, hint.ShouldRevise)
		assert.NotEmpty(t, hint.Suggestion)
	}
}

func TestOutputVerifier_NoClaims(t *testing.T) {
	backend := NewMockBackend(0.8)
	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultOutputVerifierConfig()
	config.Enabled = true
	config.SkipShortResponses = 10

	verifier := NewOutputVerifier(detector, config)
	ctx := context.Background()

	// Response with only questions and imperatives (no assertive claims)
	response := "What do you think? Please consider the options. Let me know your preference."
	context := "Some context."

	result, err := verifier.VerifyOutput(ctx, response, context)

	require.NoError(t, err)
	assert.True(t, result.Skipped)
	assert.Contains(t, result.SkipReason, "no assertive claims")
}

func TestOutputVerifier_Stats(t *testing.T) {
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[No specific evidence provided]" {
			return 0.3
		}
		return 0.9
	})

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultOutputVerifierConfig()
	config.Enabled = true
	config.SkipShortResponses = 10

	verifier := NewOutputVerifier(detector, config)
	ctx := context.Background()

	// Verify multiple responses
	verifier.VerifyOutput(ctx, "The Earth is round. The Sun is hot.", "Scientific facts.")
	verifier.VerifyOutput(ctx, "Water is wet. Fire is hot.", "Basic observations.")

	stats := verifier.Stats()
	assert.Equal(t, int64(2), stats.ResponsesVerified)
	assert.GreaterOrEqual(t, stats.ClaimsVerified, int64(2))
}

func TestOutputVerifier_MaxClaims(t *testing.T) {
	backend := NewMockBackend(0.8)
	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultOutputVerifierConfig()
	config.Enabled = true
	config.SkipShortResponses = 10
	config.MaxClaimsToVerify = 2 // Only verify first 2 claims

	verifier := NewOutputVerifier(detector, config)
	ctx := context.Background()

	// Response with many claims
	response := "Claim one here. Claim two here. Claim three here. Claim four here."
	context := "Some context."

	result, err := verifier.VerifyOutput(ctx, response, context)

	require.NoError(t, err)
	assert.LessOrEqual(t, result.VerifiedClaims, 2, "should only verify MaxClaimsToVerify")
}

func TestOutputVerifier_WarnOnFlag(t *testing.T) {
	// Backend returns low probability with evidence (will flag)
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[No specific evidence provided]" {
			return 0.5
		}
		return 0.1 // Low with evidence - will flag
	})

	detector := NewDetector(backend, DefaultDetectorConfig())
	config := DefaultOutputVerifierConfig()
	config.Enabled = true
	config.SkipShortResponses = 10
	config.FlagThresholdBits = 0.5
	config.WarnOnFlag = true

	verifier := NewOutputVerifier(detector, config)
	ctx := context.Background()

	response := "Unverified claim one. Unverified claim two. Unverified claim three."
	context := "Different context."

	result, err := verifier.VerifyOutput(ctx, response, context)

	require.NoError(t, err)
	if result.Flagged {
		assert.NotEmpty(t, result.Warning, "should generate warning when WarnOnFlag is true")
	}
}

func TestOutputVerifier_NilDetector(t *testing.T) {
	config := DefaultOutputVerifierConfig()
	config.Enabled = true

	verifier := NewOutputVerifier(nil, config)
	ctx := context.Background()

	result, err := verifier.VerifyOutput(ctx, "Some response.", "context")

	require.NoError(t, err)
	assert.True(t, result.Skipped)
	assert.Contains(t, result.SkipReason, "disabled")
}

func TestDefaultOutputVerifierConfig(t *testing.T) {
	cfg := DefaultOutputVerifierConfig()

	assert.False(t, cfg.Enabled)
	assert.Equal(t, 5.0, cfg.FlagThresholdBits)
	assert.True(t, cfg.WarnOnFlag)
	assert.Equal(t, 20, cfg.MaxClaimsToVerify)
	assert.Equal(t, 50, cfg.SkipShortResponses)
	assert.True(t, cfg.IncludeExplanations)
}
