package filepicker

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/tui/components/dialogs"
)

func TestNewFilePickerCmp(t *testing.T) {
	fp := NewFilePickerCmp("")

	require.NotNil(t, fp)
	assert.Equal(t, dialogs.DialogID(FilePickerID), fp.ID())
}

func TestNewFilePickerCmp_WithWorkingDir(t *testing.T) {
	tempDir := t.TempDir()
	fp := NewFilePickerCmp(tempDir).(*model)

	require.NotNil(t, fp)
	assert.Equal(t, tempDir, fp.filePicker.CurrentDirectory)
}

func TestFilePickerID(t *testing.T) {
	fp := NewFilePickerCmp("")

	assert.Equal(t, dialogs.DialogID("filepicker"), fp.ID())
	assert.Equal(t, dialogs.DialogID(FilePickerID), fp.ID())
}

func TestFilePicker_Init(t *testing.T) {
	fp := NewFilePickerCmp("")

	// Init should return a command (filepicker init)
	cmd := fp.Init()
	// Just verify it doesn't panic - cmd may be nil or not
	_ = cmd
}

func TestFilePicker_WindowResize(t *testing.T) {
	fp := NewFilePickerCmp("").(*model)

	// Send window size message
	fp.Update(tea.WindowSizeMsg{Width: 120, Height: 50})

	assert.Equal(t, 120, fp.wWidth)
	assert.Equal(t, 50, fp.wHeight)
	assert.Equal(t, 70, fp.width) // max(70, wWidth)
}

func TestFilePicker_WindowResize_SmallWindow(t *testing.T) {
	fp := NewFilePickerCmp("").(*model)

	// Send small window size message
	fp.Update(tea.WindowSizeMsg{Width: 50, Height: 30})

	assert.Equal(t, 50, fp.wWidth)
	assert.Equal(t, 30, fp.wHeight)
	assert.Equal(t, 50, fp.width) // min(70, 50) = 50
}

func TestFilePicker_Position(t *testing.T) {
	fp := NewFilePickerCmp("").(*model)

	// Set window dimensions
	fp.wWidth = 100
	fp.wHeight = 50
	fp.width = 70

	row, col := fp.Position()

	// Col should center the dialog
	expectedCol := 100/2 - 70/2
	assert.Equal(t, expectedCol, col)
	// Row should be calculated based on dialog height
	assert.GreaterOrEqual(t, row, 0)
}

func TestFilePicker_CloseKey(t *testing.T) {
	fp := NewFilePickerCmp("").(*model)

	// Test escape key should close
	_, cmd := fp.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	// Should return a command (close dialog)
	assert.NotNil(t, cmd)
}

func TestFilePicker_View(t *testing.T) {
	fp := NewFilePickerCmp("").(*model)
	fp.wWidth = 100
	fp.wHeight = 50
	fp.width = 70

	// View should not panic and return content
	view := fp.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Add Image")
}

func TestFilePicker_ImagePreviewSize(t *testing.T) {
	tests := []struct {
		name     string
		wHeight  int
		expectW  int
		expectH  int
	}{
		{
			name:     "large window shows preview",
			wHeight:  50,
			expectW:  66, // width - 4
			expectH:  20, // previewHeight
		},
		{
			name:     "small window hides preview",
			wHeight:  30, // too small for preview
			expectW:  0,
			expectH:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp := NewFilePickerCmp("").(*model)
			fp.wWidth = 100
			fp.wHeight = tt.wHeight
			fp.width = 70

			w, h := fp.imagePreviewSize()
			assert.Equal(t, tt.expectW, w)
			assert.Equal(t, tt.expectH, h)
		})
	}
}

func TestFilePicker_CurrentImage(t *testing.T) {
	fp := NewFilePickerCmp("").(*model)

	// Initially no image highlighted
	img := fp.currentImage()
	// May or may not be empty depending on directory contents
	_ = img
}

func TestDefaultKeyMap(t *testing.T) {
	km := DefaultKeyMap()

	// Verify key bindings exist
	assert.NotEmpty(t, km.Select.Keys())
	assert.NotEmpty(t, km.Down.Keys())
	assert.NotEmpty(t, km.Up.Keys())
	assert.NotEmpty(t, km.Forward.Keys())
	assert.NotEmpty(t, km.Backward.Keys())
	assert.NotEmpty(t, km.Close.Keys())
}

func TestKeyMap_KeyBindings(t *testing.T) {
	km := DefaultKeyMap()
	bindings := km.KeyBindings()

	assert.Len(t, bindings, 6) // 6 key bindings total
}

func TestKeyMap_ShortHelp(t *testing.T) {
	km := DefaultKeyMap()
	help := km.ShortHelp()

	assert.Len(t, help, 3) // Navigate, Select, Close
}

func TestKeyMap_FullHelp(t *testing.T) {
	km := DefaultKeyMap()
	help := km.FullHelp()

	assert.NotEmpty(t, help)
	// Should be grouped in rows of 4
	for _, row := range help {
		assert.LessOrEqual(t, len(row), 4)
	}
}

func TestFilePickedMsg(t *testing.T) {
	msg := FilePickedMsg{}
	// Just verify struct can be created
	assert.NotNil(t, msg)
}

func TestConstants(t *testing.T) {
	assert.Equal(t, int64(5*1024*1024), MaxAttachmentSize)
	assert.Equal(t, "filepicker", FilePickerID)
	assert.Equal(t, 10, fileSelectionHeight)
	assert.Equal(t, 20, previewHeight)
}

func TestAllowedTypes(t *testing.T) {
	assert.Contains(t, AllowedTypes, ".jpg")
	assert.Contains(t, AllowedTypes, ".jpeg")
	assert.Contains(t, AllowedTypes, ".png")
	assert.Len(t, AllowedTypes, 3)
}

func TestIsFileTooBig(t *testing.T) {
	// Create temp file
	tempDir := t.TempDir()
	smallFile := filepath.Join(tempDir, "small.txt")
	largeContent := make([]byte, 6*1024*1024) // 6MB > 5MB limit
	smallContent := []byte("small content")

	// Test small file
	err := os.WriteFile(smallFile, smallContent, 0644)
	require.NoError(t, err)

	isBig, err := IsFileTooBig(smallFile, MaxAttachmentSize)
	require.NoError(t, err)
	assert.False(t, isBig)

	// Test large file
	largeFile := filepath.Join(tempDir, "large.txt")
	err = os.WriteFile(largeFile, largeContent, 0644)
	require.NoError(t, err)

	isBig, err = IsFileTooBig(largeFile, MaxAttachmentSize)
	require.NoError(t, err)
	assert.True(t, isBig)
}

func TestIsFileTooBig_NonExistentFile(t *testing.T) {
	_, err := IsFileTooBig("/non/existent/file.txt", MaxAttachmentSize)
	assert.Error(t, err)
}

func TestIsFileTooBig_ExactLimit(t *testing.T) {
	tempDir := t.TempDir()
	exactFile := filepath.Join(tempDir, "exact.txt")
	exactContent := make([]byte, MaxAttachmentSize) // Exactly 5MB

	err := os.WriteFile(exactFile, exactContent, 0644)
	require.NoError(t, err)

	isBig, err := IsFileTooBig(exactFile, MaxAttachmentSize)
	require.NoError(t, err)
	assert.False(t, isBig) // Equal to limit is OK
}

func TestIsFileTooBig_OneByteOver(t *testing.T) {
	tempDir := t.TempDir()
	overFile := filepath.Join(tempDir, "over.txt")
	overContent := make([]byte, MaxAttachmentSize+1) // 5MB + 1 byte

	err := os.WriteFile(overFile, overContent, 0644)
	require.NoError(t, err)

	isBig, err := IsFileTooBig(overFile, MaxAttachmentSize)
	require.NoError(t, err)
	assert.True(t, isBig) // Over limit
}
