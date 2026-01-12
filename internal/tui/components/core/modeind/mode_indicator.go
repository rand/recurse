// Package modeind provides a TUI component for displaying RLM/Direct mode selection info.
package modeind

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/rand/recurse/internal/rlm"
	"github.com/rand/recurse/internal/tui/styles"
	"github.com/rand/recurse/internal/tui/util"
)

// ModeIndicator displays the current execution mode and classification info.
type ModeIndicator interface {
	util.Model
	SetModeInfo(info *rlm.ModeSelectionInfo)
	GetModeInfo() *rlm.ModeSelectionInfo
}

type modeIndicatorCmp struct {
	info      *rlm.ModeSelectionInfo
	width     int
	showHints bool
}

// NewModeIndicator creates a new mode indicator component.
func NewModeIndicator() ModeIndicator {
	return &modeIndicatorCmp{
		showHints: true,
	}
}

func (m *modeIndicatorCmp) Init() tea.Cmd {
	return nil
}

func (m *modeIndicatorCmp) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case ModeInfoMsg:
		m.info = msg.Info
	}
	return m, nil
}

func (m *modeIndicatorCmp) View() string {
	if m.info == nil {
		return m.renderEmpty()
	}
	return m.renderIndicator()
}

func (m *modeIndicatorCmp) renderEmpty() string {
	t := styles.CurrentTheme()
	return t.S().Base.
		Foreground(t.FgMuted).
		Render("Mode: --")
}

func (m *modeIndicatorCmp) renderIndicator() string {
	t := styles.CurrentTheme()

	// Mode badge
	modeStyle := t.S().Base.Bold(true).Padding(0, 1)
	var modeBadge string

	if m.info.SelectedMode == rlm.ModeRLM {
		modeBadge = modeStyle.
			Background(t.Blue).
			Foreground(t.White).
			Render("RLM")
	} else {
		modeBadge = modeStyle.
			Background(t.Green).
			Foreground(t.BgSubtle).
			Render("DIRECT")
	}

	// Classification info (if available)
	var classInfo string
	if m.info.Classification != nil && m.info.Classification.Confidence >= 0.5 {
		taskType := string(m.info.Classification.Type)
		confidence := m.info.Classification.Confidence * 100

		classStyle := t.S().Base.Foreground(t.FgMuted)
		classInfo = classStyle.Render(fmt.Sprintf(" %s %.0f%%", taskType, confidence))
	}

	// Override indicator
	var overrideInfo string
	if m.info.WasOverridden {
		overrideInfo = t.S().Base.
			Foreground(t.Yellow).
			Render(" (forced)")
	}

	// Combine elements
	indicator := modeBadge + classInfo + overrideInfo

	return indicator
}

// Tooltip returns a detailed tooltip for the mode indicator.
func (m *modeIndicatorCmp) Tooltip() string {
	if m.info == nil {
		return "No mode selection info available"
	}
	return m.info.DetailedExplanation()
}

// ShortSummary returns a short one-line summary.
func (m *modeIndicatorCmp) ShortSummary() string {
	if m.info == nil {
		return "Mode: --"
	}
	return m.info.Summary()
}

// OverrideHints returns keyboard shortcut hints for mode overrides.
func (m *modeIndicatorCmp) OverrideHints() string {
	if m.info != nil {
		return m.info.OverrideHints()
	}
	return "Force RLM: Ctrl+Shift+R | Force Direct: Ctrl+Shift+D"
}

func (m *modeIndicatorCmp) SetModeInfo(info *rlm.ModeSelectionInfo) {
	m.info = info
}

func (m *modeIndicatorCmp) GetModeInfo() *rlm.ModeSelectionInfo {
	return m.info
}

// ModeInfoMsg is sent when mode selection info is updated.
type ModeInfoMsg struct {
	Info *rlm.ModeSelectionInfo
}

// SetModeInfoCmd creates a command to update the mode indicator.
func SetModeInfoCmd(info *rlm.ModeSelectionInfo) tea.Cmd {
	return func() tea.Msg {
		return ModeInfoMsg{Info: info}
	}
}

// RenderCompact renders a compact version for tight spaces.
func RenderCompact(info *rlm.ModeSelectionInfo) string {
	if info == nil {
		return "--"
	}

	t := styles.CurrentTheme()
	mode := strings.ToUpper(string(info.SelectedMode))

	var style lipgloss.Style
	if info.SelectedMode == rlm.ModeRLM {
		style = t.S().Base.Foreground(t.Blue).Bold(true)
	} else {
		style = t.S().Base.Foreground(t.Green).Bold(true)
	}

	result := style.Render(mode)

	if info.WasOverridden {
		result += t.S().Base.Foreground(t.Yellow).Render("*")
	}

	return result
}

// RenderWithReason renders mode with reason for logging/debug.
func RenderWithReason(info *rlm.ModeSelectionInfo) string {
	if info == nil {
		return "Mode: unknown"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Mode: %s", strings.ToUpper(string(info.SelectedMode))))

	if info.WasOverridden {
		sb.WriteString(" (forced)")
	}

	sb.WriteString(fmt.Sprintf(" - %s", info.Reason))

	if info.Classification != nil && info.Classification.Confidence >= 0.5 {
		sb.WriteString(fmt.Sprintf(" [%s %.0f%%]",
			info.Classification.Type,
			info.Classification.Confidence*100))
	}

	return sb.String()
}
