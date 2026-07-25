package tuicomponents_test

import (
	"strings"
	"testing"

	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

func TestDiffStat(t *testing.T) {
	p := tuicomponents.NewPalette()
	got := p.DiffStat(84, 12)
	if !strings.Contains(got, "+84") {
		t.Errorf("DiffStat output %q missing +84", got)
	}
	if !strings.Contains(got, "-12") {
		t.Errorf("DiffStat output %q missing -12", got)
	}
}

func TestDirtyCount(t *testing.T) {
	p := tuicomponents.NewPalette()
	got := p.DirtyCount(3)
	if !strings.Contains(got, "±3") {
		t.Errorf("DirtyCount output %q missing ±3", got)
	}
}

func TestDiffStatLineOmitsZeroPrefix(t *testing.T) {
	p := tuicomponents.NewPalette()
	got := p.DiffStatLine(0, 84, 12)
	if strings.Contains(got, "±0") {
		t.Errorf("DiffStatLine(0,...) output %q should not contain ±0", got)
	}
	if !strings.Contains(got, "+84") {
		t.Errorf("DiffStatLine(0,...) output %q missing +84", got)
	}
}

func TestDiffStatLineWithFiles(t *testing.T) {
	p := tuicomponents.NewPalette()
	got := p.DiffStatLine(3, 84, 12)
	if !strings.Contains(got, "±3") {
		t.Errorf("DiffStatLine(3,...) output %q missing ±3", got)
	}
	if !strings.Contains(got, "+84") {
		t.Errorf("DiffStatLine(3,...) output %q missing +84", got)
	}
	if !strings.Contains(got, "-12") {
		t.Errorf("DiffStatLine(3,...) output %q missing -12", got)
	}
}
