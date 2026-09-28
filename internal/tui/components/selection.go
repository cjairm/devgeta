package tuicomponents

import "strings"

// ansiDefaultFg resets the foreground only, leaving the background alone.
const ansiDefaultFg = "\x1b[39m"

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
	return p.RaisedLine(p.SelectedBar.Render("▌") + strings.TrimPrefix(line, " "))
}

// RaisedLine puts line on the SoftSelected background without the "▌" edge
// marker: the same raise as a selected row, for a line that stands out from
// the list but is not the cursor (the ws dashboard's folded-section bar).
// Colors already in line stay visible, for the reason SoftSelectedLine gives.
func (p *Palette) RaisedLine(line string) string {
	// The background's own opening sequence, taken from the style itself so
	// it can never drift from SoftSelected. Empty when styling is off, which
	// makes the re-open below a no-op.
	open, _, _ := strings.Cut(p.SoftSelected.Render("x"), "x")
	// SoftSelected's background is the same ANSI color as the dim text
	// (SectionHead, NoSession, ...), so dim text here would be invisible in
	// every terminal theme. It takes the default foreground instead.
	dim, _, _ := strings.Cut(p.SectionHead.Render("x"), "x")
	body := line
	if dim != "" {
		body = strings.ReplaceAll(body, dim, ansiDefaultFg)
	}
	if open != "" {
		body = strings.NewReplacer(
			"\x1b[m", "\x1b[m"+open,
			ansiReset, ansiReset+open,
		).Replace(body)
	}
	return open + body + ansiReset
}
