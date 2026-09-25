package tuicomponents

import "strings"

// SoftSelectedLine draws the soft-bar selection over line: a yellow "▌"
// replaces the line's leading margin space, and the whole line sits on the
// SoftSelected background while every glyph and text color it already carries
// stays visible.
//
// line is already styled, and each colored piece in it ends with an SGR reset.
// A reset clears the background too, so simply wrapping the line in
// SoftSelected lights up only the cells before the first reset (just the "▌").
// The background is therefore re-opened after every reset in the line.
func (p *Palette) SoftSelectedLine(line string) string {
	rest := strings.TrimPrefix(line, " ")
	// The background's own opening sequence, taken from the style itself so
	// it can never drift from SoftSelected. Empty when styling is off, which
	// makes the re-open below a no-op.
	open, _, _ := strings.Cut(p.SoftSelected.Render("x"), "x")
	body := p.SelectedBar.Render("▌") + rest
	if open != "" {
		body = strings.NewReplacer(
			"\x1b[m", "\x1b[m"+open,
			ansiReset, ansiReset+open,
		).Replace(body)
	}
	return open + body + ansiReset
}
