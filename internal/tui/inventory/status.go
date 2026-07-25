package tuiinventory

import (
	"github.com/cjairm/devgeta/internal/inventory"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

// statusGlyph returns the raw glyph for an item state (no ANSI styling).
// StateOK and StateMissing intentionally share "●" — color is the differentiator,
// mirroring the legend page's filled-vs-hollow convention.
func statusGlyph(state inventory.ItemState) string {
	if state == inventory.StateUnknown {
		return "○"
	}
	return "●"
}

// statusDot returns the colored glyph string, reusing Palette's raw colors:
// green for OK, red for MISSING, gray for UNKNOWN.
func statusDot(p *tuicomponents.Palette, state inventory.ItemState) string {
	g := statusGlyph(state)
	switch state {
	case inventory.StateOK:
		return p.Running.Render(g)
	case inventory.StateMissing:
		return p.DiffRemoved.Render(g)
	default:
		return p.NoSession.Render(g)
	}
}

// sourceTag renders a dim suffix tag for pre-existing items (e.g. "(pre-existing)").
// Installed items get no tag.
func sourceTag(p *tuicomponents.Palette, source string) string {
	if source != "pre-existing" {
		return ""
	}
	return p.Inactive.Render(" (pre-existing)")
}
