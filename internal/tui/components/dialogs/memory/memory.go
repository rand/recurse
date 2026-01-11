package memory

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/tui/components/core"
	"github.com/rand/recurse/internal/tui/components/dialogs"
	"github.com/rand/recurse/internal/tui/exp/list"
	"github.com/rand/recurse/internal/tui/styles"
	"github.com/rand/recurse/internal/tui/util"
)

const MemoryDialogID dialogs.DialogID = "memory"

// MemoryProvider provides access to memory operations.
type MemoryProvider interface {
	// GetRecent returns recent nodes from memory.
	GetRecent(limit int) ([]*hypergraph.Node, error)
	// Search searches nodes by content.
	Search(query string, limit int) ([]*hypergraph.Node, error)
	// GetStats returns memory statistics.
	GetStats() (MemoryStats, error)
}

// MemoryStats contains memory statistics.
type MemoryStats struct {
	TotalNodes int
	ByType     map[hypergraph.NodeType]int
	ByTier     map[hypergraph.Tier]int
}

// MemoryDialog interface for the memory inspector dialog.
type MemoryDialog interface {
	dialogs.DialogModel
}

// NodeItem wraps a hypergraph node for the list.
type NodeItem struct {
	Node *hypergraph.Node
}

type MemoryList = list.FilterableList[list.CompletionItem[NodeItem]]

type viewMode int

const (
	viewRecent viewMode = iota
	viewSearch
	viewStats
	viewDetails
)

type memoryDialogCmp struct {
	provider   MemoryProvider
	wWidth     int
	wHeight    int
	width      int
	keyMap     KeyMap
	nodesList  MemoryList
	help       help.Model
	mode       viewMode
	stats      MemoryStats
	selected   *hypergraph.Node
	searchMode bool
}

// NewMemoryDialog creates a new memory inspector dialog.
func NewMemoryDialog(provider MemoryProvider) MemoryDialog {
	t := styles.CurrentTheme()
	listKeyMap := list.DefaultKeyMap()
	keyMap := DefaultKeyMap()
	listKeyMap.Down.SetEnabled(false)
	listKeyMap.Up.SetEnabled(false)
	listKeyMap.DownOneItem = keyMap.Next
	listKeyMap.UpOneItem = keyMap.Previous

	inputStyle := t.S().Base.PaddingLeft(1).PaddingBottom(1)
	nodesList := list.NewFilterableList(
		[]list.CompletionItem[NodeItem]{},
		list.WithFilterPlaceholder("Search memory..."),
		list.WithFilterInputStyle(inputStyle),
		list.WithFilterListOptions(
			list.WithKeyMap(listKeyMap),
			list.WithWrapNavigation(),
		),
	)

	help := help.New()
	help.Styles = t.S().Help

	return &memoryDialogCmp{
		provider:  provider,
		keyMap:    keyMap,
		nodesList: nodesList,
		help:      help,
		mode:      viewRecent,
	}
}

func (m *memoryDialogCmp) Init() tea.Cmd {
	return tea.Batch(
		m.nodesList.Init(),
		m.loadRecent(),
	)
}

func (m *memoryDialogCmp) loadRecent() tea.Cmd {
	return func() tea.Msg {
		if m.provider == nil {
			return nodesLoadedMsg{nodes: nil}
		}
		nodes, err := m.provider.GetRecent(50)
		if err != nil {
			return nodesLoadedMsg{err: err}
		}
		return nodesLoadedMsg{nodes: nodes}
	}
}

func (m *memoryDialogCmp) loadStats() tea.Cmd {
	return func() tea.Msg {
		if m.provider == nil {
			return statsLoadedMsg{}
		}
		stats, err := m.provider.GetStats()
		if err != nil {
			return statsLoadedMsg{err: err}
		}
		return statsLoadedMsg{stats: stats}
	}
}

type nodesLoadedMsg struct {
	nodes []*hypergraph.Node
	err   error
}

type statsLoadedMsg struct {
	stats MemoryStats
	err   error
}

func (m *memoryDialogCmp) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		var cmds []tea.Cmd
		m.wWidth = msg.Width
		m.wHeight = msg.Height
		m.width = min(100, m.wWidth-8)
		m.nodesList.SetInputWidth(m.listWidth() - 2)
		cmds = append(cmds, m.nodesList.SetSize(m.listWidth(), m.listHeight()))
		return m, tea.Batch(cmds...)

	case nodesLoadedMsg:
		if msg.err != nil {
			return m, nil
		}
		items := nodesToItems(msg.nodes)
		m.nodesList.SetItems(items)
		return m, nil

	case statsLoadedMsg:
		if msg.err == nil {
			m.stats = msg.stats
		}
		return m, nil

	case tea.KeyPressMsg:
		// Handle search mode - just pass through to filterable list
		if m.searchMode {
			switch {
			case key.Matches(msg, m.keyMap.Close):
				m.searchMode = false
				m.mode = viewRecent
				return m, m.loadRecent()
			default:
				u, cmd := m.nodesList.Update(msg)
				m.nodesList = u.(MemoryList)
				return m, cmd
			}
		}

		// Handle details view
		if m.mode == viewDetails {
			if key.Matches(msg, m.keyMap.Close) {
				m.mode = viewRecent
				m.selected = nil
				return m, nil
			}
			return m, nil
		}

		// Handle stats view
		if m.mode == viewStats {
			if key.Matches(msg, m.keyMap.Close) {
				m.mode = viewRecent
				return m, nil
			}
			return m, nil
		}

		// Normal list navigation
		switch {
		case key.Matches(msg, m.keyMap.Select):
			selectedItem := m.nodesList.SelectedItem()
			if selectedItem != nil {
				m.selected = (*selectedItem).Value().Node
				m.mode = viewDetails
			}
			return m, nil

		case key.Matches(msg, m.keyMap.Search):
			m.searchMode = true
			m.mode = viewSearch
			return m, m.nodesList.Focus()

		case key.Matches(msg, m.keyMap.Recent):
			m.mode = viewRecent
			return m, m.loadRecent()

		case key.Matches(msg, m.keyMap.Stats):
			m.mode = viewStats
			return m, m.loadStats()

		case key.Matches(msg, m.keyMap.Close):
			return m, util.CmdHandler(dialogs.CloseDialogMsg{})

		default:
			u, cmd := m.nodesList.Update(msg)
			m.nodesList = u.(MemoryList)
			return m, cmd
		}
	}
	return m, nil
}

func (m *memoryDialogCmp) View() string {
	t := styles.CurrentTheme()

	var content string
	switch m.mode {
	case viewStats:
		content = m.renderStats()
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

func (m *memoryDialogCmp) getTitle() string {
	switch m.mode {
	case viewStats:
		return "Memory Stats"
	case viewDetails:
		return "Node Details"
	case viewSearch:
		return "Search Memory"
	default:
		return "Memory Inspector"
	}
}

func (m *memoryDialogCmp) renderList() string {
	return m.nodesList.View()
}

func (m *memoryDialogCmp) renderStats() string {
	t := styles.CurrentTheme()
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Total Nodes: %d\n\n", m.stats.TotalNodes))

	sb.WriteString("By Type:\n")
	for nodeType, count := range m.stats.ByType {
		sb.WriteString(fmt.Sprintf("  %s: %d\n", nodeType, count))
	}

	sb.WriteString("\nBy Tier:\n")
	for tier, count := range m.stats.ByTier {
		sb.WriteString(fmt.Sprintf("  %s: %d\n", tier, count))
	}

	return t.S().Base.
		Width(m.listWidth()).
		Height(m.listHeight()).
		Padding(1).
		Render(sb.String())
}

func (m *memoryDialogCmp) renderDetails() string {
	t := styles.CurrentTheme()
	if m.selected == nil {
		return t.S().Base.Render("No node selected")
	}

	node := m.selected
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("ID: %s\n", node.ID[:8]))
	sb.WriteString(fmt.Sprintf("Type: %s\n", node.Type))
	if node.Subtype != "" {
		sb.WriteString(fmt.Sprintf("Subtype: %s\n", node.Subtype))
	}
	sb.WriteString(fmt.Sprintf("Tier: %s\n", node.Tier))
	sb.WriteString(fmt.Sprintf("Confidence: %.2f\n", node.Confidence))
	sb.WriteString(fmt.Sprintf("Access Count: %d\n", node.AccessCount))
	sb.WriteString(fmt.Sprintf("Created: %s\n", node.CreatedAt.Format("2006-01-02 15:04")))
	sb.WriteString(fmt.Sprintf("\nContent:\n%s", truncate(node.Content, 500)))

	return t.S().Base.
		Width(m.listWidth()).
		Height(m.listHeight()).
		Padding(1).
		Render(sb.String())
}

func (m *memoryDialogCmp) Cursor() *tea.Cursor {
	if m.searchMode {
		if cursor, ok := m.nodesList.(util.Cursor); ok {
			cursor := cursor.Cursor()
			if cursor != nil {
				cursor = m.moveCursor(cursor)
			}
			return cursor
		}
	}
	return nil
}

func (m *memoryDialogCmp) style() lipgloss.Style {
	t := styles.CurrentTheme()
	return t.S().Base.
		Width(m.width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocus)
}

func (m *memoryDialogCmp) listHeight() int {
	return m.wHeight/2 - 6
}

func (m *memoryDialogCmp) listWidth() int {
	return m.width - 2
}

func (m *memoryDialogCmp) Position() (int, int) {
	row := m.wHeight/4 - 2
	col := m.wWidth / 2
	col -= m.width / 2
	return row, col
}

func (m *memoryDialogCmp) moveCursor(cursor *tea.Cursor) *tea.Cursor {
	row, col := m.Position()
	offset := row + 3
	cursor.Y += offset
	cursor.X = cursor.X + col + 2
	return cursor
}

func (m *memoryDialogCmp) ID() dialogs.DialogID {
	return MemoryDialogID
}

// Helper functions

func nodesToItems(nodes []*hypergraph.Node) []list.CompletionItem[NodeItem] {
	items := make([]list.CompletionItem[NodeItem], len(nodes))
	for i, node := range nodes {
		label := formatNodeLabel(node)
		items[i] = list.NewCompletionItem(label, NodeItem{Node: node}, list.WithCompletionID(node.ID))
	}
	return items
}

func formatNodeLabel(node *hypergraph.Node) string {
	typeIcon := getTypeIcon(node.Type)
	content := truncate(node.Content, 60)
	return fmt.Sprintf("%s %s", typeIcon, content)
}

func getTypeIcon(nodeType hypergraph.NodeType) string {
	switch nodeType {
	case hypergraph.NodeTypeFact:
		return "[F]"
	case hypergraph.NodeTypeEntity:
		return "[E]"
	case hypergraph.NodeTypeSnippet:
		return "[S]"
	case hypergraph.NodeTypeDecision:
		return "[D]"
	case hypergraph.NodeTypeExperience:
		return "[X]"
	default:
		return "[?]"
	}
}

func truncate(s string, max int) string {
	// Replace newlines with spaces for single-line display
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
