package memory

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/rand/recurse/internal/memory/evolution"
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

// ProposalProvider provides access to meta-evolution proposals.
type ProposalProvider interface {
	// GetPendingProposals returns pending schema evolution proposals.
	GetPendingProposals(ctx context.Context) ([]*evolution.Proposal, error)
	// ApproveProposal approves a proposal for application.
	ApproveProposal(ctx context.Context, id string) error
	// RejectProposal rejects a proposal.
	RejectProposal(ctx context.Context, id, reason string) error
	// DeferProposal defers a proposal for later review.
	DeferProposal(ctx context.Context, id string) error
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
	viewProposals
)

type memoryDialogCmp struct {
	provider         MemoryProvider
	proposalProvider ProposalProvider
	wWidth           int
	wHeight          int
	width            int
	keyMap           KeyMap
	nodesList        MemoryList
	help             help.Model
	mode             viewMode
	stats            MemoryStats
	selected         *hypergraph.Node
	searchMode       bool
	proposals        []*evolution.Proposal
	selectedProposal int
}

// NewMemoryDialog creates a new memory inspector dialog.
func NewMemoryDialog(provider MemoryProvider, proposalProvider ...ProposalProvider) MemoryDialog {
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

	var pp ProposalProvider
	if len(proposalProvider) > 0 {
		pp = proposalProvider[0]
	}

	return &memoryDialogCmp{
		provider:         provider,
		proposalProvider: pp,
		keyMap:           keyMap,
		nodesList:        nodesList,
		help:             help,
		mode:             viewRecent,
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

func (m *memoryDialogCmp) loadProposals() tea.Cmd {
	return func() tea.Msg {
		if m.proposalProvider == nil {
			return proposalsLoadedMsg{}
		}
		proposals, err := m.proposalProvider.GetPendingProposals(context.Background())
		if err != nil {
			return proposalsLoadedMsg{err: err}
		}
		return proposalsLoadedMsg{proposals: proposals}
	}
}

func (m *memoryDialogCmp) approveProposal(id string) tea.Cmd {
	return func() tea.Msg {
		if m.proposalProvider == nil {
			return proposalActionMsg{action: "approve", id: id, err: fmt.Errorf("no proposal provider")}
		}
		err := m.proposalProvider.ApproveProposal(context.Background(), id)
		return proposalActionMsg{action: "approve", id: id, err: err}
	}
}

func (m *memoryDialogCmp) rejectProposal(id string) tea.Cmd {
	return func() tea.Msg {
		if m.proposalProvider == nil {
			return proposalActionMsg{action: "reject", id: id, err: fmt.Errorf("no proposal provider")}
		}
		err := m.proposalProvider.RejectProposal(context.Background(), id, "rejected via TUI")
		return proposalActionMsg{action: "reject", id: id, err: err}
	}
}

func (m *memoryDialogCmp) deferProposal(id string) tea.Cmd {
	return func() tea.Msg {
		if m.proposalProvider == nil {
			return proposalActionMsg{action: "defer", id: id, err: fmt.Errorf("no proposal provider")}
		}
		err := m.proposalProvider.DeferProposal(context.Background(), id)
		return proposalActionMsg{action: "defer", id: id, err: err}
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

type proposalsLoadedMsg struct {
	proposals []*evolution.Proposal
	err       error
}

type proposalActionMsg struct {
	action string // "approve", "reject", "defer"
	id     string
	err    error
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

	case proposalsLoadedMsg:
		if msg.err == nil {
			m.proposals = msg.proposals
			m.selectedProposal = 0
		}
		return m, nil

	case proposalActionMsg:
		// After any action, reload proposals
		if msg.err == nil {
			return m, m.loadProposals()
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

		// Handle proposals view
		if m.mode == viewProposals {
			switch {
			case key.Matches(msg, m.keyMap.Close):
				m.mode = viewRecent
				return m, nil
			case key.Matches(msg, m.keyMap.Next):
				if m.selectedProposal < len(m.proposals)-1 {
					m.selectedProposal++
				}
				return m, nil
			case key.Matches(msg, m.keyMap.Previous):
				if m.selectedProposal > 0 {
					m.selectedProposal--
				}
				return m, nil
			case key.Matches(msg, m.keyMap.Approve):
				if len(m.proposals) > 0 && m.selectedProposal < len(m.proposals) {
					return m, m.approveProposal(m.proposals[m.selectedProposal].ID)
				}
				return m, nil
			case key.Matches(msg, m.keyMap.Reject):
				if len(m.proposals) > 0 && m.selectedProposal < len(m.proposals) {
					return m, m.rejectProposal(m.proposals[m.selectedProposal].ID)
				}
				return m, nil
			case key.Matches(msg, m.keyMap.Defer):
				if len(m.proposals) > 0 && m.selectedProposal < len(m.proposals) {
					return m, m.deferProposal(m.proposals[m.selectedProposal].ID)
				}
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

		case key.Matches(msg, m.keyMap.Proposals):
			m.mode = viewProposals
			return m, m.loadProposals()

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
	case viewProposals:
		content = m.renderProposals()
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
	case viewProposals:
		return "Evolution Proposals"
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

func (m *memoryDialogCmp) renderProposals() string {
	t := styles.CurrentTheme()
	var sb strings.Builder

	if len(m.proposals) == 0 {
		sb.WriteString("No pending proposals.\n\n")
		sb.WriteString("Proposals are generated when the meta-evolution\n")
		sb.WriteString("system detects patterns in memory usage that\n")
		sb.WriteString("suggest schema improvements.")
	} else {
		sb.WriteString(fmt.Sprintf("Pending Proposals: %d\n\n", len(m.proposals)))

		for i, p := range m.proposals {
			prefix := "  "
			if i == m.selectedProposal {
				prefix = "> "
			}

			sb.WriteString(fmt.Sprintf("%s[%s] %s\n", prefix, p.Type, p.Title))
			if i == m.selectedProposal {
				sb.WriteString(fmt.Sprintf("    Confidence: %.0f%%  Priority: %d\n", p.Confidence*100, p.Priority))
				if p.Description != "" {
					sb.WriteString(fmt.Sprintf("    %s\n", truncate(p.Description, 60)))
				}
				if p.Rationale != "" {
					sb.WriteString(fmt.Sprintf("    Rationale: %s\n", truncate(p.Rationale, 50)))
				}
			}
		}

		sb.WriteString("\n[a]pprove  [x]reject  [d]efer")
	}

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
