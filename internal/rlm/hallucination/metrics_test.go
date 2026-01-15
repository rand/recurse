package hallucination

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKLBernoulli tests the KL divergence calculation for Bernoulli distributions.
// [SPEC-08.06]
func TestKLBernoulli(t *testing.T) {
	tests := []struct {
		name     string
		p        float64
		q        float64
		expected float64
		delta    float64
	}{
		{
			name:     "identical distributions",
			p:        0.5,
			q:        0.5,
			expected: 0.0,
			delta:    1e-6,
		},
		{
			name:     "p=0.9 q=0.5 (high divergence)",
			p:        0.9,
			q:        0.5,
			expected: 0.368, // KL(0.9||0.5) ≈ 0.368 nats
			delta:    0.01,
		},
		{
			name:     "p=0.1 q=0.5",
			p:        0.1,
			q:        0.5,
			expected: 0.368, // Symmetric case
			delta:    0.01,
		},
		{
			name:     "p=0.7 q=0.3",
			p:        0.7,
			q:        0.3,
			expected: 0.339, // Moderate divergence
			delta:    0.01,
		},
		{
			name:     "edge case p near 0",
			p:        0.001,
			q:        0.5,
			expected: 0.688, // Should handle gracefully (clamped)
			delta:    0.1,
		},
		{
			name:     "edge case p near 1",
			p:        0.999,
			q:        0.5,
			expected: 0.688, // Should handle gracefully (clamped)
			delta:    0.1,
		},
		{
			name:     "edge case q near 0",
			p:        0.5,
			q:        0.001,
			expected: 2.76, // Large divergence (clamped q)
			delta:    0.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := KLBernoulli(tt.p, tt.q)
			assert.InDelta(t, tt.expected, result, tt.delta,
				"KL(%v || %v) = %v, expected %v", tt.p, tt.q, result, tt.expected)
		})
	}
}

// TestKLBernoulliBits verifies the bits conversion.
func TestKLBernoulliBits(t *testing.T) {
	// KL in bits should be KL in nats divided by ln(2)
	p, q := 0.9, 0.5
	nats := KLBernoulli(p, q)
	bits := KLBernoulliBits(p, q)

	assert.InDelta(t, nats/math.Ln2, bits, 1e-6)
}

// TestBinaryEntropy tests the binary entropy calculation.
func TestBinaryEntropy(t *testing.T) {
	tests := []struct {
		name     string
		p        float64
		expected float64
		delta    float64
	}{
		{
			name:     "maximum entropy at p=0.5",
			p:        0.5,
			expected: math.Ln2, // ln(2) ≈ 0.693 nats
			delta:    1e-6,
		},
		{
			name:     "low entropy at p=0.9",
			p:        0.9,
			expected: 0.325, // H(0.9) ≈ 0.325 nats
			delta:    0.01,
		},
		{
			name:     "low entropy at p=0.1",
			p:        0.1,
			expected: 0.325, // Symmetric
			delta:    0.01,
		},
		{
			name:     "near zero entropy at p=0.99",
			p:        0.99,
			expected: 0.056,
			delta:    0.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BinaryEntropy(tt.p)
			assert.InDelta(t, tt.expected, result, tt.delta)
		})
	}
}

// TestBinaryEntropyBits verifies bits conversion.
func TestBinaryEntropyBits(t *testing.T) {
	// Maximum entropy should be 1 bit at p=0.5
	assert.InDelta(t, 1.0, BinaryEntropyBits(0.5), 1e-6)
}

// TestComputeBudget tests the main budget computation.
// [SPEC-08.04] [SPEC-08.05]
func TestComputeBudget(t *testing.T) {
	tests := []struct {
		name             string
		p0               float64
		p1               float64
		targetConfidence float64
		expectGrounded   bool
		expectGapSign    int // -1 for negative, 0 for ~0, +1 for positive
	}{
		{
			name:             "well-grounded claim",
			p0:               0.3,  // Low prior without evidence
			p1:               0.9,  // High posterior with evidence
			targetConfidence: 0.8,  // Claimed confidence
			expectGrounded:   true, // Evidence strongly supports
			expectGapSign:    -1,   // Negative gap = more evidence than needed
		},
		{
			name:             "hallucinated claim - evidence doesn't help",
			p0:               0.7, // High prior without evidence
			p1:               0.7, // Same with evidence (no information gain)
			targetConfidence: 0.9, // High claimed confidence
			expectGrounded:   false,
			expectGapSign:    +1, // Positive gap = insufficient evidence
		},
		{
			name:             "contradicted claim",
			p0:               0.6, // Moderate prior
			p1:               0.1, // Evidence contradicts
			targetConfidence: 0.9,
			expectGrounded:   false,
			expectGapSign:    0, // Gap sign varies; contradiction detected by low p1, not gap
		},
		{
			name:             "marginally supported",
			p0:               0.5,
			p1:               0.75,
			targetConfidence: 0.7,
			expectGrounded:   true,
			expectGapSign:    -1,
		},
		{
			name:             "overconfident claim",
			p0:               0.5,
			p1:               0.6, // Slight improvement
			targetConfidence: 0.95, // But very high claimed confidence
			expectGrounded:   false,
			expectGapSign:    +1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ComputeBudget(tt.p0, tt.p1, tt.targetConfidence)

			// Verify structure
			assert.Equal(t, tt.p0, result.P0)
			assert.Equal(t, tt.p1, result.P1)
			assert.Equal(t, tt.targetConfidence, result.TargetConfidence)

			// Verify budget gap sign
			switch tt.expectGapSign {
			case -1:
				assert.Less(t, result.BudgetGap, 0.0,
					"expected negative budget gap, got %v", result.BudgetGap)
			case +1:
				assert.Greater(t, result.BudgetGap, 0.0,
					"expected positive budget gap, got %v", result.BudgetGap)
			}

			// Verify grounded status
			if tt.expectGrounded {
				assert.Equal(t, StatusGrounded, result.Status,
					"expected grounded, got %v", result.Status)
			} else {
				assert.NotEqual(t, StatusGrounded, result.Status,
					"expected not grounded, got %v", result.Status)
			}

			// Verify confidence is bounded
			assert.GreaterOrEqual(t, result.Confidence, 0.0)
			assert.LessOrEqual(t, result.Confidence, 1.0)
		})
	}
}

// TestDetermineStatus tests the status determination logic.
// [SPEC-08.05]
func TestDetermineStatus(t *testing.T) {
	tests := []struct {
		name             string
		p0               float64
		p1               float64
		budgetGap        float64
		targetConfidence float64
		expected         VerificationStatus
	}{
		{
			name:             "contradicted - p1 much lower than p0",
			p0:               0.7,
			p1:               0.2,
			budgetGap:        2.0,
			targetConfidence: 0.9,
			expected:         StatusContradicted,
		},
		{
			name:             "contradicted - very low p1",
			p0:               0.3,
			p1:               0.1,
			budgetGap:        1.0,
			targetConfidence: 0.8,
			expected:         StatusContradicted,
		},
		{
			name:             "unsupported - large budget gap",
			p0:               0.5,
			p1:               0.6,
			budgetGap:        3.0,
			targetConfidence: 0.9,
			expected:         StatusUnsupported,
		},
		{
			name:             "grounded - negative gap",
			p0:               0.3,
			p1:               0.9,
			budgetGap:        -2.0,
			targetConfidence: 0.8,
			expected:         StatusGrounded,
		},
		{
			name:             "grounded - small gap with high p1",
			p0:               0.5,
			p1:               0.8,
			budgetGap:        0.5,
			targetConfidence: 0.85,
			expected:         StatusGrounded,
		},
		{
			name:             "unsupported - moderate gap",
			p0:               0.5,
			p1:               0.55,
			budgetGap:        1.5,
			targetConfidence: 0.9,
			expected:         StatusUnsupported,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := determineStatus(tt.p0, tt.p1, tt.budgetGap, tt.targetConfidence)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestBitsToTrust tests the sigmoid mapping from budget gap to trust.
func TestBitsToTrust(t *testing.T) {
	// Zero gap -> 0.5 trust
	assert.InDelta(t, 0.5, BitsToTrust(0), 1e-6)

	// Negative gap (evidence exceeds requirement) -> high trust
	assert.Greater(t, BitsToTrust(-5), 0.99)

	// Positive gap (insufficient evidence) -> low trust
	assert.Less(t, BitsToTrust(5), 0.01)

	// Monotonically decreasing
	assert.Greater(t, BitsToTrust(-1), BitsToTrust(0))
	assert.Greater(t, BitsToTrust(0), BitsToTrust(1))
}

// TestTrustToBits tests the inverse mapping.
func TestTrustToBits(t *testing.T) {
	// Round-trip test
	for _, trust := range []float64{0.1, 0.3, 0.5, 0.7, 0.9} {
		bits := TrustToBits(trust)
		recovered := BitsToTrust(bits)
		assert.InDelta(t, trust, recovered, 1e-6)
	}
}

// TestInformationGain tests the pointwise information gain calculation.
func TestInformationGain(t *testing.T) {
	// Doubling probability = 1 bit of information
	gain := InformationGain(0.25, 0.5)
	assert.InDelta(t, 1.0, gain, 0.01)

	// No change = 0 bits
	gain = InformationGain(0.5, 0.5)
	assert.InDelta(t, 0.0, gain, 1e-6)

	// Negative gain when posterior < prior
	gain = InformationGain(0.5, 0.25)
	assert.InDelta(t, -1.0, gain, 0.01)
}

// TestIsHallucinated tests the hallucination detection predicate.
// [SPEC-08.05]
func TestIsHallucinated(t *testing.T) {
	assert.True(t, IsHallucinated(BudgetResult{Status: StatusUnsupported}))
	assert.True(t, IsHallucinated(BudgetResult{Status: StatusContradicted}))
	assert.False(t, IsHallucinated(BudgetResult{Status: StatusGrounded}))
	assert.False(t, IsHallucinated(BudgetResult{Status: StatusUnverifiable}))
}

// TestSeverityScore tests the severity scoring.
func TestSeverityScore(t *testing.T) {
	// Grounded claims have low severity
	score := SeverityScore(BudgetResult{Status: StatusGrounded, BudgetGap: -1})
	assert.LessOrEqual(t, score, 0.3)

	// Unsupported claims have moderate-high severity
	score = SeverityScore(BudgetResult{Status: StatusUnsupported, BudgetGap: 2})
	assert.GreaterOrEqual(t, score, 0.5)
	assert.LessOrEqual(t, score, 0.9)

	// Contradicted claims have high severity
	score = SeverityScore(BudgetResult{Status: StatusContradicted})
	assert.Equal(t, 0.95, score)

	// Unverifiable claims have neutral severity
	score = SeverityScore(BudgetResult{Status: StatusUnverifiable})
	assert.Equal(t, 0.5, score)
}

// TestComputeIntervalBudget tests interval arithmetic for uncertain probabilities.
func TestComputeIntervalBudget(t *testing.T) {
	// Case: well-supported with tight intervals
	ib := ComputeIntervalBudget(0.28, 0.32, 0.85, 0.95, 0.9)
	assert.Less(t, ib.BudgetGapMin, 0.0, "best case should have negative gap")

	// Case: poorly supported
	ib = ComputeIntervalBudget(0.45, 0.55, 0.55, 0.65, 0.95)
	assert.Greater(t, ib.BudgetGapMax, 0.0, "worst case should have positive gap")
}

// TestIntervalBudgetShouldFlag tests conservative flagging.
func TestIntervalBudgetShouldFlag(t *testing.T) {
	// Definitely should flag: even best case has positive gap
	ib := IntervalBudget{BudgetGapMin: 1.0, BudgetGapMax: 3.0}
	assert.True(t, ib.ShouldFlag())

	// Definitely should not flag: even worst case has negative gap
	ib = IntervalBudget{BudgetGapMin: -3.0, BudgetGapMax: -1.0}
	assert.False(t, ib.ShouldFlag())

	// Uncertain: best case OK, worst case bad
	ib = IntervalBudget{BudgetGapMin: -0.5, BudgetGapMax: 1.5}
	assert.False(t, ib.ShouldFlag())
	assert.True(t, ib.MightBeHallucinated())
}

// TestComputeAdjustedConfidence tests confidence adjustment.
// [SPEC-08.17]
func TestComputeAdjustedConfidence(t *testing.T) {
	// Full evidence support
	conf := computeAdjustedConfidence(0.9, 2.0, 3.0, 0.9)
	assert.InDelta(t, 0.9, conf, 0.01) // ratio > 1, capped at 1

	// Partial evidence support
	conf = computeAdjustedConfidence(0.9, 4.0, 2.0, 0.9)
	// ratio = 0.5, adjusted = min(0.9, 0.9*0.5) = 0.45
	assert.InDelta(t, 0.45, conf, 0.01)

	// Edge case: zero required bits
	conf = computeAdjustedConfidence(0.8, 0, 1.0, 0.9)
	assert.Equal(t, 0.8, conf) // Returns p1 directly
}

// TestClamp tests the clamp helper function.
func TestClamp(t *testing.T) {
	assert.Equal(t, 0.5, clamp(0.5, 0.0, 1.0))
	assert.Equal(t, 0.0, clamp(-0.5, 0.0, 1.0))
	assert.Equal(t, 1.0, clamp(1.5, 0.0, 1.0))
}

// TestKnownValues validates against Strawberry reference implementation values.
// These are specific test cases to ensure algorithm correctness.
func TestKnownValues(t *testing.T) {
	t.Run("Strawberry-style detection", func(t *testing.T) {
		// Scenario: Model claims fact with 90% confidence
		// Evidence removal test: without evidence, model gives 85% (barely changes)
		// This indicates procedural hallucination - model wasn't using evidence

		p0 := 0.85 // High confidence even without evidence (suspicious!)
		p1 := 0.90 // Only slightly higher with evidence
		target := 0.90

		result := ComputeBudget(p0, p1, target)

		// Key insight: small difference between p0 and p1 means evidence isn't being used
		// This should be flagged as potentially hallucinated

		// Observed bits should be small (little information gain)
		assert.Less(t, result.ObservedBits, 0.5,
			"little information gain from evidence")

		// Required bits to reach 90% from 85% prior is small but positive
		require.Greater(t, result.RequiredBits, 0.0)

		t.Logf("p0=%.2f, p1=%.2f, target=%.2f", p0, p1, target)
		t.Logf("RequiredBits=%.3f, ObservedBits=%.3f, Gap=%.3f",
			result.RequiredBits, result.ObservedBits, result.BudgetGap)
		t.Logf("Status=%s, Confidence=%.2f", result.Status, result.Confidence)
	})

	t.Run("Legitimate high confidence", func(t *testing.T) {
		// Scenario: Model claims fact with 90% confidence
		// Without evidence: 30% (low prior)
		// With evidence: 95% (strong support)
		// Evidence clearly helps - not a hallucination

		p0 := 0.30
		p1 := 0.95
		target := 0.90

		result := ComputeBudget(p0, p1, target)

		// Large information gain
		assert.Greater(t, result.ObservedBits, 1.0,
			"substantial information gain from evidence")

		// Should be grounded
		assert.Equal(t, StatusGrounded, result.Status)

		// Negative budget gap (evidence exceeds requirement)
		assert.Less(t, result.BudgetGap, 0.0)

		t.Logf("p0=%.2f, p1=%.2f, target=%.2f", p0, p1, target)
		t.Logf("RequiredBits=%.3f, ObservedBits=%.3f, Gap=%.3f",
			result.RequiredBits, result.ObservedBits, result.BudgetGap)
	})
}
