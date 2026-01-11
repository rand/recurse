package status

import (
	"testing"

	"github.com/rand/recurse/internal/budget"
	"github.com/stretchr/testify/assert"
)

func TestBudgetStatusView(t *testing.T) {
	limits := budget.Limits{
		MaxInputTokens:    100000,
		MaxOutputTokens:   50000,
		MaxTotalCost:      5.0,
		MaxRecursionDepth: 5,
	}
	tracker := budget.NewTracker(limits)
	_ = tracker.AddTokens(50000, 25000, 0, budget.SonnetInputCost, budget.SonnetOutputCost)

	bs := NewBudgetStatus(tracker)
	bs.SetWidth(80)

	view := bs.View()

	// Should contain token info
	assert.Contains(t, view, "TOK")
	assert.Contains(t, view, "50k")

	// Should contain cost info
	assert.Contains(t, view, "COST")
	assert.Contains(t, view, "$")

	// Progress bars should be present
	assert.Contains(t, view, "█")
	assert.Contains(t, view, "░")
}

func TestBudgetStatusWithDepth(t *testing.T) {
	limits := budget.Limits{
		MaxInputTokens:    100000,
		MaxRecursionDepth: 5,
	}
	tracker := budget.NewTracker(limits)
	_ = tracker.IncrementSubCall(2)

	bs := NewBudgetStatus(tracker)
	view := bs.View()

	// Should show depth when non-zero
	assert.Contains(t, view, "DEPTH")
	assert.Contains(t, view, "2/5")
}

func TestBudgetStatusCompactView(t *testing.T) {
	limits := budget.Limits{
		MaxInputTokens: 100000,
		MaxTotalCost:   5.0,
	}
	tracker := budget.NewTracker(limits)
	_ = tracker.AddTokens(25000, 0, 0, budget.SonnetInputCost, budget.SonnetOutputCost)

	bs := NewBudgetStatus(tracker)
	compact := bs.CompactView()

	// Should be more compact
	assert.Contains(t, compact, "25k")
	assert.Contains(t, compact, "$")
	// Should not contain labels in compact mode
	assert.NotContains(t, compact, "TOK")
	assert.NotContains(t, compact, "COST")
}

func TestBudgetStatusNilTracker(t *testing.T) {
	bs := NewBudgetStatus(nil)
	assert.Empty(t, bs.View())
	assert.Empty(t, bs.CompactView())
}

func TestMiniBar(t *testing.T) {
	bs := &BudgetStatus{}

	tests := []struct {
		percent  float64
		width    int
		expected string
	}{
		{0, 5, "░░░░░"},
		{50, 5, "██░░░"},
		{100, 5, "█████"},
		{150, 5, "█████"}, // Capped at 100%
		{-10, 5, "░░░░░"}, // Capped at 0%
	}

	for _, tt := range tests {
		result := bs.miniBar(tt.percent, tt.width)
		assert.Equal(t, tt.expected, result)
	}
}
