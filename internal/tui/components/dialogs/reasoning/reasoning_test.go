package reasoning

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/tui/components/dialogs"
)

func TestNewReasoningDialog(t *testing.T) {
	dialog := NewReasoningDialog()

	require.NotNil(t, dialog)
	assert.Equal(t, ReasoningDialogID, dialog.ID())
}

func TestReasoningDialogID(t *testing.T) {
	dialog := NewReasoningDialog()

	assert.Equal(t, dialogs.DialogID("reasoning"), dialog.ID())
	assert.Equal(t, ReasoningDialogID, dialog.ID())
}

func TestReasoningDialog_WindowResize(t *testing.T) {
	dialog := NewReasoningDialog().(*reasoningDialogCmp)

	// Send window size message
	dialog.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	assert.Equal(t, 120, dialog.wWidth)
	assert.Equal(t, 40, dialog.wHeight)
}

func TestReasoningDialog_Position(t *testing.T) {
	dialog := NewReasoningDialog().(*reasoningDialogCmp)

	// Set window dimensions
	dialog.wWidth = 100
	dialog.wHeight = 40

	row, col := dialog.Position()

	// Position should be above center
	expectedRow := 40/4 - 2
	expectedCol := 100/2 - defaultWidth/2

	assert.Equal(t, expectedRow, row)
	assert.Equal(t, expectedCol, col)
}

func TestReasoningDialog_Position_SmallWindow(t *testing.T) {
	dialog := NewReasoningDialog().(*reasoningDialogCmp)

	// Set small window dimensions
	dialog.wWidth = 60
	dialog.wHeight = 20

	row, col := dialog.Position()

	// Should still calculate valid positions
	assert.GreaterOrEqual(t, row, -10) // May be negative with small window
	assert.GreaterOrEqual(t, col, 0)
}

func TestReasoningDialog_ListDimensions(t *testing.T) {
	dialog := NewReasoningDialog().(*reasoningDialogCmp)
	dialog.wWidth = 100
	dialog.wHeight = 40

	// Test listWidth
	expectedListWidth := defaultWidth - 2
	assert.Equal(t, expectedListWidth, dialog.listWidth())

	// Test listHeight (will be based on empty list initially)
	listHeight := dialog.listHeight()
	assert.Greater(t, listHeight, 0)
	assert.LessOrEqual(t, listHeight, dialog.wHeight/2)
}

func TestReasoningDialog_CloseKey(t *testing.T) {
	dialog := NewReasoningDialog().(*reasoningDialogCmp)

	// Test escape key should close
	_, cmd := dialog.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	// Should return a command (close dialog)
	assert.NotNil(t, cmd)
}

func TestReasoningDialog_View(t *testing.T) {
	dialog := NewReasoningDialog().(*reasoningDialogCmp)
	dialog.wWidth = 100
	dialog.wHeight = 40

	// View should not panic and return content
	view := dialog.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Reasoning Effort")
}

func TestReasoningDialog_Cursor(t *testing.T) {
	dialog := NewReasoningDialog().(*reasoningDialogCmp)
	dialog.wWidth = 100
	dialog.wHeight = 40

	// Cursor may be nil if list doesn't have focus
	cursor := dialog.Cursor()
	// Just verify it doesn't panic
	_ = cursor
}

func TestDefaultReasoningDialogKeyMap(t *testing.T) {
	km := DefaultReasoningDialogKeyMap()

	// Verify key bindings exist
	assert.NotEmpty(t, km.Next.Keys())
	assert.NotEmpty(t, km.Previous.Keys())
	assert.NotEmpty(t, km.Select.Keys())
	assert.NotEmpty(t, km.Close.Keys())
}

func TestReasoningDialogKeyMap_ShortHelp(t *testing.T) {
	km := DefaultReasoningDialogKeyMap()
	help := km.ShortHelp()

	assert.Len(t, help, 2) // Select and Close
}

func TestReasoningDialogKeyMap_FullHelp(t *testing.T) {
	km := DefaultReasoningDialogKeyMap()
	help := km.FullHelp()

	assert.Len(t, help, 2) // Two rows
	assert.Len(t, help[0], 2) // Next, Previous
	assert.Len(t, help[1], 2) // Select, Close
}

func TestEffortOption_Struct(t *testing.T) {
	opt := EffortOption{
		Title:  "High",
		Effort: "high",
	}

	assert.Equal(t, "High", opt.Title)
	assert.Equal(t, "high", opt.Effort)
}

func TestReasoningEffortSelectedMsg(t *testing.T) {
	msg := ReasoningEffortSelectedMsg{
		Effort: "medium",
	}

	assert.Equal(t, "medium", msg.Effort)
}

func TestReasoningDialog_Constants(t *testing.T) {
	assert.Equal(t, dialogs.DialogID("reasoning"), ReasoningDialogID)
	assert.Equal(t, 50, defaultWidth)
}

func TestReasoningDialog_MoveCursor(t *testing.T) {
	dialog := NewReasoningDialog().(*reasoningDialogCmp)
	dialog.wWidth = 100
	dialog.wHeight = 40

	// Create a test cursor using NewCursor
	cursor := tea.NewCursor(5, 10)
	moved := dialog.moveCursor(cursor)

	// Verify cursor position was adjusted
	row, col := dialog.Position()
	assert.Equal(t, 10+row+3, moved.Y)
	assert.Equal(t, 5+col+2, moved.X)
}
