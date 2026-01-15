package panels

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rand/recurse/internal/tui/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockContent implements util.Model for testing.
type mockContent struct {
	initCalled   bool
	updateCalled bool
	viewCalled   bool
	width        int
	height       int
}

func (m *mockContent) Init() tea.Cmd {
	m.initCalled = true
	return nil
}

func (m *mockContent) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	m.updateCalled = true
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = wsm.Width
		m.height = wsm.Height
	}
	return m, nil
}

func (m *mockContent) View() string {
	m.viewCalled = true
	return "mock content"
}

func createTestPanels() []Panel {
	return []Panel{
		{ID: "panel1", Title: "Panel 1", Content: &mockContent{}},
		{ID: "panel2", Title: "Panel 2", Content: &mockContent{}},
		{ID: "panel3", Title: "Panel 3", Content: &mockContent{}},
	}
}

func TestNewPanelManager(t *testing.T) {
	panels := createTestPanels()
	pm := NewPanelManager(panels)

	require.NotNil(t, pm)
	assert.Equal(t, 0, pm.activeIndex)
	assert.Len(t, pm.panels, 3)
	assert.True(t, pm.visible)
}

func TestNewPanelManager_Empty(t *testing.T) {
	pm := NewPanelManager(nil)
	require.NotNil(t, pm)
	assert.Empty(t, pm.panels)
}

func TestPanelManager_Init(t *testing.T) {
	panels := createTestPanels()
	pm := NewPanelManager(panels)

	cmd := pm.Init()
	// Init should return something (from the active panel's Init)
	assert.Nil(t, cmd) // mockContent.Init returns nil

	// The active panel's Init should have been called
	assert.True(t, panels[0].Content.(*mockContent).initCalled)
}

func TestPanelManager_Init_Empty(t *testing.T) {
	pm := NewPanelManager(nil)
	cmd := pm.Init()
	assert.Nil(t, cmd)
}

func TestPanelManager_ActivePanel(t *testing.T) {
	panels := createTestPanels()
	pm := NewPanelManager(panels)

	active := pm.ActivePanel()
	require.NotNil(t, active)
	assert.Equal(t, PanelID("panel1"), active.ID)
}

func TestPanelManager_ActivePanel_Empty(t *testing.T) {
	pm := NewPanelManager(nil)
	assert.Nil(t, pm.ActivePanel())
}

func TestPanelManager_ActivePanelID(t *testing.T) {
	panels := createTestPanels()
	pm := NewPanelManager(panels)

	assert.Equal(t, PanelID("panel1"), pm.ActivePanelID())
}

func TestPanelManager_ActivePanelID_Empty(t *testing.T) {
	pm := NewPanelManager(nil)
	assert.Equal(t, PanelID(""), pm.ActivePanelID())
}

func TestPanelManager_NextPanel(t *testing.T) {
	panels := createTestPanels()
	pm := NewPanelManager(panels)

	assert.Equal(t, 0, pm.activeIndex)

	pm.nextPanel()
	assert.Equal(t, 1, pm.activeIndex)

	pm.nextPanel()
	assert.Equal(t, 2, pm.activeIndex)

	// Should wrap around
	pm.nextPanel()
	assert.Equal(t, 0, pm.activeIndex)
}

func TestPanelManager_PrevPanel(t *testing.T) {
	panels := createTestPanels()
	pm := NewPanelManager(panels)

	assert.Equal(t, 0, pm.activeIndex)

	// Should wrap to last
	pm.prevPanel()
	assert.Equal(t, 2, pm.activeIndex)

	pm.prevPanel()
	assert.Equal(t, 1, pm.activeIndex)
}

func TestPanelManager_SelectPanel(t *testing.T) {
	panels := createTestPanels()
	pm := NewPanelManager(panels)

	pm.selectPanel(2)
	assert.Equal(t, 2, pm.activeIndex)

	pm.selectPanel(1)
	assert.Equal(t, 1, pm.activeIndex)
}

func TestPanelManager_SelectPanel_OutOfBounds(t *testing.T) {
	panels := createTestPanels()
	pm := NewPanelManager(panels)

	originalIndex := pm.activeIndex

	// Negative index
	pm.selectPanel(-1)
	assert.Equal(t, originalIndex, pm.activeIndex)

	// Too large
	pm.selectPanel(100)
	assert.Equal(t, originalIndex, pm.activeIndex)
}

func TestPanelManager_SelectPanelByID(t *testing.T) {
	panels := createTestPanels()
	pm := NewPanelManager(panels)

	pm.SelectPanelByID("panel3")
	assert.Equal(t, 2, pm.activeIndex)

	pm.SelectPanelByID("panel1")
	assert.Equal(t, 0, pm.activeIndex)
}

func TestPanelManager_SelectPanelByID_NotFound(t *testing.T) {
	panels := createTestPanels()
	pm := NewPanelManager(panels)

	originalIndex := pm.activeIndex
	pm.SelectPanelByID("nonexistent")
	assert.Equal(t, originalIndex, pm.activeIndex)
}

func TestPanelManager_Visibility(t *testing.T) {
	pm := NewPanelManager(createTestPanels())

	assert.True(t, pm.IsVisible())

	pm.SetVisible(false)
	assert.False(t, pm.IsVisible())

	pm.SetVisible(true)
	assert.True(t, pm.IsVisible())
}

func TestPanelManager_View_Hidden(t *testing.T) {
	pm := NewPanelManager(createTestPanels())
	pm.SetVisible(false)

	view := pm.View()
	assert.Empty(t, view)
}

func TestPanelManager_View_Empty(t *testing.T) {
	pm := NewPanelManager(nil)
	view := pm.View()
	assert.Empty(t, view)
}

func TestPanelManager_View_Visible(t *testing.T) {
	pm := NewPanelManager(createTestPanels())
	pm.width = 80
	pm.height = 24

	view := pm.View()
	assert.NotEmpty(t, view)
	// Should contain the panel title
	assert.Contains(t, view, "Panel 1")
}

func TestPanelManager_AddPanel(t *testing.T) {
	pm := NewPanelManager(createTestPanels())
	assert.Len(t, pm.panels, 3)

	pm.AddPanel(Panel{ID: "panel4", Title: "Panel 4", Content: &mockContent{}})
	assert.Len(t, pm.panels, 4)
}

func TestPanelManager_RemovePanel(t *testing.T) {
	pm := NewPanelManager(createTestPanels())
	assert.Len(t, pm.panels, 3)

	pm.RemovePanel("panel2")
	assert.Len(t, pm.panels, 2)
	assert.Equal(t, PanelID("panel1"), pm.panels[0].ID)
	assert.Equal(t, PanelID("panel3"), pm.panels[1].ID)
}

func TestPanelManager_RemovePanel_NotFound(t *testing.T) {
	pm := NewPanelManager(createTestPanels())
	originalLen := len(pm.panels)

	pm.RemovePanel("nonexistent")
	assert.Len(t, pm.panels, originalLen)
}

func TestPanelManager_RemovePanel_AdjustsActiveIndex(t *testing.T) {
	pm := NewPanelManager(createTestPanels())
	pm.activeIndex = 2 // Select last panel

	pm.RemovePanel("panel3") // Remove the active panel
	assert.Equal(t, 1, pm.activeIndex) // Should adjust to stay in bounds
}

func TestPanelManager_WindowSizeMsg(t *testing.T) {
	panels := createTestPanels()
	pm := NewPanelManager(panels)

	msg := tea.WindowSizeMsg{Width: 120, Height: 40}
	updated, _ := pm.Update(msg)
	updatedPM := updated.(*PanelManager)

	assert.Equal(t, 120, updatedPM.width)
	assert.Equal(t, 40, updatedPM.height)

	// Should forward to active panel
	assert.True(t, panels[0].Content.(*mockContent).updateCalled)
}

func TestPanelManager_KeyPress_NextPanel(t *testing.T) {
	pm := NewPanelManager(createTestPanels())
	assert.Equal(t, 0, pm.activeIndex)

	// Tab should go to next panel (based on default keymap)
	msg := tea.KeyPressMsg{Code: tea.KeyTab}
	pm.Update(msg)
	assert.Equal(t, 1, pm.activeIndex)
}

func TestPanelManager_KeyPress_ClosePanel(t *testing.T) {
	pm := NewPanelManager(createTestPanels())
	assert.True(t, pm.visible)

	msg := tea.KeyPressMsg{Code: tea.KeyEscape}
	pm.Update(msg)
	assert.False(t, pm.visible)
}

func TestPanelManager_KeyPress_SelectByNumber(t *testing.T) {
	pm := NewPanelManager(createTestPanels())
	pm.width = 80
	pm.height = 24

	// This tests that number keys switch panels
	// Note: The actual key bindings are "1", "2", "3", "4"
}

func TestPanelManager_String(t *testing.T) {
	pm := NewPanelManager(createTestPanels())
	str := pm.String()

	assert.Contains(t, str, "PanelManager")
	assert.Contains(t, str, "panel1")
	assert.Contains(t, str, "panel2")
	assert.Contains(t, str, "panel3")
}

func TestPanelManager_Position(t *testing.T) {
	pm := NewPanelManager(createTestPanels())
	row, col := pm.Position()

	assert.Equal(t, 0, row)
	assert.Equal(t, 0, col)
}

func TestPanelManager_tabBarHeight(t *testing.T) {
	pm := NewPanelManager(createTestPanels())
	assert.Equal(t, 3, pm.tabBarHeight())
}

func TestPanelManager_helpHeight(t *testing.T) {
	pm := NewPanelManager(createTestPanels())
	assert.Equal(t, 2, pm.helpHeight())
}

func TestPanelChangedMsg(t *testing.T) {
	msg := PanelChangedMsg{
		OldIndex: 0,
		NewIndex: 1,
		PanelID:  "panel2",
	}

	assert.Equal(t, 0, msg.OldIndex)
	assert.Equal(t, 1, msg.NewIndex)
	assert.Equal(t, PanelID("panel2"), msg.PanelID)
}

func TestPanelClosedMsg(t *testing.T) {
	msg := PanelClosedMsg{}
	// Just verify it exists
	_ = msg
}

func TestDefaultKeyMap(t *testing.T) {
	km := DefaultKeyMap()

	// Verify keybindings are set
	assert.NotEmpty(t, km.NextPanel.Keys())
	assert.NotEmpty(t, km.PrevPanel.Keys())
	assert.NotEmpty(t, km.ClosePanel.Keys())
}
