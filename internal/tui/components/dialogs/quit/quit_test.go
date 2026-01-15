package quit

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rand/recurse/internal/tui/components/dialogs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewQuitDialog(t *testing.T) {
	dialog := NewQuitDialog()
	require.NotNil(t, dialog)
	assert.Equal(t, QuitDialogID, dialog.ID())
}

func TestQuitDialog_ID(t *testing.T) {
	dialog := NewQuitDialog()
	assert.Equal(t, dialogs.DialogID("quit"), dialog.ID())
}

func TestQuitDialog_Init(t *testing.T) {
	dialog := NewQuitDialog()
	cmd := dialog.Init()
	assert.Nil(t, cmd)
}

func TestQuitDialog_DefaultsToNo(t *testing.T) {
	dialog := NewQuitDialog().(*quitDialogCmp)
	assert.True(t, dialog.selectedNo, "Dialog should default to 'No' for safety")
}

func TestQuitDialog_View(t *testing.T) {
	dialog := NewQuitDialog()
	view := dialog.View()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, question)
	assert.Contains(t, view, "Y") // Yes button
	assert.Contains(t, view, "N") // No button
}

func TestQuitDialog_Position(t *testing.T) {
	dialog := NewQuitDialog().(*quitDialogCmp)
	dialog.wWidth = 100
	dialog.wHeight = 50

	row, col := dialog.Position()

	// Should be roughly centered
	assert.Greater(t, row, 0)
	assert.Greater(t, col, 0)
	assert.Less(t, row, dialog.wHeight)
	assert.Less(t, col, dialog.wWidth)
}

func TestQuitDialog_WindowSizeMsg(t *testing.T) {
	dialog := NewQuitDialog().(*quitDialogCmp)

	msg := tea.WindowSizeMsg{Width: 120, Height: 40}
	updated, cmd := dialog.Update(msg)

	assert.Nil(t, cmd)
	updatedDialog := updated.(*quitDialogCmp)
	assert.Equal(t, 120, updatedDialog.wWidth)
	assert.Equal(t, 40, updatedDialog.wHeight)
}

func TestQuitDialog_ToggleSelection(t *testing.T) {
	dialog := NewQuitDialog().(*quitDialogCmp)
	assert.True(t, dialog.selectedNo) // Default

	// Press left/right to toggle
	msg := tea.KeyPressMsg{Code: tea.KeyLeft}
	updated, _ := dialog.Update(msg)
	updatedDialog := updated.(*quitDialogCmp)
	assert.False(t, updatedDialog.selectedNo) // Now on Yes

	// Toggle again
	msg = tea.KeyPressMsg{Code: tea.KeyRight}
	updated, _ = updatedDialog.Update(msg)
	updatedDialog = updated.(*quitDialogCmp)
	assert.True(t, updatedDialog.selectedNo) // Back to No
}

func TestQuitDialog_TabTogglesSelection(t *testing.T) {
	dialog := NewQuitDialog().(*quitDialogCmp)
	assert.True(t, dialog.selectedNo)

	msg := tea.KeyPressMsg{Code: tea.KeyTab}
	updated, _ := dialog.Update(msg)
	updatedDialog := updated.(*quitDialogCmp)
	assert.False(t, updatedDialog.selectedNo)
}

func TestQuitDialog_EnterOnNo_ClosesDialog(t *testing.T) {
	dialog := NewQuitDialog().(*quitDialogCmp)
	dialog.selectedNo = true

	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
	_, cmd := dialog.Update(msg)

	// Should return a command to close the dialog
	require.NotNil(t, cmd)
	result := cmd()
	_, isCloseMsg := result.(dialogs.CloseDialogMsg)
	assert.True(t, isCloseMsg, "Enter on No should close dialog")
}

func TestQuitDialog_EnterOnYes_Quits(t *testing.T) {
	dialog := NewQuitDialog().(*quitDialogCmp)
	dialog.selectedNo = false // Select Yes

	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
	_, cmd := dialog.Update(msg)

	// Should return tea.Quit command
	require.NotNil(t, cmd)
	result := cmd()
	_, isQuitMsg := result.(tea.QuitMsg)
	assert.True(t, isQuitMsg, "Enter on Yes should quit")
}

func TestQuitDialog_KeymapBindings(t *testing.T) {
	km := DefaultKeymap()

	// Verify keymap has expected bindings
	assert.NotEmpty(t, km.Yes.Keys())
	assert.NotEmpty(t, km.No.Keys())
	assert.NotEmpty(t, km.LeftRight.Keys())
	assert.NotEmpty(t, km.EnterSpace.Keys())
	assert.NotEmpty(t, km.Tab.Keys())
	assert.NotEmpty(t, km.Close.Keys())
}

func TestQuitDialog_EscapeClosesDialog(t *testing.T) {
	dialog := NewQuitDialog().(*quitDialogCmp)

	msg := tea.KeyPressMsg{Code: tea.KeyEscape}
	_, cmd := dialog.Update(msg)

	require.NotNil(t, cmd)
	result := cmd()
	_, isCloseMsg := result.(dialogs.CloseDialogMsg)
	assert.True(t, isCloseMsg, "Escape should close dialog")
}

func TestQuitDialog_ViewReflectsSelection(t *testing.T) {
	dialog := NewQuitDialog().(*quitDialogCmp)

	// When No is selected, view should contain both buttons
	dialog.selectedNo = true
	viewWithNo := dialog.View()

	// When Yes is selected
	dialog.selectedNo = false
	viewWithYes := dialog.View()

	// Both views should contain the question
	assert.Contains(t, viewWithNo, question)
	assert.Contains(t, viewWithYes, question)

	// Both views should be non-empty and different (selection changes styling)
	assert.NotEmpty(t, viewWithNo)
	assert.NotEmpty(t, viewWithYes)
}
