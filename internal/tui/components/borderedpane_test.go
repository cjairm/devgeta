package tuicomponents_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

// stripANSI removes ANSI escape codes from a string.
func stripANSI(s string) string {
	re := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return re.ReplaceAllString(s, "")
}

func TestBorderedPane_TitleEmbeddedInTopBorder(t *testing.T) {
	p := tuicomponents.NewPalette()
	got := p.BorderedPane("inventory", 40, []string{"row one", "row two"})
	lines := strings.Split(got, "\n")
	if !strings.Contains(lines[0], "inventory") {
		t.Errorf("top border %q should contain the title", lines[0])
	}
	clean := stripANSI(lines[0])
	if !strings.HasPrefix(clean, "╭") {
		t.Errorf("top border %q should start with ╭", clean)
	}
}

func TestBorderedPane_BottomBorder(t *testing.T) {
	p := tuicomponents.NewPalette()
	got := p.BorderedPane("x", 20, []string{"a"})
	lines := strings.Split(got, "\n")
	last := lines[len(lines)-1]
	clean := stripANSI(last)
	if !strings.HasPrefix(clean, "╰") || !strings.HasSuffix(clean, "╯") {
		t.Errorf("bottom border %q should be ╰...╯", clean)
	}
}

func TestBorderedPane_BodyLinesWrappedInSideBorders(t *testing.T) {
	p := tuicomponents.NewPalette()
	got := p.BorderedPane("t", 20, []string{"hello"})
	lines := strings.Split(got, "\n")
	body := lines[1]
	if !strings.Contains(body, "hello") {
		t.Errorf("body line %q should contain the content", body)
	}
	clean := stripANSI(body)
	if !strings.HasPrefix(clean, "│") || !strings.HasSuffix(clean, "│") {
		t.Errorf("body line %q should be wrapped in │...│", clean)
	}
}

func TestBorderedPane_EveryLineMatchesRequestedWidth(t *testing.T) {
	p := tuicomponents.NewPalette()
	width := 30
	got := p.BorderedPane(
		"inventory",
		width,
		[]string{"short", "a much longer line of content here"},
	)
	for i, line := range strings.Split(got, "\n") {
		if w := ansi.StringWidth(line); w != width {
			t.Errorf("line %d: display width %d, want %d (%q)", i, w, width, line)
		}
	}
}

func TestBorderedPane_NoBodyLinesStillRendersBorders(t *testing.T) {
	p := tuicomponents.NewPalette()
	got := p.BorderedPane("empty", 20, nil)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected top+bottom border only (2 lines), got %d: %q", len(lines), got)
	}
}

func TestBorderedPane_TitleWiderThanWidthStillMatchesRequestedWidth(t *testing.T) {
	p := tuicomponents.NewPalette()
	width := 20
	got := p.BorderedPane("a very long title that exceeds width", width, []string{"row"})
	for i, line := range strings.Split(got, "\n") {
		if w := ansi.StringWidth(line); w != width {
			t.Errorf("line %d: display width %d, want %d (%q)", i, w, width, line)
		}
	}
}
