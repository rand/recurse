package repl

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/rlm/hallucination"
)

func TestNewHallucinationPlugin(t *testing.T) {
	backend := hallucination.NewMockBackend(0.8)
	detector := hallucination.NewDetector(backend, hallucination.DefaultDetectorConfig())

	plugin := NewHallucinationPlugin(detector, nil, nil)

	assert.NotNil(t, plugin)
	assert.Equal(t, "hallucination", plugin.Name())
}

func TestHallucinationPlugin_Functions(t *testing.T) {
	plugin := NewHallucinationPlugin(nil, nil, nil)

	funcs := plugin.Functions()

	assert.Contains(t, funcs, "verify_claim")
	assert.Contains(t, funcs, "verify_claims")
	assert.Contains(t, funcs, "audit_trace")
}

func TestHallucinationPlugin_VerifyClaim(t *testing.T) {
	// Backend returns high probability with evidence (grounded)
	backend := hallucination.NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[EVIDENCE REMOVED]" || context == "[No specific evidence provided]" {
			return 0.3
		}
		return 0.9
	})

	detector := hallucination.NewDetector(backend, hallucination.DefaultDetectorConfig())
	plugin := NewHallucinationPlugin(detector, nil, nil)
	ctx := context.Background()

	result, err := plugin.verifyClaim(ctx, "The Earth is round", "Scientific evidence", 0.8)

	require.NoError(t, err)
	assert.NotNil(t, result)

	cvr, ok := result.(ClaimVerificationResult)
	require.True(t, ok)

	assert.Equal(t, "The Earth is round", cvr.Claim)
	assert.Equal(t, "grounded", cvr.Status)
	assert.Greater(t, cvr.P1, cvr.P0)
}

func TestHallucinationPlugin_VerifyClaim_MissingArgs(t *testing.T) {
	backend := hallucination.NewMockBackend(0.8)
	detector := hallucination.NewDetector(backend, hallucination.DefaultDetectorConfig())
	plugin := NewHallucinationPlugin(detector, nil, nil)
	ctx := context.Background()

	_, err := plugin.verifyClaim(ctx, "only claim")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "evidence arguments required")
}

func TestHallucinationPlugin_VerifyClaim_NilDetector(t *testing.T) {
	plugin := NewHallucinationPlugin(nil, nil, nil)
	ctx := context.Background()

	_, err := plugin.verifyClaim(ctx, "claim", "evidence")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestHallucinationPlugin_VerifyClaims(t *testing.T) {
	backend := hallucination.NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[EVIDENCE REMOVED]" || context == "[No specific evidence provided]" {
			return 0.3
		}
		return 0.9
	})

	detector := hallucination.NewDetector(backend, hallucination.DefaultDetectorConfig())
	verifierConfig := hallucination.DefaultOutputVerifierConfig()
	verifierConfig.Enabled = true
	verifierConfig.SkipShortResponses = 10
	outputVerifier := hallucination.NewOutputVerifier(detector, verifierConfig)

	plugin := NewHallucinationPlugin(detector, outputVerifier, nil)
	ctx := context.Background()

	result, err := plugin.verifyClaims(ctx, "The Earth orbits the Sun. Water is wet.", "Scientific facts about Earth and water.")

	require.NoError(t, err)
	assert.NotNil(t, result)

	cvr, ok := result.(ClaimsVerificationResult)
	require.True(t, ok)

	assert.GreaterOrEqual(t, cvr.TotalClaims, 1)
}

func TestHallucinationPlugin_VerifyClaims_MissingArgs(t *testing.T) {
	backend := hallucination.NewMockBackend(0.8)
	detector := hallucination.NewDetector(backend, hallucination.DefaultDetectorConfig())
	verifierConfig := hallucination.DefaultOutputVerifierConfig()
	verifierConfig.Enabled = true
	outputVerifier := hallucination.NewOutputVerifier(detector, verifierConfig)

	plugin := NewHallucinationPlugin(detector, outputVerifier, nil)
	ctx := context.Background()

	_, err := plugin.verifyClaims(ctx, "only text")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context arguments required")
}

func TestHallucinationPlugin_VerifyClaims_NilVerifier(t *testing.T) {
	plugin := NewHallucinationPlugin(nil, nil, nil)
	ctx := context.Background()

	_, err := plugin.verifyClaims(ctx, "text", "context")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestHallucinationPlugin_AuditTrace(t *testing.T) {
	backend := hallucination.NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[No prior context]" {
			return 0.3
		}
		return 0.9
	})

	detector := hallucination.NewDetector(backend, hallucination.DefaultDetectorConfig())
	auditorConfig := hallucination.DefaultTraceAuditorConfig()
	auditorConfig.Enabled = true
	auditorConfig.CheckPostHoc = false
	traceAuditor := hallucination.NewTraceAuditor(detector, auditorConfig)

	plugin := NewHallucinationPlugin(detector, nil, traceAuditor)
	ctx := context.Background()

	steps := []any{
		map[string]any{"content": "Given X is true", "type": "premise"},
		map[string]any{"content": "Y follows from X", "type": "inference"},
	}

	result, err := plugin.auditTrace(ctx, steps, "Initial context", "")

	require.NoError(t, err)
	assert.NotNil(t, result)

	tar, ok := result.(TraceAuditResult)
	require.True(t, ok)

	assert.Equal(t, 2, tar.TotalSteps)
}

func TestHallucinationPlugin_AuditTrace_WithPostHoc(t *testing.T) {
	backend := hallucination.NewMockBackendWithHandler(func(claim, context string) float64 {
		if len(context) > 100 {
			return 0.9 // High for derivability
		}
		if context == "[No prior context]" {
			return 0.3
		}
		return 0.8
	})

	detector := hallucination.NewDetector(backend, hallucination.DefaultDetectorConfig())
	auditorConfig := hallucination.DefaultTraceAuditorConfig()
	auditorConfig.Enabled = true
	auditorConfig.CheckPostHoc = true
	traceAuditor := hallucination.NewTraceAuditor(detector, auditorConfig)

	plugin := NewHallucinationPlugin(detector, nil, traceAuditor)
	ctx := context.Background()

	steps := []any{
		map[string]any{"content": "X equals 5"},
		map[string]any{"content": "Y equals X plus 3"},
	}

	result, err := plugin.auditTrace(ctx, steps, "Calculate Y", "Y equals 8")

	require.NoError(t, err)
	assert.NotNil(t, result)

	tar, ok := result.(TraceAuditResult)
	require.True(t, ok)

	assert.Greater(t, tar.DerivabilityScore, 0.0)
}

func TestHallucinationPlugin_AuditTrace_MissingArgs(t *testing.T) {
	backend := hallucination.NewMockBackend(0.8)
	detector := hallucination.NewDetector(backend, hallucination.DefaultDetectorConfig())
	auditorConfig := hallucination.DefaultTraceAuditorConfig()
	auditorConfig.Enabled = true
	traceAuditor := hallucination.NewTraceAuditor(detector, auditorConfig)

	plugin := NewHallucinationPlugin(detector, nil, traceAuditor)
	ctx := context.Background()

	_, err := plugin.auditTrace(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "steps argument required")
}

func TestHallucinationPlugin_AuditTrace_InvalidSteps(t *testing.T) {
	backend := hallucination.NewMockBackend(0.8)
	detector := hallucination.NewDetector(backend, hallucination.DefaultDetectorConfig())
	auditorConfig := hallucination.DefaultTraceAuditorConfig()
	auditorConfig.Enabled = true
	traceAuditor := hallucination.NewTraceAuditor(detector, auditorConfig)

	plugin := NewHallucinationPlugin(detector, nil, traceAuditor)
	ctx := context.Background()

	// Invalid: steps must be a list
	_, err := plugin.auditTrace(ctx, "not a list")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be a list")
}

func TestHallucinationPlugin_AuditTrace_NilAuditor(t *testing.T) {
	plugin := NewHallucinationPlugin(nil, nil, nil)
	ctx := context.Background()

	steps := []any{
		map[string]any{"content": "Step 1"},
	}

	_, err := plugin.auditTrace(ctx, steps)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestHallucinationPlugin_Registration(t *testing.T) {
	backend := hallucination.NewMockBackend(0.8)
	detector := hallucination.NewDetector(backend, hallucination.DefaultDetectorConfig())
	plugin := NewHallucinationPlugin(detector, nil, nil)

	pm := NewPluginManager()
	ctx := context.Background()

	err := pm.Register(ctx, plugin)
	require.NoError(t, err)

	// Check functions are registered with plugin prefix
	assert.True(t, pm.HasFunction("hallucination_verify_claim"))
	assert.True(t, pm.HasFunction("hallucination_verify_claims"))
	assert.True(t, pm.HasFunction("hallucination_audit_trace"))
}

func TestHallucinationPlugin_ManifestGeneration(t *testing.T) {
	backend := hallucination.NewMockBackend(0.8)
	detector := hallucination.NewDetector(backend, hallucination.DefaultDetectorConfig())
	plugin := NewHallucinationPlugin(detector, nil, nil)

	pm := NewPluginManager()
	ctx := context.Background()

	err := pm.Register(ctx, plugin)
	require.NoError(t, err)

	manifest := pm.GenerateManifest()
	assert.Contains(t, manifest, "hallucination")
	assert.Contains(t, manifest, "verify_claim")
	assert.Contains(t, manifest, "verify_claims")
	assert.Contains(t, manifest, "audit_trace")
}
