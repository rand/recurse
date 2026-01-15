package repl

import (
	"context"
	"fmt"

	"github.com/rand/recurse/internal/rlm/hallucination"
)

// HallucinationPlugin provides hallucination detection functions for the REPL.
// [SPEC-08.31-33]
type HallucinationPlugin struct {
	detector      *hallucination.Detector
	outputVerify  *hallucination.OutputVerifier
	traceAuditor  *hallucination.TraceAuditor
}

// NewHallucinationPlugin creates a new hallucination detection plugin.
func NewHallucinationPlugin(
	detector *hallucination.Detector,
	outputVerify *hallucination.OutputVerifier,
	traceAuditor *hallucination.TraceAuditor,
) *HallucinationPlugin {
	return &HallucinationPlugin{
		detector:     detector,
		outputVerify: outputVerify,
		traceAuditor: traceAuditor,
	}
}

func (p *HallucinationPlugin) Name() string {
	return "hallucination"
}

func (p *HallucinationPlugin) Description() string {
	return "Provides hallucination detection functions for claim verification and trace auditing"
}

func (p *HallucinationPlugin) OnLoad(ctx context.Context) error {
	return nil
}

func (p *HallucinationPlugin) OnUnload() error {
	return nil
}

// Functions returns the map of function names to their implementations.
// [SPEC-08.31]
func (p *HallucinationPlugin) Functions() map[string]REPLFunction {
	return map[string]REPLFunction{
		"verify_claim": {
			Name:        "verify_claim",
			Description: "Verify a single claim against evidence",
			Parameters: []FunctionParameter{
				{Name: "claim", Type: "string", Description: "The claim to verify", Required: true},
				{Name: "evidence", Type: "string", Description: "Evidence to verify against", Required: true},
				{Name: "confidence", Type: "float", Description: "Stated confidence level (0.0-1.0)", Required: false},
			},
			Handler: p.verifyClaim,
		},
		"verify_claims": {
			Name:        "verify_claims",
			Description: "Extract and verify all claims from text",
			Parameters: []FunctionParameter{
				{Name: "text", Type: "string", Description: "Text containing claims to verify", Required: true},
				{Name: "context", Type: "string", Description: "Context to verify claims against", Required: true},
			},
			Handler: p.verifyClaims,
		},
		"audit_trace": {
			Name:        "audit_trace",
			Description: "Audit a reasoning trace for procedural hallucinations",
			Parameters: []FunctionParameter{
				{Name: "steps", Type: "list[dict]", Description: "List of trace steps with 'content' and optional 'type' fields", Required: true},
				{Name: "context", Type: "string", Description: "Initial context for the trace", Required: false},
				{Name: "final_answer", Type: "string", Description: "Final answer to check for derivability", Required: false},
			},
			Handler: p.auditTrace,
		},
	}
}

// ClaimVerificationResult is the structured result for verify_claim.
// [SPEC-08.32]
type ClaimVerificationResult struct {
	Claim              string  `json:"claim"`
	Status             string  `json:"status"`
	P0                 float64 `json:"p0"`
	P1                 float64 `json:"p1"`
	RequiredBits       float64 `json:"required_bits"`
	ObservedBits       float64 `json:"observed_bits"`
	BudgetGap          float64 `json:"budget_gap"`
	AdjustedConfidence float64 `json:"adjusted_confidence"`
	Explanation        string  `json:"explanation"`
}

// ClaimsVerificationResult is the structured result for verify_claims.
type ClaimsVerificationResult struct {
	TotalClaims    int                       `json:"total_claims"`
	VerifiedClaims int                       `json:"verified_claims"`
	FlaggedClaims  int                       `json:"flagged_claims"`
	OverallRisk    float64                   `json:"overall_risk"`
	Results        []ClaimVerificationResult `json:"results"`
}

// TraceAuditResult is the structured result for audit_trace.
type TraceAuditResult struct {
	Valid                bool                    `json:"valid"`
	TotalSteps           int                     `json:"total_steps"`
	FlaggedSteps         []int                   `json:"flagged_steps"`
	PostHocHallucination bool                    `json:"post_hoc_hallucination"`
	DerivabilityScore    float64                 `json:"derivability_score,omitempty"`
	Overall              string                  `json:"overall"`
	StepResults          []StepVerificationResult `json:"step_results"`
	Recommendations      []string                `json:"recommendations,omitempty"`
}

// StepVerificationResult contains per-step verification results.
type StepVerificationResult struct {
	Index   int     `json:"index"`
	Content string  `json:"content"`
	Status  string  `json:"status"`
	P0      float64 `json:"p0"`
	P1      float64 `json:"p1"`
	Message string  `json:"message"`
}

// verifyClaim implements the verify_claim function.
// [SPEC-08.31]
func (p *HallucinationPlugin) verifyClaim(ctx context.Context, args ...any) (any, error) {
	if p.detector == nil {
		return nil, fmt.Errorf("hallucination detector not initialized")
	}

	if len(args) < 2 {
		return nil, fmt.Errorf("claim and evidence arguments required")
	}

	claim, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("claim must be a string")
	}

	evidence, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("evidence must be a string")
	}

	confidence := 0.9 // Default confidence
	if len(args) >= 3 {
		if c, ok := args[2].(float64); ok {
			confidence = c
		}
	}

	// Create claim object
	claimObj := &hallucination.Claim{
		Content:    claim,
		Confidence: confidence,
		Source:     "repl",
	}

	// Verify the claim
	result, err := p.detector.VerifyClaimWithEvidence(ctx, claimObj, evidence)
	if err != nil {
		return nil, fmt.Errorf("verification failed: %w", err)
	}

	// [SPEC-08.32] Return structured result
	return ClaimVerificationResult{
		Claim:              claim,
		Status:             string(result.Status),
		P0:                 result.P0,
		P1:                 result.P1,
		RequiredBits:       result.RequiredBits,
		ObservedBits:       result.ObservedBits,
		BudgetGap:          result.BudgetGap,
		AdjustedConfidence: result.AdjustedConfidence,
		Explanation:        result.Explanation,
	}, nil
}

// verifyClaims implements the verify_claims function.
// [SPEC-08.31]
func (p *HallucinationPlugin) verifyClaims(ctx context.Context, args ...any) (any, error) {
	if p.outputVerify == nil {
		return nil, fmt.Errorf("output verifier not initialized")
	}

	if len(args) < 2 {
		return nil, fmt.Errorf("text and context arguments required")
	}

	text, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("text must be a string")
	}

	context, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("context must be a string")
	}

	// Verify all claims in the text
	result, err := p.outputVerify.VerifyOutput(ctx, text, context)
	if err != nil {
		return nil, fmt.Errorf("verification failed: %w", err)
	}

	// Convert to structured result
	output := ClaimsVerificationResult{
		TotalClaims:    result.TotalClaims,
		VerifiedClaims: result.VerifiedClaims,
		FlaggedClaims:  result.FlaggedClaims,
		OverallRisk:    result.OverallRisk,
		Results:        make([]ClaimVerificationResult, 0, len(result.ClaimResults)),
	}

	for _, cr := range result.ClaimResults {
		output.Results = append(output.Results, ClaimVerificationResult{
			Claim:       cr.Claim.Content,
			Status:      string(cr.Status),
			P0:          cr.P0,
			P1:          cr.P1,
			BudgetGap:   cr.BudgetGap,
			Explanation: cr.Explanation,
		})
	}

	return output, nil
}

// auditTrace implements the audit_trace function.
// [SPEC-08.31]
func (p *HallucinationPlugin) auditTrace(ctx context.Context, args ...any) (any, error) {
	if p.traceAuditor == nil {
		return nil, fmt.Errorf("trace auditor not initialized")
	}

	if len(args) < 1 {
		return nil, fmt.Errorf("steps argument required")
	}

	// Parse steps from the argument
	stepsArg, ok := args[0].([]any)
	if !ok {
		return nil, fmt.Errorf("steps must be a list")
	}

	steps := make([]hallucination.TraceStep, 0, len(stepsArg))
	for i, stepArg := range stepsArg {
		stepMap, ok := stepArg.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("step %d must be a dict", i)
		}

		content, ok := stepMap["content"].(string)
		if !ok {
			return nil, fmt.Errorf("step %d must have a 'content' string", i)
		}

		stepType := "reasoning"
		if t, ok := stepMap["type"].(string); ok {
			stepType = t
		}

		confidence := 0.9
		if c, ok := stepMap["confidence"].(float64); ok {
			confidence = c
		}

		steps = append(steps, hallucination.TraceStep{
			Content:    content,
			StepType:   stepType,
			Confidence: confidence,
			Index:      i,
		})
	}

	// Get optional context and final answer
	initialContext := ""
	if len(args) >= 2 {
		if c, ok := args[1].(string); ok {
			initialContext = c
		}
	}

	finalAnswer := ""
	if len(args) >= 3 {
		if a, ok := args[2].(string); ok {
			finalAnswer = a
		}
	}

	// Audit the trace
	result, err := p.traceAuditor.AuditTrace(ctx, steps, initialContext, finalAnswer)
	if err != nil {
		return nil, fmt.Errorf("audit failed: %w", err)
	}

	// Convert to structured result
	output := TraceAuditResult{
		Valid:                result.TraceAudit.Valid,
		TotalSteps:           result.TraceAudit.TotalSteps,
		FlaggedSteps:         result.TraceAudit.FlaggedSteps,
		PostHocHallucination: result.TraceAudit.PostHocHallucination,
		Overall:              string(result.Overall),
		StepResults:          make([]StepVerificationResult, 0, len(result.TraceAudit.StepResults)),
		Recommendations:      result.Recommendations,
	}

	if result.PostHocResult != nil {
		output.DerivabilityScore = result.PostHocResult.DerivabilityScore
	}

	for _, sr := range result.TraceAudit.StepResults {
		stepResult := StepVerificationResult{
			Status:  string(sr.Status),
			P0:      sr.P0,
			P1:      sr.P1,
			Message: sr.Message,
		}
		if sr.Step != nil {
			stepResult.Index = sr.Step.Index
			stepResult.Content = sr.Step.Content
		}
		output.StepResults = append(output.StepResults, stepResult)
	}

	return output, nil
}

// Verify interface compliance
var _ REPLPlugin = (*HallucinationPlugin)(nil)
