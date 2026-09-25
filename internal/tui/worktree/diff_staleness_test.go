package tuiworktree

// Regression tests for B4 and B6.
//
// B4: renderDiffContent only ever checked whether m.diffContent was empty,
// never whether it belonged to the row the cursor is now on. Moving to a new
// worktree before its diff arrives left the PREVIOUS row's diff on screen
// under the new row's header. diffScroll had the same problem from the other
// side: it was reset to 0 only inside the j/k handlers, so any other way of
// changing the selection (h, l, z, the filter, a mouse click,
// placeCursorOnActive) carried the old scroll offset into a diff it had
// nothing to do with.
//
// B6: computeDiffCmd hard-cut content at exactly maxDiffBytes, which lands
// mid-line (and potentially mid ANSI escape sequence) whenever the diff's
// line lengths don't happen to divide the limit evenly.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cjairm/devgeta/internal/tooling/task"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

func TestWorktreeRowShowsLoadingInsteadOfStaleDiffFromDifferentPath(t *testing.T) {
	m := makeTestModel(testStatuses())
	// Seed a diff for feature-a, as if the cursor had just left it.
	m.diffContent = "feature-a's stale diff"
	m.diffPath = "/tmp/a"

	// Move the cursor onto feature-b (a different worktree) without a new
	// diff having landed yet - diffContent/diffPath still describe feature-a.
	if _, ok := m.focusRow(func(r row) bool {
		return r.kind == rowWorktree && r.status.Path == "/tmp/b"
	}); !ok {
		t.Fatal("test setup: expected to find feature-b's worktree row")
	}

	got := ansi.Strip(m.renderRight(100))
	if strings.Contains(got, "feature-a's stale diff") {
		t.Errorf("a worktree row must not render the previous row's stale diff, got %q", got)
	}
	if !strings.Contains(got, "(loading...)") {
		t.Errorf(
			"expected the loading placeholder while the new row's diff is in flight, got %q",
			got,
		)
	}
}

func TestSelectionChangeResetsDiffScrollEvenWithoutJK(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a", Path: "/tmp/a", TmuxWindow: "wt-feature-a"},
		{Name: "feature-b", Repo: "repo-b", Path: "/tmp/b", TmuxWindow: "wt-feature-b"},
	}
	m := makeTestModel(statuses)
	m.mgr = newTestWorktreeManager()
	m.diffScroll = 5

	// h collapses repo-a and lands the cursor on its header - a genuine
	// selection change (feature-a's worktree row -> a header with no path)
	// that never goes through the j/k handlers.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'h'})
	m = updated.(Model)

	if m.diffScroll != 0 {
		t.Errorf("expected a selection change via h to reset diffScroll, got %d", m.diffScroll)
	}
}

// TestForceDiffAloneDoesNotResetScroll is the case the fix must NOT touch:
// the 30-second slow load re-fetches the diff for the SAME selected row
// (forceDiff, ADR-0024 §3's "your own actions never wait on the timer"), and
// that is a content refresh, not a selection change - resetting the user's
// scroll position every time it lands would be its own regression.
func TestForceDiffAloneDoesNotResetScroll(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()
	m.diffScroll = 5
	before := m.selectedPath()

	m.forceDiff = true
	updated, _ := m.Update(tea.Msg(nil))
	m = updated.(Model)

	if m.selectedPath() != before {
		t.Fatalf(
			"test setup: selection must not have moved, got %q (was %q)",
			m.selectedPath(),
			before,
		)
	}
	if m.diffScroll != 5 {
		t.Errorf(
			"expected forceDiff alone (no selection change) to leave diffScroll untouched, got %d",
			m.diffScroll,
		)
	}
}

func TestComputeDiffCmdTruncatesAtLastFullLineNotMidLine(t *testing.T) {
	// 101 bytes/line does not divide maxDiffBytes evenly, so a hard byte cut
	// at exactly the limit is guaranteed to land mid-line.
	line := strings.Repeat("x", 100) + "\n"
	var b strings.Builder
	for b.Len() <= maxDiffBytes {
		b.WriteString(line)
	}

	m := makeTestModel(nil)
	m.diffFn = func(_ string) (task.BranchDiffResult, error) {
		return task.BranchDiffResult{Content: b.String(), Files: 1}, nil
	}

	cmd := m.computeDiffCmd(worktree.WorktreeStatus{Path: "/tmp/a", Name: "feature-a"})
	msg, ok := cmd().(diffMsg)
	if !ok {
		t.Fatalf("expected a diffMsg, got %T", cmd())
	}

	body := strings.TrimSuffix(msg.content, "... (truncated)")
	if !strings.HasSuffix(body, "\n") {
		tail := body
		if len(tail) > 20 {
			tail = tail[len(tail)-20:]
		}
		t.Fatalf("expected the truncated content to end at a full line boundary, got tail %q", tail)
	}
	if len(body) > maxDiffBytes {
		t.Errorf(
			"expected the truncated body to be at most %d bytes, got %d",
			maxDiffBytes,
			len(body),
		)
	}
}
