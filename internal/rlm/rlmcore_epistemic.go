package rlm

// RLMCoreEpistemicBridge provides a bridge between the Go hallucination module
// and rlm-core's epistemic verification. When enabled (RLM_USE_CORE=true), it
// provides pure function calls via the rlm-core Rust implementation.
//
// LLM-dependent verification remains in Go; this bridge exposes:
// - ClaimExtractor: Extract verifiable claims from responses
// - EvidenceScrubber: Scrub evidence for P0 hiding
// - ThresholdGate: Gate memory writes based on confidence
// - KL divergence functions: Information-theoretic calculations
// - Quick hallucination check: Heuristic risk scoring

import (
	"log/slog"

	"github.com/rand/recurse/internal/rlmcore"
)

// RLMCoreEpistemicBridge wraps rlm-core epistemic functions for use in the RLM service.
type RLMCoreEpistemicBridge struct {
	claimExtractor  *rlmcore.ClaimExtractor
	evidenceScrubber *rlmcore.EvidenceScrubber
	thresholdGate   *rlmcore.ThresholdGate
}

// NewRLMCoreEpistemicBridge creates a new epistemic bridge using rlm-core.
// Returns nil, nil if rlm-core is not available.
func NewRLMCoreEpistemicBridge() (*RLMCoreEpistemicBridge, error) {
	if !rlmcore.Available() {
		return nil, nil
	}

	claimExtractor, err := rlmcore.NewClaimExtractor()
	if err != nil {
		return nil, err
	}

	evidenceScrubber, err := rlmcore.NewEvidenceScrubber()
	if err != nil {
		claimExtractor.Free()
		return nil, err
	}

	thresholdGate, err := rlmcore.NewThresholdGate()
	if err != nil {
		claimExtractor.Free()
		evidenceScrubber.Free()
		return nil, err
	}

	slog.Info("rlm-core epistemic bridge initialized")
	return &RLMCoreEpistemicBridge{
		claimExtractor:  claimExtractor,
		evidenceScrubber: evidenceScrubber,
		thresholdGate:   thresholdGate,
	}, nil
}

// ExtractClaims extracts verifiable claims from a response.
func (b *RLMCoreEpistemicBridge) ExtractClaims(response string) ([]rlmcore.Claim, error) {
	return b.claimExtractor.Extract(response)
}

// ExtractHighSpecificityClaims extracts claims above a specificity threshold.
func (b *RLMCoreEpistemicBridge) ExtractHighSpecificityClaims(response string, threshold float64) ([]rlmcore.Claim, error) {
	return b.claimExtractor.ExtractHighSpecificity(response, threshold)
}

// ScrubEvidence scrubs evidence from text for P0 hiding.
func (b *RLMCoreEpistemicBridge) ScrubEvidence(text string) (*rlmcore.ScrubResult, error) {
	return b.evidenceScrubber.Scrub(text)
}

// EvaluateNode evaluates a node against the threshold gate.
func (b *RLMCoreEpistemicBridge) EvaluateNode(input *rlmcore.NodeInput) (*rlmcore.GateDecision, error) {
	return b.thresholdGate.Evaluate(input)
}

// QuickHallucinationCheck performs a quick heuristic check for potential hallucinations.
// Returns a risk score from 0.0 (low risk) to 1.0 (high risk).
func (b *RLMCoreEpistemicBridge) QuickHallucinationCheck(response string) (float64, error) {
	return rlmcore.QuickHallucinationCheck(response)
}

// KLBernoulliBits calculates Bernoulli KL divergence in bits.
func (b *RLMCoreEpistemicBridge) KLBernoulliBits(p, q float64) (float64, error) {
	return rlmcore.KLBernoulliBits(p, q)
}

// BinaryEntropyBits calculates binary entropy in bits.
func (b *RLMCoreEpistemicBridge) BinaryEntropyBits(p float64) (float64, error) {
	return rlmcore.BinaryEntropyBits(p)
}

// SurpriseBits calculates surprise in bits.
func (b *RLMCoreEpistemicBridge) SurpriseBits(p float64) (float64, error) {
	return rlmcore.SurpriseBits(p)
}

// MutualInformationBits calculates mutual information in bits.
func (b *RLMCoreEpistemicBridge) MutualInformationBits(pPrior, pPosterior float64) (float64, error) {
	return rlmcore.MutualInformationBits(pPrior, pPosterior)
}

// RequiredBitsForSpecificity calculates the required bits for a given specificity level.
func (b *RLMCoreEpistemicBridge) RequiredBitsForSpecificity(specificity float64) (float64, error) {
	return rlmcore.RequiredBitsForSpecificity(specificity)
}

// AggregateEvidenceBits aggregates evidence bits from multiple sources.
func (b *RLMCoreEpistemicBridge) AggregateEvidenceBits(klValues []float64) (float64, error) {
	return rlmcore.AggregateEvidenceBits(klValues)
}

// Close releases all resources.
func (b *RLMCoreEpistemicBridge) Close() error {
	if b.claimExtractor != nil {
		b.claimExtractor.Free()
	}
	if b.evidenceScrubber != nil {
		b.evidenceScrubber.Free()
	}
	if b.thresholdGate != nil {
		b.thresholdGate.Free()
	}
	return nil
}

// UseRLMCoreEpistemic returns true if rlm-core epistemic should be used.
func UseRLMCoreEpistemic() bool {
	return rlmcore.Available()
}
