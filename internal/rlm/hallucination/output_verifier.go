// Package hallucination provides information-theoretic hallucination detection.
package hallucination

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// OutputVerifier verifies agent outputs before returning to users.
// [SPEC-08.19-22]
type OutputVerifier struct {
	detector *Detector
	config   OutputVerifierConfig
	logger   *slog.Logger

	// Statistics
	responsesVerified int64
	responsesFlagged  int64
	claimsVerified    int64
	claimsFlagged     int64
}

// OutputVerifierConfig configures the output verifier.
type OutputVerifierConfig struct {
	// Enabled enables output verification.
	Enabled bool

	// FlagThresholdBits is the budget gap threshold for flagging claims.
	FlagThresholdBits float64

	// WarnOnFlag shows warnings instead of blocking.
	// [SPEC-08.22] SHOULD NOT automatically reject user-facing outputs.
	WarnOnFlag bool

	// MaxClaimsToVerify limits the number of claims to verify per response.
	MaxClaimsToVerify int

	// SkipShortResponses skips verification for responses below this length.
	SkipShortResponses int

	// IncludeExplanations includes detailed explanations in results.
	IncludeExplanations bool
}

// DefaultOutputVerifierConfig returns sensible defaults.
func DefaultOutputVerifierConfig() OutputVerifierConfig {
	return OutputVerifierConfig{
		Enabled:             false,
		FlagThresholdBits:   5.0, // Higher threshold for outputs (more permissive)
		WarnOnFlag:          true,
		MaxClaimsToVerify:   20,
		SkipShortResponses:  50,
		IncludeExplanations: true,
	}
}

// ClaimVerificationResult contains the result of verifying a single claim.
type ClaimVerificationResult struct {
	// Claim is the verified claim.
	Claim Claim

	// Status is the verification outcome.
	Status VerificationStatus

	// BudgetGap is the information budget gap.
	BudgetGap float64

	// P0 is probability without evidence.
	P0 float64

	// P1 is probability with evidence.
	P1 float64

	// Flagged indicates if this claim exceeds the threshold.
	Flagged bool

	// Explanation describes the verification result.
	Explanation string
}

// OutputVerificationResult contains the result of verifying an entire output.
type OutputVerificationResult struct {
	// Response is the original response text.
	Response string

	// Context is the conversation context used for verification.
	Context string

	// ClaimResults contains per-claim verification results.
	ClaimResults []ClaimVerificationResult

	// TotalClaims is the number of claims extracted.
	TotalClaims int

	// VerifiedClaims is the number of claims actually verified.
	VerifiedClaims int

	// FlaggedClaims is the number of claims that exceeded threshold.
	FlaggedClaims int

	// OverallRisk is an aggregate risk score (0.0-1.0).
	OverallRisk float64

	// Flagged indicates if the overall response is flagged.
	Flagged bool

	// Warning is a user-facing warning message if flagged.
	Warning string

	// Duration is how long verification took.
	Duration time.Duration

	// Skipped indicates if verification was skipped.
	Skipped bool

	// SkipReason explains why verification was skipped.
	SkipReason string
}

// NewOutputVerifier creates a new output verifier.
func NewOutputVerifier(detector *Detector, config OutputVerifierConfig) *OutputVerifier {
	if config.MaxClaimsToVerify <= 0 {
		config.MaxClaimsToVerify = 20
	}
	if config.FlagThresholdBits <= 0 {
		config.FlagThresholdBits = 5.0
	}

	return &OutputVerifier{
		detector: detector,
		config:   config,
		logger:   slog.Default(),
	}
}

// SetLogger sets the logger for the output verifier.
func (v *OutputVerifier) SetLogger(logger *slog.Logger) {
	v.logger = logger
}

// VerifyOutput verifies an agent response against conversation context.
// [SPEC-08.19] Agent responses SHALL be verified before returning to the user.
// [SPEC-08.20] Extract claims and verify each against context.
func (v *OutputVerifier) VerifyOutput(ctx context.Context, response string, conversationContext string) (*OutputVerificationResult, error) {
	start := time.Now()

	result := &OutputVerificationResult{
		Response: response,
		Context:  conversationContext,
	}

	// Check if verification should be skipped
	if !v.config.Enabled || v.detector == nil {
		result.Skipped = true
		result.SkipReason = "verification disabled"
		result.Duration = time.Since(start)
		return result, nil
	}

	if len(response) < v.config.SkipShortResponses {
		result.Skipped = true
		result.SkipReason = fmt.Sprintf("response too short (%d < %d chars)", len(response), v.config.SkipShortResponses)
		result.Duration = time.Since(start)
		return result, nil
	}

	v.responsesVerified++

	// [SPEC-08.20] Extract claims from the response
	claims := v.detector.extractor.ExtractAssertive(response, "response")
	result.TotalClaims = len(claims)

	if len(claims) == 0 {
		result.Skipped = true
		result.SkipReason = "no assertive claims found"
		result.Duration = time.Since(start)
		return result, nil
	}

	// Limit claims to verify
	claimsToVerify := claims
	if len(claimsToVerify) > v.config.MaxClaimsToVerify {
		claimsToVerify = claimsToVerify[:v.config.MaxClaimsToVerify]
	}

	// Verify each claim
	var totalBudgetGap float64
	for _, claim := range claimsToVerify {
		claimResult := v.verifyClaim(ctx, &claim, conversationContext)
		result.ClaimResults = append(result.ClaimResults, claimResult)
		result.VerifiedClaims++
		v.claimsVerified++

		if claimResult.Flagged {
			result.FlaggedClaims++
			v.claimsFlagged++
		}

		totalBudgetGap += claimResult.BudgetGap
	}

	// Calculate overall risk
	if result.VerifiedClaims > 0 {
		avgBudgetGap := totalBudgetGap / float64(result.VerifiedClaims)
		// Convert budget gap to risk score (0-1)
		// Higher budget gap = higher risk
		result.OverallRisk = 1.0 - (1.0 / (1.0 + avgBudgetGap/v.config.FlagThresholdBits))

		// Flag if too many claims are flagged
		flagRatio := float64(result.FlaggedClaims) / float64(result.VerifiedClaims)
		if flagRatio > 0.3 || result.OverallRisk > 0.5 {
			result.Flagged = true
			v.responsesFlagged++

			if v.config.WarnOnFlag {
				result.Warning = v.generateWarning(result)
			}
		}
	}

	result.Duration = time.Since(start)

	// Log results
	if result.Flagged {
		v.logger.Warn("output flagged for potential hallucination",
			"total_claims", result.TotalClaims,
			"flagged_claims", result.FlaggedClaims,
			"overall_risk", result.OverallRisk,
			"duration", result.Duration,
		)
	} else {
		v.logger.Debug("output verified",
			"total_claims", result.TotalClaims,
			"verified_claims", result.VerifiedClaims,
			"overall_risk", result.OverallRisk,
			"duration", result.Duration,
		)
	}

	return result, nil
}

// verifyClaim verifies a single claim against context.
func (v *OutputVerifier) verifyClaim(ctx context.Context, claim *Claim, context string) ClaimVerificationResult {
	result := ClaimVerificationResult{
		Claim: *claim,
	}

	// Use the detector to verify
	verificationResult, err := v.detector.VerifyClaimWithEvidence(ctx, claim, context)
	if err != nil {
		result.Status = StatusUnverifiable
		result.Explanation = fmt.Sprintf("verification error: %v", err)
		return result
	}

	result.Status = verificationResult.Status
	result.BudgetGap = verificationResult.BudgetGap
	result.P0 = verificationResult.P0
	result.P1 = verificationResult.P1

	// [SPEC-08.22] Flag but don't reject
	if result.BudgetGap > v.config.FlagThresholdBits {
		result.Flagged = true
	}

	if v.config.IncludeExplanations {
		result.Explanation = verificationResult.Explanation
	}

	return result
}

// generateWarning creates a user-facing warning for flagged responses.
func (v *OutputVerifier) generateWarning(result *OutputVerificationResult) string {
	if result.FlaggedClaims == 1 {
		return "Note: One claim in this response may not be fully supported by the available context."
	}
	return fmt.Sprintf("Note: %d claims in this response may not be fully supported by the available context.",
		result.FlaggedClaims)
}

// GetVerificationResultForSelfCorrection returns verification results for agent self-correction.
// [SPEC-08.21] Verification results SHOULD be made available to the agent for self-correction.
func (v *OutputVerifier) GetVerificationResultForSelfCorrection(result *OutputVerificationResult) *SelfCorrectionHint {
	if result == nil || result.Skipped || !result.Flagged {
		return nil
	}

	hint := &SelfCorrectionHint{
		ShouldRevise:  result.Flagged,
		OverallRisk:   result.OverallRisk,
		FlaggedClaims: make([]FlaggedClaimHint, 0, result.FlaggedClaims),
	}

	for _, cr := range result.ClaimResults {
		if cr.Flagged {
			hint.FlaggedClaims = append(hint.FlaggedClaims, FlaggedClaimHint{
				Content:     cr.Claim.Content,
				Position:    cr.Claim.Position,
				BudgetGap:   cr.BudgetGap,
				Status:      cr.Status,
				Explanation: cr.Explanation,
			})
		}
	}

	if len(hint.FlaggedClaims) > 0 {
		hint.Suggestion = v.generateSuggestion(hint)
	}

	return hint
}

// SelfCorrectionHint provides hints for agent self-correction.
type SelfCorrectionHint struct {
	// ShouldRevise indicates the agent should consider revising.
	ShouldRevise bool

	// OverallRisk is the aggregate risk score.
	OverallRisk float64

	// FlaggedClaims contains claims that should be reviewed.
	FlaggedClaims []FlaggedClaimHint

	// Suggestion is a natural language suggestion for revision.
	Suggestion string
}

// FlaggedClaimHint provides information about a flagged claim.
type FlaggedClaimHint struct {
	// Content is the claim text.
	Content string

	// Position is the character offset in the response.
	Position int

	// BudgetGap is the information budget gap.
	BudgetGap float64

	// Status is the verification status.
	Status VerificationStatus

	// Explanation describes why this claim was flagged.
	Explanation string
}

// generateSuggestion creates a suggestion for self-correction.
func (v *OutputVerifier) generateSuggestion(hint *SelfCorrectionHint) string {
	if len(hint.FlaggedClaims) == 0 {
		return ""
	}

	// Check for contradicted claims
	hasContradicted := false
	hasUnsupported := false
	for _, fc := range hint.FlaggedClaims {
		if fc.Status == StatusContradicted {
			hasContradicted = true
		}
		if fc.Status == StatusUnsupported {
			hasUnsupported = true
		}
	}

	if hasContradicted {
		return "Some claims appear to contradict the available context. Consider revising or adding qualifiers."
	}
	if hasUnsupported {
		return "Some claims lack sufficient support in the context. Consider adding citations or hedging language."
	}
	return "Some claims may not be fully grounded. Consider reviewing for accuracy."
}

// OutputVerifierStats contains statistics about output verification.
type OutputVerifierStats struct {
	ResponsesVerified int64   `json:"responses_verified"`
	ResponsesFlagged  int64   `json:"responses_flagged"`
	ClaimsVerified    int64   `json:"claims_verified"`
	ClaimsFlagged     int64   `json:"claims_flagged"`
	FlagRate          float64 `json:"flag_rate"`
}

// Stats returns current statistics.
func (v *OutputVerifier) Stats() OutputVerifierStats {
	stats := OutputVerifierStats{
		ResponsesVerified: v.responsesVerified,
		ResponsesFlagged:  v.responsesFlagged,
		ClaimsVerified:    v.claimsVerified,
		ClaimsFlagged:     v.claimsFlagged,
	}

	if v.responsesVerified > 0 {
		stats.FlagRate = float64(v.responsesFlagged) / float64(v.responsesVerified)
	}

	return stats
}

// Config returns the current configuration.
func (v *OutputVerifier) Config() OutputVerifierConfig {
	return v.config
}

// Enabled returns whether output verification is enabled.
func (v *OutputVerifier) Enabled() bool {
	return v.config.Enabled && v.detector != nil
}
