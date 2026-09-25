package tuiworktree

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// scrollTestStatuses returns two repos of three worktrees each — enough rows
// (1 header + 3 worktrees, twice) to overflow a small viewport and exercise
// scrolling, with a real "last child" per repo (a2, b2) to check the tree
// connector.
func scrollTestStatuses() []worktree.WorktreeStatus {
	return []worktree.WorktreeStatus{
		{Name: "a0", Repo: "repo-a"},
		{Name: "a1", Repo: "repo-a"},
		{Name: "a2", Repo: "repo-a"},
		{Name: "b0", Repo: "repo-b"},
		{Name: "b1", Repo: "repo-b"},
		{Name: "b2", Repo: "repo-b"},
	}
}

// TestRenderLeftScrollsWithCursor verifies the left list only renders the
// rows[start:end] window around the cursor instead of every row — moving the
// cursor from the top of the list to the bottom must bring the bottom rows
// into view and drop the top ones, rather than always emitting all 8 rows
// (2 headers + 6 worktrees) and hard-truncating the tail.
func TestRenderLeftScrollsWithCursor(t *testing.T) {
	m := makeTestModel(scrollTestStatuses())
	m.height = 6 // viewportHeight = height-2 = 4, less than the 8 total rows

	m.cursor = 1 // a0, near the top
	top := ansi.Strip(m.renderLeft(40))
	topLines := strings.Split(top, "\n")
	if len(topLines) != 4 {
		t.Fatalf("expected 4 visible lines, got %d:\n%s", len(topLines), top)
	}
	for _, want := range []string{"a0", "a1", "a2"} {
		if !strings.Contains(top, want) {
			t.Errorf("cursor near top: expected visible output to contain %q, got:\n%s", want, top)
		}
	}
	for _, notWant := range []string{"b0", "b1", "b2"} {
		if strings.Contains(top, notWant) {
			t.Errorf(
				"cursor near top: expected %q to have scrolled out of view, got:\n%s",
				notWant,
				top,
			)
		}
	}

	m.cursor = 7 // b2, near the bottom
	bottom := ansi.Strip(m.renderLeft(40))
	bottomLines := strings.Split(bottom, "\n")
	if len(bottomLines) != 4 {
		t.Fatalf("expected 4 visible lines, got %d:\n%s", len(bottomLines), bottom)
	}
	for _, want := range []string{"b0", "b1", "b2"} {
		if !strings.Contains(bottom, want) {
			t.Errorf(
				"cursor near bottom: expected visible output to contain %q, got:\n%s",
				want,
				bottom,
			)
		}
	}
	for _, notWant := range []string{"a0", "a1", "a2"} {
		if strings.Contains(bottom, notWant) {
			t.Errorf(
				"cursor near bottom: expected %q to have scrolled out of view, got:\n%s",
				notWant,
				bottom,
			)
		}
	}
}

// TestRenderLeftCursorHighlightShiftsByStart verifies the cursor-highlight
// comparison accounts for the window's start offset: with the window scrolled
// so start > 0, the row actually under the cursor (not whichever row lands at
// relative index m.cursor) must be the one styled as selected.
func TestRenderLeftCursorHighlightShiftsByStart(t *testing.T) {
	m := makeTestModel(scrollTestStatuses())
	m.height = 6 // viewportHeight = 4
	m.cursor = 5 // b0 — window is rows[3:7), so b0 sits at relative index 2

	out := m.renderLeft(40) // unstripped: need the ANSI to find the selected style
	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 visible lines, got %d:\n%s", len(lines), out)
	}

	// The soft-bar selection style (layout B) paints background ANSI color 8
	// (SGR "100"); no other row in this render uses that background, so its
	// presence pinpoints the highlighted line.
	highlighted := -1
	for i, line := range lines {
		if strings.Contains(line, "100m") {
			highlighted = i
		}
	}
	if highlighted == -1 {
		t.Fatalf("expected exactly one line with the selected-row background, found none:\n%s", out)
	}
	if !strings.Contains(ansi.Strip(lines[highlighted]), "b0") {
		t.Errorf(
			"expected the highlighted line (index %d) to be b0 (m.cursor's row), got %q",
			highlighted,
			ansi.Strip(lines[highlighted]),
		)
	}
}
