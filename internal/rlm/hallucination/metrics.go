// Package hallucination provides information-theoretic hallucination detection
// based on the Strawberry/Pythea methodology.
//
// The core insight is that procedural hallucinations occur when a model has
// correct information in context but fails to use it. Detection works by
// comparing P(claim|evidence) vs P(claim|no evidence) - if removing evidence
// doesn't change confidence, the model wasn't using it (confabulation).
package hallucination

import (
	"math"
)

// VerificationStatus represents the outcome of claim verification.
type VerificationStatus string

const (
	// StatusGrounded indicates the claim is supported by cited evidence.
	StatusGrounded VerificationStatus = "grounded"

	// StatusUnsupported indicates insufficient evidence supports the claim.
	StatusUnsupported VerificationStatus = "unsupported"

	// StatusContradicted indicates the evidence contradicts the claim.
	StatusContradicted VerificationStatus = "contradicted"

	// StatusUnverifiable indicates the claim cannot be verified.
	StatusUnverifiable VerificationStatus = "unverifiable"
)

// BudgetResult contains the information-theoretic analysis of a claim.
type BudgetResult struct {
	// P0 is the pseudo-prior: P(claim is true | WITHOUT evidence).
	P0 float64

	// P1 is the posterior: P(claim is true | WITH evidence).
	P1 float64

	// TargetConfidence is the claimed confidence level (0-1).
	TargetConfidence float64

	// RequiredBits is KL(target || p0) - information needed to reach target from baseline.
	RequiredBits float64

	// ObservedBits is KL(p1 || p0) - actual information gain from evidence.
	ObservedBits float64

	// BudgetGap is RequiredBits - ObservedBits. Positive means insufficient evidence.
	BudgetGap float64

	// Status is the verification outcome based on the budget analysis.
	Status VerificationStatus

	// Confidence is the adjusted confidence based on evidence strength.
	// Computed as min(target, observed/required) when required > 0.
	Confidence float64
}

// KLBernoulli computes the Kullback-Leibler divergence between two Bernoulli
// distributions: KL(Ber(p) || Ber(q)).
//
// Formula: p * log(p/q) + (1-p) * log((1-p)/(1-q))
//
// Returns the divergence in nats (natural logarithm). To convert to bits,
// divide by ln(2) or use KLBernoulliBits.
//
// Edge cases:
//   - Returns 0 when p == q
//   - Returns +Inf when p > 0 and q == 0, or when p < 1 and q == 1
//   - Handles p == 0 or p == 1 correctly (those terms become 0)
func KLBernoulli(p, q float64) float64 {
	// Clamp to valid probability range
	p = clamp(p, 1e-10, 1-1e-10)
	q = clamp(q, 1e-10, 1-1e-10)

	// KL(Ber(p) || Ber(q)) = p*log(p/q) + (1-p)*log((1-p)/(1-q))
	term1 := p * math.Log(p/q)
	term2 := (1 - p) * math.Log((1-p)/(1-q))

	return term1 + term2
}

// KLBernoulliBits computes KL divergence in bits instead of nats.
func KLBernoulliBits(p, q float64) float64 {
	return KLBernoulli(p, q) / math.Ln2
}

// BinaryEntropy computes the entropy of a Bernoulli distribution H(p).
// Formula: -p*log(p) - (1-p)*log(1-p)
// Returns entropy in nats.
func BinaryEntropy(p float64) float64 {
	p = clamp(p, 1e-10, 1-1e-10)
	return -p*math.Log(p) - (1-p)*math.Log(1-p)
}

// BinaryEntropyBits computes binary entropy in bits.
func BinaryEntropyBits(p float64) float64 {
	return BinaryEntropy(p) / math.Ln2
}

// ComputeBudget calculates the information budget for a claim given probability
// estimates with and without evidence.
//
// Parameters:
//   - p0: P(claim is true | without cited evidence) - the pseudo-prior
//   - p1: P(claim is true | with full context) - the posterior
//   - targetConfidence: the stated/claimed confidence level (0-1)
//
// The function computes:
//   - RequiredBits: KL divergence from target to p0 (how much info needed)
//   - ObservedBits: KL divergence from p1 to p0 (how much info evidence provides)
//   - BudgetGap: Required - Observed (positive means insufficient evidence)
//
// [SPEC-08.03] [SPEC-08.04] [SPEC-08.05]
func ComputeBudget(p0, p1, targetConfidence float64) BudgetResult {
	// Clamp inputs to valid probability range
	p0 = clamp(p0, 0.01, 0.99)
	p1 = clamp(p1, 0.01, 0.99)
	targetConfidence = clamp(targetConfidence, 0.01, 0.99)

	// [SPEC-08.04] Calculate information requirements in bits
	requiredBits := KLBernoulliBits(targetConfidence, p0)
	observedBits := KLBernoulliBits(p1, p0)
	budgetGap := requiredBits - observedBits

	// Determine status based on analysis
	status := determineStatus(p0, p1, budgetGap, targetConfidence)

	// Calculate adjusted confidence
	confidence := computeAdjustedConfidence(p1, requiredBits, observedBits, targetConfidence)

	return BudgetResult{
		P0:               p0,
		P1:               p1,
		TargetConfidence: targetConfidence,
		RequiredBits:     requiredBits,
		ObservedBits:     observedBits,
		BudgetGap:        budgetGap,
		Status:           status,
		Confidence:       confidence,
	}
}

// determineStatus classifies the verification outcome based on budget analysis.
// [SPEC-08.05]
func determineStatus(p0, p1, budgetGap, targetConfidence float64) VerificationStatus {
	// If evidence contradicts (p1 much lower than p0), mark as contradicted
	if p1 < 0.3 && p0 > 0.5 {
		return StatusContradicted
	}

	// If p1 is very low regardless of evidence, claim is likely false
	if p1 < 0.2 {
		return StatusContradicted
	}

	// [SPEC-08.05] Flag when RequiredBits > ObservedBits
	// Positive budget gap means evidence doesn't justify confidence
	if budgetGap > 0 {
		// Significant gap indicates hallucination
		if budgetGap > 2.0 {
			return StatusUnsupported
		}
		// Small gap with high p1 might still be acceptable
		if p1 > 0.7 && budgetGap < 1.0 {
			return StatusGrounded
		}
		return StatusUnsupported
	}

	// Negative budget gap means evidence provides more than needed
	return StatusGrounded
}

// computeAdjustedConfidence calculates a calibrated confidence based on
// the evidence strength relative to what's required.
// [SPEC-08.17]
func computeAdjustedConfidence(p1, requiredBits, observedBits, targetConfidence float64) float64 {
	if requiredBits <= 0 {
		return p1
	}

	// Ratio of observed to required information
	ratio := observedBits / requiredBits
	if ratio > 1 {
		ratio = 1
	}

	// Adjusted confidence is the minimum of:
	// - The posterior probability p1
	// - The target scaled by the evidence ratio
	adjusted := math.Min(p1, targetConfidence*ratio)

	return clamp(adjusted, 0, 1)
}

// BitsToTrust converts an information budget gap to a trust score (0-1).
// Negative gap (evidence exceeds requirement) maps to high trust.
// Positive gap (insufficient evidence) maps to low trust.
func BitsToTrust(budgetGap float64) float64 {
	// Use sigmoid-like mapping
	// gap = 0 -> trust = 0.5
	// gap = -5 -> trust ≈ 1.0
	// gap = +5 -> trust ≈ 0.0
	return 1.0 / (1.0 + math.Exp(budgetGap))
}

// TrustToBits converts a trust score to an equivalent budget gap.
// Inverse of BitsToTrust.
func TrustToBits(trust float64) float64 {
	trust = clamp(trust, 0.001, 0.999)
	return -math.Log(trust / (1 - trust))
}

// InformationGain computes the information gained by updating from prior to posterior.
// This is the pointwise mutual information for the observed outcome.
func InformationGain(prior, posterior float64) float64 {
	prior = clamp(prior, 1e-10, 1-1e-10)
	posterior = clamp(posterior, 1e-10, 1-1e-10)
	return math.Log(posterior/prior) / math.Ln2
}

// MinBitsForConfidence computes the minimum bits of evidence needed to
// move from a prior probability to a target confidence.
func MinBitsForConfidence(prior, target float64) float64 {
	return KLBernoulliBits(target, prior)
}

// IsHallucinated returns true if the budget analysis suggests hallucination.
// [SPEC-08.05]
func IsHallucinated(result BudgetResult) bool {
	return result.Status == StatusUnsupported || result.Status == StatusContradicted
}

// SeverityScore returns a 0-1 score indicating hallucination severity.
// 0 = definitely grounded, 1 = definitely hallucinated.
func SeverityScore(result BudgetResult) float64 {
	switch result.Status {
	case StatusGrounded:
		// Even grounded claims might have some uncertainty
		return clamp(result.BudgetGap/10.0, 0, 0.3)
	case StatusUnsupported:
		// Scale by budget gap, capped at 0.9
		return clamp(0.5+result.BudgetGap/10.0, 0.5, 0.9)
	case StatusContradicted:
		return 0.95
	case StatusUnverifiable:
		return 0.5
	default:
		return 0.5
	}
}

// clamp restricts a value to [min, max].
func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// IntervalBudget computes budget with confidence intervals for p0 and p1.
// This provides conservative bounds when probabilities are uncertain.
type IntervalBudget struct {
	// P0 interval
	P0Low, P0High float64
	// P1 interval
	P1Low, P1High float64
	// Target confidence
	TargetConfidence float64

	// Conservative budget gap (using worst-case bounds)
	BudgetGapMin float64 // Most optimistic (lowest gap)
	BudgetGapMax float64 // Most pessimistic (highest gap)
}

// ComputeIntervalBudget calculates budget with uncertainty intervals.
// Uses interval arithmetic to provide conservative bounds.
func ComputeIntervalBudget(p0Low, p0High, p1Low, p1High, target float64) IntervalBudget {
	// Required bits: KL(target || p0)
	// Higher p0 means lower required bits (easier to reach target)
	// So: requiredMin uses p0High, requiredMax uses p0Low
	requiredMin := KLBernoulliBits(target, p0High)
	requiredMax := KLBernoulliBits(target, p0Low)

	// Observed bits: KL(p1 || p0)
	// This is more complex - need to consider all combinations
	// For conservative flagging, we want the minimum observed bits
	observedMin := math.Min(
		math.Min(KLBernoulliBits(p1Low, p0Low), KLBernoulliBits(p1Low, p0High)),
		math.Min(KLBernoulliBits(p1High, p0Low), KLBernoulliBits(p1High, p0High)),
	)
	observedMax := math.Max(
		math.Max(KLBernoulliBits(p1Low, p0Low), KLBernoulliBits(p1Low, p0High)),
		math.Max(KLBernoulliBits(p1High, p0Low), KLBernoulliBits(p1High, p0High)),
	)

	return IntervalBudget{
		P0Low:            p0Low,
		P0High:           p0High,
		P1Low:            p1Low,
		P1High:           p1High,
		TargetConfidence: target,
		BudgetGapMin:     requiredMin - observedMax, // Best case
		BudgetGapMax:     requiredMax - observedMin, // Worst case
	}
}

// ShouldFlag returns true if the interval analysis suggests flagging.
// Uses conservative criterion: flag if even best-case gap is positive.
func (ib IntervalBudget) ShouldFlag() bool {
	return ib.BudgetGapMin > 0
}

// MightBeHallucinated returns true if hallucination is possible.
// Less conservative: true if worst-case gap is positive.
func (ib IntervalBudget) MightBeHallucinated() bool {
	return ib.BudgetGapMax > 0
}
