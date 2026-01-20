package rlmcore

import (
	core "github.com/rand/rlm-core/go/rlmcore"
)

// ClaimExtractor wraps the rlm-core ClaimExtractor.
// Extracts verifiable claims from LLM responses.
type ClaimExtractor struct {
	inner *core.ClaimExtractor
}

// Claim represents an atomic claim extracted from a response.
type Claim = core.Claim

// NewClaimExtractor creates a new claim extractor.
func NewClaimExtractor() (*ClaimExtractor, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner := core.NewClaimExtractor()
	return &ClaimExtractor{inner: inner}, nil
}

// Extract extracts all claims from a response.
func (e *ClaimExtractor) Extract(response string) ([]Claim, error) {
	return e.inner.Extract(response)
}

// ExtractHighSpecificity extracts claims above a specificity threshold.
func (e *ClaimExtractor) ExtractHighSpecificity(response string, threshold float64) ([]Claim, error) {
	return e.inner.ExtractHighSpecificity(response, threshold)
}

// Free releases the claim extractor resources.
func (e *ClaimExtractor) Free() {
	if e.inner != nil {
		e.inner.Free()
		e.inner = nil
	}
}

// EvidenceScrubber wraps the rlm-core EvidenceScrubber.
// Scrubs evidence from text for P0 hiding.
type EvidenceScrubber struct {
	inner *core.EvidenceScrubber
}

// ScrubResult contains the result of scrubbing evidence.
type ScrubResult = core.ScrubResult

// NewEvidenceScrubber creates a new evidence scrubber with default settings.
func NewEvidenceScrubber() (*EvidenceScrubber, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner := core.NewEvidenceScrubber()
	return &EvidenceScrubber{inner: inner}, nil
}

// NewEvidenceScrubberAggressive creates an evidence scrubber with aggressive settings.
func NewEvidenceScrubberAggressive() (*EvidenceScrubber, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner := core.NewEvidenceScrubberAggressive()
	return &EvidenceScrubber{inner: inner}, nil
}

// Scrub scrubs evidence from text.
func (s *EvidenceScrubber) Scrub(text string) (*ScrubResult, error) {
	return s.inner.Scrub(text)
}

// Free releases the evidence scrubber resources.
func (s *EvidenceScrubber) Free() {
	if s.inner != nil {
		s.inner.Free()
		s.inner = nil
	}
}

// ThresholdGate wraps the rlm-core ThresholdGate.
// Gates memory writes based on confidence thresholds.
type ThresholdGate struct {
	inner *core.ThresholdGate
}

// GateDecision contains the result of evaluating a node against the gate.
type GateDecision = core.GateDecision

// NodeInput represents input for gate evaluation.
type NodeInput = core.NodeInput

// NewThresholdGate creates a new threshold gate with default settings.
func NewThresholdGate() (*ThresholdGate, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner := core.NewThresholdGate()
	return &ThresholdGate{inner: inner}, nil
}

// NewThresholdGateStrict creates a threshold gate with strict settings.
func NewThresholdGateStrict() (*ThresholdGate, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner := core.NewThresholdGateStrict()
	return &ThresholdGate{inner: inner}, nil
}

// NewThresholdGatePermissive creates a threshold gate with permissive settings.
func NewThresholdGatePermissive() (*ThresholdGate, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner := core.NewThresholdGatePermissive()
	return &ThresholdGate{inner: inner}, nil
}

// Evaluate evaluates a node against the threshold gate.
func (g *ThresholdGate) Evaluate(input *NodeInput) (*GateDecision, error) {
	return g.inner.Evaluate(input)
}

// Free releases the threshold gate resources.
func (g *ThresholdGate) Free() {
	if g.inner != nil {
		g.inner.Free()
		g.inner = nil
	}
}

// KL Divergence Functions - exposed as package-level functions

// KLBernoulliBits calculates Bernoulli KL divergence in bits.
func KLBernoulliBits(p, q float64) (float64, error) {
	if !Available() {
		return 0, ErrNotAvailable
	}
	return core.KLBernoulliBits(p, q)
}

// BinaryEntropyBits calculates binary entropy in bits.
func BinaryEntropyBits(p float64) (float64, error) {
	if !Available() {
		return 0, ErrNotAvailable
	}
	return core.BinaryEntropyBits(p)
}

// SurpriseBits calculates surprise in bits.
func SurpriseBits(p float64) (float64, error) {
	if !Available() {
		return 0, ErrNotAvailable
	}
	return core.SurpriseBits(p)
}

// MutualInformationBits calculates mutual information in bits.
func MutualInformationBits(pPrior, pPosterior float64) (float64, error) {
	if !Available() {
		return 0, ErrNotAvailable
	}
	return core.MutualInformationBits(pPrior, pPosterior)
}

// RequiredBitsForSpecificity calculates the required bits for a given specificity level.
func RequiredBitsForSpecificity(specificity float64) (float64, error) {
	if !Available() {
		return 0, ErrNotAvailable
	}
	return core.RequiredBitsForSpecificity(specificity)
}

// AggregateEvidenceBits aggregates evidence bits from multiple sources.
func AggregateEvidenceBits(klValues []float64) (float64, error) {
	if !Available() {
		return 0, ErrNotAvailable
	}
	return core.AggregateEvidenceBits(klValues)
}

// QuickHallucinationCheck performs a quick heuristic check for potential hallucinations.
// Returns a risk score from 0.0 (low risk) to 1.0 (high risk).
func QuickHallucinationCheck(response string) (float64, error) {
	if !Available() {
		return 0, ErrNotAvailable
	}
	return core.QuickHallucinationCheck(response)
}
