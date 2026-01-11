package status

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/rand/recurse/internal/budget"
	"github.com/rand/recurse/internal/tui/styles"
)

// BudgetStatus renders budget information for the status bar.
type BudgetStatus struct {
	tracker *budget.Tracker
	width   int
}

// NewBudgetStatus creates a new budget status renderer.
func NewBudgetStatus(tracker *budget.Tracker) *BudgetStatus {
	return &BudgetStatus{tracker: tracker}
}

// SetWidth sets the available width for rendering.
func (b *BudgetStatus) SetWidth(width int) {
	b.width = width
}

// View renders the budget status bar.
func (b *BudgetStatus) View() string {
	if b.tracker == nil {
		return ""
	}

	t := styles.CurrentTheme()
	report := budget.NewReport(b.tracker)

	var parts []string

	// Tokens section
	tokensUsage := report.Usage.InputTokensPercent
	tokensColor := b.colorForPercent(tokensUsage, t)
	tokensText := fmt.Sprintf("%.0fk", float64(report.State.InputTokens)/1000)
	tokensSection := b.renderSection("TOK", tokensText, tokensUsage, tokensColor, t)
	parts = append(parts, tokensSection)

	// Cost section
	costUsage := report.Usage.CostPercent
	costColor := b.colorForPercent(costUsage, t)
	costText := fmt.Sprintf("$%.2f", report.State.TotalCost)
	costSection := b.renderSection("COST", costText, costUsage, costColor, t)
	parts = append(parts, costSection)

	// Depth section (only if non-zero)
	if report.State.RecursionDepth > 0 {
		depthUsage := report.Usage.RecursionPercent
		depthColor := b.colorForPercent(depthUsage, t)
		depthText := fmt.Sprintf("%d/%d", report.State.RecursionDepth, report.Limits.MaxRecursionDepth)
		depthSection := b.renderSection("DEPTH", depthText, depthUsage, depthColor, t)
		parts = append(parts, depthSection)
	}

	return strings.Join(parts, " ")
}

// renderSection renders a single status section.
func (b *BudgetStatus) renderSection(label, value string, percent float64, clr color.Color, t *styles.Theme) string {
	labelStyle := t.S().Base.Foreground(t.FgMuted).Bold(true)
	valueStyle := t.S().Base.Foreground(clr)
	barStyle := t.S().Base.Foreground(clr)

	bar := b.miniBar(percent, 5)

	return fmt.Sprintf("%s %s %s",
		labelStyle.Render(label),
		valueStyle.Render(value),
		barStyle.Render(bar),
	)
}

// miniBar creates a compact progress bar.
func (b *BudgetStatus) miniBar(percent float64, width int) string {
	if percent > 100 {
		percent = 100
	}
	if percent < 0 {
		percent = 0
	}

	filled := int(percent / 100 * float64(width))
	empty := width - filled

	return strings.Repeat("█", filled) + strings.Repeat("░", empty)
}

// colorForPercent returns a color based on usage percentage.
func (b *BudgetStatus) colorForPercent(percent float64, t *styles.Theme) color.Color {
	switch {
	case percent >= 90:
		return t.Red
	case percent >= 75:
		return t.Yellow
	case percent >= 50:
		return t.Blue
	default:
		return t.Green
	}
}

// CompactView renders a very compact budget status for narrow displays.
func (b *BudgetStatus) CompactView() string {
	if b.tracker == nil {
		return ""
	}

	t := styles.CurrentTheme()
	report := budget.NewReport(b.tracker)

	tokensColor := b.colorForPercent(report.Usage.InputTokensPercent, t)
	costColor := b.colorForPercent(report.Usage.CostPercent, t)

	tokensStyle := t.S().Base.Foreground(tokensColor)
	costStyle := t.S().Base.Foreground(costColor)

	return fmt.Sprintf("%s %s",
		tokensStyle.Render(fmt.Sprintf("%.0fk", float64(report.State.InputTokens)/1000)),
		costStyle.Render(fmt.Sprintf("$%.2f", report.State.TotalCost)),
	)
}
