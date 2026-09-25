package tuiworktree

// Tests for layout B's remaining look changes (step 10): the worktree row's
// diffstat suffix, the removed "∕"/"└" glyphs, and the "sessions" section
// header.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cjairm/devgeta/internal/tooling/task"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

func TestRenderLeftWorktreeRowShowsDiffstatWhenPresent(t *testing.T) {
	m := makeTestModel(testStatuses()) // feature-a at /tmp/a
	m.diffStats["/tmp/a"] = task.BranchStatsResult{Files: 1, Added: 42, Removed: 7}

	out := ansi.Strip(m.renderLeft(60))
	var wtLine string
	for i, r := range m.rows {
		if r.kind == rowWorktree && r.status.Path == "/tmp/a" {
			wtLine = strings.Split(out, "\n")[i]
		}
	}
	if wtLine == "" {
		t.Fatal("expected feature-a's worktree row to render")
	}
	if !strings.Contains(wtLine, "+42") || !strings.Contains(wtLine, "−7") {
		t.Errorf("expected the diffstat +42 −7 pinned to the row, got %q", wtLine)
	}
}

func TestRenderLeftWorktreeRowShowsNothingWhenNoChanges(t *testing.T) {
	m := makeTestModel(testStatuses()) // no diffStats entry for /tmp/a at all
	out := ansi.Strip(m.renderLeft(60))
	var wtLine string
	for i, r := range m.rows {
		if r.kind == rowWorktree && r.status.Path == "/tmp/a" {
			wtLine = strings.Split(out, "\n")[i]
		}
	}
	if wtLine == "" {
		t.Fatal("expected feature-a's worktree row to render")
	}
	if strings.ContainsAny(wtLine, "+−") {
		t.Errorf("expected no diffstat drawn for a worktree with no known changes, got %q", wtLine)
	}
}

func TestRenderLeftWorktreeRowShowsNothingWhenStatsAreZero(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.diffStats["/tmp/a"] = task.BranchStatsResult{Files: 0, Added: 0, Removed: 0}
	out := ansi.Strip(m.renderLeft(60))
	var wtLine string
	for i, r := range m.rows {
		if r.kind == rowWorktree && r.status.Path == "/tmp/a" {
			wtLine = strings.Split(out, "\n")[i]
		}
	}
	if strings.ContainsAny(wtLine, "+−") {
		t.Errorf(
			"expected no '+0 −0' drawn - nothing is shown when there are no changes, got %q",
			wtLine,
		)
	}
}

func TestRenderLeftNeverShowsTheOldBranchGlyphOrTreeConnector(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.diffStats["/tmp/a"] = task.BranchStatsResult{Added: 3, Removed: 1}
	out := ansi.Strip(m.renderLeft(60))
	for _, gone := range []string{"∕", "└"} {
		if strings.Contains(out, gone) {
			t.Errorf("expected %q to be fully removed from layout B, found it in:\n%s", gone, out)
		}
	}
}

func TestRenderLeftNameTruncatesWithEllipsis(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Name: "a-very-long-worktree-name-that-will-not-fit", Repo: "repo-a", Path: "/tmp/a"},
	}
	m := makeTestModel(statuses)
	out := ansi.Strip(m.renderLeft(20))
	if !strings.Contains(out, "…") {
		t.Errorf("expected a truncated long name to end in an ellipsis, got:\n%s", out)
	}
}

func TestRenderLeftSessionsHeaderRowIsDimAndBare(t *testing.T) {
	m := makeTestModel(nil)
	m.sessions = testSessions()
	m.rebuildRows()

	headerIdx := -1
	for i, r := range m.rows {
		if r.kind == rowSessionsHeader {
			headerIdx = i
		}
	}
	if headerIdx == -1 {
		t.Fatal("expected a rowSessionsHeader row when standalone sessions exist")
	}
	line := strings.Split(ansi.Strip(m.renderLeft(40)), "\n")[headerIdx]
	if !strings.Contains(line, "sessions") {
		t.Errorf("expected the section label 'sessions', got %q", line)
	}
}

func TestBuildRowsNoSessionsHeaderWhenNoSessions(t *testing.T) {
	rows := buildRows(testStatuses(), nil, nil, map[string]bool{}, "")
	for _, r := range rows {
		if r.kind == rowSessionsHeader {
			t.Error("expected no sessions header when there are no standalone sessions")
		}
	}
}

// The soft bar keeps the selected row's own colors: its glyph and its
// diffstat are drawn exactly as on an unselected row, not flattened.
func TestSelectedWorktreeRowKeepsItsColors(t *testing.T) {
	m := makeTestModel(testStatuses()) // feature-a at /tmp/a
	m.diffStats["/tmp/a"] = task.BranchStatsResult{Files: 1, Added: 42, Removed: 7}
	var r row
	for _, rr := range m.rows {
		if rr.kind == rowWorktree && rr.status.Path == "/tmp/a" {
			r = rr
		}
	}

	unselected := m.renderWorktreeRow(r, 60, false)
	// Drop the soft background's own re-opened sequences so what is left is
	// the row's colors alone.
	bg, _, _ := strings.Cut(m.palette.SoftSelected.Render("x"), "x")
	selected := strings.ReplaceAll(m.renderWorktreeRow(r, 60, true), bg, "")

	state := tuicomponents.SessionStateFromWorktree(r.status, r.status.AgentState, 0)
	for _, want := range []string{m.palette.StatusDot(state), m.diffstatSuffix("/tmp/a")} {
		if !strings.Contains(unselected, want) {
			t.Fatalf("test setup: unselected row lacks %q", want)
		}
		if !strings.Contains(selected, want) {
			t.Errorf("selected row lost its colored %q: %q", ansi.Strip(want), selected)
		}
	}
}
