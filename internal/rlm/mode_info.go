package rlm

import (
	"fmt"
	"strings"
	"time"
)

// ModeSelectionInfo contains detailed information about why a mode was selected.
// This is used to provide transparency to users about the mode selection process.
type ModeSelectionInfo struct {
	// SelectedMode is the final mode chosen (rlm or direct).
	SelectedMode ExecutionMode `json:"selected_mode"`

	// Reason is a human-readable explanation of why this mode was chosen.
	Reason string `json:"reason"`

	// WasOverridden indicates if the mode was manually overridden by the user.
	WasOverridden bool `json:"was_overridden"`

	// Classification contains the task type classification results.
	Classification *ClassificationInfo `json:"classification,omitempty"`

	// ContextInfo contains information about the context that influenced selection.
	ContextInfo *ContextSelectionInfo `json:"context_info,omitempty"`

	// Timestamp when the decision was made.
	Timestamp time.Time `json:"timestamp"`
}

// ClassificationInfo contains task classification details.
type ClassificationInfo struct {
	// Type is the classified task type.
	Type TaskType `json:"type"`

	// Confidence is the classification confidence (0.0-1.0).
	Confidence float64 `json:"confidence"`

	// Signals are the specific signals that led to this classification.
	Signals []string `json:"signals,omitempty"`

	// UsedLLMFallback indicates if LLM was used for classification.
	UsedLLMFallback bool `json:"used_llm_fallback"`

	// RuleBasedConfidence is the confidence from rule-based classification
	// (before any LLM fallback).
	RuleBasedConfidence float64 `json:"rule_based_confidence,omitempty"`
}

// ContextSelectionInfo contains context-related selection info.
type ContextSelectionInfo struct {
	// TotalTokens is the estimated total context size.
	TotalTokens int `json:"total_tokens"`

	// ContextCount is the number of context sources.
	ContextCount int `json:"context_count"`

	// ThresholdUsed is the token threshold that was applied.
	ThresholdUsed int `json:"threshold_used"`

	// REPLAvailable indicates if REPL was available for RLM mode.
	REPLAvailable bool `json:"repl_available"`
}

// Summary returns a short one-line summary suitable for status bar display.
func (m *ModeSelectionInfo) Summary() string {
	if m.WasOverridden {
		return fmt.Sprintf("%s (forced)", strings.ToUpper(string(m.SelectedMode)))
	}

	if m.Classification != nil && m.Classification.Confidence >= 0.5 {
		return fmt.Sprintf("%s | %s %.0f%%",
			strings.ToUpper(string(m.SelectedMode)),
			m.Classification.Type,
			m.Classification.Confidence*100)
	}

	if m.ContextInfo != nil {
		return fmt.Sprintf("%s | %dK tokens",
			strings.ToUpper(string(m.SelectedMode)),
			m.ContextInfo.TotalTokens/1000)
	}

	return strings.ToUpper(string(m.SelectedMode))
}

// DetailedExplanation returns a multi-line explanation suitable for tooltips.
func (m *ModeSelectionInfo) DetailedExplanation() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Mode: %s\n", strings.ToUpper(string(m.SelectedMode))))
	sb.WriteString(fmt.Sprintf("Reason: %s\n", m.Reason))

	if m.WasOverridden {
		sb.WriteString("(User override applied)\n")
	}

	if m.Classification != nil {
		sb.WriteString(fmt.Sprintf("\nTask Classification:\n"))
		sb.WriteString(fmt.Sprintf("  Type: %s\n", m.Classification.Type))
		sb.WriteString(fmt.Sprintf("  Confidence: %.0f%%\n", m.Classification.Confidence*100))

		if m.Classification.UsedLLMFallback {
			sb.WriteString(fmt.Sprintf("  (LLM fallback used, rule-based was %.0f%%)\n",
				m.Classification.RuleBasedConfidence*100))
		}

		if len(m.Classification.Signals) > 0 {
			sb.WriteString("  Signals:\n")
			for _, sig := range m.Classification.Signals[:min(5, len(m.Classification.Signals))] {
				sb.WriteString(fmt.Sprintf("    - %s\n", sig))
			}
			if len(m.Classification.Signals) > 5 {
				sb.WriteString(fmt.Sprintf("    ... and %d more\n", len(m.Classification.Signals)-5))
			}
		}
	}

	if m.ContextInfo != nil {
		sb.WriteString(fmt.Sprintf("\nContext:\n"))
		sb.WriteString(fmt.Sprintf("  Tokens: %d\n", m.ContextInfo.TotalTokens))
		sb.WriteString(fmt.Sprintf("  Sources: %d\n", m.ContextInfo.ContextCount))
		sb.WriteString(fmt.Sprintf("  Threshold: %d tokens\n", m.ContextInfo.ThresholdUsed))
		if !m.ContextInfo.REPLAvailable {
			sb.WriteString("  (REPL not available)\n")
		}
	}

	return sb.String()
}

// OverrideHints returns keyboard shortcut hints for mode overrides.
func (m *ModeSelectionInfo) OverrideHints() string {
	return "Force RLM: Ctrl+Shift+R | Force Direct: Ctrl+Shift+D"
}

// buildModeSelectionInfo creates a ModeSelectionInfo from selection results.
func buildModeSelectionInfo(
	mode ExecutionMode,
	reason string,
	override ModeOverride,
	classification *Classification,
	totalTokens int,
	contextCount int,
	threshold int,
	replAvailable bool,
	usedLLMFallback bool,
	ruleBasedConfidence float64,
) *ModeSelectionInfo {
	info := &ModeSelectionInfo{
		SelectedMode:  mode,
		Reason:        reason,
		WasOverridden: override != ModeOverrideAuto && override != "",
		Timestamp:     time.Now(),
	}

	if classification != nil {
		info.Classification = &ClassificationInfo{
			Type:                classification.Type,
			Confidence:          classification.Confidence,
			Signals:             classification.Signals,
			UsedLLMFallback:     usedLLMFallback,
			RuleBasedConfidence: ruleBasedConfidence,
		}
	}

	info.ContextInfo = &ContextSelectionInfo{
		TotalTokens:   totalTokens,
		ContextCount:  contextCount,
		ThresholdUsed: threshold,
		REPLAvailable: replAvailable,
	}

	return info
}
