package tuicomponents

import (
	"strings"
	"testing"

	lip "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestHighlightMatch(t *testing.T) {
	p := NewPalette()
	open, _, _ := strings.Cut(p.Match.Render("x"), "x")
	if open == "" {
		t.Fatal("test setup: Match rendered no style sequence")
	}
	base := lip.NewStyle()

	got := p.HighlightMatch("fix-Stale-notify", "stale", base)
	if ansi.Strip(got) != "fix-Stale-notify" {
		t.Errorf("text changed: %q", ansi.Strip(got))
	}
	if !strings.Contains(got, p.Match.Render("Stale")) {
		t.Errorf("expected the case-insensitive match styled, got %q", got)
	}

	for _, query := range []string{"", "absent"} {
		if got := p.HighlightMatch("fix-stale", query, base); strings.Contains(got, open) {
			t.Errorf("query %q: nothing should be highlighted, got %q", query, got)
		}
	}
}
