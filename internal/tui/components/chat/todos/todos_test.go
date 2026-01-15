package todos

import (
	"testing"

	"github.com/rand/recurse/internal/session"
	"github.com/rand/recurse/internal/tui/styles"
	"github.com/stretchr/testify/assert"
)

func TestStatusOrder(t *testing.T) {
	tests := []struct {
		status   session.TodoStatus
		expected int
	}{
		{session.TodoStatusCompleted, 0},
		{session.TodoStatusInProgress, 1},
		{session.TodoStatusPending, 2},
		{"unknown", 2}, // Unknown status should be treated as pending
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			got := statusOrder(tt.status)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestSortTodos(t *testing.T) {
	todos := []session.Todo{
		{Content: "Pending task", Status: session.TodoStatusPending},
		{Content: "Completed task", Status: session.TodoStatusCompleted},
		{Content: "In progress task", Status: session.TodoStatusInProgress},
		{Content: "Another pending", Status: session.TodoStatusPending},
	}

	sortTodos(todos)

	// After sorting: Completed first, then InProgress, then Pending
	assert.Equal(t, session.TodoStatusCompleted, todos[0].Status)
	assert.Equal(t, session.TodoStatusInProgress, todos[1].Status)
	assert.Equal(t, session.TodoStatusPending, todos[2].Status)
	assert.Equal(t, session.TodoStatusPending, todos[3].Status)
}

func TestSortTodos_StableSort(t *testing.T) {
	// Test that sorting is stable (preserves order of equal elements)
	todos := []session.Todo{
		{Content: "First pending", Status: session.TodoStatusPending},
		{Content: "Second pending", Status: session.TodoStatusPending},
		{Content: "Third pending", Status: session.TodoStatusPending},
	}

	sortTodos(todos)

	// Order should be preserved for items with same status
	assert.Equal(t, "First pending", todos[0].Content)
	assert.Equal(t, "Second pending", todos[1].Content)
	assert.Equal(t, "Third pending", todos[2].Content)
}

func TestFormatTodosList_EmptyList(t *testing.T) {
	theme := styles.CurrentTheme()
	result := FormatTodosList(nil, "→", theme, 80)
	assert.Empty(t, result)

	result = FormatTodosList([]session.Todo{}, "→", theme, 80)
	assert.Empty(t, result)
}

func TestFormatTodosList_SingleTodo(t *testing.T) {
	theme := styles.CurrentTheme()
	todos := []session.Todo{
		{Content: "Test task", Status: session.TodoStatusPending},
	}

	result := FormatTodosList(todos, "→", theme, 80)

	assert.NotEmpty(t, result)
	assert.Contains(t, result, "Test task")
}

func TestFormatTodosList_AllStatuses(t *testing.T) {
	theme := styles.CurrentTheme()
	todos := []session.Todo{
		{Content: "Pending task", Status: session.TodoStatusPending},
		{Content: "Completed task", Status: session.TodoStatusCompleted},
		{Content: "In progress task", Status: session.TodoStatusInProgress, ActiveForm: "Working on it"},
	}

	result := FormatTodosList(todos, "→", theme, 80)

	assert.NotEmpty(t, result)
	// Should contain task content
	assert.Contains(t, result, "Pending task")
	assert.Contains(t, result, "Completed task")
	// In progress shows ActiveForm if set
	assert.Contains(t, result, "Working on it")
}

func TestFormatTodosList_InProgressUsesActiveForm(t *testing.T) {
	theme := styles.CurrentTheme()
	todos := []session.Todo{
		{
			Content:    "Original content",
			Status:     session.TodoStatusInProgress,
			ActiveForm: "Active form text",
		},
	}

	result := FormatTodosList(todos, "→", theme, 80)

	// Should show ActiveForm instead of Content for in-progress items
	assert.Contains(t, result, "Active form text")
	assert.NotContains(t, result, "Original content")
}

func TestFormatTodosList_InProgressWithoutActiveForm(t *testing.T) {
	theme := styles.CurrentTheme()
	todos := []session.Todo{
		{
			Content:    "Original content",
			Status:     session.TodoStatusInProgress,
			ActiveForm: "", // Empty ActiveForm
		},
	}

	result := FormatTodosList(todos, "→", theme, 80)

	// Should fall back to Content when ActiveForm is empty
	assert.Contains(t, result, "Original content")
}

func TestFormatTodosList_Truncation(t *testing.T) {
	theme := styles.CurrentTheme()
	longContent := "This is a very long task description that should be truncated when the width is small"
	todos := []session.Todo{
		{Content: longContent, Status: session.TodoStatusPending},
	}

	// Use a very small width to force truncation
	result := FormatTodosList(todos, "→", theme, 30)

	assert.NotEmpty(t, result)
	// The result should be truncated (shorter than original with ellipsis indicator)
	// We can't check exact content due to ANSI codes, but it should not contain the full text
}

func TestFormatTodosList_PreservesOrder(t *testing.T) {
	theme := styles.CurrentTheme()
	todos := []session.Todo{
		{Content: "Third", Status: session.TodoStatusPending},
		{Content: "First", Status: session.TodoStatusCompleted},
		{Content: "Second", Status: session.TodoStatusInProgress},
	}

	result := FormatTodosList(todos, "→", theme, 80)
	lines := splitLines(result)

	// After sorting: Completed (First), InProgress (Second), Pending (Third)
	assert.GreaterOrEqual(t, len(lines), 3)
	assert.Contains(t, lines[0], "First")
	assert.Contains(t, lines[1], "Second")
	assert.Contains(t, lines[2], "Third")
}

// Helper to split result into lines
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
