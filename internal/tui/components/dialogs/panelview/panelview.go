package panelview

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/rand/recurse/internal/tui/components/core/panels"
	"github.com/rand/recurse/internal/tui/components/dialogs"
	"github.com/rand/recurse/internal/tui/styles"
	"github.com/rand/recurse/internal/tui/util"
)

const PanelViewDialogID dialogs.DialogID = "panelview"

// PanelViewDialog displays multiple panels in a tabbed view.
type PanelViewDialog interface {
	dialogs.DialogModel
}

type panelViewDialogCmp struct {
	panelManager *panels.PanelManager
	wWidth       int
	wHeight      int
	width        int
	height       int
}

// NewPanelViewDialog creates a new panel view dialog with the given panels.
func NewPanelViewDialog(panelList []panels.Panel) PanelViewDialog {
	pm := panels.NewPanelManager(panelList)
	return &panelViewDialogCmp{
		panelManager: pm,
	}
}

func (d *panelViewDialogCmp) Init() tea.Cmd {
	return d.panelManager.Init()
}

func (d *panelViewDialogCmp) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.wWidth = msg.Width
		d.wHeight = msg.Height
		// Use most of the screen
		d.width = min(d.wWidth-4, 120)
		d.height = min(d.wHeight-4, d.wHeight*3/4)

		// Forward adjusted size to panel manager
		u, cmd := d.panelManager.Update(tea.WindowSizeMsg{
			Width:  d.width - 2,  // Account for border
			Height: d.height - 2, // Account for border
		})
		d.panelManager = u.(*panels.PanelManager)
		return d, cmd

	case panels.PanelClosedMsg:
		return d, util.CmdHandler(dialogs.CloseDialogMsg{})

	default:
		u, cmd := d.panelManager.Update(msg)
		d.panelManager = u.(*panels.PanelManager)
		return d, cmd
	}
}

func (d *panelViewDialogCmp) View() string {
	t := styles.CurrentTheme()

	content := d.panelManager.View()

	return t.S().Base.
		Width(d.width).
		Height(d.height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocus).
		Render(content)
}

func (d *panelViewDialogCmp) Cursor() *tea.Cursor {
	return d.panelManager.Cursor()
}

func (d *panelViewDialogCmp) Position() (int, int) {
	row := (d.wHeight - d.height) / 2
	col := (d.wWidth - d.width) / 2
	return row, col
}

func (d *panelViewDialogCmp) ID() dialogs.DialogID {
	return PanelViewDialogID
}

// ActivePanelID returns the ID of the currently active panel.
func (d *panelViewDialogCmp) ActivePanelID() panels.PanelID {
	return d.panelManager.ActivePanelID()
}

// SelectPanelByID switches to the panel with the given ID.
func (d *panelViewDialogCmp) SelectPanelByID(id panels.PanelID) tea.Cmd {
	return d.panelManager.SelectPanelByID(id)
}
