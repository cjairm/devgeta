package tuicomponents_test

import (
	"testing"

	"github.com/charmbracelet/x/ansi"

	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

func TestHintBarContainsKeyAndDesc(t *testing.T) {
	p := tuicomponents.NewPalette()
	hints := []tuicomponents.KeyHint{
		{Key: "↵", Desc: "attach"},
		{Key: "q", Desc: "quit"},
	}
	got := p.HintBar(hints, 200)
	if !containsStr(got, "↵") {
		t.Errorf("HintBar output missing key ↵: %q", got)
	}
	if !containsStr(got, "attach") {
		t.Errorf("HintBar output missing desc attach: %q", got)
	}
	if !containsStr(got, "q") {
		t.Errorf("HintBar output missing key q: %q", got)
	}
	if !containsStr(got, "quit") {
		t.Errorf("HintBar output missing desc quit: %q", got)
	}
}

func TestHintBarEmptyReturnsEmpty(t *testing.T) {
	p := tuicomponents.NewPalette()
	if got := p.HintBar(nil, 100); got != "" {
		t.Errorf("HintBar(nil, 100) = %q, want empty", got)
	}
	if got := p.HintBar([]tuicomponents.KeyHint{}, 100); got != "" {
		t.Errorf("HintBar([], 100) = %q, want empty", got)
	}
}

func TestHintBarZeroWidthReturnsEmpty(t *testing.T) {
	p := tuicomponents.NewPalette()
	hints := []tuicomponents.KeyHint{{Key: "q", Desc: "quit"}}
	if got := p.HintBar(hints, 0); got != "" {
		t.Errorf("HintBar(..., 0) = %q, want empty", got)
	}
	if got := p.HintBar(hints, -1); got != "" {
		t.Errorf("HintBar(..., -1) = %q, want empty", got)
	}
}

func TestHintBarTruncatesToWidth(t *testing.T) {
	p := tuicomponents.NewPalette()
	hints := []tuicomponents.KeyHint{
		{Key: "↵", Desc: "attach"},
		{Key: "j/k", Desc: "move"},
		{Key: "h/l", Desc: "fold"},
		{Key: "z", Desc: "all"},
		{Key: "q", Desc: "quit"},
	}
	width := 20
	got := p.HintBar(hints, width)
	if w := ansi.StringWidth(got); w > width {
		t.Errorf("HintBar display width %d exceeds limit %d", w, width)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
