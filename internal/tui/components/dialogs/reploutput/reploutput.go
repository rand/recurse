package reploutput

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

const REPLOutputDialogID dialogs.DialogID = "reploutput"

// ExecutionRecord represents a single REPL execution in the history.
type ExecutionRecord struct {
	ID         string        `json:"id"`
	Code       string        `json:"code"`
	Output     string        `json:"output"`
	ReturnVal  string        `json:"return_value"`
	Error      string        `json:"error,omitempty"`
	Duration   time.Duration `json:"duration"`
	Timestamp  time.Time     `json:"timestamp"`
	MemoryUsed int64         `json:"memory_used"` // bytes
}

// REPLHistoryProvider provides access to REPL execution history.
type REPLHistoryProvider interface {
	// GetHistory returns recent execution records.
	GetHistory(limit int) ([]ExecutionRecord, error)

	// GetRecord returns a specific execution record by ID.
	GetRecord(id string) (*ExecutionRecord, error)

	// ClearHistory clears the execution history.
	ClearHistory() error

	// Stats returns execution statistics.
	Stats() REPLStats
}

// REPLStats contains REPL execution statistics.
type REPLStats struct {
	TotalExecutions int
	TotalDuration   time.Duration
	SuccessCount    int
	ErrorCount      int
	TotalMemoryUsed int64 // bytes
}

// REPLOutputDialog interface for the REPL output view dialog.
type REPLOutputDialog interface {
	dialogs.DialogModel
}

type viewMode int

const (
	viewList viewMode = iota
	viewDetails
)

type replOutputDialogCmp struct {
	provider REPLHistoryProvider
	wWidth   int
	wHeight  int
	width    int
	keyMap   KeyMap
	help     help.Model
	mode     viewMode
	records  []ExecutionRecord
	selected int
	stats    REPLStats
}

// NewREPLOutputDialog creates a new REPL output view dialog.
func NewREPLOutputDialog(provider REPLHistoryProvider) REPLOutputDialog {
	t := styles.CurrentTheme()
	help := help.New()
	help.Styles = t.S().Help

	return &replOutputDialogCmp{
		provider: provider,
		keyMap:   DefaultKeyMap(),
		help:     help,
		mode:     viewList,
	}
}

func (m *replOutputDialogCmp) Init() tea.Cmd {
	return m.loadHistory()
}

func (m *replOutputDialogCmp) loadHistory() tea.Cmd {
	return func() tea.Msg {
		if m.provider == nil {
			return historyLoadedMsg{records: nil}
		}
		records, err := m.provider.GetHistory(100)
		if err != nil {
			return historyLoadedMsg{err: err}
		}
		stats := m.provider.Stats()
		return historyLoadedMsg{records: records, stats: stats}
	}
}

type historyLoadedMsg struct {
	records []ExecutionRecord
	stats   REPLStats
	err     error
}

func (m *replOutputDialogCmp) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.wWidth = msg.Width
		m.wHeight = msg.Height
		m.width = min(110, m.wWidth-8)
		return m, nil

	case historyLoadedMsg:
		if msg.err == nil {
			m.records = msg.records
			m.stats = msg.stats
		}
		return m, nil

	case tea.KeyPressMsg:
		if m.mode == viewDetails {
			if key.Matches(msg, m.keyMap.Close) || key.Matches(msg, m.keyMap.Details) {
				m.mode = viewList
				return m, nil
			}
			return m, nil
		}

		switch {
		case key.Matches(msg, m.keyMap.Next):
			if m.selected < len(m.records)-1 {
				m.selected++
			}
			return m, nil

		case key.Matches(msg, m.keyMap.Previous):
			if m.selected > 0 {
				m.selected--
			}
			return m, nil

		case key.Matches(msg, m.keyMap.Select), key.Matches(msg, m.keyMap.Details):
			if len(m.records) > 0 && m.selected < len(m.records) {
				m.mode = viewDetails
			}
			return m, nil

		case key.Matches(msg, m.keyMap.Clear):
			if m.provider != nil {
				m.provider.ClearHistory()
				return m, m.loadHistory()
			}
			return m, nil

		case key.Matches(msg, m.keyMap.Close):
			return m, util.CmdHandler(dialogs.CloseDialogMsg{})
		}
	}
	return m, nil
}

func (m *replOutputDialogCmp) View() string {
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

func (m *replOutputDialogCmp) getTitle() string {
	if m.mode == viewDetails {
		return "Execution Details"
	}
	successRate := 0
	if m.stats.TotalExecutions > 0 {
		successRate = m.stats.SuccessCount * 100 / m.stats.TotalExecutions
	}
	return fmt.Sprintf("REPL Output (%d executions, %d%% success)", m.stats.TotalExecutions, successRate)
}

func (m *replOutputDialogCmp) renderList() string {
	t := styles.CurrentTheme()

	if len(m.records) == 0 {
		return t.S().Base.
			Width(m.listWidth()).
			Height(m.listHeight()).
			Padding(1).
			Render("No REPL executions yet.\n\nPython code executions will appear here.")
	}

	var sb strings.Builder
	visibleStart := max(0, m.selected-m.listHeight()+3)
	visibleEnd := min(len(m.records), visibleStart+m.listHeight()-2)

	for i := visibleStart; i < visibleEnd; i++ {
		record := m.records[i]
		line := m.formatRecordLine(record)

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

func (m *replOutputDialogCmp) formatRecordLine(record ExecutionRecord) string {
	status := "v" // success
	if record.Error != "" {
		status = "x" // error
	}

	// Format the code preview (first line, truncated)
	codePreview := strings.Split(record.Code, "\n")[0]
	codePreview = strings.TrimSpace(codePreview)

	line := fmt.Sprintf("[%s] %s  %s", status, record.Timestamp.Format("15:04:05"), codePreview)

	// Add duration
	if record.Duration > 0 {
		line += fmt.Sprintf(" (%s)", formatDuration(record.Duration))
	}

	// Truncate if needed
	maxLen := m.listWidth() - 4
	if len(line) > maxLen {
		line = line[:maxLen-3] + "..."
	}

	return line
}

func (m *replOutputDialogCmp) renderDetails() string {
	t := styles.CurrentTheme()

	if m.selected >= len(m.records) {
		return t.S().Base.Render("No execution selected")
	}

	record := m.records[m.selected]
	var sb strings.Builder

	// Time and duration
	sb.WriteString(fmt.Sprintf("Time: %s\n", record.Timestamp.Format("2006-01-02 15:04:05.000")))
	sb.WriteString(fmt.Sprintf("Duration: %s\n", formatDuration(record.Duration)))
	if record.MemoryUsed > 0 {
		sb.WriteString(fmt.Sprintf("Memory: %s\n", formatBytes(record.MemoryUsed)))
	}
	sb.WriteString("\n")

	// Code
	sb.WriteString("Code:\n")
	sb.WriteString(m.formatCode(record.Code))
	sb.WriteString("\n\n")

	// Output or Error
	if record.Error != "" {
		sb.WriteString("Error:\n")
		sb.WriteString(truncate(record.Error, 400))
	} else {
		if record.Output != "" {
			sb.WriteString("Output:\n")
			sb.WriteString(truncate(record.Output, 300))
			sb.WriteString("\n")
		}
		if record.ReturnVal != "" {
			sb.WriteString("\nReturn Value:\n")
			sb.WriteString(truncate(record.ReturnVal, 200))
		}
	}

	return t.S().Base.
		Width(m.listWidth()).
		Height(m.listHeight()).
		Padding(1).
		Render(sb.String())
}

func (m *replOutputDialogCmp) formatCode(code string) string {
	lines := strings.Split(code, "\n")
	var sb strings.Builder
	for i, line := range lines {
		if i >= 10 {
			sb.WriteString(fmt.Sprintf("  ... (%d more lines)", len(lines)-10))
			break
		}
		sb.WriteString(fmt.Sprintf("  %s\n", line))
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

func (m *replOutputDialogCmp) Cursor() *tea.Cursor {
	return nil
}

func (m *replOutputDialogCmp) style() lipgloss.Style {
	t := styles.CurrentTheme()
	return t.S().Base.
		Width(m.width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocus)
}

func (m *replOutputDialogCmp) listHeight() int {
	return m.wHeight/2 - 6
}

func (m *replOutputDialogCmp) listWidth() int {
	return m.width - 2
}

func (m *replOutputDialogCmp) Position() (int, int) {
	row := m.wHeight/4 - 2
	col := m.wWidth / 2
	col -= m.width / 2
	return row, col
}

func (m *replOutputDialogCmp) ID() dialogs.DialogID {
	return REPLOutputDialogID
}

// Helper functions

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dμs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
