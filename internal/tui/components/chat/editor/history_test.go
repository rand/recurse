package editor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInputHistory_AddAndNavigate(t *testing.T) {
	h := NewInputHistory("", 100, false)

	// Add entries
	h.Add("first")
	h.Add("second")
	h.Add("third")

	assert.Equal(t, 3, h.Len())

	// Start navigation with current input
	h.StartNavigation("current draft")

	// Navigate backwards (up arrow)
	prev, ok := h.Previous()
	assert.True(t, ok)
	assert.Equal(t, "third", prev)

	prev, ok = h.Previous()
	assert.True(t, ok)
	assert.Equal(t, "second", prev)

	prev, ok = h.Previous()
	assert.True(t, ok)
	assert.Equal(t, "first", prev)

	// No more previous
	_, ok = h.Previous()
	assert.False(t, ok)

	// Navigate forward (down arrow)
	next, ok := h.Next()
	assert.True(t, ok)
	assert.Equal(t, "second", next)

	next, ok = h.Next()
	assert.True(t, ok)
	assert.Equal(t, "third", next)

	// Back to draft
	next, ok = h.Next()
	assert.True(t, ok)
	assert.Equal(t, "current draft", next)
}

func TestInputHistory_DuplicateRejection(t *testing.T) {
	h := NewInputHistory("", 100, false)

	h.Add("first")
	h.Add("first") // Duplicate, should be rejected

	assert.Equal(t, 1, h.Len())
}

func TestInputHistory_MaxItems(t *testing.T) {
	h := NewInputHistory("", 3, false)

	h.Add("1")
	h.Add("2")
	h.Add("3")
	h.Add("4") // Should trim "1"

	assert.Equal(t, 3, h.Len())

	h.StartNavigation("")
	prev, _ := h.Previous()
	assert.Equal(t, "4", prev)
	prev, _ = h.Previous()
	assert.Equal(t, "3", prev)
	prev, _ = h.Previous()
	assert.Equal(t, "2", prev)
}

func TestInputHistory_EmptyInput(t *testing.T) {
	h := NewInputHistory("", 100, false)

	h.Add("")

	// Empty strings are not added (trimming is done by caller)
	assert.Equal(t, 0, h.Len())
}

func TestInputHistory_Reset(t *testing.T) {
	h := NewInputHistory("", 100, false)

	h.Add("entry")
	h.StartNavigation("draft")
	h.Previous()

	assert.True(t, h.IsNavigating())

	h.Reset()

	assert.False(t, h.IsNavigating())
}
