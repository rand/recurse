package modeind

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rand/recurse/internal/rlm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewModeIndicator(t *testing.T) {
	ind := NewModeIndicator()
	require.NotNil(t, ind)
	assert.Nil(t, ind.GetModeInfo())
}

func TestModeIndicator_Init(t *testing.T) {
	ind := NewModeIndicator()
	cmd := ind.Init()
	assert.Nil(t, cmd)
}

func TestModeIndicator_SetModeInfo(t *testing.T) {
	ind := NewModeIndicator()

	info := &rlm.ModeSelectionInfo{
		SelectedMode: rlm.ModeRLM,
		Reason:       "test reason",
	}

	ind.SetModeInfo(info)
	assert.Equal(t, info, ind.GetModeInfo())
}

func TestModeIndicator_Update_WindowSize(t *testing.T) {
	ind := NewModeIndicator()

	msg := tea.WindowSizeMsg{Width: 100, Height: 50}
	updated, cmd := ind.Update(msg)

	assert.NotNil(t, updated)
	assert.Nil(t, cmd)
}

func TestModeIndicator_Update_ModeInfoMsg(t *testing.T) {
	ind := NewModeIndicator()

	info := &rlm.ModeSelectionInfo{
		SelectedMode: rlm.ModeRLM,
		Reason:       "test",
	}

	msg := ModeInfoMsg{Info: info}
	updated, cmd := ind.Update(msg)

	assert.NotNil(t, updated)
	assert.Nil(t, cmd)

	// Cast back to check info was set
	indicator := updated.(ModeIndicator)
	assert.Equal(t, info, indicator.GetModeInfo())
}

func TestModeIndicator_View_Empty(t *testing.T) {
	ind := NewModeIndicator()

	view := ind.View()

	assert.Contains(t, view, "--")
}

func TestModeIndicator_View_RLM(t *testing.T) {
	ind := NewModeIndicator()
	ind.SetModeInfo(&rlm.ModeSelectionInfo{
		SelectedMode: rlm.ModeRLM,
		Reason:       "computational task",
		Classification: &rlm.ClassificationInfo{
			Type:       rlm.TaskTypeComputational,
			Confidence: 0.85,
		},
	})

	view := ind.View()

	assert.Contains(t, view, "RLM")
}

func TestModeIndicator_View_Direct(t *testing.T) {
	ind := NewModeIndicator()
	ind.SetModeInfo(&rlm.ModeSelectionInfo{
		SelectedMode: rlm.ModeDirecte,
		Reason:       "retrieval task",
	})

	view := ind.View()

	assert.Contains(t, view, "DIRECT")
}

func TestModeIndicator_View_Forced(t *testing.T) {
	ind := NewModeIndicator()
	ind.SetModeInfo(&rlm.ModeSelectionInfo{
		SelectedMode:  rlm.ModeRLM,
		Reason:        "forced",
		WasOverridden: true,
	})

	view := ind.View()

	assert.Contains(t, view, "forced")
}

func TestSetModeInfoCmd(t *testing.T) {
	info := &rlm.ModeSelectionInfo{
		SelectedMode: rlm.ModeRLM,
	}

	cmd := SetModeInfoCmd(info)
	require.NotNil(t, cmd)

	msg := cmd()
	modeMsg, ok := msg.(ModeInfoMsg)
	require.True(t, ok)
	assert.Equal(t, info, modeMsg.Info)
}

func TestRenderCompact_Nil(t *testing.T) {
	result := RenderCompact(nil)
	assert.Equal(t, "--", result)
}

func TestRenderCompact_RLM(t *testing.T) {
	info := &rlm.ModeSelectionInfo{
		SelectedMode: rlm.ModeRLM,
	}

	result := RenderCompact(info)

	assert.Contains(t, result, "RLM")
}

func TestRenderCompact_Direct(t *testing.T) {
	info := &rlm.ModeSelectionInfo{
		SelectedMode: rlm.ModeDirecte,
	}

	result := RenderCompact(info)

	assert.Contains(t, result, "DIRECT")
}

func TestRenderCompact_Overridden(t *testing.T) {
	info := &rlm.ModeSelectionInfo{
		SelectedMode:  rlm.ModeRLM,
		WasOverridden: true,
	}

	result := RenderCompact(info)

	assert.Contains(t, result, "*")
}

func TestRenderWithReason_Nil(t *testing.T) {
	result := RenderWithReason(nil)
	assert.Contains(t, result, "unknown")
}

func TestRenderWithReason_Full(t *testing.T) {
	info := &rlm.ModeSelectionInfo{
		SelectedMode: rlm.ModeRLM,
		Reason:       "computational task",
		Classification: &rlm.ClassificationInfo{
			Type:       rlm.TaskTypeComputational,
			Confidence: 0.9,
		},
	}

	result := RenderWithReason(info)

	assert.Contains(t, result, "RLM")
	assert.Contains(t, result, "computational task")
	assert.Contains(t, result, "90%")
}

func TestRenderWithReason_Forced(t *testing.T) {
	info := &rlm.ModeSelectionInfo{
		SelectedMode:  rlm.ModeDirecte,
		Reason:        "user override",
		WasOverridden: true,
	}

	result := RenderWithReason(info)

	assert.Contains(t, result, "forced")
}

func TestModeIndicator_Tooltip(t *testing.T) {
	ind := NewModeIndicator().(*modeIndicatorCmp)

	// Empty
	tooltip := ind.Tooltip()
	assert.Contains(t, tooltip, "No mode selection")

	// With info
	ind.SetModeInfo(&rlm.ModeSelectionInfo{
		SelectedMode: rlm.ModeRLM,
		Reason:       "test reason",
	})

	tooltip = ind.Tooltip()
	assert.Contains(t, tooltip, "RLM")
	assert.Contains(t, tooltip, "test reason")
}

func TestModeIndicator_ShortSummary(t *testing.T) {
	ind := NewModeIndicator().(*modeIndicatorCmp)

	// Empty
	summary := ind.ShortSummary()
	assert.Contains(t, summary, "--")

	// With info
	ind.SetModeInfo(&rlm.ModeSelectionInfo{
		SelectedMode: rlm.ModeRLM,
		Reason:       "test",
	})

	summary = ind.ShortSummary()
	assert.Contains(t, summary, "RLM")
}

func TestModeIndicator_OverrideHints(t *testing.T) {
	ind := NewModeIndicator().(*modeIndicatorCmp)

	hints := ind.OverrideHints()

	assert.Contains(t, hints, "Ctrl+Shift+R")
	assert.Contains(t, hints, "Ctrl+Shift+D")
}
