// Package hallucination provides information-theoretic hallucination detection.
package hallucination

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// ErrFactNotGrounded is returned when a fact fails verification.
var ErrFactNotGrounded = errors.New("fact not grounded in evidence")

// ErrFactContradicted is returned when a fact is contradicted by evidence.
var ErrFactContradicted = errors.New("fact contradicted by evidence")

// MemoryGate gates memory storage based on hallucination detection.
// [SPEC-08.15-18]
type MemoryGate struct {
	detector *Detector
	config   MemoryGateConfig
	logger   *slog.Logger

	// Statistics
	factsVerified   int64
	factsRejected   int64
	factsAdjusted   int64
	rejectedLog     []RejectedFact
	rejectedLogSize int
}

// MemoryGateConfig configures the memory gate.
type MemoryGateConfig struct {
	// Enabled enables verification for facts.
	Enabled bool

	// MinConfidence is the minimum confidence required to store.
	MinConfidence float64

	// RejectUnsupported rejects unsupported facts instead of adjusting confidence.
	RejectUnsupported bool

	// FlagThresholdBits is the budget gap threshold for flagging.
	FlagThresholdBits float64

	// LogRejectedFacts enables logging of rejected facts.
	LogRejectedFacts bool

	// MaxRejectedLog limits the size of the rejected facts log.
	MaxRejectedLog int

	// GracefulDegradation allows storage on verification errors.
	GracefulDegradation bool

	// ConfidenceReductionOnError is the confidence multiplier on verification errors.
	ConfidenceReductionOnError float64
}

// DefaultMemoryGateConfig returns sensible defaults.
func DefaultMemoryGateConfig() MemoryGateConfig {
	return MemoryGateConfig{
		Enabled:                    false,
		MinConfidence:              0.5,
		RejectUnsupported:          true,
		FlagThresholdBits:          2.0,
		LogRejectedFacts:           true,
		MaxRejectedLog:             100,
		GracefulDegradation:        true,
		ConfidenceReductionOnError: 0.5,
	}
}

// RejectedFact records a fact that was rejected by the memory gate.
type RejectedFact struct {
	Content     string
	Evidence    string
	Reason      string
	Status      VerificationStatus
	BudgetGap   float64
	P0          float64
	P1          float64
	Confidence  float64
	RejectedAt  time.Time
}

// NewMemoryGate creates a new memory gate.
func NewMemoryGate(detector *Detector, config MemoryGateConfig) *MemoryGate {
	if config.MaxRejectedLog <= 0 {
		config.MaxRejectedLog = 100
	}
	if config.ConfidenceReductionOnError <= 0 {
		config.ConfidenceReductionOnError = 0.5
	}

	return &MemoryGate{
		detector:        detector,
		config:          config,
		logger:          slog.Default(),
		rejectedLog:     make([]RejectedFact, 0, config.MaxRejectedLog),
		rejectedLogSize: config.MaxRejectedLog,
	}
}

// SetLogger sets the logger for the memory gate.
func (g *MemoryGate) SetLogger(logger *slog.Logger) {
	g.logger = logger
}

// VerifyFactResult contains the result of fact verification.
type VerifyFactResult struct {
	// Allowed indicates whether the fact should be stored.
	Allowed bool

	// AdjustedConfidence is the recommended confidence for storage.
	AdjustedConfidence float64

	// Status is the verification outcome.
	Status VerificationStatus

	// BudgetGap is the information budget gap.
	BudgetGap float64

	// Error is any verification error (may still allow with graceful degradation).
	Error error

	// Explanation describes the verification result.
	Explanation string
}

// VerifyFact verifies a fact before storage.
// [SPEC-08.15] Verify facts before storage
// [SPEC-08.16] Reject facts with high BudgetGap
// [SPEC-08.17] Adjust confidence based on verification
// [SPEC-08.18] Log rejected facts
func (g *MemoryGate) VerifyFact(ctx context.Context, content string, evidence string, confidence float64) (*VerifyFactResult, error) {
	if !g.config.Enabled || g.detector == nil {
		// Gate disabled - allow with original confidence
		return &VerifyFactResult{
			Allowed:            true,
			AdjustedConfidence: confidence,
			Status:             StatusUnverifiable,
			Explanation:        "verification disabled",
		}, nil
	}

	g.factsVerified++

	// Create a claim for verification
	claim := &Claim{
		Content:    content,
		Confidence: confidence,
	}

	// Verify with evidence
	result, err := g.detector.VerifyClaimWithEvidence(ctx, claim, evidence)
	if err != nil {
		g.logger.Warn("fact verification failed",
			"error", err,
			"content", truncate(content, 50),
		)

		if g.config.GracefulDegradation {
			// Allow with reduced confidence
			adjustedConfidence := confidence * g.config.ConfidenceReductionOnError
			return &VerifyFactResult{
				Allowed:            true,
				AdjustedConfidence: adjustedConfidence,
				Status:             StatusUnverifiable,
				Error:              err,
				Explanation:        fmt.Sprintf("verification failed, storing with reduced confidence: %v", err),
			}, nil
		}

		return &VerifyFactResult{
			Allowed:     false,
			Status:      StatusUnverifiable,
			Error:       err,
			Explanation: fmt.Sprintf("verification failed: %v", err),
		}, nil
	}

	vr := &VerifyFactResult{
		Status:      result.Status,
		BudgetGap:   result.BudgetGap,
		Explanation: result.Explanation,
	}

	switch result.Status {
	case StatusGrounded:
		// Well-supported - allow with potentially higher confidence
		vr.Allowed = true
		vr.AdjustedConfidence = result.AdjustedConfidence
		g.logger.Debug("fact verified as grounded",
			"content", truncate(content, 50),
			"confidence", confidence,
			"adjusted_confidence", vr.AdjustedConfidence,
			"budget_gap", result.BudgetGap,
		)

	case StatusContradicted:
		// [SPEC-08.16] Evidence contradicts the claim
		vr.Allowed = false
		vr.AdjustedConfidence = 0
		g.factsRejected++
		g.recordRejectedFact(content, evidence, "contradicted by evidence", result)
		g.logger.Warn("fact contradicted by evidence",
			"content", truncate(content, 50),
			"p1", result.P1,
		)

	case StatusUnsupported:
		// [SPEC-08.16] Evidence doesn't support the claim
		if g.config.RejectUnsupported && result.BudgetGap > g.config.FlagThresholdBits {
			vr.Allowed = false
			vr.AdjustedConfidence = 0
			g.factsRejected++
			g.recordRejectedFact(content, evidence, "unsupported - high budget gap", result)
			g.logger.Warn("fact rejected - unsupported",
				"content", truncate(content, 50),
				"budget_gap", result.BudgetGap,
				"threshold", g.config.FlagThresholdBits,
			)
		} else {
			// [SPEC-08.17] Allow with reduced confidence
			vr.Allowed = true
			vr.AdjustedConfidence = adjustConfidence(confidence, result)
			g.factsAdjusted++
			g.logger.Info("fact adjusted - partially supported",
				"content", truncate(content, 50),
				"original_confidence", confidence,
				"adjusted_confidence", vr.AdjustedConfidence,
				"budget_gap", result.BudgetGap,
			)
		}

	case StatusUnverifiable:
		// Unable to verify - apply graceful degradation
		vr.Error = result.Error // Propagate any underlying error
		if g.config.GracefulDegradation {
			vr.Allowed = true
			vr.AdjustedConfidence = confidence * g.config.ConfidenceReductionOnError
		} else {
			vr.Allowed = false
			vr.AdjustedConfidence = 0
			g.factsRejected++
			g.recordRejectedFact(content, evidence, "unverifiable", result)
		}
	}

	// Apply minimum confidence threshold
	if vr.Allowed && vr.AdjustedConfidence < g.config.MinConfidence {
		vr.Allowed = false
		vr.AdjustedConfidence = 0
		g.factsRejected++
		g.recordRejectedFact(content, evidence, "below minimum confidence threshold", result)
		g.logger.Info("fact rejected - below minimum confidence",
			"content", truncate(content, 50),
			"adjusted_confidence", vr.AdjustedConfidence,
			"min_confidence", g.config.MinConfidence,
		)
	}

	return vr, nil
}

// adjustConfidence computes an adjusted confidence based on verification results.
func adjustConfidence(originalConfidence float64, result *VerificationResult) float64 {
	// If evidence provides more than needed, keep original or increase slightly
	if result.BudgetGap <= 0 {
		return originalConfidence
	}

	// Reduce confidence proportionally to the gap
	// The larger the gap, the more we reduce confidence
	ratio := result.ObservedBits / result.RequiredBits
	if ratio > 1 {
		ratio = 1
	}
	if ratio < 0 {
		ratio = 0
	}

	// Blend original confidence with ratio
	adjusted := originalConfidence * (0.5 + 0.5*ratio)

	// Use P1 as a floor (evidence-based probability)
	if adjusted < result.P1 {
		adjusted = result.P1
	}

	return clamp(adjusted, 0, 1)
}

// recordRejectedFact logs a rejected fact.
// [SPEC-08.18]
func (g *MemoryGate) recordRejectedFact(content, evidence, reason string, result *VerificationResult) {
	if !g.config.LogRejectedFacts {
		return
	}

	rejected := RejectedFact{
		Content:    content,
		Evidence:   truncate(evidence, 200),
		Reason:     reason,
		Status:     result.Status,
		BudgetGap:  result.BudgetGap,
		P0:         result.P0,
		P1:         result.P1,
		Confidence: result.Claim.Confidence,
		RejectedAt: time.Now(),
	}

	// Circular buffer for rejected facts
	if len(g.rejectedLog) >= g.rejectedLogSize {
		g.rejectedLog = g.rejectedLog[1:]
	}
	g.rejectedLog = append(g.rejectedLog, rejected)
}

// MemoryGateStats contains statistics about the memory gate.
type MemoryGateStats struct {
	FactsVerified int64 `json:"facts_verified"`
	FactsRejected int64 `json:"facts_rejected"`
	FactsAdjusted int64 `json:"facts_adjusted"`
	RejectionRate float64 `json:"rejection_rate"`
}

// Stats returns current statistics.
func (g *MemoryGate) Stats() MemoryGateStats {
	stats := MemoryGateStats{
		FactsVerified: g.factsVerified,
		FactsRejected: g.factsRejected,
		FactsAdjusted: g.factsAdjusted,
	}

	if g.factsVerified > 0 {
		stats.RejectionRate = float64(g.factsRejected) / float64(g.factsVerified)
	}

	return stats
}

// RejectedFacts returns the recent rejected facts log.
func (g *MemoryGate) RejectedFacts() []RejectedFact {
	return append([]RejectedFact{}, g.rejectedLog...)
}

// ClearRejectedLog clears the rejected facts log.
func (g *MemoryGate) ClearRejectedLog() {
	g.rejectedLog = g.rejectedLog[:0]
}

// Config returns the current configuration.
func (g *MemoryGate) Config() MemoryGateConfig {
	return g.config
}

// Enabled returns whether the gate is enabled.
func (g *MemoryGate) Enabled() bool {
	return g.config.Enabled && g.detector != nil
}

// VerifyFactSimple is a simplified interface for FactVerifier compatibility.
// Returns (allowed, adjustedConfidence, error) instead of the full result struct.
func (g *MemoryGate) VerifyFactSimple(ctx context.Context, content string, evidence string, confidence float64) (bool, float64, error) {
	result, err := g.VerifyFact(ctx, content, evidence, confidence)
	if err != nil {
		return false, 0, err
	}
	return result.Allowed, result.AdjustedConfidence, result.Error
}

// FactVerifierAdapter wraps MemoryGate to implement the tiers.FactVerifier interface.
// This allows MemoryGate to be used with TaskMemory for fact verification.
type FactVerifierAdapter struct {
	gate *MemoryGate
}

// NewFactVerifierAdapter creates an adapter for using MemoryGate with TaskMemory.
func NewFactVerifierAdapter(gate *MemoryGate) *FactVerifierAdapter {
	return &FactVerifierAdapter{gate: gate}
}

// VerifyFact implements the tiers.FactVerifier interface.
func (a *FactVerifierAdapter) VerifyFact(ctx context.Context, content string, evidence string, confidence float64) (bool, float64, error) {
	if a.gate == nil {
		return true, confidence, nil
	}
	return a.gate.VerifyFactSimple(ctx, content, evidence, confidence)
}

// Enabled implements the tiers.FactVerifier interface.
func (a *FactVerifierAdapter) Enabled() bool {
	if a.gate == nil {
		return false
	}
	return a.gate.Enabled()
}
