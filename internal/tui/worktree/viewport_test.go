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

// TestRenderLeftTreeConnectorsAtWindowEdges is a regression test for the trap
// called out in the plan: isLastChild must keep scanning the full m.rows
// slice, not the visible window, or the "└" tree connector breaks at the
// window edge.
//
// With cursor=5 (b0), the window is rows[3:7): a2, the repo-b header, b0, b1.
//   - a2 lands as the FIRST visible row and is the true last child of
//     repo-a (its sibling a-worktrees are scrolled out of view) — it must
//     still show "└".
//   - b1 lands as the LAST visible row but is NOT the true last child of
//     repo-b (b2 is, just outside the window) — it must NOT show "└", even
//     though it is the last row rendered.
func TestRenderLeftTreeConnectorsAtWindowEdges(t *testing.T) {
	m := makeTestModel(scrollTestStatuses())
	m.height = 6 // viewportHeight = 4
	m.cursor = 5 // b0

	out := ansi.Strip(m.renderLeft(40))
	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 visible lines, got %d:\n%s", len(lines), out)
	}

	first := lines[0]
	if !strings.Contains(first, "a2") {
		t.Fatalf("expected first visible line to be a2, got %q", first)
	}
	if !strings.Contains(first, "└") {
		t.Errorf(
			"expected a2 (true last child of repo-a, first visible row) to show the \"└\" connector, got %q",
			first,
		)
	}

	last := lines[3]
	if !strings.Contains(last, "b1") {
		t.Fatalf("expected last visible line to be b1, got %q", last)
	}
	if strings.Contains(last, "└") {
		t.Errorf(
			"expected b1 (not repo-b's true last child, merely the last visible row) to NOT show the \"└\" connector, got %q",
			last,
		)
	}
}

// TestRenderLeftTreeConnectorTrueLastChildAtWindowEnd is the companion
// positive case: when the window's last visible row genuinely is the tree's
// last child (b2, with cursor on it), the connector must still render.
func TestRenderLeftTreeConnectorTrueLastChildAtWindowEnd(t *testing.T) {
	m := makeTestModel(scrollTestStatuses())
	m.height = 6 // viewportHeight = 4
	m.cursor = 7 // b2, the real last child of repo-b and the last row overall

	out := ansi.Strip(m.renderLeft(40))
	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 visible lines, got %d:\n%s", len(lines), out)
	}
	last := lines[3]
	if !strings.Contains(last, "b2") {
		t.Fatalf("expected last visible line to be b2, got %q", last)
	}
	if !strings.Contains(last, "└") {
		t.Errorf(
			"expected b2 (true last child, last visible row) to show the \"└\" connector, got %q",
			last,
		)
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

	// The Selected style paints background ANSI color 4 (SGR "44"); no other
	// row in this render uses that background, so its presence pinpoints the
	// highlighted line.
	highlighted := -1
	for i, line := range lines {
		if strings.Contains(line, "44m") {
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
