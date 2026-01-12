package rlm

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestModeSelectionInfo_Summary(t *testing.T) {
	tests := []struct {
		name     string
		info     *ModeSelectionInfo
		contains string
	}{
		{
			name: "RLM with classification",
			info: &ModeSelectionInfo{
				SelectedMode: ModeRLM,
				Reason:       "computational task",
				Classification: &ClassificationInfo{
					Type:       TaskTypeComputational,
					Confidence: 0.85,
				},
			},
			contains: "RLM",
		},
		{
			name: "Direct with classification",
			info: &ModeSelectionInfo{
				SelectedMode: ModeDirecte,
				Reason:       "retrieval task",
				Classification: &ClassificationInfo{
					Type:       TaskTypeRetrieval,
					Confidence: 0.9,
				},
			},
			contains: "DIRECT",
		},
		{
			name: "Forced mode",
			info: &ModeSelectionInfo{
				SelectedMode:  ModeRLM,
				Reason:        "user override",
				WasOverridden: true,
			},
			contains: "forced",
		},
		{
			name: "Size-based selection",
			info: &ModeSelectionInfo{
				SelectedMode: ModeRLM,
				Reason:       "context size",
				ContextInfo: &ContextSelectionInfo{
					TotalTokens: 8000,
				},
			},
			contains: "8K",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := tt.info.Summary()
			assert.Contains(t, summary, tt.contains)
		})
	}
}

func TestModeSelectionInfo_DetailedExplanation(t *testing.T) {
	info := &ModeSelectionInfo{
		SelectedMode:  ModeRLM,
		Reason:        "computational task (85% confidence)",
		WasOverridden: false,
		Classification: &ClassificationInfo{
			Type:            TaskTypeComputational,
			Confidence:      0.85,
			Signals:         []string{"keyword:count", "pattern:how_many_times"},
			UsedLLMFallback: false,
		},
		ContextInfo: &ContextSelectionInfo{
			TotalTokens:   5000,
			ContextCount:  2,
			ThresholdUsed: 500,
			REPLAvailable: true,
		},
		Timestamp: time.Now(),
	}

	explanation := info.DetailedExplanation()

	assert.Contains(t, explanation, "Mode: RLM")
	assert.Contains(t, explanation, "computational")
	assert.Contains(t, explanation, "85%")
	assert.Contains(t, explanation, "keyword:count")
	assert.Contains(t, explanation, "5000")
}

func TestModeSelectionInfo_DetailedExplanation_LLMFallback(t *testing.T) {
	info := &ModeSelectionInfo{
		SelectedMode: ModeRLM,
		Reason:       "via LLM fallback",
		Classification: &ClassificationInfo{
			Type:                TaskTypeAnalytical,
			Confidence:          0.8,
			UsedLLMFallback:     true,
			RuleBasedConfidence: 0.55,
		},
	}

	explanation := info.DetailedExplanation()

	assert.Contains(t, explanation, "LLM fallback")
	assert.Contains(t, explanation, "55%")
}

func TestModeSelectionInfo_DetailedExplanation_Override(t *testing.T) {
	info := &ModeSelectionInfo{
		SelectedMode:  ModeDirecte,
		Reason:        "user override",
		WasOverridden: true,
	}

	explanation := info.DetailedExplanation()

	assert.Contains(t, explanation, "override")
}

func TestModeSelectionInfo_OverrideHints(t *testing.T) {
	info := &ModeSelectionInfo{}

	hints := info.OverrideHints()

	assert.Contains(t, hints, "Ctrl+Shift+R")
	assert.Contains(t, hints, "Ctrl+Shift+D")
}

func TestBuildModeSelectionInfo(t *testing.T) {
	classification := &Classification{
		Type:       TaskTypeComputational,
		Confidence: 0.9,
		Signals:    []string{"keyword:count"},
	}

	info := buildModeSelectionInfo(
		ModeRLM,
		"computational task",
		ModeOverrideAuto,
		classification,
		5000,
		2,
		500,
		true,
		false,
		0.9,
	)

	assert.Equal(t, ModeRLM, info.SelectedMode)
	assert.Equal(t, "computational task", info.Reason)
	assert.False(t, info.WasOverridden)
	assert.NotNil(t, info.Classification)
	assert.Equal(t, TaskTypeComputational, info.Classification.Type)
	assert.Equal(t, 0.9, info.Classification.Confidence)
	assert.NotNil(t, info.ContextInfo)
	assert.Equal(t, 5000, info.ContextInfo.TotalTokens)
	assert.Equal(t, 2, info.ContextInfo.ContextCount)
	assert.True(t, info.ContextInfo.REPLAvailable)
}

func TestBuildModeSelectionInfo_WithOverride(t *testing.T) {
	info := buildModeSelectionInfo(
		ModeRLM,
		"forced RLM",
		ModeOverrideRLM,
		nil,
		1000,
		1,
		4000,
		true,
		false,
		0,
	)

	assert.True(t, info.WasOverridden)
}

func TestBuildModeSelectionInfo_WithLLMFallback(t *testing.T) {
	classification := &Classification{
		Type:       TaskTypeAnalytical,
		Confidence: 0.8,
	}

	info := buildModeSelectionInfo(
		ModeRLM,
		"via LLM fallback",
		ModeOverrideAuto,
		classification,
		8000,
		1,
		4000,
		true,
		true,
		0.55,
	)

	assert.True(t, info.Classification.UsedLLMFallback)
	assert.Equal(t, 0.55, info.Classification.RuleBasedConfidence)
}

func TestClassificationInfo_Signals_Truncation(t *testing.T) {
	info := &ModeSelectionInfo{
		SelectedMode: ModeRLM,
		Reason:       "test",
		Classification: &ClassificationInfo{
			Type:       TaskTypeComputational,
			Confidence: 0.9,
			Signals: []string{
				"signal1", "signal2", "signal3", "signal4",
				"signal5", "signal6", "signal7", "signal8",
			},
		},
	}

	explanation := info.DetailedExplanation()

	// Should show first 5 signals and indicate more
	assert.Contains(t, explanation, "signal1")
	assert.Contains(t, explanation, "signal5")
	assert.Contains(t, explanation, "and 3 more")
}

func TestContextSelectionInfo_REPLUnavailable(t *testing.T) {
	info := &ModeSelectionInfo{
		SelectedMode: ModeDirecte,
		Reason:       "REPL not available",
		ContextInfo: &ContextSelectionInfo{
			TotalTokens:   10000,
			REPLAvailable: false,
		},
	}

	explanation := info.DetailedExplanation()

	assert.Contains(t, explanation, "REPL not available")
}

func TestModeSelectionInfo_Summary_LowConfidence(t *testing.T) {
	// Classification with low confidence should not be shown
	info := &ModeSelectionInfo{
		SelectedMode: ModeDirecte,
		Reason:       "size-based",
		Classification: &ClassificationInfo{
			Type:       TaskTypeUnknown,
			Confidence: 0.3,
		},
		ContextInfo: &ContextSelectionInfo{
			TotalTokens: 2000,
		},
	}

	summary := info.Summary()

	// Should show tokens, not classification
	assert.Contains(t, summary, "DIRECT")
	assert.Contains(t, summary, "2K")
	assert.NotContains(t, strings.ToLower(summary), "unknown")
}
