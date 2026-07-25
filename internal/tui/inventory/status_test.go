package tuiinventory

import (
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/inventory"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

func TestStatusGlyph_NoANSI(t *testing.T) {
	cases := map[inventory.ItemState]string{
		inventory.StateOK:      "●",
		inventory.StateMissing: "●",
		inventory.StateUnknown: "○",
	}
	for state, want := range cases {
		got := statusGlyph(state)
		if got != want {
			t.Errorf("state %v: got %q, want %q", state, got, want)
		}
		if strings.ContainsRune(got, '\x1b') {
			t.Errorf("state %v: statusGlyph must not contain ANSI escape bytes", state)
		}
	}
}

func TestStatusDot_ContainsGlyph(t *testing.T) {
	p := tuicomponents.NewPalette()
	cases := map[inventory.ItemState]string{
		inventory.StateOK:      "●",
		inventory.StateMissing: "●",
		inventory.StateUnknown: "○",
	}
	for state, glyph := range cases {
		got := statusDot(p, state)
		if !strings.Contains(got, glyph) {
			t.Errorf("state %v: statusDot %q does not contain glyph %q", state, got, glyph)
		}
	}
}

func TestSourceTag_PreExistingIsTagged(t *testing.T) {
	p := tuicomponents.NewPalette()
	if got := sourceTag(p, "pre-existing"); !strings.Contains(got, "pre-existing") {
		t.Errorf("expected pre-existing tag, got %q", got)
	}
	if got := sourceTag(p, "installed"); got != "" {
		t.Errorf("installed items should have no tag, got %q", got)
	}
}
