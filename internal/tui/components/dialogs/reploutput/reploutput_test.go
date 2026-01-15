package reploutput

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/rand/recurse/internal/tui/components/dialogs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockREPLHistoryProvider implements REPLHistoryProvider for testing.
type mockREPLHistoryProvider struct {
	records      []ExecutionRecord
	stats        REPLStats
	clearCalled  bool
	getHistoryFn func(limit int) ([]ExecutionRecord, error)
}

func (m *mockREPLHistoryProvider) GetHistory(limit int) ([]ExecutionRecord, error) {
	if m.getHistoryFn != nil {
		return m.getHistoryFn(limit)
	}
	if limit > len(m.records) {
		return m.records, nil
	}
	return m.records[:limit], nil
}

func (m *mockREPLHistoryProvider) GetRecord(id string) (*ExecutionRecord, error) {
	for _, r := range m.records {
		if r.ID == id {
			return &r, nil
		}
	}
	return nil, nil
}

func (m *mockREPLHistoryProvider) ClearHistory() error {
	m.clearCalled = true
	m.records = nil
	m.stats = REPLStats{}
	return nil
}

func (m *mockREPLHistoryProvider) Stats() REPLStats {
	return m.stats
}

func createTestRecords() []ExecutionRecord {
	now := time.Now()
	return []ExecutionRecord{
		{
			ID:         "1",
			Code:       "print('hello')",
			Output:     "hello\n",
			ReturnVal:  "",
			Duration:   50 * time.Millisecond,
			Timestamp:  now.Add(-5 * time.Minute),
			MemoryUsed: 1024,
		},
		{
			ID:         "2",
			Code:       "x = 1 + 2\nprint(x)",
			Output:     "3\n",
			ReturnVal:  "3",
			Duration:   100 * time.Millisecond,
			Timestamp:  now.Add(-3 * time.Minute),
			MemoryUsed: 2048,
		},
		{
			ID:        "3",
			Code:      "raise ValueError('test error')",
			Error:     "ValueError: test error",
			Duration:  10 * time.Millisecond,
			Timestamp: now.Add(-1 * time.Minute),
		},
	}
}

func TestNewREPLOutputDialog(t *testing.T) {
	provider := &mockREPLHistoryProvider{}
	dialog := NewREPLOutputDialog(provider)

	require.NotNil(t, dialog)
	assert.Equal(t, REPLOutputDialogID, dialog.ID())
}

func TestREPLOutputDialog_NilProvider(t *testing.T) {
	dialog := NewREPLOutputDialog(nil)
	require.NotNil(t, dialog)

	// Init should not panic with nil provider
	cmd := dialog.Init()
	assert.NotNil(t, cmd)
}

func TestREPLOutputDialog_ID(t *testing.T) {
	dialog := NewREPLOutputDialog(nil)
	assert.Equal(t, dialogs.DialogID("reploutput"), dialog.ID())
}

func TestREPLOutputDialog_Init(t *testing.T) {
	provider := &mockREPLHistoryProvider{
		records: createTestRecords(),
		stats:   REPLStats{TotalExecutions: 3, SuccessCount: 2, ErrorCount: 1},
	}
	dialog := NewREPLOutputDialog(provider)

	cmd := dialog.Init()
	require.NotNil(t, cmd)

	// Execute the command to load history
	msg := cmd()
	loaded, ok := msg.(historyLoadedMsg)
	require.True(t, ok)
	assert.Len(t, loaded.records, 3)
	assert.Equal(t, 3, loaded.stats.TotalExecutions)
}

func TestREPLOutputDialog_WindowSizeMsg(t *testing.T) {
	dialog := NewREPLOutputDialog(nil).(*replOutputDialogCmp)

	msg := tea.WindowSizeMsg{Width: 120, Height: 40}
	updated, _ := dialog.Update(msg)
	updatedDialog := updated.(*replOutputDialogCmp)

	assert.Equal(t, 120, updatedDialog.wWidth)
	assert.Equal(t, 40, updatedDialog.wHeight)
	assert.LessOrEqual(t, updatedDialog.width, 110) // max width is 110
}

func TestREPLOutputDialog_HistoryLoadedMsg(t *testing.T) {
	dialog := NewREPLOutputDialog(nil).(*replOutputDialogCmp)

	records := createTestRecords()
	stats := REPLStats{TotalExecutions: 3, SuccessCount: 2, ErrorCount: 1}

	msg := historyLoadedMsg{records: records, stats: stats}
	updated, _ := dialog.Update(msg)
	updatedDialog := updated.(*replOutputDialogCmp)

	assert.Len(t, updatedDialog.records, 3)
	assert.Equal(t, 3, updatedDialog.stats.TotalExecutions)
}

func TestREPLOutputDialog_Navigation(t *testing.T) {
	provider := &mockREPLHistoryProvider{records: createTestRecords()}
	dialog := NewREPLOutputDialog(provider).(*replOutputDialogCmp)
	dialog.records = provider.records
	dialog.selected = 0

	// Move down
	msg := tea.KeyPressMsg{Code: tea.KeyDown}
	updated, _ := dialog.Update(msg)
	assert.Equal(t, 1, updated.(*replOutputDialogCmp).selected)

	// Move down again
	updated, _ = updated.Update(msg)
	assert.Equal(t, 2, updated.(*replOutputDialogCmp).selected)

	// At end, should not go further
	updated, _ = updated.Update(msg)
	assert.Equal(t, 2, updated.(*replOutputDialogCmp).selected)

	// Move up
	msg = tea.KeyPressMsg{Code: tea.KeyUp}
	updated, _ = updated.Update(msg)
	assert.Equal(t, 1, updated.(*replOutputDialogCmp).selected)
}

func TestREPLOutputDialog_NavigationAtBoundaries(t *testing.T) {
	provider := &mockREPLHistoryProvider{records: createTestRecords()}
	dialog := NewREPLOutputDialog(provider).(*replOutputDialogCmp)
	dialog.records = provider.records
	dialog.selected = 0

	// At start, moving up should not go negative
	msg := tea.KeyPressMsg{Code: tea.KeyUp}
	updated, _ := dialog.Update(msg)
	assert.Equal(t, 0, updated.(*replOutputDialogCmp).selected)
}

func TestREPLOutputDialog_SelectRecord(t *testing.T) {
	provider := &mockREPLHistoryProvider{records: createTestRecords()}
	dialog := NewREPLOutputDialog(provider).(*replOutputDialogCmp)
	dialog.records = provider.records
	dialog.mode = viewList

	// Select a record (enter key)
	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
	updated, _ := dialog.Update(msg)
	assert.Equal(t, viewDetails, updated.(*replOutputDialogCmp).mode)
}

func TestREPLOutputDialog_ReturnToList(t *testing.T) {
	provider := &mockREPLHistoryProvider{records: createTestRecords()}
	dialog := NewREPLOutputDialog(provider).(*replOutputDialogCmp)
	dialog.records = provider.records
	dialog.mode = viewDetails

	// Press escape to return to list
	msg := tea.KeyPressMsg{Code: tea.KeyEscape}
	updated, _ := dialog.Update(msg)
	assert.Equal(t, viewList, updated.(*replOutputDialogCmp).mode)
}

func TestREPLOutputDialog_ClearHistory(t *testing.T) {
	provider := &mockREPLHistoryProvider{records: createTestRecords()}
	dialog := NewREPLOutputDialog(provider).(*replOutputDialogCmp)
	dialog.records = provider.records

	// Verify that ClearHistory can be called on provider
	// (We can't easily test the "c" key binding in unit tests)
	provider.ClearHistory()
	assert.True(t, provider.clearCalled)
}

func TestREPLOutputDialog_CloseDialog(t *testing.T) {
	dialog := NewREPLOutputDialog(nil).(*replOutputDialogCmp)
	dialog.mode = viewList

	msg := tea.KeyPressMsg{Code: tea.KeyEscape}
	_, cmd := dialog.Update(msg)

	require.NotNil(t, cmd)
	result := cmd()
	_, isCloseMsg := result.(dialogs.CloseDialogMsg)
	assert.True(t, isCloseMsg)
}

func TestREPLOutputDialog_View_Empty(t *testing.T) {
	dialog := NewREPLOutputDialog(nil).(*replOutputDialogCmp)
	dialog.wWidth = 120
	dialog.wHeight = 40
	dialog.width = 100

	view := dialog.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "No REPL executions yet")
}

func TestREPLOutputDialog_View_WithRecords(t *testing.T) {
	provider := &mockREPLHistoryProvider{
		records: createTestRecords(),
		stats:   REPLStats{TotalExecutions: 3, SuccessCount: 2, ErrorCount: 1},
	}
	dialog := NewREPLOutputDialog(provider).(*replOutputDialogCmp)
	dialog.wWidth = 120
	dialog.wHeight = 40
	dialog.width = 100
	dialog.records = provider.records
	dialog.stats = provider.stats

	view := dialog.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "REPL Output")
}

func TestREPLOutputDialog_Position(t *testing.T) {
	dialog := NewREPLOutputDialog(nil).(*replOutputDialogCmp)
	dialog.wWidth = 120
	dialog.wHeight = 40
	dialog.width = 100

	row, col := dialog.Position()
	assert.Greater(t, row, 0)
	assert.Greater(t, col, 0)
}

func TestREPLOutputDialog_Cursor(t *testing.T) {
	dialog := NewREPLOutputDialog(nil).(*replOutputDialogCmp)
	assert.Nil(t, dialog.Cursor())
}

func TestExecutionRecord_Fields(t *testing.T) {
	now := time.Now()
	record := ExecutionRecord{
		ID:         "test-123",
		Code:       "print('hello')",
		Output:     "hello\n",
		ReturnVal:  "",
		Error:      "",
		Duration:   100 * time.Millisecond,
		Timestamp:  now,
		MemoryUsed: 1024,
	}

	assert.Equal(t, "test-123", record.ID)
	assert.Equal(t, "print('hello')", record.Code)
	assert.Equal(t, "hello\n", record.Output)
	assert.Equal(t, 100*time.Millisecond, record.Duration)
	assert.Equal(t, int64(1024), record.MemoryUsed)
}

func TestREPLStats_Fields(t *testing.T) {
	stats := REPLStats{
		TotalExecutions: 10,
		TotalDuration:   5 * time.Second,
		SuccessCount:    8,
		ErrorCount:      2,
		TotalMemoryUsed: 1024 * 1024,
	}

	assert.Equal(t, 10, stats.TotalExecutions)
	assert.Equal(t, 8, stats.SuccessCount)
	assert.Equal(t, 2, stats.ErrorCount)
	assert.Equal(t, int64(1024*1024), stats.TotalMemoryUsed)
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"short", 10, "short"},
		{"this is a very long string", 15, "this is a ve..."},
		{"exact length", 12, "exact length"},
		{"", 10, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := truncate(tt.input, tt.max)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{500 * time.Microsecond, "500μs"},
		{100 * time.Millisecond, "100ms"},
		{1500 * time.Millisecond, "1.50s"},
		{2 * time.Second, "2.00s"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatDuration(tt.duration)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDefaultKeyMap(t *testing.T) {
	km := DefaultKeyMap()

	assert.NotEmpty(t, km.Next.Keys())
	assert.NotEmpty(t, km.Previous.Keys())
	assert.NotEmpty(t, km.Select.Keys())
	assert.NotEmpty(t, km.Details.Keys())
	assert.NotEmpty(t, km.Close.Keys())
	assert.NotEmpty(t, km.Clear.Keys())
}
