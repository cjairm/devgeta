package tuicomponents

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Every colored piece of a row ends with an SGR reset, which also clears the
// background. The selection must re-open it after each one, or only the "▌"
// cell before the first reset gets the soft background.
func TestSoftSelectedLineKeepsBackgroundAcrossResets(t *testing.T) {
	p := NewPalette()
	open, _, _ := strings.Cut(p.SoftSelected.Render("x"), "x")
	if open == "" {
		t.Fatal("test setup: SoftSelected rendered no background sequence")
	}
	line := " " + p.RepoHeader.Render("●") + " name " + p.SectionHead.Render("+3 −1")

	got := p.SoftSelectedLine(line)

	// Every reset except the closing one must be followed by the background.
	body := strings.TrimSuffix(got, ansiReset)
	for _, reset := range []string{"\x1b[m", ansiReset} {
		for i := strings.Index(body, reset); i >= 0; {
			after := body[i+len(reset):]
			if !strings.HasPrefix(after, open) {
				t.Errorf("reset at byte %d not followed by the background: %q", i, got)
			}
			next := strings.Index(after, reset)
			if next < 0 {
				break
			}
			i += len(reset) + next
		}
	}
	if !strings.HasPrefix(got, open) {
		t.Errorf("expected the line to open with the background, got %q", got)
	}
	if want := "▌● name +3 −1"; ansi.Strip(got) != want {
		t.Errorf("text changed: got %q, want %q", ansi.Strip(got), want)
	}
	if w := ansi.StringWidth(got); w != ansi.StringWidth(line) {
		t.Errorf("width changed: got %d, want %d", w, ansi.StringWidth(line))
	}
}

// SoftSelected's background and the dim text color are the same ANSI color
// (8), so dim text on a raised line is invisible in every terminal theme -
// an idle agent row, whose every piece is dim, rendered as an empty bar.
// A raised line must never carry the dim foreground.
func TestRaisedLineNeverKeepsDimTextOnItsBackground(t *testing.T) {
	p := NewPalette()
	dim, _, _ := strings.Cut(p.SectionHead.Render("x"), "x")
	if dim == "" {
		t.Fatal("test setup: SectionHead rendered no color sequence")
	}
	line := " " + p.NoSession.Render("○ devgeta") + " " + p.SectionHead.Render(":1")

	for name, got := range map[string]string{
		"SoftSelectedLine": p.SoftSelectedLine(line),
		"RaisedLine":       p.RaisedLine(line),
	} {
		if strings.Contains(got, dim) {
			t.Errorf("%s kept the dim foreground on the raised background: %q", name, got)
		}
		if !strings.Contains(ansi.Strip(got), "○ devgeta :1") {
			t.Errorf("%s changed the text: %q", name, ansi.Strip(got))
		}
	}
}
