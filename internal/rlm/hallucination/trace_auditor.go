// Package hallucination provides information-theoretic hallucination detection.
package hallucination

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// TraceAuditor audits RLM execution traces for procedural hallucinations.
// [SPEC-08.23-26]
type TraceAuditor struct {
	detector *Detector
	config   TraceAuditorConfig
	logger   *slog.Logger

	// Statistics
	tracesAudited        int64
	tracesFlagged        int64
	stepsAudited         int64
	stepsFlagged         int64
	postHocDetected      int64
}

// TraceAuditorConfig configures the trace auditor.
type TraceAuditorConfig struct {
	// Enabled enables trace auditing.
	Enabled bool

	// CheckPostHoc enables post-hoc hallucination detection.
	// [SPEC-08.25]
	CheckPostHoc bool

	// StopOnContradiction stops processing when contradiction detected.
	StopOnContradiction bool

	// MaxStepsToAudit limits the number of steps to audit per trace.
	MaxStepsToAudit int

	// FlagThresholdP1 is the P1 threshold below which steps are flagged.
	FlagThresholdP1 float64

	// DerivabilityThreshold for post-hoc detection (0.0-1.0).
	DerivabilityThreshold float64
}

// DefaultTraceAuditorConfig returns sensible defaults.
func DefaultTraceAuditorConfig() TraceAuditorConfig {
	return TraceAuditorConfig{
		Enabled:               false,
		CheckPostHoc:          true,
		StopOnContradiction:   false,
		MaxStepsToAudit:       100,
		FlagThresholdP1:       0.3,
		DerivabilityThreshold: 0.5,
	}
}

// TraceAuditResult contains the complete results of auditing a trace.
type TraceAuditResult struct {
	// TraceAudit contains per-step results.
	*TraceAudit

	// PostHocResult contains post-hoc detection results if checked.
	PostHocResult *PostHocResult

	// Overall indicates if the trace passed auditing.
	Overall TraceOverallStatus

	// Summary provides a human-readable summary.
	Summary string

	// Recommendations suggests improvements.
	Recommendations []string
}

// TraceOverallStatus indicates the overall trace status.
type TraceOverallStatus string

const (
	TraceValid       TraceOverallStatus = "VALID"
	TraceWarning     TraceOverallStatus = "WARNING"
	TraceInvalid     TraceOverallStatus = "INVALID"
	TraceUnauditable TraceOverallStatus = "UNAUDITABLE"
)

// PostHocResult contains results of post-hoc hallucination detection.
// [SPEC-08.25-26]
type PostHocResult struct {
	// FinalAnswer is the final answer from the trace.
	FinalAnswer string

	// Derivable indicates if the answer can be derived from trace steps.
	Derivable bool

	// DerivabilityScore is the confidence that the answer is derivable (0.0-1.0).
	DerivabilityScore float64

	// MissingSteps lists reasoning gaps.
	MissingSteps []string

	// Explanation describes the derivability assessment.
	Explanation string

	// Duration is how long post-hoc detection took.
	Duration time.Duration
}

// NewTraceAuditor creates a new trace auditor.
func NewTraceAuditor(detector *Detector, config TraceAuditorConfig) *TraceAuditor {
	if config.MaxStepsToAudit <= 0 {
		config.MaxStepsToAudit = 100
	}
	if config.FlagThresholdP1 <= 0 {
		config.FlagThresholdP1 = 0.3
	}
	if config.DerivabilityThreshold <= 0 {
		config.DerivabilityThreshold = 0.5
	}

	return &TraceAuditor{
		detector: detector,
		config:   config,
		logger:   slog.Default(),
	}
}

// SetLogger sets the logger for the trace auditor.
func (a *TraceAuditor) SetLogger(logger *slog.Logger) {
	a.logger = logger
}

// AuditTrace audits a reasoning trace for procedural hallucinations.
// [SPEC-08.23] RLM execution traces SHALL be audited for procedural hallucinations.
func (a *TraceAuditor) AuditTrace(ctx context.Context, steps []TraceStep, initialContext string, finalAnswer string) (*TraceAuditResult, error) {
	result := &TraceAuditResult{
		Overall: TraceValid,
	}

	// Check if auditing should be skipped
	if !a.config.Enabled || a.detector == nil {
		result.Overall = TraceUnauditable
		result.Summary = "trace auditing disabled"
		return result, nil
	}

	if len(steps) == 0 {
		result.Overall = TraceUnauditable
		result.Summary = "no trace steps to audit"
		return result, nil
	}

	a.tracesAudited++

	// Limit steps to audit
	stepsToAudit := steps
	if len(stepsToAudit) > a.config.MaxStepsToAudit {
		stepsToAudit = stepsToAudit[:a.config.MaxStepsToAudit]
	}

	// [SPEC-08.24] Audit each step
	audit, err := a.auditSteps(ctx, stepsToAudit, initialContext)
	if err != nil {
		result.Overall = TraceUnauditable
		result.Summary = fmt.Sprintf("audit error: %v", err)
		return result, err
	}

	result.TraceAudit = audit
	a.stepsAudited += int64(audit.TotalSteps)
	a.stepsFlagged += int64(len(audit.FlaggedSteps))

	// [SPEC-08.25-26] Post-hoc hallucination detection
	if a.config.CheckPostHoc && finalAnswer != "" {
		postHocResult := a.checkPostHoc(ctx, stepsToAudit, finalAnswer)
		result.PostHocResult = postHocResult

		if postHocResult != nil && !postHocResult.Derivable {
			audit.PostHocHallucination = true
			a.postHocDetected++
		}
	}

	// Determine overall status
	result.Overall = a.determineOverallStatus(audit)

	// Generate summary and recommendations
	result.Summary = a.generateSummary(result)
	result.Recommendations = a.generateRecommendations(result)

	// Log results
	if result.Overall != TraceValid {
		a.tracesFlagged++
		a.logger.Warn("trace flagged",
			"status", result.Overall,
			"flagged_steps", len(audit.FlaggedSteps),
			"post_hoc", audit.PostHocHallucination,
			"duration", audit.Duration,
		)
	} else {
		a.logger.Debug("trace audited",
			"steps", audit.TotalSteps,
			"status", result.Overall,
			"duration", audit.Duration,
		)
	}

	return result, nil
}

// auditSteps audits individual trace steps.
func (a *TraceAuditor) auditSteps(ctx context.Context, steps []TraceStep, initialContext string) (*TraceAudit, error) {
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
		stepResult := StepVerification{
			Step: &steps[i],
		}

		// Estimate P1 (with prior context)
		p1, err := a.detector.backend.EstimateProbability(ctx, step.Content, accumulatedContext)
		if err != nil {
			stepResult.Status = StepUnverifiable
			stepResult.Message = fmt.Sprintf("verification error: %v", err)
			audit.StepResults[i] = stepResult
			accumulatedContext += "\n" + step.Content
			continue
		}

		stepResult.P1 = p1

		// Estimate P0 (without prior context)
		p0, err := a.detector.backend.EstimateProbability(ctx, step.Content, "[No prior context]")
		if err != nil {
			stepResult.P0 = 0.5 // Neutral default
		} else {
			stepResult.P0 = p0
		}

		// Determine step status
		stepResult.Gap = p1 - p0
		stepResult.Status = a.classifyStep(p0, p1, step.Confidence)
		stepResult.Message = a.generateStepMessage(stepResult)

		if stepResult.Status != StepEntailed {
			audit.FlaggedSteps = append(audit.FlaggedSteps, i)
			audit.Valid = false

			// Stop on contradiction if configured
			if a.config.StopOnContradiction && stepResult.Status == StepContradicted {
				audit.StepResults[i] = stepResult
				audit.Duration = time.Since(start)
				return audit, nil
			}
		}

		audit.StepResults[i] = stepResult

		// Add this step to accumulated context for next iteration
		accumulatedContext += "\n" + step.Content
	}

	audit.Duration = time.Since(start)
	return audit, nil
}

// classifyStep determines the status of a trace step.
// [SPEC-08.24]
func (a *TraceAuditor) classifyStep(p0, p1, confidence float64) StepStatus {
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

// checkPostHoc performs post-hoc hallucination detection.
// [SPEC-08.25-26]
func (a *TraceAuditor) checkPostHoc(ctx context.Context, steps []TraceStep, finalAnswer string) *PostHocResult {
	start := time.Now()

	result := &PostHocResult{
		FinalAnswer: finalAnswer,
	}

	if len(steps) == 0 {
		result.Derivable = false
		result.DerivabilityScore = 0
		result.Explanation = "no trace steps to derive answer from"
		result.Duration = time.Since(start)
		return result
	}

	// Build the trace context from all steps
	var traceContext strings.Builder
	traceContext.WriteString("Given the following reasoning steps:\n")
	for i, step := range steps {
		traceContext.WriteString(fmt.Sprintf("%d. %s\n", i+1, step.Content))
	}
	traceContext.WriteString("\nDetermine if the following answer can be logically derived: ")
	traceContext.WriteString(finalAnswer)

	// [SPEC-08.26] Use independent executor (the detector backend) to assess derivability
	derivabilityScore, err := a.detector.backend.EstimateProbability(ctx, finalAnswer, traceContext.String())
	if err != nil {
		result.Derivable = false
		result.DerivabilityScore = 0
		result.Explanation = fmt.Sprintf("derivability check failed: %v", err)
		result.Duration = time.Since(start)
		return result
	}

	result.DerivabilityScore = derivabilityScore
	result.Derivable = derivabilityScore >= a.config.DerivabilityThreshold

	if result.Derivable {
		result.Explanation = fmt.Sprintf("answer appears derivable from trace steps (score=%.2f)", derivabilityScore)
	} else {
		result.Explanation = fmt.Sprintf("answer may not be derivable from trace steps (score=%.2f < threshold=%.2f)",
			derivabilityScore, a.config.DerivabilityThreshold)

		// Try to identify missing steps
		result.MissingSteps = a.identifyMissingSteps(ctx, steps, finalAnswer)
	}

	result.Duration = time.Since(start)
	return result
}

// identifyMissingSteps attempts to identify what reasoning steps are missing.
func (a *TraceAuditor) identifyMissingSteps(ctx context.Context, steps []TraceStep, finalAnswer string) []string {
	// This is a heuristic approach - we check if the answer mentions concepts
	// not present in the trace steps
	var missing []string

	// Simple keyword extraction from the answer
	answerWords := strings.Fields(strings.ToLower(finalAnswer))

	// Build set of words from trace steps
	traceWords := make(map[string]bool)
	for _, step := range steps {
		for _, word := range strings.Fields(strings.ToLower(step.Content)) {
			traceWords[word] = true
		}
	}

	// Find significant words in answer not in trace
	// (filter common words)
	commonWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "shall": true, "can": true,
		"to": true, "of": true, "in": true, "for": true, "on": true, "with": true,
		"at": true, "by": true, "from": true, "or": true, "and": true, "but": true,
		"not": true, "this": true, "that": true, "these": true, "those": true,
		"it": true, "its": true, "as": true, "if": true, "then": true, "so": true,
	}

	seen := make(map[string]bool)
	for _, word := range answerWords {
		if len(word) < 4 || commonWords[word] || seen[word] {
			continue
		}
		if !traceWords[word] {
			missing = append(missing, fmt.Sprintf("concept '%s' not established in trace", word))
			seen[word] = true
		}
		if len(missing) >= 3 {
			break
		}
	}

	return missing
}

// determineOverallStatus determines the overall trace status.
func (a *TraceAuditor) determineOverallStatus(audit *TraceAudit) TraceOverallStatus {
	if audit == nil {
		return TraceUnauditable
	}

	// Post-hoc hallucination is the most severe
	if audit.PostHocHallucination {
		return TraceInvalid
	}

	// Check for contradictions
	hasContradiction := false
	hasNotInContext := false
	for _, result := range audit.StepResults {
		if result.Status == StepContradicted {
			hasContradiction = true
		}
		if result.Status == StepNotInContext {
			hasNotInContext = true
		}
	}

	if hasContradiction {
		return TraceInvalid
	}

	if hasNotInContext || len(audit.FlaggedSteps) > 0 {
		return TraceWarning
	}

	return TraceValid
}

// generateSummary creates a human-readable summary.
func (a *TraceAuditor) generateSummary(result *TraceAuditResult) string {
	if result.TraceAudit == nil {
		return "Trace could not be audited"
	}

	audit := result.TraceAudit

	var parts []string
	parts = append(parts, fmt.Sprintf("Audited %d steps", audit.TotalSteps))

	if len(audit.FlaggedSteps) > 0 {
		parts = append(parts, fmt.Sprintf("%d flagged", len(audit.FlaggedSteps)))
	}

	if audit.PostHocHallucination {
		parts = append(parts, "post-hoc hallucination detected")
	}

	parts = append(parts, fmt.Sprintf("status: %s", result.Overall))

	return strings.Join(parts, ", ")
}

// generateRecommendations suggests improvements.
func (a *TraceAuditor) generateRecommendations(result *TraceAuditResult) []string {
	var recs []string

	if result.TraceAudit == nil {
		return recs
	}

	// Check for specific issues
	hasContradiction := false
	hasNotInContext := false
	for _, sv := range result.TraceAudit.StepResults {
		if sv.Status == StepContradicted {
			hasContradiction = true
		}
		if sv.Status == StepNotInContext {
			hasNotInContext = true
		}
	}

	if hasContradiction {
		recs = append(recs, "Review and correct steps that contradict prior context")
	}

	if hasNotInContext {
		recs = append(recs, "Add explicit grounding for facts not established in context")
	}

	if result.PostHocResult != nil && !result.PostHocResult.Derivable {
		recs = append(recs, "Ensure final answer follows logically from reasoning steps")

		for _, missing := range result.PostHocResult.MissingSteps {
			recs = append(recs, fmt.Sprintf("Consider adding step: %s", missing))
		}
	}

	return recs
}

// generateStepMessage creates a message for a step verification result.
func (a *TraceAuditor) generateStepMessage(sv StepVerification) string {
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

// TraceAuditorStats contains statistics about trace auditing.
type TraceAuditorStats struct {
	TracesAudited   int64   `json:"traces_audited"`
	TracesFlagged   int64   `json:"traces_flagged"`
	StepsAudited    int64   `json:"steps_audited"`
	StepsFlagged    int64   `json:"steps_flagged"`
	PostHocDetected int64   `json:"post_hoc_detected"`
	FlagRate        float64 `json:"flag_rate"`
}

// Stats returns current statistics.
func (a *TraceAuditor) Stats() TraceAuditorStats {
	stats := TraceAuditorStats{
		TracesAudited:   a.tracesAudited,
		TracesFlagged:   a.tracesFlagged,
		StepsAudited:    a.stepsAudited,
		StepsFlagged:    a.stepsFlagged,
		PostHocDetected: a.postHocDetected,
	}

	if a.tracesAudited > 0 {
		stats.FlagRate = float64(a.tracesFlagged) / float64(a.tracesAudited)
	}

	return stats
}

// Config returns the current configuration.
func (a *TraceAuditor) Config() TraceAuditorConfig {
	return a.config
}

// Enabled returns whether trace auditing is enabled.
func (a *TraceAuditor) Enabled() bool {
	return a.config.Enabled && a.detector != nil
}

// ConvertTraceEvents converts RLM TraceEvents to TraceSteps for auditing.
// This bridges the RLM trace system with the auditor.
func ConvertTraceEvents(events []TraceEvent) []TraceStep {
	var steps []TraceStep

	for i, event := range events {
		// Only convert reasoning-type events
		if event.Type != "reasoning" && event.Type != "step" && event.Type != "inference" {
			continue
		}

		step := TraceStep{
			Content:    event.Details,
			StepType:   event.Type,
			Confidence: 0.9, // Default confidence
			Index:      i,
		}

		steps = append(steps, step)
	}

	return steps
}

// TraceEvent represents a trace event from the RLM system.
// This mirrors the orchestrator TraceEvent for decoupling.
type TraceEvent struct {
	ID        string        `json:"id"`
	Type      string        `json:"type"`
	Action    string        `json:"action"`
	Details   string        `json:"details"`
	Tokens    int           `json:"tokens"`
	Duration  time.Duration `json:"duration"`
	Timestamp time.Time     `json:"timestamp"`
	Depth     int           `json:"depth"`
	ParentID  string        `json:"parent_id,omitempty"`
	Status    string        `json:"status"`
}
