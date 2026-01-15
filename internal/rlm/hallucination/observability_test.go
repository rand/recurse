package hallucination

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/rlm/observability"
)

func TestNewHallucinationMetrics(t *testing.T) {
	registry := observability.NewRegistry()
	metrics := NewHallucinationMetrics(registry)

	assert.NotNil(t, metrics)
	assert.NotNil(t, metrics.claimsVerified)
	assert.NotNil(t, metrics.verificationLatency)
}

func TestHallucinationMetrics_WithRegistry(t *testing.T) {
	registry := observability.NewRegistry()
	metrics := NewHallucinationMetrics(registry)

	assert.NotNil(t, metrics)
	assert.Equal(t, registry, metrics.registry)
}

func TestHallucinationMetrics_RecordVerification_Grounded(t *testing.T) {
	metrics := NewHallucinationMetrics(observability.NewRegistry())

	result := &VerificationResult{
		Status:             StatusGrounded,
		P0:                 0.5,
		P1:                 0.9,
		RequiredBits:       1.0,
		ObservedBits:       1.5,
		BudgetGap:          -0.5,
		AdjustedConfidence: 0.85,
	}

	metrics.RecordVerification(result, 100*time.Millisecond)

	stats := metrics.Stats()
	assert.Equal(t, int64(1), stats.TotalVerifications)
	assert.Equal(t, int64(0), stats.TotalHallucinations)
	assert.Equal(t, int64(1), stats.StatusCounts[StatusGrounded])
}

func TestHallucinationMetrics_RecordVerification_Unsupported(t *testing.T) {
	metrics := NewHallucinationMetrics(observability.NewRegistry())

	result := &VerificationResult{
		Status:    StatusUnsupported,
		BudgetGap: 3.0,
	}

	metrics.RecordVerification(result, 50*time.Millisecond)

	stats := metrics.Stats()
	assert.Equal(t, int64(1), stats.TotalVerifications)
	assert.Equal(t, int64(1), stats.TotalHallucinations)
	assert.Equal(t, int64(1), stats.StatusCounts[StatusUnsupported])
}

func TestHallucinationMetrics_RecordVerification_Contradicted(t *testing.T) {
	metrics := NewHallucinationMetrics(observability.NewRegistry())

	result := &VerificationResult{
		Status: StatusContradicted,
	}

	metrics.RecordVerification(result, 75*time.Millisecond)

	stats := metrics.Stats()
	assert.Equal(t, int64(1), stats.TotalHallucinations)
	assert.Equal(t, int64(1), stats.StatusCounts[StatusContradicted])
}

func TestHallucinationMetrics_RecordVerification_Unverifiable(t *testing.T) {
	metrics := NewHallucinationMetrics(observability.NewRegistry())

	result := &VerificationResult{
		Status: StatusUnverifiable,
	}

	metrics.RecordVerification(result, 25*time.Millisecond)

	stats := metrics.Stats()
	assert.Equal(t, int64(1), stats.TotalVerifications)
	assert.Equal(t, int64(0), stats.TotalHallucinations) // Unverifiable doesn't count as hallucination
	assert.Equal(t, int64(1), stats.StatusCounts[StatusUnverifiable])
}

func TestHallucinationMetrics_CacheHitRate(t *testing.T) {
	metrics := NewHallucinationMetrics(observability.NewRegistry())

	// No hits or misses
	assert.Equal(t, 0.0, metrics.CacheHitRate())

	// Record some hits and misses
	metrics.RecordCacheHit()
	metrics.RecordCacheHit()
	metrics.RecordCacheHit()
	metrics.RecordCacheMiss()

	assert.Equal(t, 0.75, metrics.CacheHitRate())
}

func TestHallucinationMetrics_HallucinationRate(t *testing.T) {
	metrics := NewHallucinationMetrics(observability.NewRegistry())

	// No verifications
	assert.Equal(t, 0.0, metrics.HallucinationRate())

	// Record mix of results
	metrics.RecordVerification(&VerificationResult{Status: StatusGrounded}, time.Millisecond)
	metrics.RecordVerification(&VerificationResult{Status: StatusGrounded}, time.Millisecond)
	metrics.RecordVerification(&VerificationResult{Status: StatusUnsupported}, time.Millisecond)
	metrics.RecordVerification(&VerificationResult{Status: StatusContradicted}, time.Millisecond)

	// 2 hallucinations out of 4 = 50%
	assert.Equal(t, 0.5, metrics.HallucinationRate())
}

func TestHallucinationMetrics_RecordFactRejected(t *testing.T) {
	metrics := NewHallucinationMetrics(observability.NewRegistry())

	metrics.RecordFactRejected("Some claim", "insufficient evidence")
	metrics.RecordFactRejected("Another claim", "contradicted")

	stats := metrics.Stats()
	assert.Equal(t, int64(2), stats.FactsRejected)
}

func TestHallucinationMetrics_RecordBackendError(t *testing.T) {
	metrics := NewHallucinationMetrics(observability.NewRegistry())

	metrics.RecordBackendError(ErrBackendTimeout)
	metrics.RecordBackendError(ErrRateLimited)

	stats := metrics.Stats()
	assert.Equal(t, int64(2), stats.BackendErrors)
}

func TestHallucinationMetrics_RecordTraceAudit(t *testing.T) {
	metrics := NewHallucinationMetrics(observability.NewRegistry())

	result := &TraceAuditResult{
		TraceAudit: &TraceAudit{
			Valid:       true,
			TotalSteps:  5,
			FlaggedSteps: []int{},
		},
		Overall: TraceValid,
	}

	metrics.RecordTraceAudit(result, 200*time.Millisecond)

	stats := metrics.Stats()
	assert.Equal(t, int64(1), stats.TracesAudited)
	assert.Equal(t, int64(5), stats.StepsAudited)
}

func TestHallucinationMetrics_LatencyStats(t *testing.T) {
	metrics := NewHallucinationMetrics(observability.NewRegistry())

	// Record several verifications with different latencies
	metrics.RecordVerification(&VerificationResult{Status: StatusGrounded}, 10*time.Millisecond)
	metrics.RecordVerification(&VerificationResult{Status: StatusGrounded}, 20*time.Millisecond)
	metrics.RecordVerification(&VerificationResult{Status: StatusGrounded}, 30*time.Millisecond)
	metrics.RecordVerification(&VerificationResult{Status: StatusGrounded}, 40*time.Millisecond)
	metrics.RecordVerification(&VerificationResult{Status: StatusGrounded}, 50*time.Millisecond)

	latency := metrics.LatencyStats()
	assert.Equal(t, int64(5), latency.Count)
	assert.InDelta(t, 0.030, latency.Mean, 0.001) // Mean should be ~30ms
}

func TestHallucinationMetrics_Export(t *testing.T) {
	metrics := NewHallucinationMetrics(observability.NewRegistry())

	// Record some activity
	metrics.RecordVerification(&VerificationResult{Status: StatusGrounded}, 10*time.Millisecond)
	metrics.RecordVerification(&VerificationResult{Status: StatusUnsupported}, 20*time.Millisecond)
	metrics.RecordCacheHit()
	metrics.RecordCacheMiss()
	metrics.RecordFactRejected("claim", "reason")
	metrics.RecordBackendError(ErrBackendTimeout)

	exported := metrics.Export()

	assert.Equal(t, int64(2), exported["claims_verified"])
	assert.Equal(t, int64(1), exported["hallucinations_found"])
	assert.Equal(t, 0.5, exported["hallucination_rate"])
	assert.Equal(t, 0.5, exported["cache_hit_rate"])
	assert.Equal(t, int64(1), exported["cache_hits"])
	assert.Equal(t, int64(1), exported["cache_misses"])
	assert.Equal(t, int64(1), exported["facts_rejected"])
	assert.Equal(t, int64(1), exported["backend_errors"])
	assert.Equal(t, int64(1), exported["status_grounded"])
	assert.Equal(t, int64(1), exported["status_unsupported"])
}

func TestVerificationTimer(t *testing.T) {
	metrics := NewHallucinationMetrics(observability.NewRegistry())

	timer := metrics.StartVerificationTimer()
	time.Sleep(5 * time.Millisecond) // Small delay

	result := &VerificationResult{Status: StatusGrounded}
	timer.ObserveResult(result)

	// Check duration is reasonable
	assert.True(t, timer.Duration() >= 5*time.Millisecond)

	stats := metrics.Stats()
	assert.Equal(t, int64(1), stats.TotalVerifications)
}

func TestObservableDetector(t *testing.T) {
	backend := NewMockBackend(0.8)
	detector := NewDetector(backend, DefaultDetectorConfig())
	metrics := NewHallucinationMetrics(observability.NewRegistry())

	od := NewObservableDetector(detector, metrics)

	assert.NotNil(t, od.Detector())
	assert.NotNil(t, od.Metrics())
	assert.Equal(t, detector, od.Detector())
	assert.Equal(t, metrics, od.Metrics())
}

func TestObservableDetector_VerifyClaim(t *testing.T) {
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[EVIDENCE REMOVED]" || context == "[No specific evidence provided]" {
			return 0.3
		}
		return 0.9
	})
	detector := NewDetector(backend, DefaultDetectorConfig())
	metrics := NewHallucinationMetrics(observability.NewRegistry())

	od := NewObservableDetector(detector, metrics)
	ctx := context.Background()

	claim := &Claim{
		Content:    "The sky is blue",
		Confidence: 0.9,
	}

	result, err := od.VerifyClaim(ctx, claim, "")
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Check metrics were recorded
	stats := metrics.Stats()
	assert.Equal(t, int64(1), stats.TotalVerifications)
}

func TestObservableDetector_VerifyClaimWithEvidence(t *testing.T) {
	backend := NewMockBackendWithHandler(func(claim, context string) float64 {
		if context == "[EVIDENCE REMOVED]" || context == "[No specific evidence provided]" {
			return 0.3
		}
		return 0.9
	})
	detector := NewDetector(backend, DefaultDetectorConfig())
	metrics := NewHallucinationMetrics(observability.NewRegistry())

	od := NewObservableDetector(detector, metrics)
	ctx := context.Background()

	claim := &Claim{
		Content:    "The sky is blue",
		Confidence: 0.9,
	}

	result, err := od.VerifyClaimWithEvidence(ctx, claim, "Scientific evidence about the color of the sky")
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Check metrics were recorded
	stats := metrics.Stats()
	assert.Equal(t, int64(1), stats.TotalVerifications)
}

func TestObservableDetector_NilMetrics(t *testing.T) {
	backend := NewMockBackend(0.8)
	detector := NewDetector(backend, DefaultDetectorConfig())

	// Should create default metrics if nil
	od := NewObservableDetector(detector, nil)

	assert.NotNil(t, od.Metrics())
}

func TestHallucinationMetrics_Stats_AllStatuses(t *testing.T) {
	metrics := NewHallucinationMetrics(observability.NewRegistry())

	// Record one of each status
	metrics.RecordVerification(&VerificationResult{Status: StatusGrounded}, time.Millisecond)
	metrics.RecordVerification(&VerificationResult{Status: StatusUnsupported}, time.Millisecond)
	metrics.RecordVerification(&VerificationResult{Status: StatusContradicted}, time.Millisecond)
	metrics.RecordVerification(&VerificationResult{Status: StatusUnverifiable}, time.Millisecond)

	stats := metrics.Stats()

	assert.Equal(t, int64(4), stats.TotalVerifications)
	assert.Equal(t, int64(2), stats.TotalHallucinations) // unsupported + contradicted
	assert.Equal(t, int64(1), stats.StatusCounts[StatusGrounded])
	assert.Equal(t, int64(1), stats.StatusCounts[StatusUnsupported])
	assert.Equal(t, int64(1), stats.StatusCounts[StatusContradicted])
	assert.Equal(t, int64(1), stats.StatusCounts[StatusUnverifiable])
}

func TestHallucinationMetrics_RecordOutputVerification(t *testing.T) {
	metrics := NewHallucinationMetrics(observability.NewRegistry())

	result := &OutputVerificationResult{
		TotalClaims:    10,
		VerifiedClaims: 8,
		FlaggedClaims:  2,
		OverallRisk:    0.2,
	}

	// Should not panic
	metrics.RecordOutputVerification(result, 500*time.Millisecond)
}
