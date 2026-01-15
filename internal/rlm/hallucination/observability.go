// Package hallucination provides information-theoretic hallucination detection.
package hallucination

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/rand/recurse/internal/rlm/observability"
)

// Metric names for hallucination detection.
// [SPEC-08.40]
const (
	// Claims verified total
	MetricClaimsVerified = "hallucination_claims_verified_total"

	// Hallucinations detected by status
	MetricHallucinationsDetected = "hallucination_detections_total"

	// Verification latency
	MetricVerificationLatency = "hallucination_verification_duration_seconds"

	// Cache metrics
	MetricCacheHits   = "hallucination_cache_hits_total"
	MetricCacheMisses = "hallucination_cache_misses_total"

	// Facts rejected at memory gate
	MetricFactsRejected = "hallucination_facts_rejected_total"

	// Backend errors
	MetricBackendErrors = "hallucination_backend_errors_total"

	// Trace auditing
	MetricTracesAudited = "hallucination_traces_audited_total"
	MetricStepsAudited  = "hallucination_steps_audited_total"
)

// HallucinationMetrics provides observability for hallucination detection.
// [SPEC-08.40] [SPEC-08.41]
type HallucinationMetrics struct {
	registry *observability.Registry
	logger   *slog.Logger

	// Pre-allocated counters for hot paths
	claimsVerified *observability.Counter
	cacheHits      *observability.Counter
	cacheMisses    *observability.Counter
	factsRejected  *observability.Counter
	backendErrors  *observability.Counter
	tracesAudited  *observability.Counter
	stepsAudited   *observability.Counter

	// Histogram for latency
	verificationLatency *observability.Histogram

	// Counters by status (pre-allocated for common statuses)
	detectedGrounded     *observability.Counter
	detectedUnsupported  *observability.Counter
	detectedContradicted *observability.Counter
	detectedUnverifiable *observability.Counter

	// Atomic counters for quick stats access
	totalVerifications   int64
	totalHallucinations  int64
	totalCacheHits       int64
	totalCacheMisses     int64
}

// NewHallucinationMetrics creates hallucination metrics using the given registry.
func NewHallucinationMetrics(registry *observability.Registry) *HallucinationMetrics {
	if registry == nil {
		registry = observability.DefaultRegistry()
	}

	m := &HallucinationMetrics{
		registry: registry,
		logger:   slog.Default(),
	}

	// Pre-allocate common metrics
	m.claimsVerified = registry.Counter(MetricClaimsVerified, nil)
	m.cacheHits = registry.Counter(MetricCacheHits, nil)
	m.cacheMisses = registry.Counter(MetricCacheMisses, nil)
	m.factsRejected = registry.Counter(MetricFactsRejected, nil)
	m.backendErrors = registry.Counter(MetricBackendErrors, nil)
	m.tracesAudited = registry.Counter(MetricTracesAudited, nil)
	m.stepsAudited = registry.Counter(MetricStepsAudited, nil)

	// Histogram with latency buckets optimized for verification (faster than default)
	latencyBuckets := []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0}
	m.verificationLatency = registry.Histogram(MetricVerificationLatency, nil, latencyBuckets)

	// Pre-allocate status counters
	m.detectedGrounded = registry.Counter(MetricHallucinationsDetected, observability.Labels{"status": "grounded"})
	m.detectedUnsupported = registry.Counter(MetricHallucinationsDetected, observability.Labels{"status": "unsupported"})
	m.detectedContradicted = registry.Counter(MetricHallucinationsDetected, observability.Labels{"status": "contradicted"})
	m.detectedUnverifiable = registry.Counter(MetricHallucinationsDetected, observability.Labels{"status": "unverifiable"})

	return m
}

// SetLogger sets the logger for detailed event logging.
func (m *HallucinationMetrics) SetLogger(logger *slog.Logger) {
	m.logger = logger
}

// RecordVerification records a claim verification with its result and duration.
// [SPEC-08.40] [SPEC-08.41]
func (m *HallucinationMetrics) RecordVerification(result *VerificationResult, duration time.Duration) {
	m.claimsVerified.Inc()
	m.verificationLatency.Observe(duration.Seconds())
	atomic.AddInt64(&m.totalVerifications, 1)

	// Increment status-specific counter
	switch result.Status {
	case StatusGrounded:
		m.detectedGrounded.Inc()
	case StatusUnsupported:
		m.detectedUnsupported.Inc()
		atomic.AddInt64(&m.totalHallucinations, 1)
	case StatusContradicted:
		m.detectedContradicted.Inc()
		atomic.AddInt64(&m.totalHallucinations, 1)
	case StatusUnverifiable:
		m.detectedUnverifiable.Inc()
	}

	// [SPEC-08.41] Detailed logging for debugging
	m.logger.Debug("claim verified",
		"status", result.Status,
		"p0", result.P0,
		"p1", result.P1,
		"required_bits", result.RequiredBits,
		"observed_bits", result.ObservedBits,
		"budget_gap", result.BudgetGap,
		"adjusted_confidence", result.AdjustedConfidence,
		"duration_ms", duration.Milliseconds(),
	)
}

// RecordCacheHit records a verification cache hit.
func (m *HallucinationMetrics) RecordCacheHit() {
	m.cacheHits.Inc()
	atomic.AddInt64(&m.totalCacheHits, 1)
}

// RecordCacheMiss records a verification cache miss.
func (m *HallucinationMetrics) RecordCacheMiss() {
	m.cacheMisses.Inc()
	atomic.AddInt64(&m.totalCacheMisses, 1)
}

// RecordFactRejected records a fact rejected by the memory gate.
func (m *HallucinationMetrics) RecordFactRejected(claim string, reason string) {
	m.factsRejected.Inc()

	m.logger.Info("fact rejected by memory gate",
		"claim", truncate(claim, 100),
		"reason", reason,
	)
}

// RecordBackendError records an error from the verification backend.
func (m *HallucinationMetrics) RecordBackendError(err error) {
	m.backendErrors.Inc()

	m.logger.Warn("verification backend error",
		"error", err,
	)
}

// RecordTraceAudit records a trace audit with its results.
func (m *HallucinationMetrics) RecordTraceAudit(result *TraceAuditResult, duration time.Duration) {
	m.tracesAudited.Inc()
	if result.TraceAudit != nil {
		m.stepsAudited.Add(int64(result.TraceAudit.TotalSteps))

		m.logger.Debug("trace audited",
			"valid", result.TraceAudit.Valid,
			"total_steps", result.TraceAudit.TotalSteps,
			"flagged_steps", len(result.TraceAudit.FlaggedSteps),
			"post_hoc_hallucination", result.TraceAudit.PostHocHallucination,
			"overall", result.Overall,
			"duration_ms", duration.Milliseconds(),
		)
	}
}

// RecordOutputVerification records an output verification with its results.
func (m *HallucinationMetrics) RecordOutputVerification(result *OutputVerificationResult, duration time.Duration) {
	m.logger.Info("output verified",
		"total_claims", result.TotalClaims,
		"verified_claims", result.VerifiedClaims,
		"flagged_claims", result.FlaggedClaims,
		"overall_risk", result.OverallRisk,
		"duration_ms", duration.Milliseconds(),
	)
}

// CacheHitRate returns the cache hit rate (0-1).
// [SPEC-08.40]
func (m *HallucinationMetrics) CacheHitRate() float64 {
	hits := atomic.LoadInt64(&m.totalCacheHits)
	misses := atomic.LoadInt64(&m.totalCacheMisses)
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// HallucinationRate returns the rate of hallucinations detected (0-1).
func (m *HallucinationMetrics) HallucinationRate() float64 {
	total := atomic.LoadInt64(&m.totalVerifications)
	hallucinations := atomic.LoadInt64(&m.totalHallucinations)
	if total == 0 {
		return 0
	}
	return float64(hallucinations) / float64(total)
}

// Stats returns current statistics.
func (m *HallucinationMetrics) Stats() HallucinationStats {
	return HallucinationStats{
		TotalVerifications:  atomic.LoadInt64(&m.totalVerifications),
		TotalHallucinations: atomic.LoadInt64(&m.totalHallucinations),
		CacheHits:           atomic.LoadInt64(&m.totalCacheHits),
		CacheMisses:         atomic.LoadInt64(&m.totalCacheMisses),
		CacheHitRate:        m.CacheHitRate(),
		HallucinationRate:   m.HallucinationRate(),
		FactsRejected:       m.factsRejected.Value(),
		BackendErrors:       m.backendErrors.Value(),
		TracesAudited:       m.tracesAudited.Value(),
		StepsAudited:        m.stepsAudited.Value(),
		StatusCounts: map[VerificationStatus]int64{
			StatusGrounded:     m.detectedGrounded.Value(),
			StatusUnsupported:  m.detectedUnsupported.Value(),
			StatusContradicted: m.detectedContradicted.Value(),
			StatusUnverifiable: m.detectedUnverifiable.Value(),
		},
	}
}

// HallucinationStats contains aggregated statistics.
// [SPEC-08.40]
type HallucinationStats struct {
	TotalVerifications  int64                          `json:"total_verifications"`
	TotalHallucinations int64                          `json:"total_hallucinations"`
	CacheHits           int64                          `json:"cache_hits"`
	CacheMisses         int64                          `json:"cache_misses"`
	CacheHitRate        float64                        `json:"cache_hit_rate"`
	HallucinationRate   float64                        `json:"hallucination_rate"`
	FactsRejected       int64                          `json:"facts_rejected"`
	BackendErrors       int64                          `json:"backend_errors"`
	TracesAudited       int64                          `json:"traces_audited"`
	StepsAudited        int64                          `json:"steps_audited"`
	StatusCounts        map[VerificationStatus]int64   `json:"status_counts"`
}

// LatencyStats returns latency statistics from the histogram.
func (m *HallucinationMetrics) LatencyStats() LatencyStats {
	snap := m.verificationLatency.Snapshot()
	return LatencyStats{
		Count:       snap.Count,
		Mean:        snap.Mean(),
		P50:         snap.Percentile(50),
		P90:         snap.Percentile(90),
		P99:         snap.Percentile(99),
	}
}

// LatencyStats contains latency percentile information.
type LatencyStats struct {
	Count int64   `json:"count"`
	Mean  float64 `json:"mean_seconds"`
	P50   float64 `json:"p50_seconds"`
	P90   float64 `json:"p90_seconds"`
	P99   float64 `json:"p99_seconds"`
}

// Export returns all metrics in a format suitable for monitoring systems.
func (m *HallucinationMetrics) Export() map[string]interface{} {
	stats := m.Stats()
	latency := m.LatencyStats()

	return map[string]interface{}{
		"claims_verified":       stats.TotalVerifications,
		"hallucinations_found":  stats.TotalHallucinations,
		"hallucination_rate":    stats.HallucinationRate,
		"cache_hit_rate":        stats.CacheHitRate,
		"cache_hits":            stats.CacheHits,
		"cache_misses":          stats.CacheMisses,
		"facts_rejected":        stats.FactsRejected,
		"backend_errors":        stats.BackendErrors,
		"traces_audited":        stats.TracesAudited,
		"steps_audited":         stats.StepsAudited,
		"latency_mean_ms":       latency.Mean * 1000,
		"latency_p50_ms":        latency.P50 * 1000,
		"latency_p90_ms":        latency.P90 * 1000,
		"latency_p99_ms":        latency.P99 * 1000,
		"status_grounded":       stats.StatusCounts[StatusGrounded],
		"status_unsupported":    stats.StatusCounts[StatusUnsupported],
		"status_contradicted":   stats.StatusCounts[StatusContradicted],
		"status_unverifiable":   stats.StatusCounts[StatusUnverifiable],
	}
}

// VerificationTimer creates a timer for measuring verification duration.
type VerificationTimer struct {
	start   time.Time
	metrics *HallucinationMetrics
}

// StartVerificationTimer starts a timer for verification.
func (m *HallucinationMetrics) StartVerificationTimer() *VerificationTimer {
	return &VerificationTimer{
		start:   time.Now(),
		metrics: m,
	}
}

// ObserveResult records the result and stops the timer.
func (t *VerificationTimer) ObserveResult(result *VerificationResult) {
	t.metrics.RecordVerification(result, time.Since(t.start))
}

// Duration returns the elapsed duration.
func (t *VerificationTimer) Duration() time.Duration {
	return time.Since(t.start)
}

// ObservableDetector wraps a Detector with metrics collection.
type ObservableDetector struct {
	detector *Detector
	metrics  *HallucinationMetrics
}

// NewObservableDetector creates a detector with observability.
func NewObservableDetector(detector *Detector, metrics *HallucinationMetrics) *ObservableDetector {
	if metrics == nil {
		metrics = NewHallucinationMetrics(nil)
	}
	return &ObservableDetector{
		detector: detector,
		metrics:  metrics,
	}
}

// VerifyClaim verifies a claim with metrics collection (alias for VerifyClaimWithEvidence).
func (od *ObservableDetector) VerifyClaim(ctx context.Context, claim *Claim, evidence string) (*VerificationResult, error) {
	return od.VerifyClaimWithEvidence(ctx, claim, evidence)
}

// VerifyClaimWithEvidence verifies a claim with evidence and metrics.
func (od *ObservableDetector) VerifyClaimWithEvidence(ctx context.Context, claim *Claim, evidence string) (*VerificationResult, error) {
	timer := od.metrics.StartVerificationTimer()

	result, err := od.detector.VerifyClaimWithEvidence(ctx, claim, evidence)
	if err != nil {
		od.metrics.RecordBackendError(err)
		return nil, err
	}

	timer.ObserveResult(result)
	return result, nil
}

// Metrics returns the underlying metrics.
func (od *ObservableDetector) Metrics() *HallucinationMetrics {
	return od.metrics
}

// Detector returns the underlying detector.
func (od *ObservableDetector) Detector() *Detector {
	return od.detector
}
