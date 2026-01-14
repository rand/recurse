package panels

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/rand/recurse/internal/tui/styles"
	"github.com/rand/recurse/internal/tui/util"
)

// PanelID identifies a panel type.
type PanelID string

// Panel represents a single panel in the panel manager.
type Panel struct {
	ID      PanelID
	Title   string
	Content util.Model
	Shortcut string // e.g., "Ctrl+T"
}

// PanelManager manages a set of switchable panels with a tab bar.
type PanelManager struct {
	panels      []Panel
	activeIndex int
	width       int
	height      int
	keyMap      KeyMap
	help        help.Model
	visible     bool // Whether the panel manager is visible
}

// NewPanelManager creates a new panel manager with the given panels.
func NewPanelManager(panels []Panel) *PanelManager {
	t := styles.CurrentTheme()
	h := help.New()
	h.Styles = t.S().Help

	return &PanelManager{
		panels:      panels,
		activeIndex: 0,
		keyMap:      DefaultKeyMap(),
		help:        h,
		visible:     true,
	}
}

// Init initializes the panel manager.
func (pm *PanelManager) Init() tea.Cmd {
	if len(pm.panels) == 0 {
		return nil
	}
	// Initialize the active panel
	return pm.panels[pm.activeIndex].Content.Init()
}

// Update handles messages for the panel manager.
func (pm *PanelManager) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		pm.width = msg.Width
		pm.height = msg.Height
		// Forward to active panel with adjusted height for tab bar
		if len(pm.panels) > 0 && pm.panels[pm.activeIndex].Content != nil {
			contentHeight := pm.height - pm.tabBarHeight() - pm.helpHeight()
			u, cmd := pm.panels[pm.activeIndex].Content.Update(tea.WindowSizeMsg{
				Width:  pm.width,
				Height: contentHeight,
			})
			pm.panels[pm.activeIndex].Content = u
			cmds = append(cmds, cmd)
		}
		return pm, tea.Batch(cmds...)

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, pm.keyMap.NextPanel):
			return pm, pm.nextPanel()
		case key.Matches(msg, pm.keyMap.PrevPanel):
			return pm, pm.prevPanel()
		case key.Matches(msg, pm.keyMap.Panel1):
			return pm, pm.selectPanel(0)
		case key.Matches(msg, pm.keyMap.Panel2):
			return pm, pm.selectPanel(1)
		case key.Matches(msg, pm.keyMap.Panel3):
			return pm, pm.selectPanel(2)
		case key.Matches(msg, pm.keyMap.Panel4):
			return pm, pm.selectPanel(3)
		case key.Matches(msg, pm.keyMap.ClosePanel):
			pm.visible = false
			return pm, util.CmdHandler(PanelClosedMsg{})
		default:
			// Forward to active panel
			if len(pm.panels) > 0 && pm.panels[pm.activeIndex].Content != nil {
				u, cmd := pm.panels[pm.activeIndex].Content.Update(msg)
				pm.panels[pm.activeIndex].Content = u
				cmds = append(cmds, cmd)
			}
		}
	default:
		// Forward other messages to active panel
		if len(pm.panels) > 0 && pm.panels[pm.activeIndex].Content != nil {
			u, cmd := pm.panels[pm.activeIndex].Content.Update(msg)
			pm.panels[pm.activeIndex].Content = u
			cmds = append(cmds, cmd)
		}
	}

	return pm, tea.Batch(cmds...)
}

// View renders the panel manager.
func (pm *PanelManager) View() string {
	if !pm.visible || len(pm.panels) == 0 {
		return ""
	}

	t := styles.CurrentTheme()

	// Render tab bar
	tabBar := pm.renderTabBar()

	// Render active panel content
	var content string
	if pm.panels[pm.activeIndex].Content != nil {
		content = pm.panels[pm.activeIndex].Content.View()
	}

	// Render help
	helpView := t.S().Base.
		Width(pm.width).
		PaddingLeft(1).
		Render(pm.help.View(pm.keyMap))

	return lipgloss.JoinVertical(
		lipgloss.Left,
		tabBar,
		content,
		helpView,
	)
}

// renderTabBar renders the tab bar with panel titles.
func (pm *PanelManager) renderTabBar() string {
	t := styles.CurrentTheme()

	var tabs []string
	for i, panel := range pm.panels {
		tabStyle := t.S().Base.
			Padding(0, 2).
			Border(lipgloss.RoundedBorder()).
			BorderBottom(false)

		if i == pm.activeIndex {
			tabStyle = tabStyle.
				BorderForeground(t.Primary).
				Foreground(t.Primary).
				Bold(true)
		} else {
			tabStyle = tabStyle.
				BorderForeground(t.Border).
				Foreground(t.FgMuted)
		}

		label := fmt.Sprintf("%d %s", i+1, panel.Title)
		if panel.Shortcut != "" {
			label = fmt.Sprintf("%s (%s)", label, panel.Shortcut)
		}
		tabs = append(tabs, tabStyle.Render(label))
	}

	// Join tabs horizontally
	tabBar := lipgloss.JoinHorizontal(lipgloss.Bottom, tabs...)

	// Add bottom border to fill width
	borderStyle := t.S().Base.
		Width(pm.width).
		Border(lipgloss.NormalBorder()).
		BorderTop(false).
		BorderLeft(false).
		BorderRight(false).
		BorderForeground(t.Border)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		tabBar,
		borderStyle.Render(""),
	)
}

func (pm *PanelManager) tabBarHeight() int {
	return 3 // Tab height + border
}

func (pm *PanelManager) helpHeight() int {
	return 2
}

// nextPanel switches to the next panel.
func (pm *PanelManager) nextPanel() tea.Cmd {
	if len(pm.panels) == 0 {
		return nil
	}
	newIndex := (pm.activeIndex + 1) % len(pm.panels)
	return pm.selectPanel(newIndex)
}

// prevPanel switches to the previous panel.
func (pm *PanelManager) prevPanel() tea.Cmd {
	if len(pm.panels) == 0 {
		return nil
	}
	newIndex := pm.activeIndex - 1
	if newIndex < 0 {
		newIndex = len(pm.panels) - 1
	}
	return pm.selectPanel(newIndex)
}

// selectPanel switches to the panel at the given index.
func (pm *PanelManager) selectPanel(index int) tea.Cmd {
	if index < 0 || index >= len(pm.panels) {
		return nil
	}

	oldIndex := pm.activeIndex
	pm.activeIndex = index

	var cmds []tea.Cmd

	// Initialize the new panel if needed
	if pm.panels[pm.activeIndex].Content != nil {
		cmd := pm.panels[pm.activeIndex].Content.Init()
		cmds = append(cmds, cmd)

		// Send window size to new panel
		if pm.width > 0 && pm.height > 0 {
			contentHeight := pm.height - pm.tabBarHeight() - pm.helpHeight()
			u, cmd := pm.panels[pm.activeIndex].Content.Update(tea.WindowSizeMsg{
				Width:  pm.width,
				Height: contentHeight,
			})
			pm.panels[pm.activeIndex].Content = u
			cmds = append(cmds, cmd)
		}
	}

	cmds = append(cmds, util.CmdHandler(PanelChangedMsg{
		OldIndex: oldIndex,
		NewIndex: pm.activeIndex,
		PanelID:  pm.panels[pm.activeIndex].ID,
	}))

	return tea.Batch(cmds...)
}

// ActivePanel returns the currently active panel.
func (pm *PanelManager) ActivePanel() *Panel {
	if len(pm.panels) == 0 || pm.activeIndex >= len(pm.panels) {
		return nil
	}
	return &pm.panels[pm.activeIndex]
}

// ActivePanelID returns the ID of the currently active panel.
func (pm *PanelManager) ActivePanelID() PanelID {
	if panel := pm.ActivePanel(); panel != nil {
		return panel.ID
	}
	return ""
}

// SetVisible sets the visibility of the panel manager.
func (pm *PanelManager) SetVisible(visible bool) {
	pm.visible = visible
}

// IsVisible returns whether the panel manager is visible.
func (pm *PanelManager) IsVisible() bool {
	return pm.visible
}

// AddPanel adds a new panel to the manager.
func (pm *PanelManager) AddPanel(panel Panel) {
	pm.panels = append(pm.panels, panel)
}

// RemovePanel removes a panel by ID.
func (pm *PanelManager) RemovePanel(id PanelID) {
	for i, panel := range pm.panels {
		if panel.ID == id {
			pm.panels = append(pm.panels[:i], pm.panels[i+1:]...)
			if pm.activeIndex >= len(pm.panels) {
				pm.activeIndex = max(0, len(pm.panels)-1)
			}
			return
		}
	}
}

// SelectPanelByID switches to the panel with the given ID.
func (pm *PanelManager) SelectPanelByID(id PanelID) tea.Cmd {
	for i, panel := range pm.panels {
		if panel.ID == id {
			return pm.selectPanel(i)
		}
	}
	return nil
}

// Cursor returns the cursor from the active panel if it implements util.Cursor.
func (pm *PanelManager) Cursor() *tea.Cursor {
	if len(pm.panels) == 0 {
		return nil
	}
	if cursor, ok := pm.panels[pm.activeIndex].Content.(util.Cursor); ok {
		return cursor.Cursor()
	}
	return nil
}

// Position returns the position for overlay rendering.
func (pm *PanelManager) Position() (int, int) {
	return 0, 0
}

// PanelChangedMsg is sent when the active panel changes.
type PanelChangedMsg struct {
	OldIndex int
	NewIndex int
	PanelID  PanelID
}

// PanelClosedMsg is sent when the panel manager is closed.
type PanelClosedMsg struct{}

// String returns a string representation for debugging.
func (pm *PanelManager) String() string {
	var panelNames []string
	for _, p := range pm.panels {
		panelNames = append(panelNames, string(p.ID))
	}
	return fmt.Sprintf("PanelManager{active: %d, panels: [%s]}", pm.activeIndex, strings.Join(panelNames, ", "))
}
