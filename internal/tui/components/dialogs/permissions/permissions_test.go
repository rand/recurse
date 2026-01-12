package permissions

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/agent/tools"
	"github.com/rand/recurse/internal/permission"
	"github.com/rand/recurse/internal/tui/components/dialogs"
)

func createTestPermission(toolName string) permission.PermissionRequest {
	req := permission.PermissionRequest{
		ID:          "test-perm-1",
		ToolName:    toolName,
		Path:        "/test/path",
		Description: "Test permission request",
	}

	// Add tool-specific params
	switch toolName {
	case tools.BashToolName:
		req.Params = tools.BashPermissionsParams{
			Command:     "echo hello",
			Description: "Test command",
		}
	case tools.EditToolName:
		req.Params = tools.EditPermissionsParams{
			FilePath:   "/test/file.go",
			OldContent: "old content",
			NewContent: "new content",
		}
	case tools.WriteToolName:
		req.Params = tools.WritePermissionsParams{
			FilePath:   "/test/file.go",
			OldContent: "",
			NewContent: "new file content",
		}
	case tools.FetchToolName:
		req.Params = tools.FetchPermissionsParams{
			URL: "https://example.com",
		}
	case tools.ViewToolName:
		req.Params = tools.ViewPermissionsParams{
			FilePath: "/test/file.go",
			Offset:   0,
			Limit:    100,
		}
	case tools.LSToolName:
		req.Params = tools.LSPermissionsParams{
			Path:   "/test/dir",
			Ignore: []string{".git"},
		}
	case tools.DownloadToolName:
		req.Params = tools.DownloadPermissionsParams{
			URL:      "https://example.com/file.zip",
			FilePath: "/test/download.zip",
			Timeout:  30,
		}
	}

	return req
}

func TestNewPermissionDialogCmp(t *testing.T) {
	perm := createTestPermission(tools.BashToolName)
	dialog := NewPermissionDialogCmp(perm, nil)

	require.NotNil(t, dialog)
	assert.Equal(t, PermissionsDialogID, dialog.ID())
}

func TestNewPermissionDialogCmp_WithOptions(t *testing.T) {
	perm := createTestPermission(tools.EditToolName)

	tests := []struct {
		name     string
		opts     *Options
		wantNil  bool
	}{
		{"nil options", nil, false},
		{"split mode", &Options{DiffMode: "split"}, false},
		{"unified mode", &Options{DiffMode: "unified"}, false},
		{"empty mode", &Options{DiffMode: ""}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialog := NewPermissionDialogCmp(perm, tt.opts)
			require.NotNil(t, dialog)
		})
	}
}

func TestPermissionDialogCmp_ID(t *testing.T) {
	perm := createTestPermission(tools.BashToolName)
	dialog := NewPermissionDialogCmp(perm, nil)

	assert.Equal(t, dialogs.DialogID("permissions"), dialog.ID())
	assert.Equal(t, PermissionsDialogID, dialog.ID())
}

func TestPermissionDialogCmp_Init(t *testing.T) {
	perm := createTestPermission(tools.BashToolName)
	dialog := NewPermissionDialogCmp(perm, nil)

	// Init should not panic - viewport.Init() may return nil
	_ = dialog.Init()
}

func TestPermissionDialogCmp_OptionNavigation(t *testing.T) {
	perm := createTestPermission(tools.BashToolName)
	dialog := NewPermissionDialogCmp(perm, nil).(*permissionDialogCmp)

	// Initial state: Allow (0) selected
	assert.Equal(t, 0, dialog.selectedOption)

	// Move right to "Allow for Session" (1)
	dialog.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	assert.Equal(t, 1, dialog.selectedOption)

	// Move right to "Deny" (2)
	dialog.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	assert.Equal(t, 2, dialog.selectedOption)

	// Move right wraps to "Allow" (0)
	dialog.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	assert.Equal(t, 0, dialog.selectedOption)

	// Move left wraps to "Deny" (2)
	dialog.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	assert.Equal(t, 2, dialog.selectedOption)

	// Move left to "Allow for Session" (1)
	dialog.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	assert.Equal(t, 1, dialog.selectedOption)
}

func TestPermissionDialogCmp_TabNavigation(t *testing.T) {
	perm := createTestPermission(tools.BashToolName)
	dialog := NewPermissionDialogCmp(perm, nil).(*permissionDialogCmp)

	// Tab moves right
	dialog.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	assert.Equal(t, 1, dialog.selectedOption)

	dialog.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	assert.Equal(t, 2, dialog.selectedOption)

	dialog.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	assert.Equal(t, 0, dialog.selectedOption)
}

func TestPermissionDialogCmp_WindowResize(t *testing.T) {
	perm := createTestPermission(tools.BashToolName)
	dialog := NewPermissionDialogCmp(perm, nil).(*permissionDialogCmp)

	// Send window size message
	dialog.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	assert.Equal(t, 100, dialog.wWidth)
	assert.Equal(t, 50, dialog.wHeight)
	assert.True(t, dialog.contentDirty)
}

func TestPermissionDialogCmp_SupportsDiffView(t *testing.T) {
	tests := []struct {
		toolName    string
		supportsDiff bool
	}{
		{tools.EditToolName, true},
		{tools.WriteToolName, true},
		{tools.MultiEditToolName, true},
		{tools.BashToolName, false},
		{tools.FetchToolName, false},
		{tools.ViewToolName, false},
		{tools.LSToolName, false},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			perm := createTestPermission(tt.toolName)
			dialog := NewPermissionDialogCmp(perm, nil).(*permissionDialogCmp)

			assert.Equal(t, tt.supportsDiff, dialog.supportsDiffView())
		})
	}
}

func TestPermissionDialogCmp_SetSize(t *testing.T) {
	tests := []struct {
		toolName      string
		windowWidth   int
		windowHeight  int
		expectedRatio float64 // Expected width ratio
	}{
		{tools.BashToolName, 100, 50, 0.8},
		{tools.EditToolName, 100, 50, 0.8},
		{tools.WriteToolName, 100, 50, 0.8},
		{tools.FetchToolName, 100, 50, 0.8},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			perm := createTestPermission(tt.toolName)
			dialog := NewPermissionDialogCmp(perm, nil).(*permissionDialogCmp)

			dialog.wWidth = tt.windowWidth
			dialog.wHeight = tt.windowHeight
			dialog.SetSize()

			expectedWidth := int(float64(tt.windowWidth) * tt.expectedRatio)
			// Allow for max width cap of 180
			if expectedWidth > 180 {
				expectedWidth = 180
			}
			assert.Equal(t, expectedWidth, dialog.width)
		})
	}
}

func TestPermissionDialogCmp_ScrollFunctions(t *testing.T) {
	perm := createTestPermission(tools.EditToolName)
	dialog := NewPermissionDialogCmp(perm, nil).(*permissionDialogCmp)

	// Test scroll down
	dialog.scrollDown()
	assert.Equal(t, 1, dialog.diffYOffset)
	assert.True(t, dialog.contentDirty)

	// Reset dirty flag
	dialog.contentDirty = false

	// Test scroll up
	dialog.scrollUp()
	assert.Equal(t, 0, dialog.diffYOffset)
	assert.True(t, dialog.contentDirty)

	// Scroll up at 0 stays at 0
	dialog.contentDirty = false
	dialog.scrollUp()
	assert.Equal(t, 0, dialog.diffYOffset)

	// Test scroll right
	dialog.scrollRight()
	assert.Equal(t, 5, dialog.diffXOffset)
	assert.True(t, dialog.contentDirty)

	// Test scroll left
	dialog.contentDirty = false
	dialog.scrollLeft()
	assert.Equal(t, 0, dialog.diffXOffset)
	assert.True(t, dialog.contentDirty)

	// Scroll left at 0 stays at 0
	dialog.contentDirty = false
	dialog.scrollLeft()
	assert.Equal(t, 0, dialog.diffXOffset)
}

func TestPermissionDialogCmp_IsMouseOverDialog(t *testing.T) {
	perm := createTestPermission(tools.BashToolName)
	dialog := NewPermissionDialogCmp(perm, nil).(*permissionDialogCmp)

	// Set up dialog position
	dialog.positionRow = 10
	dialog.positionCol = 20
	dialog.width = 50
	dialog.finalDialogHeight = 30

	tests := []struct {
		name     string
		x, y     int
		expected bool
	}{
		{"inside", 25, 15, true},
		{"top-left corner", 20, 10, true},
		{"outside left", 19, 15, false},
		{"outside top", 25, 9, false},
		{"outside right", 71, 15, false},
		{"outside bottom", 25, 41, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dialog.isMouseOverDialog(tt.x, tt.y)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPermissionDialogCmp_IsMouseOverDialog_EmptyID(t *testing.T) {
	perm := permission.PermissionRequest{} // Empty ID
	dialog := NewPermissionDialogCmp(perm, nil).(*permissionDialogCmp)

	// Should return false when permission ID is empty
	assert.False(t, dialog.isMouseOverDialog(0, 0))
}

func TestPermissionDialogCmp_UseDiffSplitMode(t *testing.T) {
	perm := createTestPermission(tools.EditToolName)

	// Test with split option
	splitOpts := &Options{DiffMode: "split"}
	dialog := NewPermissionDialogCmp(perm, splitOpts).(*permissionDialogCmp)
	assert.True(t, dialog.useDiffSplitMode())

	// Test with unified option
	unifiedOpts := &Options{DiffMode: "unified"}
	dialog = NewPermissionDialogCmp(perm, unifiedOpts).(*permissionDialogCmp)
	assert.False(t, dialog.useDiffSplitMode())

	// Test with default (uses defaultDiffSplitMode)
	dialog = NewPermissionDialogCmp(perm, nil).(*permissionDialogCmp)
	dialog.defaultDiffSplitMode = true
	assert.True(t, dialog.useDiffSplitMode())

	dialog.defaultDiffSplitMode = false
	assert.False(t, dialog.useDiffSplitMode())
}

func TestPermissionDialogCmp_Position(t *testing.T) {
	perm := createTestPermission(tools.BashToolName)
	dialog := NewPermissionDialogCmp(perm, nil).(*permissionDialogCmp)

	dialog.positionRow = 10
	dialog.positionCol = 20

	row, col := dialog.Position()
	assert.Equal(t, 10, row)
	assert.Equal(t, 20, col)
}

func TestOptions_IsSplitMode(t *testing.T) {
	tests := []struct {
		mode     string
		expected *bool
	}{
		{"split", ptrBool(true)},
		{"unified", ptrBool(false)},
		{"", nil},
		{"invalid", nil},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			opts := Options{DiffMode: tt.mode}
			result := opts.isSplitMode()

			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, *tt.expected, *result)
			}
		})
	}
}

func TestPermissionAction_Constants(t *testing.T) {
	assert.Equal(t, PermissionAction("allow"), PermissionAllow)
	assert.Equal(t, PermissionAction("allow_session"), PermissionAllowForSession)
	assert.Equal(t, PermissionAction("deny"), PermissionDeny)
}

func TestDefaultKeyMap(t *testing.T) {
	km := DefaultKeyMap()

	// Verify key bindings exist
	assert.NotEmpty(t, km.Left.Keys())
	assert.NotEmpty(t, km.Right.Keys())
	assert.NotEmpty(t, km.Tab.Keys())
	assert.NotEmpty(t, km.Allow.Keys())
	assert.NotEmpty(t, km.AllowSession.Keys())
	assert.NotEmpty(t, km.Deny.Keys())
	assert.NotEmpty(t, km.Select.Keys())
	assert.NotEmpty(t, km.ToggleDiffMode.Keys())
	assert.NotEmpty(t, km.ScrollDown.Keys())
	assert.NotEmpty(t, km.ScrollUp.Keys())
	assert.NotEmpty(t, km.ScrollLeft.Keys())
	assert.NotEmpty(t, km.ScrollRight.Keys())
}

func TestKeyMap_KeyBindings(t *testing.T) {
	km := DefaultKeyMap()
	bindings := km.KeyBindings()

	assert.Len(t, bindings, 12) // 12 key bindings total
}

func TestKeyMap_ShortHelp(t *testing.T) {
	km := DefaultKeyMap()
	help := km.ShortHelp()

	assert.NotEmpty(t, help)
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

// Helper function
func ptrBool(b bool) *bool {
	return &b
}
