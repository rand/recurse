package rlmtrace

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/rand/recurse/internal/tui/components/core"
	"github.com/rand/recurse/internal/tui/components/dialogs"
	"github.com/rand/recurse/internal/tui/styles"
	"github.com/rand/recurse/internal/tui/util"
)

const RLMTraceDialogID dialogs.DialogID = "rlmtrace"

// TraceEventType identifies the type of RLM trace event.
type TraceEventType string

const (
	EventDecision    TraceEventType = "decision"
	EventDecompose   TraceEventType = "decompose"
	EventSubcall     TraceEventType = "subcall"
	EventSynthesize  TraceEventType = "synthesize"
	EventMemoryQuery TraceEventType = "memory_query"
	EventExecute     TraceEventType = "execute"
)

// TraceEvent represents a single RLM operation in the trace.
type TraceEvent struct {
	ID        string         `json:"id"`
	Type      TraceEventType `json:"type"`
	Action    string         `json:"action"`
	Details   string         `json:"details"`
	Tokens    int            `json:"tokens"`
	Duration  time.Duration  `json:"duration"`
	Timestamp time.Time      `json:"timestamp"`
	Depth     int            `json:"depth"`
	ParentID  string         `json:"parent_id,omitempty"`
	Children  []string       `json:"children,omitempty"`
	Status    string         `json:"status"` // pending, running, completed, failed
}

// TraceProvider provides access to RLM trace events.
type TraceProvider interface {
	// GetEvents returns recent trace events.
	GetEvents(limit int) ([]TraceEvent, error)

	// GetEvent returns a specific event by ID.
	GetEvent(id string) (*TraceEvent, error)

	// ClearEvents clears the trace history.
	ClearEvents() error

	// Stats returns trace statistics.
	Stats() TraceStats
}

// TraceStats contains trace statistics.
type TraceStats struct {
	TotalEvents    int
	TotalTokens    int
	TotalDuration  time.Duration
	MaxDepth       int
	EventsByType   map[TraceEventType]int
}

// TraceDialog interface for the RLM trace view dialog.
type TraceDialog interface {
	dialogs.DialogModel
}

type viewMode int

const (
	viewList viewMode = iota
	viewDetails
)

type traceDialogCmp struct {
	provider    TraceProvider
	wWidth      int
	wHeight     int
	width       int
	keyMap      KeyMap
	help        help.Model
	mode        viewMode
	events      []TraceEvent
	selected    int
	expanded    map[string]bool
	stats       TraceStats
}

// NewTraceDialog creates a new RLM trace view dialog.
func NewTraceDialog(provider TraceProvider) TraceDialog {
	t := styles.CurrentTheme()
	help := help.New()
	help.Styles = t.S().Help

	return &traceDialogCmp{
		provider: provider,
		keyMap:   DefaultKeyMap(),
		help:     help,
		mode:     viewList,
		expanded: make(map[string]bool),
	}
}

func (m *traceDialogCmp) Init() tea.Cmd {
	return m.loadEvents()
}

func (m *traceDialogCmp) loadEvents() tea.Cmd {
	return func() tea.Msg {
		if m.provider == nil {
			return eventsLoadedMsg{events: nil}
		}
		events, err := m.provider.GetEvents(100)
		if err != nil {
			return eventsLoadedMsg{err: err}
		}
		stats := m.provider.Stats()
		return eventsLoadedMsg{events: events, stats: stats}
	}
}

type eventsLoadedMsg struct {
	events []TraceEvent
	stats  TraceStats
	err    error
}

func (m *traceDialogCmp) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.wWidth = msg.Width
		m.wHeight = msg.Height
		m.width = min(110, m.wWidth-8)
		return m, nil

	case eventsLoadedMsg:
		if msg.err == nil {
			m.events = msg.events
			m.stats = msg.stats
		}
		return m, nil

	case tea.KeyPressMsg:
		if m.mode == viewDetails {
			if key.Matches(msg, m.keyMap.Close) || key.Matches(msg, m.keyMap.Collapse) {
				m.mode = viewList
				return m, nil
			}
			return m, nil
		}

		switch {
		case key.Matches(msg, m.keyMap.Next):
			if m.selected < len(m.events)-1 {
				m.selected++
			}
			return m, nil

		case key.Matches(msg, m.keyMap.Previous):
			if m.selected > 0 {
				m.selected--
			}
			return m, nil

		case key.Matches(msg, m.keyMap.Select), key.Matches(msg, m.keyMap.Expand):
			if len(m.events) > 0 && m.selected < len(m.events) {
				m.mode = viewDetails
			}
			return m, nil

		case key.Matches(msg, m.keyMap.Clear):
			if m.provider != nil {
				m.provider.ClearEvents()
				return m, m.loadEvents()
			}
			return m, nil

		case key.Matches(msg, m.keyMap.Close):
			return m, util.CmdHandler(dialogs.CloseDialogMsg{})
		}
	}
	return m, nil
}

func (m *traceDialogCmp) View() string {
	t := styles.CurrentTheme()

	var content string
	switch m.mode {
	case viewDetails:
		content = m.renderDetails()
	default:
		content = m.renderList()
	}

	title := m.getTitle()
	view := lipgloss.JoinVertical(
		lipgloss.Left,
		t.S().Base.Padding(0, 1, 1, 1).Render(core.Title(title, m.width-4)),
		content,
		"",
		t.S().Base.Width(m.width-2).PaddingLeft(1).AlignHorizontal(lipgloss.Left).Render(m.help.View(m.keyMap)),
	)

	return m.style().Render(view)
}

func (m *traceDialogCmp) getTitle() string {
	if m.mode == viewDetails {
		return "Event Details"
	}
	return fmt.Sprintf("RLM Trace (%d events, %d tokens)", m.stats.TotalEvents, m.stats.TotalTokens)
}

func (m *traceDialogCmp) renderList() string {
	t := styles.CurrentTheme()

	if len(m.events) == 0 {
		return t.S().Base.
			Width(m.listWidth()).
			Height(m.listHeight()).
			Padding(1).
			Render("No trace events yet.\n\nRLM operations will appear here as they execute.")
	}

	var sb strings.Builder
	visibleStart := max(0, m.selected-m.listHeight()+3)
	visibleEnd := min(len(m.events), visibleStart+m.listHeight()-2)

	for i := visibleStart; i < visibleEnd; i++ {
		event := m.events[i]
		line := m.formatEventLine(event)

		if i == m.selected {
			line = t.TextSelection.Render(line)
		}

		sb.WriteString(line)
		sb.WriteString("\n")
	}

	return t.S().Base.
		Width(m.listWidth()).
		Height(m.listHeight()).
		Render(sb.String())
}

func (m *traceDialogCmp) formatEventLine(event TraceEvent) string {
	icon := m.getEventIcon(event.Type)
	status := m.getStatusIcon(event.Status)
	indent := strings.Repeat("  ", event.Depth)

	line := fmt.Sprintf("%s%s %s %s", indent, icon, status, event.Action)
	if event.Tokens > 0 {
		line += fmt.Sprintf(" [%dt]", event.Tokens)
	}

	// Truncate if needed
	maxLen := m.listWidth() - 4
	if len(line) > maxLen {
		line = line[:maxLen-3] + "..."
	}

	return line
}

func (m *traceDialogCmp) getEventIcon(eventType TraceEventType) string {
	switch eventType {
	case EventDecision:
		return "[D]"
	case EventDecompose:
		return "[/]"
	case EventSubcall:
		return "[>]"
	case EventSynthesize:
		return "[+]"
	case EventMemoryQuery:
		return "[?]"
	case EventExecute:
		return "[X]"
	default:
		return "[*]"
	}
}

func (m *traceDialogCmp) getStatusIcon(status string) string {
	switch status {
	case "completed":
		return "v"
	case "running":
		return "~"
	case "failed":
		return "x"
	default:
		return "."
	}
}

func (m *traceDialogCmp) renderDetails() string {
	t := styles.CurrentTheme()

	if m.selected >= len(m.events) {
		return t.S().Base.Render("No event selected")
	}

	event := m.events[m.selected]
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("ID: %s\n", event.ID))
	sb.WriteString(fmt.Sprintf("Type: %s\n", event.Type))
	sb.WriteString(fmt.Sprintf("Action: %s\n", event.Action))
	sb.WriteString(fmt.Sprintf("Status: %s\n", event.Status))
	sb.WriteString(fmt.Sprintf("Depth: %d\n", event.Depth))
	sb.WriteString(fmt.Sprintf("Tokens: %d\n", event.Tokens))
	sb.WriteString(fmt.Sprintf("Duration: %s\n", event.Duration))
	sb.WriteString(fmt.Sprintf("Time: %s\n", event.Timestamp.Format("15:04:05.000")))

	if event.ParentID != "" {
		sb.WriteString(fmt.Sprintf("Parent: %s\n", event.ParentID))
	}

	if len(event.Children) > 0 {
		sb.WriteString(fmt.Sprintf("Children: %d\n", len(event.Children)))
	}

	if event.Details != "" {
		sb.WriteString(fmt.Sprintf("\nDetails:\n%s", truncate(event.Details, 500)))
	}

	return t.S().Base.
		Width(m.listWidth()).
		Height(m.listHeight()).
		Padding(1).
		Render(sb.String())
}

func (m *traceDialogCmp) Cursor() *tea.Cursor {
	return nil
}

func (m *traceDialogCmp) style() lipgloss.Style {
	t := styles.CurrentTheme()
	return t.S().Base.
		Width(m.width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocus)
}

func (m *traceDialogCmp) listHeight() int {
	return m.wHeight/2 - 6
}

func (m *traceDialogCmp) listWidth() int {
	return m.width - 2
}

func (m *traceDialogCmp) Position() (int, int) {
	row := m.wHeight/4 - 2
	col := m.wWidth / 2
	col -= m.width / 2
	return row, col
}

func (m *traceDialogCmp) ID() dialogs.DialogID {
	return RLMTraceDialogID
}

// Helper functions

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
