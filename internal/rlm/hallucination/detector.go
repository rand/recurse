// Package hallucination provides information-theoretic hallucination detection.
package hallucination

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Detector orchestrates all hallucination detection components.
// [SPEC-08.01] [SPEC-08.02]
type Detector struct {
	backend   VerifierBackend
	extractor *ClaimExtractor
	scrubber  *EvidenceScrubber
	config    DetectorConfig
}

// DetectorConfig configures the hallucination detector.
type DetectorConfig struct {
	// MemoryGateEnabled enables verification for memory storage.
	MemoryGateEnabled bool

	// OutputVerifyEnabled enables verification of agent outputs.
	OutputVerifyEnabled bool

	// TraceAuditEnabled enables reasoning trace auditing.
	TraceAuditEnabled bool

	// FlagThresholdBits is the budget gap threshold for flagging.
	// Claims with BudgetGap > threshold are flagged.
	FlagThresholdBits float64

	// MinConfidence is the minimum confidence to verify.
	// Claims below this are auto-flagged.
	MinConfidence float64

	// Timeout for verification operations.
	Timeout time.Duration

	// RejectUnsupported rejects unsupported claims in memory gate.
	RejectUnsupported bool
}

// DefaultDetectorConfig returns sensible defaults.
func DefaultDetectorConfig() DetectorConfig {
	return DetectorConfig{
		MemoryGateEnabled:   false, // Disabled by default for performance
		OutputVerifyEnabled: false,
		TraceAuditEnabled:   false,
		FlagThresholdBits:   2.0,
		MinConfidence:       0.5,
		Timeout:             2 * time.Second,
		RejectUnsupported:   false,
	}
}

// NewDetector creates a new hallucination detector.
func NewDetector(backend VerifierBackend, config DetectorConfig) *Detector {
	return &Detector{
		backend:   backend,
		extractor: NewClaimExtractor(),
		scrubber:  NewEvidenceScrubber(),
		config:    config,
	}
}

// VerificationResult contains the result of verifying a single claim.
type VerificationResult struct {
	// Claim is the verified claim.
	Claim *Claim

	// P0 is the pseudo-prior: P(claim | WITHOUT evidence).
	P0 float64

	// P1 is the posterior: P(claim | WITH evidence).
	P1 float64

	// RequiredBits is the information needed to reach target confidence.
	RequiredBits float64

	// ObservedBits is the actual information provided by evidence.
	ObservedBits float64

	// BudgetGap is RequiredBits - ObservedBits.
	BudgetGap float64

	// Status is the verification outcome.
	Status VerificationStatus

	// AdjustedConfidence is the evidence-adjusted confidence.
	AdjustedConfidence float64

	// Explanation describes the verification result.
	Explanation string

	// Duration is how long verification took.
	Duration time.Duration

	// Error is any error that occurred (non-fatal).
	Error error
}

// VerifyClaimWithEvidence verifies a claim against provided evidence.
func (d *Detector) VerifyClaimWithEvidence(ctx context.Context, claim *Claim, evidence string) (*VerificationResult, error) {
	start := time.Now()

	// Apply timeout
	if d.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d.config.Timeout)
		defer cancel()
	}

	result := &VerificationResult{
		Claim: claim,
	}

	// Get P1: probability with evidence
	p1, err := d.backend.EstimateProbability(ctx, claim.Content, evidence)
	if err != nil {
		result.Error = fmt.Errorf("p1 estimation failed: %w", err)
		result.Status = StatusUnverifiable
		result.Explanation = "verification failed - returning unverifiable"
		result.Duration = time.Since(start)
		return result, nil // Non-fatal error
	}
	result.P1 = p1

	// Get P0: probability without evidence (scrubbed context)
	scrubbedEvidence := d.scrubber.ScrubByPattern(evidence, evidence).Scrubbed
	if scrubbedEvidence == evidence {
		// If scrubbing didn't change anything, use empty context
		scrubbedEvidence = "[No specific evidence provided]"
	}

	p0, err := d.backend.EstimateProbability(ctx, claim.Content, scrubbedEvidence)
	if err != nil {
		result.Error = fmt.Errorf("p0 estimation failed: %w", err)
		result.Status = StatusUnverifiable
		result.Explanation = "p0 estimation failed - returning unverifiable"
		result.Duration = time.Since(start)
		return result, nil
	}
	result.P0 = p0

	// Compute budget metrics
	budget := ComputeBudget(p0, p1, claim.Confidence)

	result.RequiredBits = budget.RequiredBits
	result.ObservedBits = budget.ObservedBits
	result.BudgetGap = budget.BudgetGap
	result.Status = budget.Status
	result.AdjustedConfidence = budget.Confidence
	result.Duration = time.Since(start)

	// Generate explanation
	result.Explanation = d.generateExplanation(result)

	slog.Debug("claim verified",
		"claim", truncate(claim.Content, 50),
		"p0", fmt.Sprintf("%.3f", p0),
		"p1", fmt.Sprintf("%.3f", p1),
		"budget_gap", fmt.Sprintf("%.3f", result.BudgetGap),
		"status", result.Status,
		"duration_ms", result.Duration.Milliseconds(),
	)

	return result, nil
}

// VerifyText extracts claims from text and verifies each against the context.
func (d *Detector) VerifyText(ctx context.Context, text, evidence string) ([]VerificationResult, error) {
	// Extract assertive claims only
	claims := d.extractor.ExtractAssertive(text, "text")

	if len(claims) == 0 {
		return nil, nil
	}

	results := make([]VerificationResult, 0, len(claims))

	for i := range claims {
		result, err := d.VerifyClaimWithEvidence(ctx, &claims[i], evidence)
		if err != nil {
			// Log but continue with other claims
			slog.Warn("claim verification failed", "error", err, "claim", truncate(claims[i].Content, 50))
			continue
		}
		results = append(results, *result)
	}

	return results, nil
}

// VerifyTextBatch is a more efficient batch verification.
func (d *Detector) VerifyTextBatch(ctx context.Context, text, evidence string) ([]VerificationResult, error) {
	claims := d.extractor.ExtractAssertive(text, "text")
	if len(claims) == 0 {
		return nil, nil
	}

	// Prepare claim contents for batch estimation
	claimContents := make([]string, len(claims))
	for i, c := range claims {
		claimContents[i] = c.Content
	}

	// Get P1s in batch
	p1s, err := d.backend.BatchEstimate(ctx, claimContents, evidence)
	if err != nil {
		// Fall back to sequential
		return d.VerifyText(ctx, text, evidence)
	}

	// Get P0s in batch with scrubbed evidence
	scrubbedEvidence := "[No specific evidence provided]"
	p0s, err := d.backend.BatchEstimate(ctx, claimContents, scrubbedEvidence)
	if err != nil {
		// Fall back to sequential
		return d.VerifyText(ctx, text, evidence)
	}

	// Build results
	results := make([]VerificationResult, len(claims))
	for i := range claims {
		budget := ComputeBudget(p0s[i], p1s[i], claims[i].Confidence)

		results[i] = VerificationResult{
			Claim:              &claims[i],
			P0:                 p0s[i],
			P1:                 p1s[i],
			RequiredBits:       budget.RequiredBits,
			ObservedBits:       budget.ObservedBits,
			BudgetGap:          budget.BudgetGap,
			Status:             budget.Status,
			AdjustedConfidence: budget.Confidence,
		}
		results[i].Explanation = d.generateExplanation(&results[i])
	}

	return results, nil
}

// TraceStep represents a step in a reasoning trace.
type TraceStep struct {
	// Content is the reasoning step content.
	Content string

	// StepType categorizes the step: "claim", "inference", "calculation", "assumption".
	StepType string

	// Confidence is the stated confidence for this step.
	Confidence float64

	// Index is the step number in the trace.
	Index int
}

// StepStatus represents the verification status of a trace step.
type StepStatus string

const (
	// StepEntailed means the step is supported by prior steps/context.
	StepEntailed StepStatus = "ENTAILED"

	// StepContradicted means the step contradicts prior steps/context.
	StepContradicted StepStatus = "CONTRADICTED"

	// StepNotInContext means the step asserts something not in prior context.
	StepNotInContext StepStatus = "NOT_IN_CONTEXT"

	// StepUnverifiable means the step cannot be verified.
	StepUnverifiable StepStatus = "UNVERIFIABLE"
)

// StepVerification contains verification results for a trace step.
type StepVerification struct {
	Step    *TraceStep
	Status  StepStatus
	P0      float64
	P1      float64
	Gap     float64
	Message string
}

// TraceAudit contains the results of auditing a reasoning trace.
// [SPEC-08.23-26]
type TraceAudit struct {
	// Valid indicates whether the trace is valid overall.
	Valid bool

	// FlaggedSteps lists indices of problematic steps.
	FlaggedSteps []int

	// PostHocHallucination indicates the answer isn't derivable from steps.
	PostHocHallucination bool

	// StepResults contains per-step verification results.
	StepResults []StepVerification

	// TotalSteps is the number of steps audited.
	TotalSteps int

	// Duration is how long the audit took.
	Duration time.Duration
}

// AuditTrace audits a reasoning trace for procedural hallucinations.
// [SPEC-08.23]
func (d *Detector) AuditTrace(ctx context.Context, steps []TraceStep, initialContext string) (*TraceAudit, error) {
	start := time.Now()

	audit := &TraceAudit{
		Valid:        true,
		TotalSteps:   len(steps),
		FlaggedSteps: make([]int, 0),
		StepResults:  make([]StepVerification, len(steps)),
	}

	// Build accumulated context as we verify each step
	accumulatedContext := initialContext

	for i, step := range steps {
		// [SPEC-08.24] Verify this step against accumulated context
		stepResult := StepVerification{
			Step: &steps[i],
		}

		// Estimate P1 (with prior context)
		p1, err := d.backend.EstimateProbability(ctx, step.Content, accumulatedContext)
		if err != nil {
			stepResult.Status = StepUnverifiable
			stepResult.Message = fmt.Sprintf("verification error: %v", err)
		} else {
			stepResult.P1 = p1

			// Estimate P0 (without prior context)
			p0, err := d.backend.EstimateProbability(ctx, step.Content, "[No prior context]")
			if err != nil {
				stepResult.P0 = 0.5 // Neutral default
			} else {
				stepResult.P0 = p0
			}

			// Determine step status
			stepResult.Gap = p1 - p0
			stepResult.Status = d.classifyStep(p0, p1, step.Confidence)

			if stepResult.Status != StepEntailed {
				audit.FlaggedSteps = append(audit.FlaggedSteps, i)
				audit.Valid = false
			}

			stepResult.Message = d.generateStepMessage(stepResult)
		}

		audit.StepResults[i] = stepResult

		// Add this step to accumulated context for next iteration
		accumulatedContext += "\n" + step.Content
	}

	audit.Duration = time.Since(start)

	return audit, nil
}

func (d *Detector) classifyStep(p0, p1, confidence float64) StepStatus {
	// Very low P1 means contradiction
	if p1 < 0.2 {
		return StepContradicted
	}

	// If P1 is high and much higher than P0, it's entailed
	if p1 > 0.7 && p1-p0 > 0.2 {
		return StepEntailed
	}

	// If P0 is high (step would be true without context), it might be not from context
	if p0 > 0.7 && p1-p0 < 0.1 {
		return StepNotInContext
	}

	// If confidence is high but P1 is low, it's unsupported
	if confidence > 0.8 && p1 < 0.5 {
		return StepContradicted
	}

	// Default to entailed for moderate cases
	if p1 > 0.5 {
		return StepEntailed
	}

	return StepUnverifiable
}

func (d *Detector) generateExplanation(result *VerificationResult) string {
	switch result.Status {
	case StatusGrounded:
		if result.BudgetGap < -1 {
			return "Strongly supported: evidence provides more information than needed"
		}
		return "Grounded: evidence adequately supports the claim"

	case StatusUnsupported:
		if result.P1 < result.P0 {
			return fmt.Sprintf("Unsupported: evidence reduces confidence (p0=%.2f > p1=%.2f)", result.P0, result.P1)
		}
		return fmt.Sprintf("Unsupported: evidence insufficient (gap=%.2f bits)", result.BudgetGap)

	case StatusContradicted:
		return fmt.Sprintf("Contradicted: evidence opposes claim (p1=%.2f)", result.P1)

	case StatusUnverifiable:
		if result.Error != nil {
			return fmt.Sprintf("Unverifiable: %v", result.Error)
		}
		return "Unverifiable: insufficient information to verify"

	default:
		return "Unknown status"
	}
}

func (d *Detector) generateStepMessage(sv StepVerification) string {
	switch sv.Status {
	case StepEntailed:
		return "Step follows from prior context"
	case StepContradicted:
		return fmt.Sprintf("Step contradicts prior context (p1=%.2f)", sv.P1)
	case StepNotInContext:
		return "Step introduces information not in prior context"
	case StepUnverifiable:
		return "Step cannot be verified"
	default:
		return "Unknown status"
	}
}

// Summary returns a summary of verification results.
func Summary(results []VerificationResult) VerificationSummary {
	summary := VerificationSummary{
		TotalClaims: len(results),
	}

	for _, r := range results {
		switch r.Status {
		case StatusGrounded:
			summary.Grounded++
		case StatusUnsupported:
			summary.Unsupported++
		case StatusContradicted:
			summary.Contradicted++
		case StatusUnverifiable:
			summary.Unverifiable++
		}

		if r.BudgetGap > summary.MaxBudgetGap {
			summary.MaxBudgetGap = r.BudgetGap
		}
		summary.TotalDuration += r.Duration
	}

	if summary.Unsupported+summary.Contradicted > 0 {
		summary.OverallRisk = float64(summary.Unsupported+summary.Contradicted) / float64(summary.TotalClaims)
	}

	return summary
}

// VerificationSummary summarizes multiple verification results.
type VerificationSummary struct {
	TotalClaims   int
	Grounded      int
	Unsupported   int
	Contradicted  int
	Unverifiable  int
	MaxBudgetGap  float64
	OverallRisk   float64
	TotalDuration time.Duration
}

// HasHallucinations returns true if any claims were flagged.
func (s VerificationSummary) HasHallucinations() bool {
	return s.Unsupported > 0 || s.Contradicted > 0
}

// Helper functions

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
